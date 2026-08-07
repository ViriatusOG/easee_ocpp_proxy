package admin

import (
	"bytes"
	"strings"
	"testing"
)

// Executes the dashboard template (parsing alone doesn't catch bad struct-field
// references) and checks the synchronised option is offered only where CanSync is set.
func TestDashboardTemplateSynchronisedOption(t *testing.T) {
	tmpl := buildTemplates()["dashboard"]
	rows := []cpRow{
		{ID: "CP1", Name: "CP1", Mode: "proxied", Proxied: true},
		{ID: "CP2", Name: "CP2", Mode: "synchronised", CanSync: true},
		{ID: "CP3", Name: "CP3", Mode: "always_off"}, // no proxied peer offered here
	}
	data := map[string]any{
		"Title":        "Dashboard",
		"User":         "admin",
		"Rows":         rows,
		"Chargepoints": []string{"CP1", "CP2", "CP3"},
		"ProxiedID":    "CP1",
		"Warnings":     []string(nil),
		"Flash":        "",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	html := buf.String()

	if c := strings.Count(html, `value="synchronised"`); c != 1 {
		t.Errorf("expected exactly one synchronised option (CP2), found %d", c)
	}
	if !strings.Contains(html, `<option value="synchronised" selected>synchronised</option>`) {
		t.Errorf("CP2's synchronised option should be selected:\n%s", html)
	}
	// always_off is offered on every row, and selected for CP3.
	if c := strings.Count(html, `value="always_off"`); c != 3 {
		t.Errorf("expected an always_off option on all 3 rows, found %d", c)
	}
	if !strings.Contains(html, `<option value="always_off" selected>always off</option>`) {
		t.Errorf("CP3's always_off option should be selected:\n%s", html)
	}
}
