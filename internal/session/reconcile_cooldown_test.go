package session

import (
	"testing"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
	"github.com/ipeel/easee-ocpp-proxy/internal/state"
)

func reconcileFrames(t *testing.T, h *localCSMS) []*ocpp.Frame {
	t.Helper()
	var out []*ocpp.Frame
	h.reconcileSchedule(func(b []byte) error {
		if f, err := ocpp.Parse(b); err == nil {
			out = append(out, f)
		}
		return nil
	})
	return out
}

func hasAction(frames []*ocpp.Frame, action string) bool {
	for _, f := range frames {
		if f.Action == action {
			return true
		}
	}
	return false
}

// A stop must fire even within the reconcile cooldown — e.g. switching to always-off just
// after charging started (FR-49). Otherwise the charger keeps running.
func TestReconcileStopBypassesCooldown(t *testing.T) {
	h, m := newLocalCfg(t, func(c *config.Config) { c.ChargingOff = []string{"CP1"} })
	m.State().Update("CP1", func(cp *state.CP) {
		cp.DownstreamUp = true
		cp.TxnActive = true
		cp.TxnID = 7
		cp.ConnectorStatus = "Charging"
		cp.LastScheduleAction = time.Now() // well within the 45s cooldown
	})
	if !hasAction(reconcileFrames(t, h), "RemoteStopTransaction") {
		t.Fatal("always-off must stop the active charge even inside the cooldown")
	}
}

// The cooldown still guards starts (thrash protection is preserved).
func TestReconcileStartRespectsCooldown(t *testing.T) {
	h, m := newLocalCfg(t, nil) // no schedule → always allowed; auto-start enabled
	m.State().Update("CP1", func(cp *state.CP) {
		cp.DownstreamUp = true
		cp.TxnActive = false
		cp.ConnectorStatus = "SuspendedEV" // waiting: would otherwise start
		cp.LastScheduleAction = time.Now() // within cooldown
	})
	if hasAction(reconcileFrames(t, h), "RemoteStartTransaction") {
		t.Fatal("start should be suppressed within the cooldown")
	}
}
