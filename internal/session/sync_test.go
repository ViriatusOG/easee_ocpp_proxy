package session

import (
	"testing"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
	"github.com/ipeel/easee-ocpp-proxy/internal/state"
)

// A synchronised local CP starts only while the proxied unit is charging, and does so
// even with the local auto-start toggle off (FR-48).
func TestSynchronisedMirrorsProxiedCharging(t *testing.T) {
	setup := func(c *config.Config) {
		c.Chargepoints = []string{"CP1", "CP2"}
		c.ProxiedID = "CP2"
		c.Synchronised = []string{"CP1"}
		c.LocalAutoStart.Enabled = false // synchronised must ignore this
	}
	h, m := newLocalCfg(t, setup)

	// Proxied unit idle → synchronised CP1 must NOT start on Preparing.
	if findRemoteStart(handleFrames(t, h, "1", "StatusNotification",
		ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})) != nil {
		t.Fatal("synchronised: proxied idle → no auto-start expected")
	}

	// Re-arm (leave Preparing), then mark the proxied unit as charging.
	handleFrames(t, h, "2", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Available"})
	m.State().Update("CP2", func(cp *state.CP) { cp.DownstreamUp = true; cp.TxnActive = true })

	// Now Preparing must mirror the proxied unit and start — despite auto-start disabled.
	if findRemoteStart(handleFrames(t, h, "3", "StatusNotification",
		ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})) == nil {
		t.Fatal("synchronised: proxied charging → CP1 should start")
	}
}

// The synchronised reconcile decision follows the proxied-charging gate: start when it's
// charging and the local car is waiting, stop the local charge when it isn't (FR-48).
func TestSynchronisedReconcileDecision(t *testing.T) {
	// Reuses decideSchedule with allowed = proxiedCharging (the reconciler's substitution).
	if got := decideSchedule(true, "SuspendedEV", false, true); got != schedStart {
		t.Errorf("proxied charging + waiting → want schedStart, got %v", got)
	}
	if got := decideSchedule(false, "Charging", true, true); got != schedStop {
		t.Errorf("proxied stopped + local active → want schedStop, got %v", got)
	}
	if got := decideSchedule(true, "Available", false, true); got != schedNone {
		t.Errorf("proxied charging + unplugged → want schedNone, got %v", got)
	}
}
