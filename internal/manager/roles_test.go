package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/state"
)

func newManagerMulti(t *testing.T) *Manager {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	body := `listen_addr: ":9000"
heartbeat_interval_s: 300
upstream_timeout_s: 30
chargepoints: [CP1, CP2, CP3]
timezone: UTC
schedules:
  - {name: night, start: "23:30", stop: "05:30"}
device_schedules: {CP1: night}
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg)
}

// Synchronised mode is only selectable while another CP is proxied (FR-48).
func TestSynchronisedRequiresProxied(t *testing.T) {
	m := newManagerMulti(t)
	if err := m.SetRole("CP2", "synchronised"); err == nil {
		t.Fatal("synchronised should be rejected when nothing is proxied")
	}
	if err := m.SetRole("CP1", "proxied"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRole("CP2", "synchronised"); err != nil {
		t.Fatalf("synchronised should be allowed once CP1 is proxied: %v", err)
	}
	if m.RoleName("CP2") != "synchronised" || !m.IsSynchronised("CP2") {
		t.Fatalf("CP2 role = %q, want synchronised", m.RoleName("CP2"))
	}
}

// A unit displaced from proxied resumes the role it held before (FR-48), including
// synchronised.
func TestProxiedDisplacementResumesSavedRole(t *testing.T) {
	m := newManagerMulti(t)

	// CP1 starts scheduled (schedule assigned). Make it proxied, then let CP2 take over.
	if err := m.SetRole("CP1", "proxied"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRole("CP2", "proxied"); err != nil {
		t.Fatal(err)
	}
	if got := m.RoleName("CP1"); got != "scheduled" {
		t.Fatalf("displaced CP1 role = %q, want scheduled (its prior role)", got)
	}
	if m.RoleName("CP2") != "proxied" {
		t.Fatal("CP2 should be proxied")
	}

	// Now make CP2 synchronised-capable: CP2 is proxied; set CP3 synchronised, then make
	// CP3 proxied so CP2 is displaced and should resume... CP2's saved role was scheduled?
	// No — CP2 was scheduled by default. Instead verify the synchronised-resume path:
	// CP1 -> synchronised, then CP1 -> proxied (saves "synchronised"), displaced by CP2.
	if err := m.SetRole("CP1", "synchronised"); err != nil { // CP2 is proxied
		t.Fatal(err)
	}
	if err := m.SetRole("CP1", "proxied"); err != nil { // saves CP1 prev = synchronised
		t.Fatal(err)
	}
	if err := m.SetRole("CP2", "proxied"); err != nil { // displaces CP1
		t.Fatal(err)
	}
	if got := m.RoleName("CP1"); got != "synchronised" {
		t.Fatalf("displaced CP1 role = %q, want synchronised (resumed)", got)
	}
}

// When the last proxied unit goes local, synchronised units fall back to their saved
// role (FR-48).
func TestSynchronisedDemotedWhenNothingProxied(t *testing.T) {
	m := newManagerMulti(t)
	if err := m.SetRole("CP1", "proxied"); err != nil {
		t.Fatal(err)
	}
	// CP2 is always_on by default; becomes synchronised (saves always_on).
	if err := m.SetRole("CP2", "synchronised"); err != nil {
		t.Fatal(err)
	}
	// Take CP1 out of proxied with nothing else proxied.
	if err := m.SetRole("CP1", "always_on"); err != nil {
		t.Fatal(err)
	}
	if m.ConfigSnapshot().ProxiedID != "" {
		t.Fatal("nothing should be proxied")
	}
	if m.IsSynchronised("CP2") {
		t.Fatal("CP2 should have been demoted out of synchronised")
	}
	if got := m.RoleName("CP2"); got != "always_on" {
		t.Fatalf("CP2 fell back to %q, want always_on (its saved role)", got)
	}
}

// ProxiedCharging tracks the proxied CP's live transaction state (FR-48).
func TestProxiedCharging(t *testing.T) {
	m := newManagerMulti(t)
	if m.ProxiedCharging() {
		t.Fatal("nothing proxied → not charging")
	}
	if err := m.SetRole("CP1", "proxied"); err != nil {
		t.Fatal(err)
	}
	if m.ProxiedCharging() {
		t.Fatal("proxied but idle → not charging")
	}
	m.State().Update("CP1", func(cp *state.CP) { cp.DownstreamUp = true; cp.TxnActive = true })
	if !m.ProxiedCharging() {
		t.Fatal("proxied with active txn → charging")
	}
	// A dropped session (DownstreamUp false) must not read as charging.
	m.State().Update("CP1", func(cp *state.CP) { cp.DownstreamUp = false })
	if m.ProxiedCharging() {
		t.Fatal("proxied session down → not charging even if TxnActive lingered")
	}
}
