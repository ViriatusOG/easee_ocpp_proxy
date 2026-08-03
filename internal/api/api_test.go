package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
)

func newTestAPI(t *testing.T, token string) (*httptest.Server, *manager.Manager) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	body := "listen_addr: \":9000\"\nheartbeat_interval_s: 300\nupstream_timeout_s: 30\n" +
		"chargepoints: [CP1]\naliases: {CP1: Garage}\napi_token: \"" + token + "\"\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	m := manager.New(cfg)
	h := New(m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, m
}

func TestAPIRequiresToken(t *testing.T) {
	ts, _ := newTestAPI(t, "secret")

	// No token → 401.
	resp, _ := http.Get(ts.URL + "/api/chargepoints")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no auth = %d, want 401", resp.StatusCode)
	}

	// Wrong token → 401.
	req, _ := http.NewRequest("GET", ts.URL+"/api/chargepoints", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", resp.StatusCode)
	}
}

func TestAPIDisabledWithoutToken(t *testing.T) {
	ts, _ := newTestAPI(t, "") // no token configured
	req, _ := http.NewRequest("GET", ts.URL+"/api/chargepoints", nil)
	req.Header.Set("Authorization", "Bearer anything")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("disabled api = %d, want 403", resp.StatusCode)
	}
}

func TestAPIListAndSetMode(t *testing.T) {
	ts, m := newTestAPI(t, "secret")

	get := func(path string) *http.Response {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	resp := get("/api/chargepoints")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list = %d, want 200", resp.StatusCode)
	}
	var list []chargepoint
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 1 || list[0].ID != "CP1" || list[0].Name != "Garage" {
		t.Fatalf("unexpected list: %+v", list)
	}
	if list[0].Role != "always_on" {
		t.Errorf("role = %q, want always_on", list[0].Role)
	}
	// No schedule assigned → only two modes offered.
	if strings.Join(list[0].Modes, ",") != "proxied,always_on" {
		t.Errorf("modes = %v, want [proxied always_on]", list[0].Modes)
	}

	// Set to proxied via the API.
	req, _ := http.NewRequest("POST", ts.URL+"/api/chargepoints/CP1/mode", strings.NewReader(`{"mode":"proxied"}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("setMode = %v / %d, want 200", err, resp.StatusCode)
	}
	if m.ConfigSnapshot().ProxiedID != "CP1" {
		t.Error("mode change did not take effect")
	}

	// Bad mode → 400.
	req, _ = http.NewRequest("POST", ts.URL+"/api/chargepoints/CP1/mode", strings.NewReader(`{"mode":"nope"}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad mode = %d, want 400", resp.StatusCode)
	}
}
