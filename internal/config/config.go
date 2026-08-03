// Package config defines the proxy configuration, loaded from and persisted to a
// YAML file. Admin changes are written back atomically (FR-33, FR-34).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// MaxDownstreamIDLen is Easee's limit on chargepoint IDs (FR-5a). Longer upstream
// IDs are supported via the short→long mapping (D-7).
const MaxDownstreamIDLen = 25

// Remote is the upstream CSMS connection (used only when a CP is proxied).
type Remote struct {
	URL         string `yaml:"url"`          // base: scheme + host [+ optional path prefix] (D-8)
	UpstreamID  string `yaml:"upstream_id"`  // appended as the final path segment; may exceed 25 chars (D-7)
	Username    string `yaml:"username"`     // HTTP Basic auth, Profile 2 (D-3)
	PasswordEnc string `yaml:"password_enc"` // stored obfuscated/encrypted at rest (FR-35)
}

// BootAnonymise holds the values that replace the Easee's real manufacturer fields
// in BootNotification before it is forwarded upstream (D-6, FR-21a). Empty firmware
// or serial means "pass the real value through".
type BootAnonymise struct {
	ChargePointVendor string `yaml:"charge_point_vendor"`
	ChargePointModel  string `yaml:"charge_point_model"`
	FirmwareVersion   string `yaml:"firmware_version,omitempty"`
	SerialNumber      string `yaml:"serial_number,omitempty"`
}

// Admin holds credentials for the /admin UI (D-4, FR-31).
type Admin struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"` // bcrypt
}

// Schedule is a named daily charging window (local wall-clock time, DST-aware),
// defined centrally and applied per device (FR-41).
type Schedule struct {
	Name  string `yaml:"name"`
	Start string `yaml:"start"` // "HH:MM"
	Stop  string `yaml:"stop"`  // "HH:MM"; may be earlier than Start (wraps midnight)
}

// LocalAutoStart controls whether the local Central System proactively sends a
// RemoteStartTransaction when an auto-authorised chargepoint reports a connector as
// "Preparing" but does not self-start (the active side of Q8). Needed for chargers
// that wait for backend authorisation before charging.
type LocalAutoStart struct {
	Enabled bool   `yaml:"enabled"`
	IDTag   string `yaml:"id_tag"`
}

// Config is the full proxy configuration.
type Config struct {
	ListenAddr         string        `yaml:"listen_addr"`          // one port for ws:// OCPP and http:// /admin (D-2)
	HeartbeatIntervalS int           `yaml:"heartbeat_interval_s"` // advertised to local CPs; liveness = 2× (FR-24)
	UpstreamTimeoutS   int           `yaml:"upstream_timeout_s"`   // initial upstream dial timeout (FR-17)
	Chargepoints       []string      `yaml:"chargepoints"`         // allow-list of downstream Easee CP IDs (FR-3)
	ProxiedID          string        `yaml:"proxied_id"`           // the single remotely-managed CP, or "" for none (FR-6/FR-7)
	Aliases            map[string]string `yaml:"aliases"`          // CP ID → friendly name, e.g. "Garage Left" (FR-42)
	Timezone           string            `yaml:"timezone"`         // IANA zone for schedules; "" = server local, DST-aware (FR-41)
	Schedules          []Schedule        `yaml:"schedules"`        // central schedule definitions (FR-41)
	DeviceSchedules    map[string]string `yaml:"device_schedules"` // CP ID → schedule name (FR-41)
	SchedulePaused     map[string]bool   `yaml:"schedule_paused"`  // CP IDs whose assigned schedule is paused (always-on) (FR-43)
	Remote             Remote            `yaml:"remote"`
	BootAnonymise      BootAnonymise     `yaml:"boot_anonymise"`
	LocalAutoStart     LocalAutoStart    `yaml:"local_auto_start"`
	Admin              Admin             `yaml:"admin"`

	path string // source path, remembered for Save()
}

// Default returns a Config pre-populated with sensible defaults. Fields present in
// the YAML file override these.
func Default() Config {
	return Config{
		ListenAddr:         ":9000",
		HeartbeatIntervalS: 300,
		UpstreamTimeoutS:   30,
		LocalAutoStart:     LocalAutoStart{Enabled: true, IDTag: "PROXY"},
	}
}

