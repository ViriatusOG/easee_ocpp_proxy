package manager

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// meterState is the persisted virtual-meter high-water mark for proxy_normalise_meter
// (FR-47). It lives in a sidecar file next to the config so it survives restarts and
// never churns config.yaml (it updates on every meter reading).
type meterState struct {
	Initialised bool    `json:"initialised"`
	VirtualWh   float64 `json:"virtual_wh"`
}

// initMeter derives the sidecar path from the config path and loads any saved state.
// With no config path (tests), state stays purely in-memory.
func (m *Manager) initMeter() {
	if p := m.cfg.Path(); p != "" {
		m.meterPath = filepath.Join(filepath.Dir(p), ".easee-proxy-meter.json")
	}
	if m.meterPath == "" {
		return
	}
	b, err := os.ReadFile(m.meterPath)
	if err != nil {
		return
	}
	var st meterState
	if json.Unmarshal(b, &st) == nil {
		m.virtualWh = st.VirtualWh
		m.meterInit = st.Initialised
	}
}

// VirtualMeterWh returns the current virtual-meter high-water mark (Wh) and whether it
// has been pegged to a real reading yet.
func (m *Manager) VirtualMeterWh() (wh float64, initialised bool) {
	m.meterMu.Lock()
	defer m.meterMu.Unlock()
	return m.virtualWh, m.meterInit
}

// BootstrapVirtualMeter pegs the high-water mark to wh and marks it initialised — used
// the first time a real reading is seen (offset then starts at zero).
func (m *Manager) BootstrapVirtualMeter(wh float64) {
	m.meterMu.Lock()
	defer m.meterMu.Unlock()
	m.virtualWh = wh
	m.meterInit = true
	m.saveMeterLocked()
}

// AdvanceVirtualMeter raises the high-water mark to at least wh (the mark only ever
// climbs). Persisted only when it actually moves.
func (m *Manager) AdvanceVirtualMeter(wh float64) {
	m.meterMu.Lock()
	defer m.meterMu.Unlock()
	if wh > m.virtualWh {
		m.virtualWh = wh
		m.saveMeterLocked()
	}
}

// ResetVirtualMeter forgets the high-water mark so the next real reading re-pegs the
// virtual meter to the physical one. Called when normalisation is switched on, so it
// starts clean from the currently-proxied unit instead of a stale mark.
func (m *Manager) ResetVirtualMeter() {
	m.meterMu.Lock()
	defer m.meterMu.Unlock()
	m.virtualWh = 0
	m.meterInit = false
	m.saveMeterLocked()
}

// saveMeterLocked atomically writes the sidecar file. Caller holds meterMu.
func (m *Manager) saveMeterLocked() {
	if m.meterPath == "" {
		return
	}
	b, err := json.Marshal(meterState{Initialised: m.meterInit, VirtualWh: m.virtualWh})
	if err != nil {
		return
	}
	tmp := m.meterPath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, m.meterPath)
	}
}
