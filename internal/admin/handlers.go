package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
)

// dashboard is the home page: live state for every chargepoint plus the one-click
// proxied-CP selector (FR-32b).
func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	user, _ := h.currentUser(r)
	cfg := h.m.ConfigSnapshot()
	now := time.Now()

	h.render(w, "dashboard", map[string]any{
		"Title":        "Dashboard",
		"User":         user,
		"Rows":         h.buildRows(&cfg, now),
		"Chargepoints": cfg.Chargepoints,
		"ProxiedID":    cfg.ProxiedID,
		"Warnings":     (&cfg).Warnings(),
		"Flash":        r.URL.Query().Get("msg"),
	})
}

// dashboardState returns the dashboard rows as JSON for the in-place background
// refresh (FR-40) — cookie-authenticated, so it needs no API token.
func (h *Handler) dashboardState(w http.ResponseWriter, r *http.Request) {
	cfg := h.m.ConfigSnapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.buildRows(&cfg, time.Now()))
}

// buildRows assembles the per-chargepoint dashboard view models.
func (h *Handler) buildRows(cfg *config.Config, now time.Time) []cpRow {
	rows := make([]cpRow, 0, len(cfg.Chargepoints))
	for _, id := range cfg.Chargepoints {
		st, _ := h.m.State().Get(id)
		row := makeRow(id, id == cfg.ProxiedID, st, now)
		row.Name = cfg.DisplayName(id)
		row.HasSchedule = cfg.HasSchedule(id)
		row.CanSync = cfg.ProxiedID != "" && id != cfg.ProxiedID
		switch {
		case id == cfg.ProxiedID:
			row.Mode = "proxied"
		case cfg.IsSynchronised(id):
			row.Mode = "synchronised"
		case cfg.ScheduleActive(id):
			row.Mode = "scheduled"
		default:
			row.Mode = "always_on"
		}
		if row.Mode != "proxied" && row.HasSchedule {
			row.Schedule = cfg.DeviceSchedules[id]
			switch {
			case row.Mode == "always_on":
				row.ScheduleState = "paused"
			case h.m.AllowedNow(id):
				row.ScheduleState = "open"
			default:
				row.ScheduleState = "closed"
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// setProxied handles the dashboard's proxied-CP selection (FR-32b, D-9).
func (h *Handler) setProxied(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	id := r.FormValue("proxied_id")
	if id == noneValue {
		id = ""
	}
	if err := h.m.SetProxiedID(id); err != nil {
		redirectMsg(w, r, "/admin", "error: "+err.Error())
		return
	}
	if id == "" {
		h.log.Info("proxied chargepoint cleared")
		redirectMsg(w, r, "/admin", "No chargepoint is remotely managed.")
	} else {
		h.log.Info("proxied chargepoint changed", "cp", id)
		redirectMsg(w, r, "/admin", "Now remotely managing "+id+".")
	}
}

// setMode handles the dashboard role selector: proxied / always-on / scheduled /
// synchronised (FR-43/FR-48).
func (h *Handler) setMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	id := r.FormValue("id")
	cfg := h.m.ConfigSnapshot()
	name := cfg.DisplayName(id)
	mode := r.FormValue("mode")
	msgByMode := map[string]string{
		"proxied":      "Now remotely managing " + name + ".",
		"always_on":    name + " is now always-on (local).",
		"scheduled":    name + " now follows its schedule.",
		"synchronised": name + " now mirrors the proxied chargepoint.",
	}
	msg, ok := msgByMode[mode]
	if !ok {
		http.Error(w, "unknown mode", http.StatusBadRequest)
		return
	}
	if err := h.m.SetRole(id, mode); err != nil {
		redirectMsg(w, r, "/admin", "error: "+err.Error())
		return
	}
	h.log.Info("chargepoint mode changed", "cp", id, "mode", mode)
	redirectMsg(w, r, "/admin", msg)
}

// chargepoints lists, adds and removes allow-listed CP IDs (rare task, FR-29a).
func (h *Handler) chargepoints(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		id := r.FormValue("id")
		switch r.FormValue("action") {
		case "add":
			if err := h.m.AddChargepoint(id); err != nil {
				redirectMsg(w, r, "/admin/chargepoints", "error: "+err.Error())
				return
			}
			redirectMsg(w, r, "/admin/chargepoints", "Added "+id+".")
		case "remove":
			if err := h.m.RemoveChargepoint(id); err != nil {
				redirectMsg(w, r, "/admin/chargepoints", "error: "+err.Error())
				return
			}
			redirectMsg(w, r, "/admin/chargepoints", "Removed "+id+".")
		case "alias":
			if err := h.m.SetAlias(id, r.FormValue("alias")); err != nil {
				redirectMsg(w, r, "/admin/chargepoints", "error: "+err.Error())
				return
			}
			redirectMsg(w, r, "/admin/chargepoints", "Updated name for "+id+".")
		case "assign":
			if err := h.m.AssignSchedule(id, r.FormValue("schedule")); err != nil {
				redirectMsg(w, r, "/admin/chargepoints", "error: "+err.Error())
				return
			}
			redirectMsg(w, r, "/admin/chargepoints", "Updated schedule for "+id+".")
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
		return
	}

	user, _ := h.currentUser(r)
	cfg := h.m.ConfigSnapshot()
	type cpEntry struct {
		ID       string
		Alias    string
		Schedule string
		TooLong  bool
	}
	entries := make([]cpEntry, 0, len(cfg.Chargepoints))
	for _, id := range cfg.Chargepoints {
		entries = append(entries, cpEntry{
			ID:       id,
			Alias:    cfg.Aliases[id],
			Schedule: cfg.DeviceSchedules[id],
			TooLong:  len(id) > config.MaxDownstreamIDLen,
		})
	}
	scheduleNames := make([]string, 0, len(cfg.Schedules))
	for _, s := range cfg.Schedules {
		scheduleNames = append(scheduleNames, s.Name)
	}
	h.render(w, "chargepoints", map[string]any{
		"Title":     "Chargepoints",
		"User":      user,
		"Entries":   entries,
		"Schedules": scheduleNames,
		"MaxLen":    config.MaxDownstreamIDLen,
		"Flash":     r.URL.Query().Get("msg"),
	})
}

// schedules manages central schedule definitions and the timezone (FR-41).
func (h *Handler) schedules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		switch r.FormValue("action") {
		case "add":
			s := config.Schedule{Name: r.FormValue("name"), Start: r.FormValue("start"), Stop: r.FormValue("stop")}
			if err := h.m.AddOrUpdateSchedule(s); err != nil {
				redirectMsg(w, r, "/admin/schedules", "error: "+err.Error())
				return
			}
			redirectMsg(w, r, "/admin/schedules", "Saved schedule "+s.Name+".")
		case "remove":
			if err := h.m.RemoveSchedule(r.FormValue("name")); err != nil {
				redirectMsg(w, r, "/admin/schedules", "error: "+err.Error())
				return
			}
			redirectMsg(w, r, "/admin/schedules", "Removed schedule.")
		case "timezone":
			if err := h.m.SetTimezone(r.FormValue("timezone")); err != nil {
				redirectMsg(w, r, "/admin/schedules", "error: "+err.Error())
				return
			}
			redirectMsg(w, r, "/admin/schedules", "Timezone updated.")
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
		return
	}

	user, _ := h.currentUser(r)
	cfg := h.m.ConfigSnapshot()
	now := time.Now().In(cfg.Location())
	h.render(w, "schedules", map[string]any{
		"Title":     "Schedules",
		"User":      user,
		"Schedules": cfg.Schedules,
		"Timezone":  cfg.Timezone,
		"LocalTime": now.Format("Mon 15:04 MST"),
		"Flash":     r.URL.Query().Get("msg"),
	})
}

// remote configures the upstream CSMS and BootNotification anonymisation (rare task).
func (h *Handler) remote(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		cur := h.m.ConfigSnapshot()
		// Password is write-only: a blank field keeps the stored value (FR-32a).
		password := cur.Remote.PasswordEnc
		if p := r.FormValue("password"); p != "" {
			password = p // TODO(M7/FR-35): encrypt at rest instead of storing plaintext.
		}
		remote := config.Remote{
			URL:         r.FormValue("url"),
			UpstreamID:  r.FormValue("upstream_id"),
			Username:    r.FormValue("username"),
			PasswordEnc: password,
		}
		boot := config.BootAnonymise{
			ChargePointVendor: r.FormValue("vendor"),
			ChargePointModel:  r.FormValue("model"),
			FirmwareVersion:   r.FormValue("firmware"),
			SerialNumber:      r.FormValue("serial"),
		}
		if err := h.m.SetRemote(remote); err != nil {
			redirectMsg(w, r, "/admin/remote", "error: "+err.Error())
			return
		}
		if err := h.m.SetBootAnonymise(boot); err != nil {
			redirectMsg(w, r, "/admin/remote", "error: "+err.Error())
			return
		}
		// Unchecked checkboxes submit nothing, so absence means "off".
		forceBoot := r.FormValue("force_boot") != ""
		if err := h.m.SetProxyForceBoot(forceBoot); err != nil {
			redirectMsg(w, r, "/admin/remote", "error: "+err.Error())
			return
		}
		normaliseMeter := r.FormValue("normalise_meter") != ""
		if err := h.m.SetProxyNormaliseMeter(normaliseMeter); err != nil {
			redirectMsg(w, r, "/admin/remote", "error: "+err.Error())
			return
		}
		h.log.Info("remote configuration updated", "force_boot", forceBoot, "normalise_meter", normaliseMeter)
		redirectMsg(w, r, "/admin/remote", "Remote configuration saved.")
		return
	}

	user, _ := h.currentUser(r)
	cfg := h.m.ConfigSnapshot()
	h.render(w, "remote", map[string]any{
		"Title":          "Remote server",
		"User":           user,
		"Remote":         cfg.Remote,
		"Boot":           cfg.BootAnonymise,
		"ForceBoot":      cfg.ProxyForceBoot,
		"NormaliseMeter": cfg.ProxyNormaliseMeter,
		"PasswordSet":    cfg.Remote.PasswordEnc != "",
		"Flash":          r.URL.Query().Get("msg"),
	})
}

// account changes the admin username/password and manages the API token (D-4, FR-45).
func (h *Handler) account(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		switch r.FormValue("action") {
		case "gentoken":
			if _, err := h.m.GenerateAPIToken(); err != nil {
				redirectMsg(w, r, "/admin/account", "error: "+err.Error())
				return
			}
			h.log.Info("api token generated")
			redirectMsg(w, r, "/admin/account", "New API token generated.")
			return
		case "cleartoken":
			if err := h.m.SetAPIToken(""); err != nil {
				redirectMsg(w, r, "/admin/account", "error: "+err.Error())
				return
			}
			redirectMsg(w, r, "/admin/account", "API token cleared (API disabled).")
			return
		default: // password change
			username := r.FormValue("username")
			password := r.FormValue("password")
			if len(password) < minPasswordLn {
				redirectMsg(w, r, "/admin/account", "error: password must be at least 8 characters")
				return
			}
			if password != r.FormValue("confirm") {
				redirectMsg(w, r, "/admin/account", "error: passwords do not match")
				return
			}
			hash, err := hashPassword(password)
			if err != nil {
				http.Error(w, "hash error", http.StatusInternalServerError)
				return
			}
			if username == "" {
				username = h.m.ConfigSnapshot().Admin.Username
			}
			if err := h.m.SetAdminCredentials(username, hash); err != nil {
				http.Error(w, "save error", http.StatusInternalServerError)
				return
			}
			h.log.Info("admin credentials changed", "username", username)
			redirectMsg(w, r, "/admin/account", "Admin credentials updated.")
			return
		}
	}

	user, _ := h.currentUser(r)
	cfg := h.m.ConfigSnapshot()
	h.render(w, "account", map[string]any{
		"Title":    "Admin account",
		"User":     user,
		"Username": cfg.Admin.Username,
		"APIToken": cfg.APIToken,
		"Flash":    r.URL.Query().Get("msg"),
	})
}

func redirectMsg(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, path+"?msg="+urlEscape(msg), http.StatusSeeOther)
}
