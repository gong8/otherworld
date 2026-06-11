// Package budget meters token spend so the world can rest instead of
// overspending. The Meter is a fixed hourly window with a reset-on-rollover
// counter, NOT a sliding window: the window starts at the first Add/Allow and
// resets on the first call past the hour. The trade-off is documented honesty
// over precision — across a window boundary the world may spend up to 2× the
// hourly limit inside one rolling 60-minute span (the tail of one window plus
// the head of the next). That is acceptable because the meter is a tripwire
// against runaway think loops, not an invoice; the simplicity buys an
// obviously-correct, race-free implementation.
package budget

import (
	"sync"
	"time"
)

// Meter is a thread-safe hourly token meter. The zero limit means unlimited.
type Meter struct {
	mu          sync.Mutex
	limit       int // tokens per hour; 0 = unlimited
	used        int
	windowStart time.Time

	// Now is the injected clock, consulted under the mutex on every Add and
	// Allow. It defaults to time.Now; tests may replace it after New and
	// before first use — never concurrently with Add/Allow.
	Now func() time.Time
}

// New builds a Meter with the given hourly token limit. 0 means unlimited:
// Allow always reports true and Add only counts.
func New(limitPerHour int) *Meter {
	return &Meter{limit: limitPerHour, Now: time.Now}
}

// roll resets the counter when the current window has lapsed. Lock held.
// The zero windowStart (first ever call) reads as lapsed, which starts the
// first window — no special-casing needed.
func (m *Meter) roll() {
	if now := m.Now(); now.Sub(m.windowStart) >= time.Hour {
		m.windowStart, m.used = now, 0
	}
}

// Add records in+out tokens against the current window.
func (m *Meter) Add(in, out int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roll()
	m.used += in + out
}

// Allow reports whether the budget still has room this hour. A limit of 0
// always allows.
func (m *Meter) Allow() bool {
	if m.limit <= 0 {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roll()
	return m.used < m.limit
}

// Resting is !Allow — the same answer, named for callers asking about the
// world's state rather than permission to spend.
func (m *Meter) Resting() bool { return !m.Allow() }
