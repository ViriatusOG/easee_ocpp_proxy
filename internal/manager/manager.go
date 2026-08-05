// Package manager holds the shared, mutable runtime state: the current config
// snapshot and (in later milestones) the registry of active sessions and per-CP
// live state. Access is guarded by a single RWMutex.
package manager

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/schedule"
	"github.com/ipeel/easee-ocpp-proxy/internal/state"
)

// Role classifies how a connecting chargepoint is handled.
type Role int

const (
	// RoleLocal: the proxy acts as the Central System and auto-authorises (§5.3).
	RoleLocal Role = iota
	// RoleProxied: the session is relayed to the remote CSMS (§5.4).
	RoleProxied
)

func (r Role) String() string {
	if r == RoleProxied {
		return "proxied"
	}
	return "local"
}

// Manager is the central coordinator.
type Manager struct {
	mu       sync.RWMutex
	cfg      *config.Config
	states   *state.Store
	txn      atomic.Int64
	msgID    atomic.Int64
	sessions map[string]*SessionHandle

	// Virtual-meter high-water mark for proxy_normalise_meter (FR-47), persisted to a
	// sidecar file so it survives restarts. Guarded by its own mutex.
	meterMu   sync.Mutex
	virtualWh float64
	meterInit bool
	meterPath string
}

// New creates a Manager over the given config.
func New(cfg *config.Config) *Manager {
	m := &Manager{
		cfg:      cfg,
		states:   state.NewStore(),
		sessions: make(map[string]*SessionHandle),
	}
	m.initMeter()
	return m
}

// State returns the live per-chargepoint state store.
func (m *Manager) State() *state.Store { return m.states }

// NextTransactionID returns a unique, monotonically increasing transaction id (FR-11).
func (m *Manager) NextTransactionID() int { return int(m.txn.Add(1)) }

// NextMessageID returns a unique OCPP message id for a proxy-initiated CALL.
func (m *Manager) NextMessageID() string { return "srv-" + strconv.FormatInt(m.msgID.Add(1), 10) }

// ConfigSnapshot returns a copy of the current config that is safe to read without
// holding the lock. Slices and maps are copied so later mutations don't race.
func (m *Manager) ConfigSnapshot() config.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() config.Config {
	c := *m.cfg
	c.Chargepoints = append([]string(nil), m.cfg.Chargepoints...)
	c.Schedules = append([]config.Schedule(nil), m.cfg.Schedules...)
	c.Aliases = copyMap(m.cfg.Aliases)
	c.DeviceSchedules = copyMap(m.cfg.DeviceSchedules)
	c.SchedulePaused = copyBoolMap(m.cfg.SchedulePaused)
	c.Synchronised = append([]string(nil), m.cfg.Synchronised...)
	c.SavedRoles = copyMap(m.cfg.SavedRoles)
	return c
}

func copyMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyBoolMap(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// AllowedAt reports whether charging is permitted for cpID at time t. A CP with no
// assigned schedule is always allowed. Config errors fail open (allowed) so a typo
// never silently blocks charging (FR-41).
func (m *Manager) AllowedAt(cpID string, t time.Time) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Paused schedule = always-on: charging allowed at any time (FR-43).
	if m.cfg.SchedulePaused[cpID] {
		return true
	}
	name := m.cfg.DeviceSchedules[cpID]
	if name == "" {
		return true
	}
	s, ok := m.cfg.FindSchedule(name)
	if !ok {
		return true
	}
	win, err := schedule.Parse(s.Start, s.Stop)
	if err != nil {
		return true
	}
	return win.Allowed(t.In(m.cfg.Location()))
}

// AllowedNow reports whether charging is permitted for cpID right now.
func (m *Manager) AllowedNow(cpID string) bool { return m.AllowedAt(cpID, time.Now()) }

// Config returns the current config snapshot pointer. Callers must treat it as
// read-only; mutation goes through Manager methods so it stays serialised.
func (m *Manager) Config() *config.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Allowed reports whether a chargepoint ID is in the allow-list (FR-3).
func (m *Manager) Allowed(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.HasChargepoint(id)
}

// RoleFor returns the role for a chargepoint ID given the current config (FR-6).
func (m *Manager) RoleFor(id string) Role {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if id != "" && id == m.cfg.ProxiedID {
		return RoleProxied
	}
	return RoleLocal
}
