package admin

import (
	"strings"
	"testing"
	"time"
)

func TestSessionRoundTrip(t *testing.T) {
	key := "bcrypt-hash-as-key"
	tok := signSession("admin", key, time.Now().Add(time.Hour).Unix())
	user, ok := verifySession(tok, key)
	if !ok || user != "admin" {
		t.Fatalf("verify = %q,%v; want admin,true", user, ok)
	}
}

// A password change (different hash → different key) must invalidate existing cookies.
func TestSessionWrongKey(t *testing.T) {
	tok := signSession("admin", "key1", time.Now().Add(time.Hour).Unix())
	if _, ok := verifySession(tok, "key2"); ok {
		t.Error("token should not verify under a different key")
	}
}

func TestSessionExpired(t *testing.T) {
	tok := signSession("admin", "key", time.Now().Add(-time.Minute).Unix())
	if _, ok := verifySession(tok, "key"); ok {
		t.Error("expired token should not verify")
	}
}

func TestSessionTampered(t *testing.T) {
	key := "key"
	tok := signSession("admin", key, time.Now().Add(time.Hour).Unix())
	tampered := "root" + tok[strings.Index(tok, "|"):] // swap the username
	if _, ok := verifySession(tampered, key); ok {
		t.Error("tampered token should not verify")
	}
}

func TestSessionUsernameWithPipe(t *testing.T) {
	key := "key"
	tok := signSession("ad|min", key, time.Now().Add(time.Hour).Unix())
	user, ok := verifySession(tok, key)
	if !ok || user != "ad|min" {
		t.Errorf("verify = %q,%v; want ad|min,true", user, ok)
	}
}
