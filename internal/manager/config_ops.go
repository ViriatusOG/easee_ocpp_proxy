package manager

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/schedule"
)

// These operations mutate the config, persist it atomically (FR-34), and, where a
// live connection is affected, drop it so it re-establishes in its new role.
//
// Slices are replaced (copy-on-write) rather than mutated in place so concurrent
// ConfigSnapshot readers never race.

// AddChargepoint adds id to the allow-list.
func (m *Manager) AddChargepoint(id string) error {
	if id == "" {
		return fmt.Errorf("chargepoint id must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.HasChargepoint(id) {
		return fmt.Errorf("chargepoint %q already exists", id)
	}
	m.cfg.Chargepoints = append(append([]string(nil), m.cfg.Chargepoints...), id)
	return m.cfg.Save()
}

// RemoveChargepoint removes id from the allow-list, clearing the proxied selection
// if it pointed at id, and drops any live session for it.
func (m *Manager) RemoveChargepoint(id string) error {
	m.mu.Lock()
	next := make([]string, 0, len(m.cfg.Chargepoints))
	found := false
	for _, x := range m.cfg.Chargepoints {
		if x == id {
			found = true
			continue
		}
		next = append(next, x)
	}
	if !found {
		m.mu.Unlock()
		return fmt.Errorf("chargepoint %q not found", id)
	}
	m.cfg.Chargepoints = next
	if m.cfg.ProxiedID == id {
		m.cfg.ProxiedID = ""
	}
	delete(m.cfg.Aliases, id)
	delete(m.cfg.DeviceSchedules, id)
	delete(m.cfg.SchedulePaused, id)
	err := m.cfg.Save()
	m.mu.Unlock()

	if err != nil {
		return err
	}
	m.DropSession(id)
	return nil
}

// SetAlias sets (or clears, if alias == "") the friendly name for a chargepoint (FR-42).
func (m *Manager) SetAlias(id, alias string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cfg.HasChargepoint(id) {
		return fmt.Errorf("chargepoint %q not found", id)
	}
	if m.cfg.Aliases == nil {
		m.cfg.Aliases = map[string]string{}
	}
	if alias == "" {
		delete(m.cfg.Aliases, id)
	} else {
		m.cfg.Aliases[id] = alias
	}
	return m.cfg.Save()
}

// AddOrUpdateSchedule creates or replaces a named schedule (FR-41).
func (m *Manager) AddOrUpdateSchedule(s config.Schedule) error {
	if s.Name == "" {
		return errors.New("schedule name is required")
	}
	if _, err := schedule.Parse(s.Start, s.Stop); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := append([]config.Schedule(nil), m.cfg.Schedules...)
	found := false
	for i := range next {
		if next[i].Name == s.Name {
			next[i] = s
			found = true
		}
	}
	if !found {
		next = append(next, s)
	}
	m.cfg.Schedules = next
	return m.cfg.Save()
}

// RemoveSchedule deletes a schedule and unassigns any device using it (FR-41).
func (m *Manager) RemoveSchedule(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := make([]config.Schedule, 0, len(m.cfg.Schedules))
	for _, s := range m.cfg.Schedules {
		if s.Name != name {
			next = append(next, s)
		}
	}
	m.cfg.Schedules = next
	for id, n := range m.cfg.DeviceSchedules {
		if n == name {
			delete(m.cfg.DeviceSchedules, id)
		}
	}
	return m.cfg.Save()
}

// AssignSchedule assigns a schedule to a chargepoint ("" to clear) (FR-41).
func (m *Manager) AssignSchedule(id, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cfg.HasChargepoint(id) {
		return fmt.Errorf("chargepoint %q not found", id)
	}
	if name != "" {
		if _, ok := m.cfg.FindSchedule(name); !ok {
			return fmt.Errorf("no such schedule %q", name)
		}
	}
	if m.cfg.DeviceSchedules == nil {
		m.cfg.DeviceSchedules = map[string]string{}
	}
	if name == "" {
		delete(m.cfg.DeviceSchedules, id)
	} else {
		m.cfg.DeviceSchedules[id] = name
		// Assigning a schedule enforces it (clears any always-on pause) so the CP
		// starts in "scheduled" mode; the user can flip it to always-on afterwards.
		delete(m.cfg.SchedulePaused, id)
	}
	return m.cfg.Save()
}

// SetLocalMode switches a chargepoint to a local mode: scheduled (enforce its assigned
// schedule) or always-on (pause it). It clears any proxied role first, dropping that
// session so the CP reconnects locally (FR-43).
func (m *Manager) SetLocalMode(id string, scheduled bool) error {
	m.mu.Lock()
	if !m.cfg.HasChargepoint(id) {
		m.mu.Unlock()
		return fmt.Errorf("chargepoint %q not found", id)
	}
	wasProxied := m.cfg.ProxiedID == id
	if wasProxied {
		m.cfg.ProxiedID = ""
	}
	if m.cfg.SchedulePaused == nil {
		m.cfg.SchedulePaused = map[string]bool{}
	}
	if scheduled {
		delete(m.cfg.SchedulePaused, id) // enforce the schedule
	} else {
		m.cfg.SchedulePaused[id] = true // always-on
	}
	err := m.cfg.Save()
	m.mu.Unlock()

	if err != nil {
		return err
	}
	if wasProxied {
		m.DropSession(id) // reconnect as a local CP
	}
	return nil
}

// SetTimezone sets the IANA timezone for schedule evaluation ("" = server local) (FR-41).
func (m *Manager) SetTimezone(tz string) error {
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("invalid timezone %q: %w", tz, err)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Timezone = tz
	return m.cfg.Save()
}

// SetProxiedID selects which chargepoint is remotely managed ("" for none). Per D-9
// it drops the previously-proxied and newly-selected downstream sessions so both
// reconnect in their new roles (the old proxied session's upstream link, once it
// exists in M4, is torn down as part of that drop).
func (m *Manager) SetProxiedID(newID string) error {
	m.mu.Lock()
	if newID != "" && !m.cfg.HasChargepoint(newID) {
		m.mu.Unlock()
		return fmt.Errorf("chargepoint %q is not in the allow-list", newID)
	}
	old := m.cfg.ProxiedID
	m.cfg.ProxiedID = newID
	err := m.cfg.Save()
	m.mu.Unlock()

	if err != nil {
		return err
	}
	if old != "" && old != newID {
		m.DropSession(old)
	}
	if newID != "" {
		m.DropSession(newID)
	}
	return nil
}

// SetRemote replaces the upstream CSMS configuration and drops the current proxied
// session so it re-dials with the new settings.
func (m *Manager) SetRemote(r config.Remote) error {
	m.mu.Lock()
	m.cfg.Remote = r
	proxied := m.cfg.ProxiedID
	err := m.cfg.Save()
	m.mu.Unlock()

	if err != nil {
		return err
	}
	if proxied != "" {
		m.DropSession(proxied)
	}
	return nil
}

// SetBootAnonymise replaces the BootNotification anonymisation values (D-6).
func (m *Manager) SetBootAnonymise(b config.BootAnonymise) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.BootAnonymise = b
	return m.cfg.Save()
}

// GenerateAPIToken creates a new random API token, persists it, and returns it (FR-45).
func (m *Manager) GenerateAPIToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.APIToken = token
	return token, m.cfg.Save()
}

// SetAPIToken sets or clears the API token ("" disables the API) (FR-45).
func (m *Manager) SetAPIToken(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.APIToken = token
	return m.cfg.Save()
}

// SetAdminCredentials updates the admin username and bcrypt password hash (D-4).
func (m *Manager) SetAdminCredentials(username, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Admin.Username = username
	m.cfg.Admin.PasswordHash = passwordHash
	return m.cfg.Save()
}
