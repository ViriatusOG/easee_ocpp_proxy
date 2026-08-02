package session

import (
	"testing"
	"time"

	"github.com/ipeel/easee-ocpp-proxy/internal/clock"
)

func TestWatchdogFiresAfterInterval(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var fired bool
	wd := newWatchdog(fc, 20*time.Second, func() { fired = true })
	defer wd.stop()

	fc.Advance(19 * time.Second)
	if fired {
		t.Fatal("watchdog fired early")
	}
	fc.Advance(1 * time.Second)
	if !fired {
		t.Fatal("watchdog did not fire at the interval")
	}
}

func TestWatchdogResetOnKick(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var fired bool
	wd := newWatchdog(fc, 20*time.Second, func() { fired = true })
	defer wd.stop()

	fc.Advance(19 * time.Second)
	wd.kick() // resets the 20s window from t=19s
	fc.Advance(19 * time.Second)
	if fired {
		t.Fatal("watchdog fired despite being kicked")
	}
	fc.Advance(1 * time.Second) // now at t=39s = kick@19s + 20s
	if !fired {
		t.Fatal("watchdog did not fire after the reset window elapsed")
	}
}

func TestWatchdogSetInterval(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var fired bool
	wd := newWatchdog(fc, 20*time.Second, func() { fired = true })
	defer wd.stop()

	// Tighten to 6s (as if the CSMS advertised interval=3, so 2×3).
	wd.setInterval(6 * time.Second)
	fc.Advance(5 * time.Second)
	if fired {
		t.Fatal("fired before the tightened interval")
	}
	fc.Advance(1 * time.Second)
	if !fired {
		t.Fatal("did not fire at the tightened interval")
	}
}

func TestWatchdogStopPreventsFiring(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var fired bool
	wd := newWatchdog(fc, 20*time.Second, func() { fired = true })
	wd.stop()

	fc.Advance(60 * time.Second)
	if fired {
		t.Fatal("stopped watchdog should never fire")
	}
}
