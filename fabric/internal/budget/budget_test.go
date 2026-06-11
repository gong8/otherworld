package budget

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a settable clock for the window tests.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newTestMeter(limit int) (*Meter, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC)}
	m := New(limit)
	m.Now = func() time.Time { return clock.now }
	return m, clock
}

func TestUnderLimitAllows(t *testing.T) {
	m, _ := newTestMeter(100)
	if !m.Allow() {
		t.Fatal("a fresh meter must allow")
	}
	m.Add(40, 9) // 49 used
	if !m.Allow() {
		t.Fatal("49 of 100 must allow")
	}
	if m.Resting() {
		t.Fatal("Resting must be !Allow")
	}
}

func TestOverLimitRests(t *testing.T) {
	m, _ := newTestMeter(100)
	m.Add(60, 40) // exactly the limit: spent means spent
	if m.Allow() {
		t.Fatal("100 of 100 must rest")
	}
	if !m.Resting() {
		t.Fatal("Resting must report true at the limit")
	}
	m.Add(1, 0)
	if m.Allow() {
		t.Fatal("past the limit must still rest")
	}
}

func TestHourRolloverResets(t *testing.T) {
	m, clock := newTestMeter(100)
	m.Add(200, 0)
	if m.Allow() {
		t.Fatal("over limit must rest")
	}
	clock.Advance(59 * time.Minute)
	if m.Allow() {
		t.Fatal("59 minutes in, the window has not lapsed: still resting")
	}
	clock.Advance(time.Minute) // the full hour from window start
	if !m.Allow() {
		t.Fatal("the first call past the hour must reset the window")
	}
	// The counter actually reset: room for a fresh hour of spend.
	m.Add(99, 0)
	if !m.Allow() {
		t.Fatal("99 of 100 in the new window must allow")
	}
}

func TestWindowStartsAtFirstUse(t *testing.T) {
	m, clock := newTestMeter(100)
	// New does not start the window; the first call does.
	clock.Advance(5 * time.Hour)
	m.Add(100, 0)
	if m.Allow() {
		t.Fatal("the limit was spent inside the window that began at first use")
	}
	clock.Advance(59 * time.Minute)
	if m.Allow() {
		t.Fatal("window must be anchored at first use, not at New")
	}
}

func TestZeroLimitAlwaysAllows(t *testing.T) {
	m, _ := newTestMeter(0)
	m.Add(1<<30, 1<<30)
	if !m.Allow() {
		t.Fatal("0 = unlimited: Allow must always report true")
	}
	if m.Resting() {
		t.Fatal("an unlimited meter never rests")
	}
}

// Concurrent Add/Allow must be race-clean (run with -race) and lose no tokens.
func TestConcurrentAddAllow(t *testing.T) {
	m := New(0) // real clock; a test run never spans an hour
	const goroutines, iters = 8, 500
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iters {
				m.Add(1, 1)
				_ = m.Allow()
				_ = m.Resting()
			}
		}()
	}
	wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if want := goroutines * iters * 2; m.used != want {
		t.Fatalf("used = %d, want %d (lost tokens under concurrency)", m.used, want)
	}
}
