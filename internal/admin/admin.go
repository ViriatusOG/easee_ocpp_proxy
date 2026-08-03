// Package admin serves the /admin management interface: plain HTTP on the shared
// port (D-2), an HTML form UI (D-5), authenticated with a username + bcrypt
// password (D-4). The dashboard is the home page and the daily surface for choosing
// which chargepoint is remotely managed (§5.6 IA); settings live on separate pages.
package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
)

const (
	cookieName    = "easee_admin"
	sessionTTL    = 30 * 24 * time.Hour // long-lived so a trusted device stays signed in
	minPasswordLn = 8
)

// Handler serves the admin UI.
type Handler struct {
	m    *manager.Manager
	log  *slog.Logger
	tmpl map[string]*template.Template
	mux  *http.ServeMux
}

// New builds the admin Handler and its route table.
func New(m *manager.Manager, log *slog.Logger) *Handler {
	h := &Handler{
		m:    m,
		log:  log,
		tmpl: buildTemplates(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/admin/login", h.login)
	mux.HandleFunc("/admin/logout", h.logout)
	mux.HandleFunc("/admin/state", h.requireAuth(h.dashboardState))
	mux.HandleFunc("/admin/proxied", h.requireAuth(h.setProxied))
	mux.HandleFunc("/admin/mode", h.requireAuth(h.setMode))
	mux.HandleFunc("/admin/chargepoints", h.requireAuth(h.chargepoints))
	mux.HandleFunc("/admin/schedules", h.requireAuth(h.schedules))
	mux.HandleFunc("/admin/remote", h.requireAuth(h.remote))
	mux.HandleFunc("/admin/account", h.requireAuth(h.account))
	mux.HandleFunc("/admin/", h.requireAuth(h.dashboard))
	mux.HandleFunc("/admin", h.requireAuth(h.dashboard))
	h.mux = mux
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// requireAuth redirects to the login/setup page unless the request carries a valid
// session cookie.
func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		h.startSession(w, user) // sliding expiry: refresh the cookie on each request
		next(w, r)
	}
}

// currentUser returns the signed-in user from the session cookie, if valid. Sessions
// are stateless: the cookie is an HMAC over (username, expiry) keyed by the admin
// password hash, so it survives restarts and is invalidated by a password change.
func (h *Handler) currentUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	key := h.m.ConfigSnapshot().Admin.PasswordHash
	if key == "" {
		return "", false
	}
	return verifySession(c.Value, key)
}

// login handles both first-run setup (when no admin password is set) and normal
// sign-in.
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	cfg := h.m.ConfigSnapshot()
	setupMode := cfg.Admin.PasswordHash == ""

	if r.Method == http.MethodGet {
		page := "login"
		if setupMode {
			page = "setup"
		}
		h.render(w, page, map[string]any{"Title": "Sign in", "Error": r.URL.Query().Get("error")})
		return
	}

	// POST
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	if setupMode {
		if len(password) < minPasswordLn {
			h.render(w, "setup", map[string]any{"Title": "Set up", "Error": "password must be at least 8 characters"})
			return
		}
		if password != r.FormValue("confirm") {
			h.render(w, "setup", map[string]any{"Title": "Set up", "Error": "passwords do not match"})
			return
		}
		if username == "" {
			username = "admin"
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "hash error", http.StatusInternalServerError)
			return
		}
		if err := h.m.SetAdminCredentials(username, string(hash)); err != nil {
			http.Error(w, "save error", http.StatusInternalServerError)
			return
		}
		h.log.Info("admin credentials initialised", "username", username)
		h.startSession(w, username)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	if username != cfg.Admin.Username ||
		bcrypt.CompareHashAndPassword([]byte(cfg.Admin.PasswordHash), []byte(password)) != nil {
		h.log.Warn("failed admin login", "username", username, "remote", r.RemoteAddr)
		http.Redirect(w, r, "/admin/login?error=invalid+credentials", http.StatusSeeOther)
		return
	}
	h.startSession(w, username)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin", MaxAge: -1})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *Handler) startSession(w http.ResponseWriter, username string) {
	key := h.m.ConfigSnapshot().Admin.PasswordHash
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    signSession(username, key, time.Now().Add(sessionTTL).Unix()),
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// signSession builds a stateless session token: "username|expiry|HMAC".
func signSession(username, key string, expiry int64) string {
	msg := username + "|" + strconv.FormatInt(expiry, 10)
	return msg + "|" + mac(msg, key)
}

// verifySession validates a session token and returns the username if the signature
// is valid and it has not expired.
func verifySession(value, key string) (string, bool) {
	i := strings.LastIndex(value, "|")
	if i < 0 {
		return "", false
	}
	msg, sig := value[:i], value[i+1:]
	if !hmac.Equal([]byte(sig), []byte(mac(msg, key))) {
		return "", false
	}
	j := strings.LastIndex(msg, "|") // msg = username|expiry
	if j < 0 {
		return "", false
	}
	exp, err := strconv.ParseInt(msg[j+1:], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false
	}
	return msg[:j], true
}

func mac(msg, key string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// render executes a named page template, logging (not exposing) any error.
func (h *Handler) render(w http.ResponseWriter, page string, data any) {
	t, ok := h.tmpl[page]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		h.log.Error("template render failed", "page", page, "err", err)
	}
}
