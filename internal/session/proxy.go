package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/ipeel/easee-ocpp-proxy/internal/clock"
	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
	"github.com/ipeel/easee-ocpp-proxy/internal/state"
	"github.com/ipeel/easee-ocpp-proxy/internal/upstream"
)

// upstreamDialer is overridable in tests so the relay can target an in-process CSMS.
var upstreamDialer = func(ctx context.Context, r config.Remote) (*websocket.Conn, *http.Response, error) {
	return upstream.Dial(ctx, r, nil)
}

// runProxy relays the proxied chargepoint's session to the remote CSMS (§5.4). It
// dials upstream, then pumps frames both ways: downstream→upstream with
// BootNotification anonymised (D-6/FR-21a), upstream→downstream verbatim (including
// CSMS-initiated commands, D-10). Either side closing tears down both (FR-22/FR-23).
func runProxy(ctx context.Context, downstream *websocket.Conn, id string, m *manager.Manager, clk clock.Clock, log *slog.Logger) {
	cfg := m.ConfigSnapshot()

	dialCtx, cancelDial := context.WithTimeout(ctx, cfg.UpstreamTimeout())
	up, resp, err := upstreamDialer(dialCtx, cfg.Remote)
	cancelDial()
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		log.Warn("upstream dial failed; dropping downstream so it reconnects", "err", err, "status", status)
		return // Serve's defer closes the downstream connection (FR-27)
	}
	defer up.CloseNow()
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	log.Info("upstream connected", "url", upstream.URL(cfg.Remote), "subprotocol", up.Subprotocol(), "status", status)
	if up.Subprotocol() != ocpp.Subprotocol {
		log.Warn("upstream did not negotiate ocpp1.6 subprotocol", "got", up.Subprotocol())
	}
	m.State().Update(id, func(cp *state.CP) { cp.UpstreamUp = true })

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Independent liveness per direction: a one-sided dead link (e.g. the CSMS app
	// hangs while TCP stays open) is only caught if each side is timed separately —
	// downstream (FR-24) and upstream (FR-25). Start at 2× the local heartbeat, then
	// tighten to 2× the CSMS-supplied boot interval once known.
	initial := 2 * cfg.HeartbeatInterval()
	downstreamWD := newWatchdog(clk, initial, func() {
		log.Warn("downstream idle timeout; closing both links", "after", initial.String())
		cancel()
	})
	defer downstreamWD.stop()
	upstreamWD := newWatchdog(clk, initial, func() {
		log.Warn("upstream idle timeout (CSMS silent); closing both links", "after", initial.String())
		cancel()
	})
	defer upstreamWD.stop()

	// bootUID correlates the CSMS's BootNotification.conf back to the boot we forwarded,
	// so we can read the CSMS-supplied heartbeat interval and tighten liveness (FR-25).
	var bootMu sync.Mutex
	var bootUID string

	// downstream → upstream (anonymise BootNotification, observe telemetry)
	go func() {
		defer cancel()
		for {
			typ, data, err := downstream.Read(ctx)
			if err != nil {
				log.Info("downstream closed; tearing down upstream", "reason", disconnectReason(err))
				log.Debug("downstream disconnect detail", "err", err)
				return
			}
			downstreamWD.kick()
			out := data
			if typ == websocket.MessageText {
				if f, perr := ocpp.Parse(data); perr == nil {
					log.Debug("cp → csms", "kind", f.Type.String(), "action", f.Action, "uid", f.UniqueID)
					observeProxyFrame(m, id, f, log)
					if f.Type == ocpp.CALL && f.Action == "BootNotification" {
						bootMu.Lock()
						bootUID = f.UniqueID
						bootMu.Unlock()
						out = anonymiseBoot(f, cfg.BootAnonymise, log)
					}
				}
			}
			if err := up.Write(ctx, typ, out); err != nil {
				log.Info("upstream write failed; tearing down", "err", err)
				return
			}
		}
	}()

	// upstream → downstream (verbatim)
	go func() {
		defer cancel()
		receivedAny := false
		for {
			typ, data, err := up.Read(ctx)
			if err != nil {
				if !receivedAny && ctx.Err() == nil {
					// The CSMS accepted the WebSocket then closed before sending any
					// OCPP message — almost always an application-level rejection
					// (bad credentials, or the charge point already connected).
					log.Warn("upstream closed before sending any OCPP message — the CSMS rejected the session; check the remote username/password (authorization key) and that this charge point isn't already connected elsewhere", "err", err)
				} else {
					log.Info("upstream closed; tearing down downstream", "reason", disconnectReason(err))
					log.Debug("upstream disconnect detail", "err", err)
				}
				return
			}
			receivedAny = true
			upstreamWD.kick()
			if typ == websocket.MessageText {
				if f, perr := ocpp.Parse(data); perr == nil {
					// Tighten liveness to the CSMS-supplied heartbeat interval carried in
					// the BootNotification.conf (FR-25).
					if f.Type == ocpp.CALLRESULT {
						bootMu.Lock()
						isBoot := f.UniqueID != "" && f.UniqueID == bootUID
						bootMu.Unlock()
						if isBoot {
							var conf ocpp.BootNotificationConf
							if json.Unmarshal(f.Payload, &conf) == nil && conf.Interval > 0 {
								d := 2 * time.Duration(conf.Interval) * time.Second
								downstreamWD.setInterval(d)
								upstreamWD.setInterval(d)
								log.Info("liveness tightened to CSMS heartbeat interval", "intervalSeconds", conf.Interval)
							}
						}
					}
					if f.Type == ocpp.CALL && notableCSMSCommand(f.Action) {
						log.Info("csms command", "action", f.Action, "uid", f.UniqueID, "payload", string(f.Payload))
					} else {
						log.Debug("csms → cp", "kind", f.Type.String(), "action", f.Action, "uid", f.UniqueID)
					}
				}
			}
			if err := downstream.Write(ctx, typ, data); err != nil {
				log.Info("downstream write failed; tearing down", "err", err)
				return
			}
		}
	}()

	<-ctx.Done()
}

