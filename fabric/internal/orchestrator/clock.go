package orchestrator

import "time"

type Clock interface {
	Now() time.Time
	// Schedule fires fn after d. Returns a cancel func.
	Schedule(d time.Duration, fn func()) (cancel func())
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }
func (RealClock) Schedule(d time.Duration, fn func()) func() {
	t := time.AfterFunc(d, fn)
	return func() { t.Stop() }
}

// FakeClock for tests: time advances only when told to.
type FakeClock struct {
	now    time.Time
	timers []*fakeTimer
}
type fakeTimer struct {
	at  time.Time
	fn  func()
	off bool
}

func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{now: start} }
func (c *FakeClock) Now() time.Time           { return c.now }
func (c *FakeClock) Schedule(d time.Duration, fn func()) func() {
	t := &fakeTimer{at: c.now.Add(d), fn: fn}
	c.timers = append(c.timers, t)
	return func() { t.off = true }
}

// Advance moves time forward, firing due timers in order.
func (c *FakeClock) Advance(d time.Duration) {
	target := c.now.Add(d)
	for {
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.off && !t.at.After(target) && (next == nil || t.at.Before(next.at)) {
				next = t
			}
		}
		if next == nil {
			break
		}
		c.now = next.at
		next.off = true
		next.fn()
	}
	c.now = target
}
