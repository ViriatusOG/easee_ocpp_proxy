package session

import (
	"testing"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
)

func TestDecideSchedule(t *testing.T) {
	cases := []struct {
		name      string
		allowed   bool
		status    string
		txnActive bool
		autoStart bool
		want      schedAction
	}{
		{"closed, charging → stop", false, "Charging", true, true, schedStop},
		{"closed, idle → none", false, "Available", false, true, schedNone},
		{"open, waiting, no txn → start", true, "SuspendedEV", false, true, schedStart},
		{"open, preparing → start", true, "Preparing", false, true, schedStart},
		{"open, already charging → none", true, "Charging", true, true, schedNone},
		{"open, waiting, autostart off → none", true, "Preparing", false, false, schedNone},
		{"open, available (unplugged) → none", true, "Available", false, true, schedNone},
	}
	for _, c := range cases {
		if got := decideSchedule(c.allowed, c.status, c.txnActive, c.autoStart); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestAutoStartRespectsSchedule verifies the read-loop Preparing auto-start is gated by
// the schedule window (evaluated against the injected clock).
func TestAutoStartRespectsSchedule(t *testing.T) {
	// Fake clock is at 16:00 UTC (see newLocalCfg).
	insideWindow := func(c *config.Config) {
		c.Timezone = "UTC"
		c.Schedules = []config.Schedule{{Name: "day", Start: "15:00", Stop: "17:00"}}
		c.DeviceSchedules = map[string]string{"CP1": "day"}
	}
	outsideWindow := func(c *config.Config) {
		c.Timezone = "UTC"
		c.Schedules = []config.Schedule{{Name: "night", Start: "23:30", Stop: "05:30"}}
		c.DeviceSchedules = map[string]string{"CP1": "night"}
	}

	h, _ := newLocalCfg(t, insideWindow)
	if findRemoteStart(handleFrames(t, h, "1", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})) == nil {
		t.Error("inside the window, Preparing should auto-start")
	}

	h2, _ := newLocalCfg(t, outsideWindow)
	if findRemoteStart(handleFrames(t, h2, "1", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})) != nil {
		t.Error("outside the window, Preparing should NOT auto-start")
	}
}

func TestManagerAllowedAt(t *testing.T) {
	c := config.Default()
	c.Chargepoints = []string{"CP1"}
	c.Timezone = "UTC"
	c.Schedules = []config.Schedule{{Name: "night", Start: "23:30", Stop: "05:30"}}
	c.DeviceSchedules = map[string]string{"CP1": "night"}
	h, m := newLocalCfg(t, func(cc *config.Config) { *cc = c })
	_ = h

	if m.AllowedAt("CP1", time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC)) != true {
		t.Error("02:00 should be inside the overnight window")
	}
	if m.AllowedAt("CP1", time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)) != false {
		t.Error("12:00 should be outside the overnight window")
	}
	// A CP with no schedule is always allowed.
	if !m.AllowedAt("OTHER", time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)) {
		t.Error("unscheduled CP should always be allowed")
	}
}
