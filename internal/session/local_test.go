package session

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/clock"
	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
)

func newLocal(t *testing.T) (*localCSMS, *manager.Manager, *clock.Fake) {
	t.Helper()
	c := config.Default()
	c.Chargepoints = []string{"CP1"}
	m := manager.New(&c)
	fc := clock.NewFake(time.Date(2026, 8, 2, 16, 0, 0, 0, time.UTC))
	h := &localCSMS{id: "CP1", m: m, clk: fc, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return h, m, fc
}

// handleFrames runs a CALL through the handler and returns all response frames.
func handleFrames(t *testing.T, h *localCSMS, uid, action string, payload any) []*ocpp.Frame {
	t.Helper()
	raw, err := ocpp.Call(uid, action, payload)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ocpp.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	respBytes, err := h.handle(f)
	if err != nil {
		t.Fatalf("handle %s: %v", action, err)
	}
	out := make([]*ocpp.Frame, 0, len(respBytes))
	for _, b := range respBytes {
		fr, err := ocpp.Parse(b)
		if err != nil {
			t.Fatalf("parse response: %v", err)
		}
		out = append(out, fr)
	}
	return out
}

// call runs a CALL and returns the confirmation (first response frame).
func call(t *testing.T, h *localCSMS, uid, action string, payload any) *ocpp.Frame {
	t.Helper()
	out := handleFrames(t, h, uid, action, payload)
	if len(out) == 0 {
		return nil
	}
	if out[0].UniqueID != uid {
		t.Fatalf("response uid = %q, want %q", out[0].UniqueID, uid)
	}
	return out[0]
}

func TestBootNotificationAccepted(t *testing.T) {
	h, m, _ := newLocal(t)
	resp := call(t, h, "42", "BootNotification", ocpp.BootNotificationReq{
		ChargePointVendor: "Easee ASA", ChargePointModel: "Easee One",
	})
	if resp.Type != ocpp.CALLRESULT {
		t.Fatalf("type = %v, want CALLRESULT", resp.Type)
	}
	var conf ocpp.BootNotificationConf
	json.Unmarshal(resp.Payload, &conf)
	if conf.Status != "Accepted" {
		t.Errorf("status = %q, want Accepted", conf.Status)
	}
	if conf.Interval != m.Config().HeartbeatIntervalS {
		t.Errorf("interval = %d, want %d", conf.Interval, m.Config().HeartbeatIntervalS)
	}
}

func TestTransactionLifecycle(t *testing.T) {
	h, m, _ := newLocal(t)

	start := call(t, h, "1", "StartTransaction", ocpp.StartTransactionReq{
		ConnectorID: 1, IDTag: "abc", MeterStart: 1000,
	})
	var sc ocpp.StartTransactionConf
	json.Unmarshal(start.Payload, &sc)
	if sc.TransactionID != 1 {
		t.Errorf("transactionId = %d, want 1", sc.TransactionID)
	}
	if sc.IdTagInfo.Status != "Accepted" {
		t.Errorf("start status = %q, want Accepted", sc.IdTagInfo.Status)
	}

	cp, _ := m.State().Get("CP1")
	if !cp.TxnActive || cp.TxnID != 1 || cp.IDTag != "abc" {
		t.Errorf("state after start = %+v", cp)
	}

	call(t, h, "2", "StopTransaction", ocpp.StopTransactionReq{
		TransactionID: 1, MeterStop: 3500, Reason: "Local",
	})
	cp, _ = m.State().Get("CP1")
	if cp.TxnActive {
		t.Error("transaction should be inactive after stop")
	}
	if cp.EnergyWh != 2500 {
		t.Errorf("session energy = %v Wh, want 2500", cp.EnergyWh)
	}
	if cp.LastStopReason != "Local" {
		t.Errorf("stop reason = %q, want Local", cp.LastStopReason)
	}
}

func TestUniqueTransactionIDs(t *testing.T) {
	h, _, _ := newLocal(t)
	seen := map[int]bool{}
	for i := 0; i < 5; i++ {
		resp := call(t, h, "x", "StartTransaction", ocpp.StartTransactionReq{ConnectorID: 1, MeterStart: 0})
		var sc ocpp.StartTransactionConf
		json.Unmarshal(resp.Payload, &sc)
		if seen[sc.TransactionID] {
			t.Fatalf("duplicate transaction id %d", sc.TransactionID)
		}
		seen[sc.TransactionID] = true
	}
}

func TestMeterValuesUnitsHonoured(t *testing.T) {
	h, m, _ := newLocal(t)
	// Active transaction so session energy is computed from the delta.
	call(t, h, "1", "StartTransaction", ocpp.StartTransactionReq{ConnectorID: 1, MeterStart: 12_000_000})

	// Easee reports the register in kWh (Appendix A): 12272.427 kWh = 12,272,427 Wh.
	call(t, h, "2", "MeterValues", ocpp.MeterValuesReq{
		ConnectorID: 1,
		MeterValue: []ocpp.MeterValue{{
			Timestamp: "2026-08-02T16:24:41Z",
			SampledValue: []ocpp.SampledValue{
				{Value: "12272.427", Measurand: "Energy.Active.Import.Register", Unit: "kWh"},
				{Value: "7000.0", Measurand: "Power.Active.Import", Unit: "W"},
			},
		}},
	})

	cp, _ := m.State().Get("CP1")
	if cp.MeterLastWh != 12_272_427 {
		t.Errorf("meter last = %v Wh, want 12272427 (kWh→Wh)", cp.MeterLastWh)
	}
	if cp.PowerW != 7000 {
		t.Errorf("power = %v W, want 7000", cp.PowerW)
	}
	if cp.EnergyWh != 272_427 {
		t.Errorf("session energy = %v Wh, want 272427", cp.EnergyWh)
	}
}

func TestStatusNotificationTracked(t *testing.T) {
	h, m, _ := newLocal(t)
	call(t, h, "1", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, ErrorCode: "NoError", Status: "Charging"})
	cp, _ := m.State().Get("CP1")
	if cp.ConnectorStatus != "Charging" {
		t.Errorf("connector status = %q, want Charging", cp.ConnectorStatus)
	}
}

func TestUnknownActionNotSupported(t *testing.T) {
	h, _, _ := newLocal(t)
	resp := call(t, h, "9", "FooBar", struct{}{})
	if resp.Type != ocpp.CALLERROR {
		t.Fatalf("type = %v, want CALLERROR", resp.Type)
	}
	if resp.ErrorCode != "NotSupported" {
		t.Errorf("error code = %q, want NotSupported", resp.ErrorCode)
	}
}

func TestDataTransferAccepted(t *testing.T) {
	h, _, _ := newLocal(t)
	resp := call(t, h, "7", "DataTransfer", map[string]any{"vendorId": "com.example"})
	var conf ocpp.DataTransferConf
	json.Unmarshal(resp.Payload, &conf)
	if conf.Status != "Accepted" {
		t.Errorf("status = %q, want Accepted", conf.Status)
	}
}
