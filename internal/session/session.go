// Package session handles a single downstream (Easee-facing) WebSocket connection.
//
// Milestone 2/3: local (auto-authorised) chargepoints are served by the local Central
// System (local.go), including schedule-driven start/stop (FR-41). Proxied
// chargepoints remain observe-only until Milestone 4. A liveness watchdog closes a
// connection silent for 2× the heartbeat interval (FR-24).
package session

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/ipeel/easee-ocpp-proxy/internal/clock"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
	"github.com/ipeel/easee-ocpp-proxy/internal/state"
)

// scheduleTick is how often the background reconciler re-evaluates a CP's window.
const scheduleTick = 15 * time.Second

// Serve runs the read loop for one downstream connection until it closes or ctx is
// cancelled. It takes ownership of c and closes it on return.
func Serve(ctx context.Context, c *websocket.Conn, id string, role manager.Role, m *manager.Manager, clk clock.Clock, logger *slog.Logger) {
	log := logger.With("cp", id, "role", role.String())

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Register so the admin UI can drop this session on a role change (D-9), and so a
	// duplicate connection for the same id replaces this one (Q6).
	h := m.RegisterSession(id, cancel)
	defer m.UnregisterSession(id, h)

	m.State().Update(id, func(cp *state.CP) {
		cp.ID = id
		cp.Role = role.String()
		cp.DownstreamUp = true
	})
	defer m.State().Update(id, func(cp *state.CP) {
		cp.DownstreamUp = false
		cp.UpstreamUp = false
	})
	defer c.CloseNow()

	// Writes may come from the read loop and (for local CPs) the schedule reconciler,
	// so serialise them — the WebSocket library forbids concurrent writes.
	var writeMu sync.Mutex
	send := func(data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return c.Write(ctx, websocket.MessageText, data)
	}

	// FR-24: liveness cutoff = 2× the applicable heartbeat interval. For proxied CPs
	// Milestone 4 replaces this with the CSMS-supplied interval.
	liveness := 2 * m.Config().HeartbeatInterval()
	wd := newWatchdog(clk, liveness, func() {
		log.Warn("liveness timeout; closing connection", "after", liveness.String())
		cancel()
	})
	defer wd.stop()

	local := &localCSMS{id: id, m: m, clk: clk, log: log}
	if role == manager.RoleLocal {
		go local.runScheduler(ctx, send)
	}

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			log.Info("chargepoint disconnected", "err", err)
			return
		}
		wd.kick()

		if typ != websocket.MessageText {
			log.Warn("ignoring non-text frame", "opcode", int(typ))
			continue
		}
		f, err := ocpp.Parse(data)
		if err != nil {
			log.Warn("dropping malformed OCPP frame", "err", err)
			continue
		}

		switch role {
		case manager.RoleLocal:
			// Full frames at DEBUG for deep troubleshooting; the handler emits
			// operator-meaningful lifecycle events at INFO.
			if f.Type == ocpp.CALL {
				log.Debug("cp → proxy", "action", f.Action, "uid", f.UniqueID, "payload", string(f.Payload))
			}
			resps, herr := local.handle(f)
			if herr != nil {
				log.Error("handler error", "action", f.Action, "err", herr)
				continue
			}
			for _, resp := range resps {
				if resp == nil {
					continue
				}
				if err := send(resp); err != nil {
					log.Info("write failed; disconnecting", "err", err)
					return
				}
			}

		case manager.RoleProxied:
			// TODO(milestone-4): relay to the remote CSMS. Observe-only for now.
			log.Info("ocpp frame (proxied observe-only)", "kind", f.Type.String(), "action", f.Action, "uid", f.UniqueID)
		}
	}
}

// watchdog fires onExpire unless kicked within its interval.
type watchdog struct {
	d time.Duration
	t clock.Timer
}

func newWatchdog(clk clock.Clock, d time.Duration, onExpire func()) *watchdog {
	return &watchdog{d: d, t: clk.AfterFunc(d, onExpire)}
}

func (w *watchdog) kick() { w.t.Reset(w.d) }

func (w *watchdog) stop() { w.t.Stop() }
