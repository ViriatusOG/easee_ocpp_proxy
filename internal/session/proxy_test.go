package session

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ipeel/easee-ocpp-proxy/internal/clock"
	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
)

func ws(u string) string { return strings.Replace(u, "http", "ws", 1) }

// mockCSMS is an in-process Central System that records received frames and lets the
// test push frames back to the proxy.
type mockCSMS struct {
	srv      *httptest.Server
	received chan []byte
	mu       sync.Mutex
	conn     *websocket.Conn
}

func newMockCSMS(t *testing.T) *mockCSMS {
	t.Helper()
	m := &mockCSMS{received: make(chan []byte, 32)}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{ocpp.Subprotocol}})
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conn = c
		m.mu.Unlock()
		for {
			_, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			m.received <- data
		}
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockCSMS) send(t *testing.T, data []byte) {
	t.Helper()
	m.mu.Lock()
	c := m.conn
	m.mu.Unlock()
	if c == nil {
		t.Fatal("CSMS has no connection yet")
	}
	if err := c.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatalf("CSMS send: %v", err)
	}
}

func readFrame(t *testing.T, c *websocket.Conn) *ocpp.Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	f, err := ocpp.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

func recvFrame(t *testing.T, ch <-chan []byte) *ocpp.Frame {
	t.Helper()
	select {
	case data := <-ch:
		f, err := ocpp.Parse(data)
		if err != nil {
			t.Fatalf("parse received: %v", err)
		}
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a frame at the CSMS")
		return nil
	}
}

func TestProxyRelayAndBootAnonymisation(t *testing.T) {
	csms := newMockCSMS(t)

	// Point the proxy's upstream dialer at the mock CSMS.
	orig := upstreamDialer
	upstreamDialer = func(ctx context.Context, r config.Remote) (*websocket.Conn, *http.Response, error) {
		return websocket.Dial(ctx, ws(csms.srv.URL), &websocket.DialOptions{Subprotocols: []string{ocpp.Subprotocol}})
	}
	defer func() { upstreamDialer = orig }()

	c := config.Default()
	c.Chargepoints = []string{"P1"}
	c.ProxiedID = "P1"
	c.Remote = config.Remote{URL: "wss://dummy", UpstreamID: "up-1", Username: "u", PasswordEnc: "p"}
	c.BootAnonymise = config.BootAnonymise{ChargePointVendor: "Wallbox", ChargePointModel: "Pulsar"}
	m := manager.New(&c)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dc, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{ocpp.Subprotocol}})
		if err != nil {
			return
		}
		Serve(r.Context(), dc, "P1", manager.RoleProxied, m, clock.Real(), logger)
	}))
	defer proxy.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	easee, _, err := websocket.Dial(ctx, ws(proxy.URL), &websocket.DialOptions{Subprotocols: []string{ocpp.Subprotocol}})
	if err != nil {
		t.Fatalf("easee dial: %v", err)
	}
	defer easee.CloseNow()

	// Easee sends BootNotification with its real vendor.
	boot, _ := ocpp.Call("42", "BootNotification", ocpp.BootNotificationReq{
		ChargePointVendor: "Easee ASA", ChargePointModel: "Easee One", ChargePointSerialNumber: "UKGUAY4Y",
	})
	if err := easee.Write(ctx, websocket.MessageText, boot); err != nil {
		t.Fatal(err)
	}

	// The CSMS must receive an ANONYMISED BootNotification (never "Easee ASA").
	got := recvFrame(t, csms.received)
	if got.Action != "BootNotification" {
		t.Fatalf("CSMS got %q, want BootNotification", got.Action)
	}
	var bp ocpp.BootNotificationReq
	json.Unmarshal(got.Payload, &bp)
	if bp.ChargePointVendor != "Wallbox" || bp.ChargePointModel != "Pulsar" {
		t.Errorf("boot not anonymised upstream: vendor=%q model=%q", bp.ChargePointVendor, bp.ChargePointModel)
	}
	if strings.Contains(string(got.Payload), "Easee") {
		t.Errorf("manufacturer leaked upstream: %s", got.Payload)
	}

	// CSMS replies with a boot conf → must reach the Easee unchanged.
	csms.send(t, []byte(`[3,"42",{"currentTime":"2026-08-02T18:00:00Z","interval":30,"status":"Accepted"}]`))
	conf := readFrame(t, easee)
	if conf.Type != ocpp.CALLRESULT || conf.UniqueID != "42" {
		t.Fatalf("easee got %v/%s, want CALLRESULT/42", conf.Type, conf.UniqueID)
	}

	// CSMS-initiated command → must relay to the Easee verbatim (D-10).
	csms.send(t, []byte(`[2,"srv-9","RemoteStartTransaction",{"idTag":"ABC","connectorId":1}]`))
	cmd := readFrame(t, easee)
	if cmd.Type != ocpp.CALL || cmd.Action != "RemoteStartTransaction" {
		t.Fatalf("easee got %v/%s, want CALL/RemoteStartTransaction", cmd.Type, cmd.Action)
	}

	// Non-boot downstream frame → relayed to CSMS verbatim.
	hb, _ := ocpp.Call("43", "Heartbeat", struct{}{})
	if err := easee.Write(ctx, websocket.MessageText, hb); err != nil {
		t.Fatal(err)
	}
	up := recvFrame(t, csms.received)
	if up.Action != "Heartbeat" || up.UniqueID != "43" {
		t.Fatalf("CSMS got %v/%s, want Heartbeat/43", up.Action, up.UniqueID)
	}
}

func TestProxyDownstreamCloseTearsDownUpstream(t *testing.T) {
	csms := newMockCSMS(t)
	orig := upstreamDialer
	upstreamDialer = func(ctx context.Context, r config.Remote) (*websocket.Conn, *http.Response, error) {
		return websocket.Dial(ctx, ws(csms.srv.URL), &websocket.DialOptions{Subprotocols: []string{ocpp.Subprotocol}})
	}
	defer func() { upstreamDialer = orig }()

	c := config.Default()
	c.Chargepoints = []string{"P1"}
	c.ProxiedID = "P1"
	c.Remote = config.Remote{URL: "wss://dummy", UpstreamID: "up-1"}
	m := manager.New(&c)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dc, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{ocpp.Subprotocol}})
		if err != nil {
			return
		}
		Serve(r.Context(), dc, "P1", manager.RoleProxied, m, clock.Real(), logger)
	}))
	defer proxy.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	easee, _, err := websocket.Dial(ctx, ws(proxy.URL), &websocket.DialOptions{Subprotocols: []string{ocpp.Subprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	// Force upstream to have connected.
	hb, _ := ocpp.Call("1", "Heartbeat", struct{}{})
	easee.Write(ctx, websocket.MessageText, hb)
	recvFrame(t, csms.received)

	// Close the downstream; the CSMS read should then error (upstream torn down).
	easee.CloseNow()
	csms.mu.Lock()
	cc := csms.conn
	csms.mu.Unlock()
	rctx, rcancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer rcancel()
	if _, _, err := cc.Read(rctx); err == nil {
		t.Fatal("expected upstream CSMS connection to close after downstream closed")
	}
}
