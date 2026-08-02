package schedule

import (
	"testing"
	"time"
)

func at(h, m int) time.Time {
	return time.Date(2026, 8, 2, h, m, 0, 0, time.UTC)
}

func TestDaytimeWindow(t *testing.T) {
	w, err := Parse("09:00", "17:00")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[time.Time]bool{
		at(8, 59):  false,
		at(9, 0):   true,
		at(12, 0):  true,
		at(16, 59): true,
		at(17, 0):  false,
		at(23, 0):  false,
	}
	for tm, want := range cases {
		if got := w.Allowed(tm); got != want {
			t.Errorf("Allowed(%s) = %v, want %v", tm.Format("15:04"), got, want)
		}
	}
}

func TestOvernightWindow(t *testing.T) {
	w, err := Parse("23:30", "05:30")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[time.Time]bool{
		at(23, 29): false,
		at(23, 30): true,
		at(0, 0):   true,
		at(5, 29):  true,
		at(5, 30):  false,
		at(12, 0):  false,
	}
	for tm, want := range cases {
		if got := w.Allowed(tm); got != want {
			t.Errorf("Allowed(%s) = %v, want %v", tm.Format("15:04"), got, want)
		}
	}
}

func TestEqualStartStopIsAlwaysAllowed(t *testing.T) {
	w, _ := Parse("00:00", "00:00")
	if !w.Allowed(at(3, 0)) || !w.Allowed(at(15, 0)) {
		t.Error("equal start/stop should be always allowed")
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"", "9", "24:00", "12:60", "aa:bb", "12-30"} {
		if _, err := ParseHHMM(bad); err == nil {
			t.Errorf("ParseHHMM(%q) expected error", bad)
		}
	}
}
