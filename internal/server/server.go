// Package server wires the single HTTP listener that serves both the OCPP
// WebSocket endpoint and the /admin management UI on one port (D-2).
//
// Routing (Go 1.22 ServeMux; more specific patterns win):
//
//	/admin, /admin/  → admin UI
//	/                → OCPP WebSocket handler; CP ID = URL path (D-8)
package server

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/ipeel/easee-ocpp-proxy/internal/admin"
	"github.com/ipeel/easee-ocpp-proxy/internal/api"
	"github.com/ipeel/easee-ocpp-proxy/internal/clock"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
	"github.com/ipeel/easee-ocpp-proxy/internal/session"
)

// New builds the HTTP server for the given manager.
func New(m *manager.Manager, logger *slog.Logger) *http.Server {
	mux := http.NewServeMux()

	adminHandler := admin.New(m, logger)
	mux.Handle("/admin", adminHandler)
	mux.Handle("/admin/", adminHandler)
	mux.Handle("/api/", api.New(m, logger))
	mux.HandleFunc("/", ocppHandler(m, logger))

	return &http.Server{
		Addr:    m.Config().ListenAddr,
		Handler: mux,
	}
}

// ocppHandler validates the chargepoint and upgrades to a WebSocket (FR-1..FR-3).
func ocppHandler(m *manager.Manager, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.Trim(r.URL.Path, "/")
		if id == "" || strings.ContainsRune(id, '/') {
			http.NotFound(w, r)
			return
		}

		// Only genuine WebSocket upgrades are OCPP connection attempts. Plain HTTP
		// GETs to the root (browsers probing /favicon.ico, etc.) get a quiet 404 so
		// they aren't logged as rejected chargepoints.
		if !isWebSocketUpgrade(r) {
			logger.Debug("non-websocket request to ocpp endpoint", "path", r.URL.Path, "remote", r.RemoteAddr)
			http.NotFound(w, r)
			return
		}

		// FR-3: only allow-listed chargepoints may connect.
		if !m.Allowed(id) {
			logger.Warn("rejected unknown chargepoint", "cp", id, "remote", r.RemoteAddr)
			http.Error(w, "unknown chargepoint", http.StatusNotFound)
			return
		}

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{ocpp.Subprotocol},
		})
		if err != nil {
			logger.Warn("websocket upgrade failed", "cp", id, "err", err)
			return
		}

		// NFR-6: the OCPP 1.6 subprotocol must be negotiated.
		if c.Subprotocol() != ocpp.Subprotocol {
			logger.Warn("subprotocol not negotiated", "cp", id, "got", c.Subprotocol())
			_ = c.Close(websocket.StatusPolicyViolation, "expected subprotocol "+ocpp.Subprotocol)
			return
		}

		role := m.RoleFor(id)
		logger.Info("chargepoint connected", "cp", id, "role", role.String(), "remote", r.RemoteAddr)
		session.Serve(r.Context(), c, id, role, m, clock.Real(), logger)
	}
}

// isWebSocketUpgrade reports whether r is a WebSocket upgrade handshake.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
