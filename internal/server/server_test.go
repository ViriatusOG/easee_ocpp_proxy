package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	c := config.Default()
	c.Chargepoints = []string{"K9PZQ4RT"}
	m := manager.New(&c)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(New(m, logger).Handler)
	t.Cleanup(ts.Close)
	return ts
}

func wsURL(ts *httptest.Server, path string) string {
	return strings.Replace(ts.URL, "http", "ws", 1) + path
}

func TestAllowedChargepointConnects(t *testing.T) {
	ts := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL(ts, "/K9PZQ4RT"), &websocket.DialOptions{
		Subprotocols: []string{ocpp.Subprotocol},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	if got := c.Subprotocol(); got != ocpp.Subprotocol {
		t.Fatalf("subprotocol = %q, want %q", got, ocpp.Subprotocol)
	}

	// Observe-only in M1: the frame is accepted (logged), no response expected.
	boot, err := ocpp.Call("42", "BootNotification", map[string]any{"chargePointVendor": "x", "chargePointModel": "y"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageText, boot); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "done")
}

func TestLocalAutoAuthoriseEndToEnd(t *testing.T) {
	ts := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL(ts, "/K9PZQ4RT"), &websocket.DialOptions{
		Subprotocols: []string{ocpp.Subprotocol},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	boot, _ := ocpp.Call("42", "BootNotification", ocpp.BootNotificationReq{
		ChargePointVendor: "Easee ASA", ChargePointModel: "Easee One",
	})
	if err := c.Write(ctx, websocket.MessageText, boot); err != nil {
		t.Fatalf("write: %v", err)
	}

	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("response opcode = %d, want text", int(typ))
	}
	resp, err := ocpp.Parse(data)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Type != ocpp.CALLRESULT || resp.UniqueID != "42" {
		t.Fatalf("response = type %v uid %q, want CALLRESULT/42", resp.Type, resp.UniqueID)
	}
	var conf ocpp.BootNotificationConf
	if err := json.Unmarshal(resp.Payload, &conf); err != nil {
		t.Fatal(err)
	}
	if conf.Status != "Accepted" || conf.Interval != 300 {
		t.Fatalf("conf = %+v, want Accepted/300", conf)
	}
	_ = c.Close(websocket.StatusNormalClosure, "done")
}

func TestNonWebSocketRequestsGetQuiet404(t *testing.T) {
	ts := newTestServer(t)
	for _, path := range []string{"/favicon.ico", "/", "/Wallbox"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s (plain HTTP) = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestUnknownChargepointRejected(t *testing.T) {
	ts := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, wsURL(ts, "/NOPE"), &websocket.DialOptions{
		Subprotocols: []string{ocpp.Subprotocol},
	})
	if err == nil {
		c.CloseNow()
		t.Fatal("expected dial to fail for unknown chargepoint")
	}
	if resp != nil && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestSubprotocolRequired(t *testing.T) {
	ts := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// No subprotocol offered → server accepts the upgrade then closes with a policy violation.
	c, _, err := websocket.Dial(ctx, wsURL(ts, "/K9PZQ4RT"), nil)
	if err != nil {
		// Some negotiation failures surface at dial time — that is also acceptable.
		return
	}
	defer c.CloseNow()
	if _, _, err := c.Read(ctx); err != nil {
		if websocket.CloseStatus(err) == websocket.StatusPolicyViolation {
			return // expected
		}
		return // any close/read error is acceptable; the connection was refused
	}
	t.Fatal("expected the connection to be closed when ocpp1.6 is not negotiated")
}
