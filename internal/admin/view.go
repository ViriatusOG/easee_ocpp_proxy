package admin

import (
	"fmt"
	"net/url"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ipeel/easee-ocpp-proxy/internal/state"
)

// noneValue is the radio value representing "no chargepoint is proxied".
const noneValue = "__none__"

// cpRow is the per-chargepoint view model rendered on the dashboard.
type cpRow struct {
	ID            string
	Name          string // alias or ID (FR-42)
	Role          string // "proxied" | "local"
	Proxied       bool
	Online        bool
	ShowUpstream  bool
	UpstreamUp    bool
	Status        string // connector status
	Session       string
	Energy        string
	Power         string
	LastHeartbeat string
	LastMessage   string
	Schedule      string // assigned schedule name, or ""
	WindowState   string // "open" | "closed" | "" (no schedule)
}

func makeRow(id string, proxied bool, st state.CP, now time.Time) cpRow {
	role := "local"
	if proxied {
		role = "proxied"
	}
	r := cpRow{
		ID:            id,
		Role:          role,
		Proxied:       proxied,
		Online:        st.DownstreamUp,
		ShowUpstream:  proxied,
		UpstreamUp:    st.UpstreamUp,
		Status:        dash(st.ConnectorStatus),
		LastHeartbeat: fmtAge(now, st.LastHeartbeat),
		LastMessage:   fmtAge(now, st.LastMessage),
		Power:         fmtKW(st.PowerW),
	}
	switch {
	case st.TxnActive:
		r.Session = fmt.Sprintf("active #%d", st.TxnID)
		r.Energy = fmtKWh(st.EnergyWh)
	case st.LastStopReason != "":
		r.Session = "idle (last: " + st.LastStopReason + ")"
		r.Energy = fmtKWh(st.EnergyWh)
	default:
		r.Session = "idle"
		r.Energy = dash("")
	}
	return r
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func fmtKWh(wh float64) string { return fmt.Sprintf("%.3f kWh", wh/1000) }

func fmtKW(w float64) string { return fmt.Sprintf("%.2f kW", w/1000) }

func fmtAge(now, t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

func hashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func urlEscape(s string) string { return url.QueryEscape(s) }
