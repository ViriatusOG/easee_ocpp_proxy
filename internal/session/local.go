package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/clock"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
	"github.com/ipeel/easee-ocpp-proxy/internal/state"
)

// localCSMS implements the local Central System for auto-authorised chargepoints
// (§5.3). It answers CP-initiated CALLs and, when a connector waits for backend
// authorisation, proactively starts the charge with RemoteStartTransaction
// (config LocalAutoStart — the active side of Q8).
type localCSMS struct {
	id  string
	m   *manager.Manager
	clk clock.Clock
	log *slog.Logger
}

// handle produces the response frames for a frame from the chargepoint, updating
// live state as a side effect. It usually returns one frame (the confirmation); a
// StatusNotification that triggers auto-start returns the confirmation followed by a
// RemoteStartTransaction CALL. It returns nil when no response is due.
func (h *localCSMS) handle(f *ocpp.Frame) ([][]byte, error) {
	if f.Type != ocpp.CALL {
		// Local CPs receive one CSMS-initiated CALL from us (RemoteStartTransaction);
		// its CALLRESULT/CALLERROR reply needs no further response.
		return nil, nil
	}

	now := h.clk.Now()
	nowStr := now.UTC().Format(time.RFC3339)

	switch f.Action {
	case "BootNotification":
		h.touch(func(cp *state.CP) { cp.DownstreamUp = true })
		return frames(ocpp.Result(f.UniqueID, ocpp.BootNotificationConf{
			CurrentTime: nowStr,
			Interval:    h.m.Config().HeartbeatIntervalS,
			Status:      "Accepted",
		}))

	case "Heartbeat":
		h.touch(func(cp *state.CP) { cp.LastHeartbeat = now })
		return frames(ocpp.Result(f.UniqueID, ocpp.HeartbeatConf{CurrentTime: nowStr}))

	case "Authorize":
		return frames(ocpp.Result(f.UniqueID, ocpp.AuthorizeConf{IdTagInfo: ocpp.IdTagInfo{Status: "Accepted"}}))

	case "StartTransaction":
		var req ocpp.StartTransactionReq
		_ = json.Unmarshal(f.Payload, &req)
		txnID := h.m.NextTransactionID()
		h.touch(func(cp *state.CP) {
			cp.TxnActive = true
			cp.TxnID = txnID
			cp.IDTag = req.IDTag
			cp.TxnStart = now
			cp.MeterStartWh = float64(req.MeterStart)
			cp.MeterLastWh = float64(req.MeterStart)
			cp.EnergyWh = 0
			cp.RemoteStartSent = false
		})
		h.log.Info("transaction started", "txn", txnID, "connector", req.ConnectorID, "idTag", req.IDTag, "meterStartWh", req.MeterStart)
		return frames(ocpp.Result(f.UniqueID, ocpp.StartTransactionConf{
			TransactionID: txnID,
			IdTagInfo:     ocpp.IdTagInfo{Status: "Accepted"},
		}))

	case "StopTransaction":
		var req ocpp.StopTransactionReq
		_ = json.Unmarshal(f.Payload, &req)
		var energyWh float64
		h.touch(func(cp *state.CP) {
			if req.MeterStop > 0 {
				cp.MeterLastWh = float64(req.MeterStop)
				if cp.MeterStartWh > 0 {
					cp.EnergyWh = float64(req.MeterStop) - cp.MeterStartWh
				}
			}
			cp.TxnActive = false
			cp.LastStopReason = req.Reason
			energyWh = cp.EnergyWh
		})
		h.log.Info("transaction stopped", "txn", req.TransactionID, "reason", req.Reason, "energyWh", energyWh)
		return frames(ocpp.Result(f.UniqueID, ocpp.StopTransactionConf{IdTagInfo: &ocpp.IdTagInfo{Status: "Accepted"}}))

	case "StatusNotification":
		return h.handleStatus(f)

	case "MeterValues":
		var req ocpp.MeterValuesReq
		_ = json.Unmarshal(f.Payload, &req)
		h.applyMeterValues(&req)
		return frames(ocpp.Result(f.UniqueID, struct{}{}))

	case "DataTransfer":
		// FR-14: accept so the CP considers it handled (local CPs only).
		return frames(ocpp.Result(f.UniqueID, ocpp.DataTransferConf{Status: "Accepted"}))

	default:
		// Q7 default: unknown action → CALLERROR NotSupported (as seen in Appendix A).
		return frames(ocpp.CallError(f.UniqueID, "NotSupported", "action not supported by local proxy", nil))
	}
}

