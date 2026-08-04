package admin

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
)

// The Remote page must render the force-boot checkbox reflecting the stored value,
// and this also exercises template.Must parsing of the page (not otherwise hit by tests).
func TestRemoteTemplateForceBootCheckbox(t *testing.T) {
	tmpl := buildTemplates()["remote"]
	for _, tc := range []struct {
		on         bool
		wantChecked bool
	}{{true, true}, {false, false}} {
		var buf bytes.Buffer
		data := map[string]any{
			"Title":       "Remote server",
			"User":        "admin",
			"Remote":      config.Remote{},
			"Boot":        config.BootAnonymise{},
			"ForceBoot":   tc.on,
			"PasswordSet": false,
		}
		if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
			t.Fatalf("render remote (force=%v): %v", tc.on, err)
		}
		html := buf.String()
		if !strings.Contains(html, `name="force_boot"`) {
			t.Fatalf("force_boot checkbox missing from remote page")
		}
		checked := strings.Contains(html, `name="force_boot" type="checkbox" value="1" checked`)
		if checked != tc.wantChecked {
			t.Errorf("ForceBoot=%v: checkbox checked=%v, want %v", tc.on, checked, tc.wantChecked)
		}
	}
}
