package manager

import (
	"errors"
	"fmt"
)

// Role names as used by the dashboard/API and stored in SavedRoles (FR-48).
const (
	RoleProxiedName      = "proxied"
	RoleAlwaysOnName     = "always_on"
	RoleAlwaysOffName    = "always_off"
	RoleScheduledName    = "scheduled"
	RoleSynchronisedName = "synchronised"
)

// ProxiedCharging reports whether the proxied chargepoint currently has an active
// transaction (FR-48). Synchronised local CPs mirror this. It is false when nothing is
// proxied or the proxied session is down, so a synchronised CP stops if the proxied unit
// drops without a clean StopTransaction.
func (m *Manager) ProxiedCharging() bool {
	m.mu.RLock()
	pid := m.cfg.ProxiedID
	m.mu.RUnlock()
	if pid == "" {
		return false
	}
	st, ok := m.states.Get(pid)
	return ok && st.DownstreamUp && st.TxnActive
}

// IsSynchronised reports whether the CP is in synchronised mode (FR-48).
func (m *Manager) IsSynchronised(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.IsSynchronised(id)
}

// IsChargingOff reports whether the CP has charging disabled ("always off", FR-49).
func (m *Manager) IsChargingOff(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.IsChargingOff(id)
}

// AnyProxied reports whether some chargepoint is currently proxied (gates the
// availability of synchronised mode in the UI).
func (m *Manager) AnyProxied() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.ProxiedID != ""
}

// SetRole is the single entry point for a dashboard/API role change (FR-43, FR-48). It
// applies the target role, maintains saved-role memory (so a unit displaced from proxied
// resumes its prior role, and a synchronised unit demoted because nothing is proxied
// falls back to its saved role), and drops only the sessions whose handler changes
// (proxied↔local); local sub-role changes are picked up live by the reconciler.
func (m *Manager) SetRole(id, target string) error {
	m.mu.Lock()
	if !m.cfg.HasChargepoint(id) {
		m.mu.Unlock()
		return fmt.Errorf("chargepoint %q not found", id)
	}

	var toDrop []string
	switch target {
	case RoleProxiedName:
		old := m.cfg.ProxiedID
		if old == id {
			m.mu.Unlock()
			return nil
		}
		m.saveRoleLocked(id, m.currentRoleLocked(id))
		m.setSynchronisedLocked(id, false)
		m.cfg.ProxiedID = id
		if old != "" {
			// Displaced unit resumes the role it held before it became proxied.
			m.applyLocalRoleLocked(old, m.resumeRoleLocked(old))
			toDrop = append(toDrop, old)
		}
		toDrop = append(toDrop, id)

	case RoleSynchronisedName:
		if m.cfg.ProxiedID == "" || m.cfg.ProxiedID == id {
			m.mu.Unlock()
			return errors.New("synchronised mode requires another chargepoint to be proxied")
		}
		m.saveRoleLocked(id, m.currentRoleLocked(id))
		m.applyLocalRoleLocked(id, RoleSynchronisedName)

	case RoleAlwaysOnName, RoleScheduledName, RoleAlwaysOffName:
		if m.cfg.ProxiedID == id {
			m.cfg.ProxiedID = ""
			toDrop = append(toDrop, id)
		}
		m.applyLocalRoleLocked(id, target)
		if m.cfg.ProxiedID == "" {
			// Nothing is proxied any more: synchronised units have nothing to mirror.
			m.demoteSynchronisedLocked()
		}

	default:
		m.mu.Unlock()
		return fmt.Errorf("unknown role %q", target)
	}

	err := m.cfg.Save()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	for _, d := range toDrop {
		m.DropSession(d)
	}
	return nil
}

// currentRoleLocked derives a CP's current role from config. Caller holds m.mu.
func (m *Manager) currentRoleLocked(id string) string {
	switch {
	case id == m.cfg.ProxiedID:
		return RoleProxiedName
	case m.cfg.IsSynchronised(id):
		return RoleSynchronisedName
	case m.cfg.IsChargingOff(id):
		return RoleAlwaysOffName
	case m.cfg.DeviceSchedules[id] != "" && !m.cfg.SchedulePaused[id]:
		return RoleScheduledName
	default:
		return RoleAlwaysOnName
	}
}

// RoleName returns a CP's current role (dashboard/API view).
func (m *Manager) RoleName(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentRoleLocked(id)
}

// applyLocalRoleLocked sets the config for a non-proxied role. Caller holds m.mu.
func (m *Manager) applyLocalRoleLocked(id, role string) {
	m.setSynchronisedLocked(id, role == RoleSynchronisedName)
	m.setChargingOffLocked(id, role == RoleAlwaysOffName)
	switch role {
	case RoleScheduledName:
		delete(m.cfg.SchedulePaused, id) // enforce the assigned schedule
	case RoleAlwaysOnName:
		if m.cfg.SchedulePaused == nil {
			m.cfg.SchedulePaused = map[string]bool{}
		}
		m.cfg.SchedulePaused[id] = true
	case RoleSynchronisedName, RoleAlwaysOffName:
		// schedule state is irrelevant in these roles; leave the paused flag untouched.
	}
}

// setSynchronisedLocked adds/removes id from the synchronised set (copy-on-write so
// snapshot readers never race). Caller holds m.mu.
func (m *Manager) setSynchronisedLocked(id string, on bool) {
	next := make([]string, 0, len(m.cfg.Synchronised)+1)
	for _, x := range m.cfg.Synchronised {
		if x != id {
			next = append(next, x)
		}
	}
	if on {
		next = append(next, id)
	}
	m.cfg.Synchronised = next
}

// setChargingOffLocked adds/removes id from the charging-off set (FR-49, copy-on-write).
// Caller holds m.mu.
func (m *Manager) setChargingOffLocked(id string, on bool) {
	next := make([]string, 0, len(m.cfg.ChargingOff)+1)
	for _, x := range m.cfg.ChargingOff {
		if x != id {
			next = append(next, x)
		}
	}
	if on {
		next = append(next, id)
	}
	m.cfg.ChargingOff = next
}

func (m *Manager) saveRoleLocked(id, role string) {
	if m.cfg.SavedRoles == nil {
		m.cfg.SavedRoles = map[string]string{}
	}
	m.cfg.SavedRoles[id] = role
}

// resumeRoleLocked is the base role to restore for a unit leaving proxied; a saved
// "synchronised" is valid here because another unit is taking over proxied.
func (m *Manager) resumeRoleLocked(id string) string {
	if r := m.cfg.SavedRoles[id]; r != "" && r != RoleProxiedName {
		return r
	}
	return RoleAlwaysOnName
}

// demoteSynchronisedLocked reverts every synchronised CP to its saved base role, used
// when nothing is proxied. A saved "synchronised"/"proxied" is meaningless here, so it
// falls back to always-on. Caller holds m.mu.
func (m *Manager) demoteSynchronisedLocked() {
	for _, id := range append([]string(nil), m.cfg.Synchronised...) {
		role := m.cfg.SavedRoles[id]
		if role == "" || role == RoleSynchronisedName || role == RoleProxiedName {
			role = RoleAlwaysOnName
		}
		m.applyLocalRoleLocked(id, role)
	}
}