// handleStatus records connector status and, when a connector is Preparing without an
// active transaction, issues a one-shot RemoteStartTransaction to auto-start.
func (h *localCSMS) handleStatus(f *ocpp.Frame) ([][]byte, error) {
	var req ocpp.StatusNotificationReq
	_ = json.Unmarshal(f.Payload, &req)

	h.touch(func(cp *state.CP) {
		// connectorId 0 refers to the charge point as a whole; prefer a real
		// connector's status but fall back to it if that is all we have.
		if req.ConnectorID != 0 {
			cp.ConnectorStatus = req.Status
			cp.LastConnector = req.ConnectorID
		} else if cp.ConnectorStatus == "" {
			cp.ConnectorStatus = req.Status
		}
		// Re-arm auto-start once the connector leaves Preparing.
		if req.Status != "Preparing" {
			cp.RemoteStartSent = false
		}
	})
	h.log.Info("status", "connector", req.ConnectorID, "status", req.Status, "error", req.ErrorCode)

	conf, err := ocpp.Result(f.UniqueID, struct{}{})
	if err != nil {
		return nil, err
	}

	if !h.shouldAutoStart(req) {
		return [][]byte{conf}, nil
	}

	idTag := h.m.Config().AutoStartIDTag()
	uid := h.m.NextMessageID()
	call, err := ocpp.Call(uid, "RemoteStartTransaction", ocpp.RemoteStartTransactionReq{
		ConnectorID: req.ConnectorID,
		IDTag:       idTag,
	})
	if err != nil {
		return [][]byte{conf}, nil
	}
	h.m.State().Update(h.id, func(cp *state.CP) { cp.RemoteStartSent = true })
	h.log.Info("auto-start: sending RemoteStartTransaction", "connector", req.ConnectorID, "idTag", idTag, "uid", uid)
	return [][]byte{conf, call}, nil
}

// shouldAutoStart reports whether a Preparing status warrants a RemoteStartTransaction.
func (h *localCSMS) shouldAutoStart(req ocpp.StatusNotificationReq) bool {
	if req.Status != "Preparing" || req.ConnectorID == 0 {
		return false
	}
	if !h.m.Config().LocalAutoStart.Enabled {
		return false
	}
	// Respect the charging schedule: outside the window, wait for the reconciler to
	// start when the window opens (FR-41).
	if !h.m.AllowedAt(h.id, h.clk.Now()) {
		return false
	}
	st, _ := h.m.State().Get(h.id)
	return !st.TxnActive && !st.RemoteStartSent
}

// --- Schedule reconciliation (FR-41) ---

type schedAction int

const (
	schedNone schedAction = iota
	schedStart
	schedStop
)

// scheduleCooldown avoids re-issuing start/stop on every tick while the charger
// transitions between states.
const scheduleCooldown = 45 * time.Second

// runScheduler periodically enforces the charging window for a local CP: it starts a
// waiting-but-plugged CP when the window opens, and stops an active charge when the
// window closes.
func (h *localCSMS) runScheduler(ctx context.Context, send func([]byte) error) {
	ticker := time.NewTicker(scheduleTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.reconcileSchedule(send)
		}
	}
}

