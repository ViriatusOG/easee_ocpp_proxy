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
		{ID: "CP3", Name: "CP3", Mode: "always_on"}, // no proxied peer offered here
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
}
