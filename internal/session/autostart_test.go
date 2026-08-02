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

func newLocalCfg(t *testing.T, mutate func(*config.Config)) (*localCSMS, *manager.Manager) {
	t.Helper()
	c := config.Default()
	c.Chargepoints = []string{"CP1"}
	if mutate != nil {
		mutate(&c)
	}
	m := manager.New(&c)
	fc := clock.NewFake(time.Date(2026, 8, 2, 16, 0, 0, 0, time.UTC))
	h := &localCSMS{id: "CP1", m: m, clk: fc, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return h, m
}

// findRemoteStart returns the RemoteStartTransaction CALL among response frames, if any.
func findRemoteStart(frames []*ocpp.Frame) *ocpp.Frame {
	for _, f := range frames {
		if f.Type == ocpp.CALL && f.Action == "RemoteStartTransaction" {
			return f
		}
	}
	return nil
}

func TestAutoStartOnPreparing(t *testing.T) {
	h, _ := newLocalCfg(t, nil) // auto-start enabled by default

	out := handleFrames(t, h, "1", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})
	rs := findRemoteStart(out)
	if rs == nil {
		t.Fatal("expected a RemoteStartTransaction after Preparing")
	}
	var req ocpp.RemoteStartTransactionReq
	if err := json.Unmarshal(rs.Payload, &req); err != nil {
		t.Fatal(err)
	}
	if req.ConnectorID != 1 {
		t.Errorf("connectorId = %d, want 1", req.ConnectorID)
	}
	if req.IDTag != "PROXY" {
		t.Errorf("idTag = %q, want PROXY", req.IDTag)
	}
}

func TestAutoStartOncePerEpisode(t *testing.T) {
	h, _ := newLocalCfg(t, nil)

	if findRemoteStart(handleFrames(t, h, "1", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})) == nil {
		t.Fatal("first Preparing should auto-start")
	}
	if findRemoteStart(handleFrames(t, h, "2", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})) != nil {
		t.Fatal("repeated Preparing should NOT re-send RemoteStartTransaction")
	}
	// Leaving Preparing re-arms auto-start.
	handleFrames(t, h, "3", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Available"})
	if findRemoteStart(handleFrames(t, h, "4", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})) == nil {
		t.Fatal("Preparing after Available should auto-start again")
	}
}

func TestNoAutoStartWhenTransactionActive(t *testing.T) {
	h, _ := newLocalCfg(t, nil)
	call(t, h, "s", "StartTransaction", ocpp.StartTransactionReq{ConnectorID: 1, MeterStart: 0})
	if findRemoteStart(handleFrames(t, h, "1", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})) != nil {
		t.Fatal("should not auto-start while a transaction is active")
	}
}

func TestAutoStartDisabled(t *testing.T) {
	h, _ := newLocalCfg(t, func(c *config.Config) { c.LocalAutoStart.Enabled = false })
	if findRemoteStart(handleFrames(t, h, "1", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 1, Status: "Preparing"})) != nil {
		t.Fatal("auto-start disabled: no RemoteStartTransaction expected")
	}
}

func TestNoAutoStartOnConnectorZero(t *testing.T) {
	h, _ := newLocalCfg(t, nil)
	if findRemoteStart(handleFrames(t, h, "1", "StatusNotification", ocpp.StatusNotificationReq{ConnectorID: 0, Status: "Preparing"})) != nil {
		t.Fatal("connector 0 (whole station) should not auto-start")
	}
}
