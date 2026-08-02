// Package session handles a single downstream (Easee-facing) WebSocket connection.
//
//   - local (auto-authorised) CPs are served by the local Central System (local.go),
//     including schedule-driven start/stop (FR-41).
//   - the proxied CP is relayed to the remote CSMS (proxy.go), with BootNotification
//     anonymised upstream (D-6).
//
// A liveness watchdog closes a connection that goes silent for 2× the heartbeat
// interval (FR-24).
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/ipeel/easee-ocpp-proxy/internal/clock"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
	"github.com/ipeel/easee-ocpp-proxy/internal/state"
)

// disconnectReason classifies a WebSocket read error into a short, human-readable
// reason. The raw error (often a noisy OS TCP string) is logged separately at DEBUG.
func disconnectReason(err error) string {
	switch {
	case err == nil:
		return "closed"
	case errors.Is(err, context.Canceled):
		return "session ended"
	default:
		if code := websocket.CloseStatus(err); code != -1 {
			return fmt.Sprintf("clean close (status %d)", int(code))
		}
		return "connection lost — abrupt disconnect (e.g. powered off or network dropped)"
	}
}

// scheduleTick is how often the background reconciler re-evaluates a CP's window.
const scheduleTick = 15 * time.Second

// Serve runs one downstream connection until it closes or ctx is cancelled. It takes
// ownership of c and closes it on return.
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

	if role == manager.RoleProxied {
		runProxy(ctx, c, id, m, clk, log)
		return
	}
	runLocal(ctx, c, id, m, clk, log)
}

// runLocal serves an auto-authorised chargepoint as the local Central System.
func runLocal(ctx context.Context, c *websocket.Conn, id string, m *manager.Manager, clk clock.Clock, log *slog.Logger) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Writes may come from the read loop and the schedule reconciler, so serialise
	// them — the WebSocket library forbids concurrent writes.
	var writeMu sync.Mutex
	send := func(data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return c.Write(ctx, websocket.MessageText, data)
	}

	liveness := 2 * m.Config().HeartbeatInterval()
	wd := newWatchdog(clk, liveness, func() {
		log.Warn("liveness timeout; closing connection", "after", liveness.String())
		cancel()
	})
	defer wd.stop()

	local := &localCSMS{id: id, m: m, clk: clk, log: log}
	go local.runScheduler(ctx, send)

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			log.Info("chargepoint disconnected", "reason", disconnectReason(err))
			log.Debug("disconnect detail", "err", err)
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
	}
}

// watchdog fires onExpire unless kicked within its interval. Safe for concurrent
// kick/stop (the proxy path kicks from two goroutines).
type watchdog struct {
	mu sync.Mutex
	d  time.Duration
	t  clock.Timer
}

func newWatchdog(clk clock.Clock, d time.Duration, onExpire func()) *watchdog {
	return &watchdog{d: d, t: clk.AfterFunc(d, onExpire)}
}

func (w *watchdog) kick() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t.Reset(w.d)
}

// setInterval changes the timeout and restarts the countdown.
func (w *watchdog) setInterval(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.d = d
	w.t.Reset(d)
}

func (w *watchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t.Stop()
}
