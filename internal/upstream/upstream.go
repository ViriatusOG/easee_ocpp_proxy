// Package upstream dials the remote OCPP Central System for a proxied chargepoint:
// wss:// with HTTP Basic auth over TLS (OCPP 1.6 security profile 2, D-3), using the
// configured upstream CP ID as the final URL path segment (D-7/D-8).
package upstream

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/ocpp"
)

// URL composes the upstream WebSocket URL: base + "/" + upstream id (D-8).
func URL(r config.Remote) string {
	return strings.TrimRight(r.URL, "/") + "/" + r.UpstreamID
}

// AuthUsername returns the HTTP Basic auth username. Per OCPP 1.6 security profiles
// the charge point authenticates with its ChargePointId as the username, so this
// defaults to the upstream CP ID unless an explicit username override is configured.
func AuthUsername(r config.Remote) string {
	if r.Username != "" {
		return r.Username
	}
	return r.UpstreamID
}

// Dial opens the upstream connection. The caller supplies a ctx with the dial
// timeout. httpClient may be nil (defaults apply); tests pass an httptest client so a
// TLS mock server's certificate is trusted.
func Dial(ctx context.Context, r config.Remote, httpClient *http.Client) (*websocket.Conn, *http.Response, error) {
	if r.URL == "" || r.UpstreamID == "" {
		return nil, nil, fmt.Errorf("upstream url/upstream_id not configured")
	}
	header := http.Header{}
	if user := AuthUsername(r); user != "" || r.PasswordEnc != "" {
		creds := base64.StdEncoding.EncodeToString([]byte(user + ":" + r.PasswordEnc))
		header.Set("Authorization", "Basic "+creds)
	}
	opts := &websocket.DialOptions{
		Subprotocols: []string{ocpp.Subprotocol},
		HTTPHeader:   header,
		HTTPClient:   httpClient,
	}
	return websocket.Dial(ctx, URL(r), opts)
}
