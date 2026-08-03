package manager

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
)

func newManagerWithSchedule(t *testing.T) *Manager {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	body := `listen_addr: ":9000"
heartbeat_interval_s: 300
upstream_timeout_s: 30
chargepoints: [CP1]
timezone: UTC
schedules:
  - {name: night, start: "23:30", stop: "05:30"}
device_schedules: {CP1: night}
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg)
}

func TestLocalModeSchedulingAndAlwaysOn(t *testing.T) {
	m := newManagerWithSchedule(t)
	inside := time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC)  // 02:00 — within 23:30–05:30
	outside := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC) // 12:00 — outside

	// Default (schedule assigned, not paused) → scheduled: enforced.
	if !m.AllowedAt("CP1", inside) {
		t.Error("scheduled: 02:00 should be allowed")
	}
	if m.AllowedAt("CP1", outside) {
		t.Error("scheduled: 12:00 should be blocked")
	}

	// Always-on: schedule ignored, allowed any time.
	if err := m.SetLocalMode("CP1", false); err != nil {
		t.Fatal(err)
	}
	if !m.AllowedAt("CP1", outside) {
		t.Error("always-on: 12:00 should be allowed")
	}
	snap := m.ConfigSnapshot()
	if snap.ScheduleActive("CP1") {
		t.Error("always-on: schedule should not be active")
	}

	// Back to scheduled: enforcement resumes, schedule assignment retained.
	if err := m.SetLocalMode("CP1", true); err != nil {
		t.Fatal(err)
	}
	if m.AllowedAt("CP1", outside) {
		t.Error("scheduled again: 12:00 should be blocked")
	}
	snap2 := m.ConfigSnapshot()
	if !snap2.HasSchedule("CP1") {
		t.Error("schedule assignment should be retained across mode flips")
	}
}

func TestSetLocalModeClearsProxied(t *testing.T) {
	m := newManagerWithSchedule(t)
	if err := m.SetProxiedID("CP1"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetLocalMode("CP1", true); err != nil {
		t.Fatal(err)
	}
	if m.ConfigSnapshot().ProxiedID != "" {
		t.Error("switching to a local mode should clear the proxied role")
	}
}
