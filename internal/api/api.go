// Package api serves a small JSON API for external integrations (e.g. Home
// Assistant): read-only chargepoint state plus a role/mode command. It is separate
// from the HTML admin UI (D-5) and authenticated with a bearer token (config
// api_token); if no token is configured the API is disabled. It exposes no
// admin/settings functions — only the dashboard's view and role capability (FR-45).
package api

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
)

// Handler serves the JSON API under /api.
type Handler struct {
	m   *manager.Manager
	log *slog.Logger
	mux *http.ServeMux
}

// New builds the API handler and its routes.
func New(m *manager.Manager, log *slog.Logger) *Handler {
	h := &Handler{m: m, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/chargepoints", h.requireToken(h.listChargepoints))
	mux.HandleFunc("POST /api/chargepoints/{id}/mode", h.requireToken(h.setMode))
	h.mux = mux
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// requireToken enforces bearer-token auth; the API is disabled unless a token is set.
func (h *Handler) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := h.m.ConfigSnapshot().APIToken
		if token == "" {
			writeError(w, http.StatusForbidden, "api disabled: no api_token configured")
			return
		}
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if len(auth) <= len(prefix) || subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

// chargepoint is the JSON view of one chargepoint (mirrors the dashboard).
type chargepoint struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Role            string   `json:"role"`  // effective: proxied | scheduled | always_on
	Modes           []string `json:"modes"` // selectable modes (scheduled only if a schedule is assigned)
	Online          bool     `json:"online"`
	Upstream        bool     `json:"upstream"` // meaningful when proxied
	ConnectorStatus string   `json:"connector_status"`
	SessionActive   bool     `json:"session_active"`
	TransactionID   int      `json:"transaction_id"`
	EnergyKWh       float64  `json:"energy_kwh"`
	PowerKW         float64  `json:"power_kw"`
	Schedule        string   `json:"schedule"`
	ScheduleState   string   `json:"schedule_state"` // open | closed | paused | ""
	LastHeartbeat   string   `json:"last_heartbeat,omitempty"`
}

func (h *Handler) listChargepoints(w http.ResponseWriter, r *http.Request) {
	cfg := h.m.ConfigSnapshot()
	out := make([]chargepoint, 0, len(cfg.Chargepoints))
	for _, id := range cfg.Chargepoints {
		out = append(out, h.build(&cfg, id))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) setMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := h.m.ConfigSnapshot()
	if !cfg.HasChargepoint(id) {
		writeError(w, http.StatusNotFound, "unknown chargepoint")
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var err error
	switch body.Mode {
	case "proxied":
		err = h.m.SetProxiedID(id)
	case "always_on":
		err = h.m.SetLocalMode(id, false)
	case "scheduled":
		err = h.m.SetLocalMode(id, true)
	default:
		writeError(w, http.StatusBadRequest, "mode must be one of proxied, always_on, scheduled")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.log.Info("api: chargepoint mode changed", "cp", id, "mode", body.Mode)
	cfg = h.m.ConfigSnapshot()
	writeJSON(w, http.StatusOK, h.build(&cfg, id))
}

func (h *Handler) build(cfg *config.Config, id string) chargepoint {
	st, _ := h.m.State().Get(id)
	c := chargepoint{
		ID:              id,
		Name:            cfg.DisplayName(id),
		Online:          st.DownstreamUp,
		Upstream:        st.UpstreamUp,
		ConnectorStatus: st.ConnectorStatus,
		SessionActive:   st.TxnActive,
		TransactionID:   st.TxnID,
		EnergyKWh:       round3(st.EnergyWh / 1000),
		PowerKW:         round3(st.PowerW / 1000),
		Modes:           []string{"proxied", "always_on"},
	}
	switch {
	case id == cfg.ProxiedID:
		c.Role = "proxied"
	case cfg.ScheduleActive(id):
		c.Role = "scheduled"
	default:
		c.Role = "always_on"
	}
	if cfg.HasSchedule(id) {
		c.Modes = append(c.Modes, "scheduled")
		if c.Role != "proxied" {
			c.Schedule = cfg.DeviceSchedules[id]
			switch {
			case c.Role == "always_on":
				c.ScheduleState = "paused"
			case h.m.AllowedNow(id):
				c.ScheduleState = "open"
			default:
				c.ScheduleState = "closed"
			}
		}
	}
	if !st.LastHeartbeat.IsZero() {
		c.LastHeartbeat = st.LastHeartbeat.UTC().Format(time.RFC3339)
	}
	return c
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }
