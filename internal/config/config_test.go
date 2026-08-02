package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	p := writeTemp(t, `
listen_addr: ":9000"
heartbeat_interval_s: 300
upstream_timeout_s: 30
chargepoints: [K9PZQ4RT, M4XTR7Q2]
proxied_id: K9PZQ4RT
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.HasChargepoint("K9PZQ4RT") {
		t.Error("expected K9PZQ4RT in allow-list")
	}
	if got := cfg.HeartbeatInterval().Seconds(); got != 300 {
		t.Errorf("heartbeat = %v, want 300s", got)
	}
}

func TestProxiedIDMustBeInList(t *testing.T) {
	p := writeTemp(t, `
listen_addr: ":9000"
heartbeat_interval_s: 300
upstream_timeout_s: 30
chargepoints: [K9PZQ4RT]
proxied_id: NOTLISTED
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for proxied_id not in allow-list")
	}
}

func TestWarnOnLongID(t *testing.T) {
	cfg := Default()
	cfg.Chargepoints = []string{"THIS-ID-IS-DEFINITELY-LONGER-THAN-25"}
	if len(cfg.Warnings()) == 0 {
		t.Error("expected a warning for an over-length chargepoint id")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	p := writeTemp(t, `
listen_addr: ":9000"
heartbeat_interval_s: 300
upstream_timeout_s: 30
chargepoints: [K9PZQ4RT]
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chargepoints = append(cfg.Chargepoints, "M4XTR7Q2")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.HasChargepoint("M4XTR7Q2") {
		t.Error("saved chargepoint did not persist")
	}
}
