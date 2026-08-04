package session

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// parseCall returns the payload map of a rewritten CALL frame.
func parseCall(t *testing.T, b []byte) map[string]any {
	t.Helper()
	f, err := ocpp.Parse(b)
	if err != nil {
		t.Fatalf("parse rewritten frame: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(f.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func startTxn(t *testing.T, uid string, meterStart int) *ocpp.Frame {
	t.Helper()
	b, _ := ocpp.Call(uid, "StartTransaction", map[string]any{
		"connectorId": 1, "idTag": "TAG", "meterStart": meterStart, "timestamp": "2026-08-04T00:00:00Z",
	})
	f, _ := ocpp.Parse(b)
	return f
}

func stopTxn(t *testing.T, uid string, meterStop int) *ocpp.Frame {
	t.Helper()
	b, _ := ocpp.Call(uid, "StopTransaction", map[string]any{
		"transactionId": 1, "meterStop": meterStop, "reason": "Remote", "timestamp": "2026-08-04T01:00:00Z",
	})
	f, _ := ocpp.Parse(b)
	return f
}

// First-ever use: the virtual meter bootstraps to the real reading, so the value passes
// through unchanged (offset 0).
func TestNormaliseBootstrapPassthrough(t *testing.T) {
	c := config.Default()
	m := manager.New(&c)
	n := newMeterNormaliser(m, quietLog())

	out := n.rewrite(startTxn(t, "1", 18_938_023))
	got := parseCall(t, out)["meterStart"].(float64)
	if int(got) != 18_938_023 {
		t.Fatalf("bootstrap should pass through: got %v want 18938023", got)
	}
	if wh, init := m.VirtualMeterWh(); !init || wh != 18_938_023 {
		t.Fatalf("virtual meter = %v init=%v, want 18938023 true", wh, init)
	}
}

// The whole point (FR-47): after a high-meter unit charges, switching to a *lower*-meter
// unit must NOT make the upstream meter roll backwards.
func TestNormaliseNoBackwardsOnSwitch(t *testing.T) {
	c := config.Default()
	m := manager.New(&c)

	// Connection 1: unit A (high meter ~18,938 kWh) charges 18,938,023 → 18,946,873 Wh.
	a := newMeterNormaliser(m, quietLog())
	a.rewrite(startTxn(t, "1", 18_938_023))
	stopA := parseCall(t, a.rewrite(stopTxn(t, "2", 18_946_873)))["meterStop"].(float64)

	// Connection 2: unit B (low meter ~12,282 kWh) starts. Presented start must be >= the
	// last value the CSMS saw (never backwards) and equal to the high-water mark.
	b := newMeterNormaliser(m, quietLog())
	startB := parseCall(t, b.rewrite(startTxn(t, "3", 12_282_000)))["meterStart"].(float64)
	if startB < stopA {
		t.Fatalf("meter rolled backwards on switch: B start %v < A stop %v", startB, stopA)
	}
	// B then charges +5,000 Wh; the delta must be exactly preserved.
	stopB := parseCall(t, b.rewrite(stopTxn(t, "4", 12_287_000)))["meterStop"].(float64)
	if stopB-startB != 5000 {
		t.Fatalf("session delta not preserved: %v", stopB-startB)
	}
}

// A switch to a *higher*-meter unit stays continuous (no forward jump either): presented
// continues from the virtual high-water mark, and the session delta is preserved.
func TestNormaliseContinuousOnSwitchUp(t *testing.T) {
	c := config.Default()
	m := manager.New(&c)

	a := newMeterNormaliser(m, quietLog())
	a.rewrite(startTxn(t, "1", 12_282_000))
	stopA := parseCall(t, a.rewrite(stopTxn(t, "2", 12_290_000)))["meterStop"].(float64)

	b := newMeterNormaliser(m, quietLog())
	startB := parseCall(t, b.rewrite(startTxn(t, "3", 18_938_000)))["meterStart"].(float64)
	if startB != stopA {
		t.Fatalf("switch-up not continuous: B start %v, A stop %v", startB, stopA)
	}
	stopB := parseCall(t, b.rewrite(stopTxn(t, "4", 18_946_000)))["meterStop"].(float64)
	if stopB-startB != 8000 {
		t.Fatalf("session delta not preserved: %v", stopB-startB)
	}
}

// MeterValues: the register sample is rewritten (unit, decimals, and sibling fields kept)
// while non-energy samples like Power.Active.Import are left untouched.
func TestNormaliseMeterValuesPreservesShape(t *testing.T) {
	c := config.Default()
	m := manager.New(&c)
	// Bootstrap the virtual meter high so the register gets a non-zero offset.
	m.BootstrapVirtualMeter(20_000_000)
	n := newMeterNormaliser(m, quietLog())

	raw := `[2,"9","MeterValues",{"connectorId":1,"transactionId":1,"meterValue":[{"timestamp":"2026-08-04T00:31:38.000Z","sampledValue":[` +
		`{"value":"6407.000","context":"Sample.Periodic","measurand":"Power.Active.Import","location":"Inlet","unit":"W"},` +
		`{"value":"18938.080","context":"Sample.Periodic","measurand":"Energy.Active.Import.Register","location":"Outlet","unit":"kWh"}]}]}]`
	f, err := ocpp.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	out := n.rewrite(f)
	s := string(out)

	// Register (18,938.080 kWh real) → 20,000.000 + offset... offset = 20,000,000 - 18,938,080
	// so presented = 20,000,000 Wh = 20000.000 kWh, kept in kWh with 3 decimals.
	if !strings.Contains(s, `"value":"20000.000"`) {
		t.Errorf("register not normalised to 3-decimal kWh: %s", s)
	}
	// Power sample untouched, sibling fields retained.
	if !strings.Contains(s, `"6407.000"`) || !strings.Contains(s, `"location":"Outlet"`) || !strings.Contains(s, `"transactionId":1`) {
		t.Errorf("power sample or sibling fields altered: %s", s)
	}
}

// Non-meter frames and unparseable payloads are returned byte-for-byte.
func TestNormalisePassesThroughOtherFrames(t *testing.T) {
	c := config.Default()
	m := manager.New(&c)
	n := newMeterNormaliser(m, quietLog())
	hb, _ := ocpp.Call("1", "Heartbeat", struct{}{})
	f, _ := ocpp.Parse(hb)
	if string(n.rewrite(f)) != string(f.Raw) {
		t.Errorf("Heartbeat should pass through unchanged")
	}
}
