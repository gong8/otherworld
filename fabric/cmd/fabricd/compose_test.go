package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"otherworld/fabric/internal/budget"
)

// recordLogs swaps the slog default for a recorder for the test's duration
// and returns the captured messages.
func recordLogs(t *testing.T) *recordedLogs {
	t.Helper()
	r := &recordedLogs{}
	prev := slog.Default()
	slog.SetDefault(slog.New(r))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return r
}

type recordedLogs struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recordedLogs) Enabled(context.Context, slog.Level) bool { return true }
func (r *recordedLogs) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, rec.Message)
	return nil
}
func (r *recordedLogs) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *recordedLogs) WithGroup(string) slog.Handler      { return r }

func (r *recordedLogs) count(msg string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, m := range r.msgs {
		if strings.Contains(m, msg) {
			n++
		}
	}
	return n
}

// restLogger logs each transition exactly once: crossing the budget logs
// "the world is resting" once no matter how many calls observe the resting
// state; rolling into a fresh hour logs "the world wakes" once.
func TestRestLoggerLogsOncePerTransition(t *testing.T) {
	logs := recordLogs(t)
	clock := time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC)
	m := budget.New(10)
	m.Now = func() time.Time { return clock }
	r := &restLogger{m: m}

	if !r.Allow() {
		t.Fatal("a fresh meter must allow")
	}
	r.Add(20, 0) // crosses the budget: the world rests
	if r.Allow() {
		t.Fatal("over budget must rest")
	}
	r.Add(1, 0) // still resting: no second log
	_ = r.Allow()
	if got := logs.count("the world is resting"); got != 1 {
		t.Fatalf("resting must log exactly once per transition, got %d", got)
	}
	if got := logs.count("the world wakes"); got != 0 {
		t.Fatalf("no wake yet, got %d wake logs", got)
	}

	clock = clock.Add(time.Hour) // the window rolls: the world wakes
	if !r.Allow() {
		t.Fatal("the new window must allow")
	}
	_ = r.Allow()
	if got := logs.count("the world wakes"); got != 1 {
		t.Fatalf("waking must log exactly once per transition, got %d", got)
	}
}

// stateView exposes "resting": true only when the hook reports it; a nil
// hook (fake brains) and a false hook leave the key absent — mirroring the
// degraded pattern.
func TestStateViewExposesResting(t *testing.T) {
	s := &server{
		scopes:     map[string]*scopeState{"scope:test": {}},
		marks:      map[string]map[string]int{},
		storeFails: map[string]int{},
	}

	view := func() map[string]any { return s.stateView("scope:test").(map[string]any) }

	if _, ok := view()["resting"]; ok {
		t.Fatal("nil hook (fake brains): resting must be absent")
	}
	s.resting = func() bool { return false }
	if _, ok := view()["resting"]; ok {
		t.Fatal("awake world: resting must be absent")
	}
	s.resting = func() bool { return true }
	if got, ok := view()["resting"]; !ok || got != true {
		t.Fatalf("resting world: want resting=true, got %v (present %v)", got, ok)
	}
}
