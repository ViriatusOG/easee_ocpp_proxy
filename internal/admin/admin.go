// Package admin serves the /admin management interface: plain HTTP on the shared
// port (D-2), an HTML form UI (D-5), authenticated with a username + bcrypt
// password (D-4). The dashboard is the home page and the daily surface for choosing
// which chargepoint is remotely managed (§5.6 IA); settings live on separate pages.
package admin

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
)

const (
	cookieName    = "easee_admin"
	sessionTTL    = 8 * time.Hour
	minPasswordLn = 8
)

// Handler serves the admin UI.
type Handler struct {
	m    *manager.Manager
	log  *slog.Logger
	tmpl map[string]*template.Template
	mux  *http.ServeMux

	mu       sync.Mutex
	sessions map[string]session // token → session
}

type session struct {
	username string
	expires  time.Time
}

// New builds the admin Handler and its route table.
func New(m *manager.Manager, log *slog.Logger) *Handler {
	h := &Handler{
		m:        m,
		log:      log,
		tmpl:     buildTemplates(),
		sessions: make(map[string]session),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/admin/login", h.login)
	mux.HandleFunc("/admin/logout", h.logout)
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
		if _, ok := h.currentUser(r); !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (h *Handler) currentUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[c.Value]
	if !ok || time.Now().After(s.expires) {
		if ok {
			delete(h.sessions, c.Value)
		}
		return "", false
	}
	return s.username, true
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
	if c, err := r.Cookie(cookieName); err == nil {
		h.mu.Lock()
		delete(h.sessions, c.Value)
		h.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin", MaxAge: -1})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *Handler) startSession(w http.ResponseWriter, username string) {
	token := randomToken()
	h.mu.Lock()
	h.sessions[token] = session{username: username, expires: time.Now().Add(sessionTTL)}
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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
