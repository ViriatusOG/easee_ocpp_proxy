// Package schedule evaluates daily charging windows in local wall-clock time.
//
// A window is defined by a start and stop time (HH:MM). It is evaluated against a
// time already converted to the desired location, so daylight-saving transitions are
// handled by the caller's *time.Location. A window may wrap past midnight (start >
// stop, e.g. 23:30–05:30).
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Window is a daily charging window in minutes-of-day (local wall clock).
type Window struct {
	StartMin int
	StopMin  int
}

// ParseHHMM parses "HH:MM" into minutes since midnight.
func ParseHHMM(s string) (int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time %q (want HH:MM)", s)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour in %q", s)
	}
	m, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minute in %q", s)
	}
	return h*60 + m, nil
}

// Parse builds a Window from start/stop "HH:MM" strings.
func Parse(start, stop string) (Window, error) {
	s, err := ParseHHMM(start)
	if err != nil {
		return Window{}, err
	}
	e, err := ParseHHMM(stop)
	if err != nil {
		return Window{}, err
	}
	return Window{StartMin: s, StopMin: e}, nil
}

// Allowed reports whether t (already in the desired location) is inside the window.
// A window with equal start and stop is treated as always allowed (a full day).
func (w Window) Allowed(t time.Time) bool {
	if w.StartMin == w.StopMin {
		return true
	}
	m := t.Hour()*60 + t.Minute()
	if w.StartMin < w.StopMin {
		return m >= w.StartMin && m < w.StopMin
	}
	// wraps past midnight
	return m >= w.StartMin || m < w.StopMin
}
