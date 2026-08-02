// Package clock provides a small time abstraction so timers (e.g. the connection
// liveness watchdog, FR-24) can be tested deterministically without real sleeps.
package clock

import (
	"sync"
	"time"
)

// Timer is the subset of *time.Timer the proxy uses.
type Timer interface {
	Stop() bool
	Reset(d time.Duration) bool
}

// Clock is a source of time and timers.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Real returns a Clock backed by the standard library.
func Real() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

// Fake is a manually-advanced Clock for tests.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	nextID int
	timers map[int]*fakeTimer
}

// NewFake returns a Fake clock starting at t.
func NewFake(t time.Time) *Fake {
	return &Fake{now: t, timers: make(map[int]*fakeTimer)}
}

// Now returns the current fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// AfterFunc schedules f to run once the fake clock advances past d from now.
func (f *Fake) AfterFunc(d time.Duration, fn func()) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	t := &fakeTimer{clock: f, id: f.nextID, when: f.now.Add(d), fn: fn}
	f.timers[t.id] = t
	return t
}

// Advance moves the clock forward by d, firing any timers now due. Fired callbacks
// run synchronously (outside the lock) in unspecified order.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	var due []*fakeTimer
	for id, t := range f.timers {
		if !t.when.After(f.now) {
			due = append(due, t)
			delete(f.timers, id)
		}
	}
	f.mu.Unlock()
	for _, t := range due {
		t.fn()
	}
}

type fakeTimer struct {
	clock *Fake
	id    int
	when  time.Time
	fn    func()
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	_, active := t.clock.timers[t.id]
	delete(t.clock.timers, t.id)
	return active
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	_, active := t.clock.timers[t.id]
	t.when = t.clock.now.Add(d)
	t.clock.timers[t.id] = t
	return active
}
