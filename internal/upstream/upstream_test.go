package upstream

import (
	"testing"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
)

func TestAuthUsernameDefaultsToUpstreamID(t *testing.T) {
	r := config.Remote{UpstreamID: "00000000-0009-4000-8020-00000006d38f"}
	if got := AuthUsername(r); got != r.UpstreamID {
		t.Errorf("AuthUsername = %q, want upstream id %q", got, r.UpstreamID)
	}
}

func TestAuthUsernameOverride(t *testing.T) {
	r := config.Remote{UpstreamID: "up-1", Username: "custom"}
	if got := AuthUsername(r); got != "custom" {
		t.Errorf("AuthUsername = %q, want custom override", got)
	}
}

func TestURLComposition(t *testing.T) {
	cases := []struct{ base, id, want string }{
		{"wss://csms.example.com", "abc", "wss://csms.example.com/abc"},
		{"wss://csms.example.com/", "abc", "wss://csms.example.com/abc"},
		{"wss://host/ocpp", "abc", "wss://host/ocpp/abc"},
	}
	for _, c := range cases {
		if got := URL(config.Remote{URL: c.base, UpstreamID: c.id}); got != c.want {
			t.Errorf("URL(%q,%q) = %q, want %q", c.base, c.id, got, c.want)
		}
	}
}
