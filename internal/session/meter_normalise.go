package session

import (
	"encoding/json"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
)

// meterNormaliser rewrites the absolute energy register (Energy.Active.Import.Register,
// and the meterStart/meterStop it snapshots) in upstream-bound frames so the CSMS sees
// one monotonic virtual meter regardless of which physical charger is proxied
// (proxy_normalise_meter, FR-47).
//
// The offset is fixed for the lifetime of one upstream connection: on the first real
// register reading r, offset = V - r where V is the shared virtual high-water mark
// (bootstrapped to r the first time ever). Thereafter presented = real + offset, and V
// only ever climbs. Because meterStart and meterStop share the same offset it cancels
// in meterStop-meterStart, so per-session energy — and thus billing — is unchanged;
// only the absolute baseline shifts, and it never rolls backwards across a unit switch.
//
// Only the downstream→upstream goroutine touches a normaliser, so it needs no lock of
// its own; the shared high-water mark is guarded inside the manager.
type meterNormaliser struct {
	m          *manager.Manager
	log        *slog.Logger
	haveOffset bool
	offsetWh   float64
}

func newMeterNormaliser(m *manager.Manager, log *slog.Logger) *meterNormaliser {
	return &meterNormaliser{m: m, log: log}
}

// present maps a real Wh reading onto the virtual meter, fixing the connection offset on
// first use and advancing the high-water mark.
func (n *meterNormaliser) present(rWh float64) float64 {
	if !n.haveOffset {
		v, initialised := n.m.VirtualMeterWh()
		if !initialised {
			v = rWh
			n.m.BootstrapVirtualMeter(v)
		}
		n.offsetWh = v - rWh
		n.haveOffset = true
		if n.offsetWh != 0 {
			n.log.Info("meter normalisation engaged",
				"offsetWh", int64(math.Round(n.offsetWh)),
				"virtualWh", int64(math.Round(v)),
				"realWh", int64(math.Round(rWh)))
		}
	}
	p := rWh + n.offsetWh
	n.m.AdvanceVirtualMeter(p)
	return p
}

// rewrite returns the frame bytes to send upstream, with energy-register fields mapped
// onto the virtual meter. Non-meter frames and any parse/shape failure return the
// original bytes unchanged (fail-safe: never corrupt a frame we don't understand).
func (n *meterNormaliser) rewrite(f *ocpp.Frame) []byte {
	switch f.Action {
	case "StartTransaction":
		return n.rewriteIntField(f, "meterStart")
	case "StopTransaction":
		return n.rewriteIntField(f, "meterStop")
	case "MeterValues":
		return n.rewriteMeterValues(f)
	}
	return f.Raw
}

// rewriteIntField rewrites an integer Wh register field (meterStart/meterStop).
func (n *meterNormaliser) rewriteIntField(f *ocpp.Frame, key string) []byte {
	var payload map[string]any
	if err := json.Unmarshal(f.Payload, &payload); err != nil {
		return f.Raw
	}
	rf, ok := toFloat(payload[key])
	if !ok {
		return f.Raw
	}
	payload[key] = int64(math.Round(n.present(rf)))
	out, err := ocpp.Call(f.UniqueID, f.Action, payload)
	if err != nil {
		return f.Raw
	}
	return out
}

// rewriteMeterValues rewrites every Energy.Active.Import.Register sample, preserving the
// sample's unit, decimal precision, and all its other fields (context/format/phase/…).
func (n *meterNormaliser) rewriteMeterValues(f *ocpp.Frame) []byte {
	var payload map[string]any
	if err := json.Unmarshal(f.Payload, &payload); err != nil {
		return f.Raw
	}
	mvs, ok := payload["meterValue"].([]any)
	if !ok {
		return f.Raw
	}
	changed := false
	for _, mvAny := range mvs {
		mv, ok := mvAny.(map[string]any)
		if !ok {
			continue
		}
		svs, ok := mv["sampledValue"].([]any)
		if !ok {
			continue
		}
		for _, svAny := range svs {
			sv, ok := svAny.(map[string]any)
			if !ok {
				continue
			}
			measurand, _ := sv["measurand"].(string)
			if measurand == "" {
				measurand = "Energy.Active.Import.Register"
			}
			if measurand != "Energy.Active.Import.Register" {
				continue
			}
			valStr, ok := sv["value"].(string)
			if !ok {
				continue
			}
			val, err := strconv.ParseFloat(strings.TrimSpace(valStr), 64)
			if err != nil {
				continue
			}
			unit, _ := sv["unit"].(string)
			pWh := n.present(normaliseEnergyWh(val, unit))
			pInUnit := pWh
			if strings.EqualFold(unit, "kWh") {
				pInUnit = pWh / 1000
			}
			sv["value"] = formatLike(valStr, pInUnit)
			changed = true
		}
	}
	if !changed {
		return f.Raw
	}
	out, err := ocpp.Call(f.UniqueID, f.Action, payload)
	if err != nil {
		return f.Raw
	}
	return out
}

// toFloat coerces a JSON-decoded number (float64, or json.Number) to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// formatLike renders v with the same number of decimal places as the original string,
// so a charger sending "18938.023" continues to receive 3-decimal values.
func formatLike(orig string, v float64) string {
	decimals := 0
	if i := strings.IndexByte(orig, '.'); i >= 0 {
		decimals = len(orig) - i - 1
	}
	return strconv.FormatFloat(v, 'f', decimals, 64)
}