func (h *localCSMS) reconcileSchedule(send func([]byte) error) {
	st, ok := h.m.State().Get(h.id)
	if !ok || !st.DownstreamUp {
		return
	}
	action := decideSchedule(h.m.AllowedNow(h.id), st.ConnectorStatus, st.TxnActive, h.m.Config().LocalAutoStart.Enabled)
	if action == schedNone {
		return
	}
	now := time.Now()
	if !st.LastScheduleAction.IsZero() && now.Sub(st.LastScheduleAction) < scheduleCooldown {
		return
	}

	uid := h.m.NextMessageID()
	var call []byte
	var err error
	switch action {
	case schedStart:
		connector := st.LastConnector
		if connector == 0 {
			connector = 1
		}
		call, err = ocpp.Call(uid, "RemoteStartTransaction", ocpp.RemoteStartTransactionReq{
			ConnectorID: connector, IDTag: h.m.Config().AutoStartIDTag(),
		})
		h.log.Info("schedule: window open — starting charge", "connector", connector, "uid", uid)
	case schedStop:
		call, err = ocpp.Call(uid, "RemoteStopTransaction", ocpp.RemoteStopTransactionReq{TransactionID: st.TxnID})
		h.log.Info("schedule: window closed — stopping charge", "txn", st.TxnID, "uid", uid)
	}
	if err != nil || send(call) != nil {
		return
	}
	h.m.State().Update(h.id, func(cp *state.CP) { cp.LastScheduleAction = now })
}

// decideSchedule maps the current allowed/plugged/active state to a start/stop action.
func decideSchedule(allowed bool, status string, txnActive, autoStartEnabled bool) schedAction {
	if !allowed {
		if txnActive {
			return schedStop
		}
		return schedNone
	}
	if !txnActive && autoStartEnabled && isWaiting(status) {
		return schedStart
	}
	return schedNone
}

// isWaiting reports whether a connector status means a car is plugged in but not
// charging (so it can be started).
func isWaiting(status string) bool {
	switch status {
	case "Preparing", "SuspendedEV", "SuspendedEVSE":
		return true
	}
	return false
}

// touch updates the CP state, always bumping LastMessage, then applying fn.
func (h *localCSMS) touch(fn func(*state.CP)) {
	h.m.State().Update(h.id, func(cp *state.CP) {
		cp.LastMessage = h.clk.Now()
		fn(cp)
	})
}

func (h *localCSMS) applyMeterValues(req *ocpp.MeterValuesReq) {
	applyMeterValuesToState(h.m, h.id, req)
	h.touch(func(*state.CP) {}) // bump LastMessage
}

// applyMeterValuesToState extracts energy/power (honouring units) and updates state.
// Shared by the local handler and the proxy observer.
func applyMeterValuesToState(m *manager.Manager, id string, req *ocpp.MeterValuesReq) {
	var energyWh, powerW float64
	var haveEnergy, havePower bool

	for _, mv := range req.MeterValue {
		for _, sv := range mv.SampledValue {
			measurand := sv.Measurand
			if measurand == "" {
				measurand = "Energy.Active.Import.Register"
			}
			val, err := strconv.ParseFloat(strings.TrimSpace(sv.Value), 64)
			if err != nil {
				continue
			}
			switch measurand {
			case "Energy.Active.Import.Register":
				energyWh, haveEnergy = normaliseEnergyWh(val, sv.Unit), true
			case "Power.Active.Import":
				powerW, havePower = normalisePowerW(val, sv.Unit), true
			}
		}
	}

	m.State().Update(id, func(cp *state.CP) {
		if havePower {
			cp.PowerW = powerW
		}
		if haveEnergy {
			cp.MeterLastWh = energyWh
			if cp.TxnActive && cp.MeterStartWh > 0 {
				cp.EnergyWh = energyWh - cp.MeterStartWh
			}
		}
	})
}

// frames wraps a single builder result as the ([][]byte, error) the loop expects.
func frames(b []byte, err error) ([][]byte, error) {
	if err != nil {
		return nil, err
	}
	return [][]byte{b}, nil
}

// normaliseEnergyWh converts a metered energy value to Wh, honouring the explicit
// unit (Appendix A: Easee reports this register in kWh).
func normaliseEnergyWh(v float64, unit string) float64 {
	if strings.EqualFold(unit, "kWh") {
		return v * 1000
	}
	return v // "Wh" or unspecified
}

// normalisePowerW converts a metered power value to W, honouring the explicit unit.
func normalisePowerW(v float64, unit string) float64 {
	if strings.EqualFold(unit, "kW") {
		return v * 1000
	}
	return v // "W" or unspecified
}