// AutoStartIDTag returns the idTag used for proxy-initiated RemoteStartTransaction.
func (c *Config) AutoStartIDTag() string {
	if c.LocalAutoStart.IDTag != "" {
		return c.LocalAutoStart.IDTag
	}
	return "PROXY"
}

// DisplayName returns the CP's alias if set, otherwise its ID (FR-42).
func (c *Config) DisplayName(id string) string {
	if a := c.Aliases[id]; a != "" {
		return a
	}
	return id
}

// Location resolves the timezone for schedule evaluation; server local if unset or
// invalid (DST-aware via the OS zone).
func (c *Config) Location() *time.Location {
	if c.Timezone == "" {
		return time.Local
	}
	if loc, err := time.LoadLocation(c.Timezone); err == nil {
		return loc
	}
	return time.Local
}

// HasSchedule reports whether a schedule is assigned to the CP.
func (c *Config) HasSchedule(id string) bool {
	return c.DeviceSchedules[id] != ""
}

// ScheduleActive reports whether the CP has an assigned schedule that is currently
// enforced (assigned and not paused / "always-on") (FR-43).
func (c *Config) ScheduleActive(id string) bool {
	return c.DeviceSchedules[id] != "" && !c.SchedulePaused[id]
}

// FindSchedule returns the named schedule.
func (c *Config) FindSchedule(name string) (Schedule, bool) {
	for _, s := range c.Schedules {
		if s.Name == name {
			return s, true
		}
	}
	return Schedule{}, false
}

// Load reads, parses and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.path = path
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate returns a fatal error for configuration that cannot be run.
func (c *Config) Validate() error {
	if c.ListenAddr == "" {
		return errors.New("listen_addr must be set")
	}
	if c.HeartbeatIntervalS <= 0 {
		return errors.New("heartbeat_interval_s must be > 0")
	}
	if c.UpstreamTimeoutS <= 0 {
		return errors.New("upstream_timeout_s must be > 0")
	}
	if c.ProxiedID != "" && !c.HasChargepoint(c.ProxiedID) {
		return fmt.Errorf("proxied_id %q is not in the chargepoints allow-list", c.ProxiedID)
	}
	return nil
}

// Warnings returns non-fatal configuration issues worth surfacing at startup or in
// the admin UI (e.g. a CP ID longer than Easee accepts — FR-5a).
func (c *Config) Warnings() []string {
	var w []string
	for _, id := range c.Chargepoints {
		if len(id) > MaxDownstreamIDLen {
			w = append(w, fmt.Sprintf("chargepoint id %q is %d chars; Easee accepts at most %d", id, len(id), MaxDownstreamIDLen))
		}
	}
	if c.ProxiedID != "" && (c.Remote.URL == "" || c.Remote.UpstreamID == "") {
		w = append(w, "proxied_id is set but remote.url/upstream_id is incomplete; proxying will fail until configured")
	}
	return w
}

// HasChargepoint reports whether id is in the allow-list.
func (c *Config) HasChargepoint(id string) bool {
	for _, x := range c.Chargepoints {
		if x == id {
			return true
		}
	}
	return false
}

// HeartbeatInterval is the configured local-CP heartbeat interval.
func (c *Config) HeartbeatInterval() time.Duration {
	return time.Duration(c.HeartbeatIntervalS) * time.Second
}

// UpstreamTimeout is the initial upstream dial timeout.
func (c *Config) UpstreamTimeout() time.Duration {
	return time.Duration(c.UpstreamTimeoutS) * time.Second
}

// Path returns the source path this config was loaded from.
func (c *Config) Path() string { return c.path }

// Save atomically writes the config back to its source path (FR-34).
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("config has no source path to save to")
	}
	return c.SaveAs(c.path)
}

// SaveAs atomically writes the config to path: write a temp file in the same
// directory, fsync, then rename over the target. os.Rename replaces an existing
// destination on both Unix and Windows.
func (c *Config) SaveAs(path string) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".easee-proxy-*.yaml.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