// notableCSMSCommand reports whether a CSMS-initiated command is worth surfacing at
// INFO (control actions), as opposed to provisioning chatter (GetConfiguration,
// ChangeConfiguration, TriggerMessage, …) which stays at DEBUG.
func notableCSMSCommand(action string) bool {
	switch action {
	case "RemoteStartTransaction", "RemoteStopTransaction", "Reset", "UnlockConnector":
		return true
	}
	return false
}

// anonymiseBoot rewrites the manufacturer-identifying fields of a BootNotification
// before it is forwarded upstream (D-6/FR-21a). Empty config values pass the real
// value through. On any error it forwards the original frame unchanged.
func anonymiseBoot(f *ocpp.Frame, ba config.BootAnonymise, log *slog.Logger) []byte {
	var payload map[string]any
	if err := json.Unmarshal(f.Payload, &payload); err != nil {
		return f.Raw
	}
	if ba.ChargePointVendor != "" {
		payload["chargePointVendor"] = ba.ChargePointVendor
	}
	if ba.ChargePointModel != "" {
		payload["chargePointModel"] = ba.ChargePointModel
	}
	if ba.FirmwareVersion != "" {
		payload["firmwareVersion"] = ba.FirmwareVersion
	}
	if ba.SerialNumber != "" {
		payload["chargePointSerialNumber"] = ba.SerialNumber
	}
	out, err := ocpp.Call(f.UniqueID, "BootNotification", payload)
	if err != nil {
		return f.Raw
	}
	log.Info("BootNotification anonymised upstream", "vendor", ba.ChargePointVendor, "model", ba.ChargePointModel)
	return out
}

// observeProxyFrame updates dashboard state from relayed frames without altering
// them, and logs the same charge-lifecycle events as a local session (INFO) so
// proxied sessions have console parity. MeterValues stay silent (too frequent).
func observeProxyFrame(m *manager.Manager, id string, f *ocpp.Frame, log *slog.Logger) {
	if f.Type != ocpp.CALL {
		return
	}
	switch f.Action {
	case "StatusNotification":
		var req ocpp.StatusNotificationReq
		if json.Unmarshal(f.Payload, &req) == nil {
			m.State().Update(id, func(cp *state.CP) {
				if req.ConnectorID != 0 {
					cp.ConnectorStatus = req.Status
					cp.LastConnector = req.ConnectorID
				} else if cp.ConnectorStatus == "" {
					cp.ConnectorStatus = req.Status
				}
			})
			log.Info("status", "connector", req.ConnectorID, "status", req.Status, "error", req.ErrorCode)
		}
	case "StartTransaction":
		var req ocpp.StartTransactionReq
		if json.Unmarshal(f.Payload, &req) == nil {
			m.State().Update(id, func(cp *state.CP) {
				cp.TxnActive = true
				cp.IDTag = req.IDTag
				cp.MeterStartWh = float64(req.MeterStart)
				cp.MeterLastWh = float64(req.MeterStart)
				cp.EnergyWh = 0
			})
			// The CSMS assigns the transactionId in its StartTransaction.conf, so it's
			// not known here; log what the CP reported.
			log.Info("transaction started (remote)", "connector", req.ConnectorID, "idTag", req.IDTag, "meterStartWh", req.MeterStart)
		}
	case "StopTransaction":
		var req ocpp.StopTransactionReq
		if json.Unmarshal(f.Payload, &req) == nil {
			var energyWh float64
			m.State().Update(id, func(cp *state.CP) {
				if req.MeterStop > 0 && cp.MeterStartWh > 0 {
					cp.EnergyWh = float64(req.MeterStop) - cp.MeterStartWh
				}
				cp.TxnActive = false
				cp.LastStopReason = req.Reason
				energyWh = cp.EnergyWh
			})
			log.Info("transaction stopped (remote)", "txn", req.TransactionID, "reason", req.Reason, "energyWh", energyWh)
		}
	case "MeterValues":
		var req ocpp.MeterValuesReq
		if json.Unmarshal(f.Payload, &req) == nil {
			applyMeterValuesToState(m, id, &req)
		}
	}
}
