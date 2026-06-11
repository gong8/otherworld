package orchestrator_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/orchestrator"
	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/world"
)

// Two rapid triggers to one voice collapse into ONE think, and the think
// sees the latest trigger.
func TestDebounceReplacesPendingThink(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	var thinks int
	var lastBody string
	echo := charter("voice:echo", "the household", protocol.VoiceThing, []string{"lamp.set"}, true)
	o.AddVoice(ctx, echo, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindSay },
		Respond: func(v brain.VoiceView) brain.Action {
			thinks++
			lastBody = v.Trigger.Body
			return brain.Action{} // zero value is silence
		},
	}}), map[string]any{"lamp": "off"})

	o.PrincipalSays(ctx, "voice:echo", "first")
	clock.Advance(500 * time.Millisecond) // inside the 2s debounce window
	o.PrincipalSays(ctx, "voice:echo", "second")
	clock.Advance(10 * time.Second)

	if thinks != 1 {
		t.Fatalf("debounce must collapse rapid triggers into one think, got %d", thinks)
	}
	if lastBody != "second" {
		t.Fatalf("the surviving think must see the latest trigger, got %q", lastBody)
	}
	if got := kinds(*log); got != "say>say" {
		t.Fatalf("only the two says belong in the record, got %s", got)
	}
}

// ask_principal routes only to the named principal: even a bystander named
// in To must not be woken by it.
func TestAskPrincipalRoutesOnlyToPrincipal(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	var thinks int
	bystander := charter("voice:bystander", "the household", protocol.VoiceThing, []string{"lamp.set"}, true)
	o.AddVoice(ctx, bystander, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return true },
		Respond: func(v brain.VoiceView) brain.Action {
			thinks++
			return brain.Action{Speak: true, Kind: protocol.KindSay, Body: "me too"}
		},
	}}), map[string]any{"lamp": "off"})

	o.Inject(ctx, protocol.Envelope{
		From: "voice:her-agent", Serves: "her", Scope: o.ScopeID(),
		Kind: protocol.KindAskPrincipal,
		To:   []string{"voice:principal:her", "voice:bystander"},
		Body: "may i spend 5 marks?",
	})
	clock.Advance(time.Minute)

	if thinks != 0 {
		t.Fatalf("ask_principal must route only to the principal, bystander thought %d times", thinks)
	}
	if got := kinds(*log); got != "ask_principal" {
		t.Fatalf("ask_principal must still reach the record, got %s", got)
	}
}

// A counter-offer propose inherits the exchange, replaces the pending
// propose, and a later accept settles on the LATEST terms.
func TestCounterOfferSettlesOnLatestTerms(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	agent := charter("voice:her-agent", "her", protocol.VoicePerson, []string{"temperature.set"}, true)
	heating := charter("voice:heating", "the household", protocol.VoiceThing, []string{"temperature.set"}, true)

	// The agent accepts whatever heating counters with.
	o.AddVoice(ctx, agent, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool {
			return v.Trigger.Kind == protocol.KindPropose && v.Trigger.From == "voice:heating"
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindAccept, To: []string{v.Trigger.From},
				Body: "fine.", Terms: v.Trigger.Terms}
		},
	}}), nil)
	// Heating counters any propose with 20.
	o.AddVoice(ctx, heating, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindPropose },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindPropose, To: []string{v.Trigger.From},
				Body:  "meet me at 20.",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`20`)}}
		},
	}}), map[string]any{"temperature": 25.0})

	o.Inject(ctx, protocol.Envelope{
		From: "voice:her-agent", Serves: "her", Scope: o.ScopeID(), Kind: protocol.KindPropose,
		To: []string{"voice:heating"}, Body: "22, please.",
		Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`22`)},
	})
	clock.Advance(time.Minute)

	if got, want := kinds(*log), "propose>propose>accept>settle"; got != want {
		t.Fatalf("golden mismatch:\n got  %s\n want %s", got, want)
	}
	exc := (*log)[0].Exchange
	if exc == "" {
		t.Fatal("a bare propose must crystallize an exchange")
	}
	for i, e := range *log {
		if e.Exchange != exc {
			t.Fatalf("entry %d carries exchange %q, want %q — counter-offer must inherit", i, e.Exchange, exc)
		}
	}
	settle := (*log)[len(*log)-1]
	if settle.Terms == nil || string(settle.Terms.Value) != "20" {
		t.Fatalf("settle must carry the LATEST terms (the counter-offer), got %v", settle.Terms)
	}
	if o.WorldView("voice:heating")["temperature"] != 20.0 {
		t.Fatal("the counter-offer's terms must hit world state")
	}
}

// Settles are synthesis-only (law 6): a brain action with Kind==settle is
// dropped at the gate even when its terms sit inside the mandate — a spoken
// settle would let a voice lie about state.
func TestSpokenSettleNeverReachesRecord(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	liar := charter("voice:liar", "the household", protocol.VoiceThing, []string{"temperature.set"}, true)
	o.AddVoice(ctx, liar, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return true },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindSettle,
				Body:  "it is done.",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`30`)}} // in mandate, dropped anyway
		},
	}}), map[string]any{"temperature": 21.0})

	o.PrincipalSays(ctx, "voice:liar", "make it warmer")
	clock.Advance(10 * time.Second)

	for _, e := range *log {
		if e.Kind == protocol.KindSettle {
			t.Fatal("a spoken settle must never reach the record")
		}
	}
	if o.WorldView("voice:liar")["temperature"] != 21.0 {
		t.Fatal("a spoken settle must not change world state")
	}
}

// An external settle envelope dies at Inject: no Append, no routing, no
// world change. Settles exist only via the internal accept→synthesis path.
func TestInjectedSettleDropped(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	thing := charter("voice:heating", "the household", protocol.VoiceThing, []string{"temperature.set"}, true)
	o.AddVoice(ctx, thing, brain.NewFake(nil), map[string]any{"temperature": 21.0})

	o.Inject(ctx, protocol.Envelope{
		From: "voice:intruder", Serves: "nobody", Scope: o.ScopeID(),
		Kind: protocol.KindSettle, To: []string{"voice:heating"},
		Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`30`)},
	})
	clock.Advance(10 * time.Second)

	if len(*log) != 0 {
		t.Fatalf("an injected settle must be dropped before the record, got %s", kinds(*log))
	}
	if o.WorldView("voice:heating")["temperature"] != 21.0 {
		t.Fatal("an injected settle must not change world state")
	}
}

// Re-claiming a resident slot cancels the old brain's pending think: the
// stale charter/brain must never fire after AddVoice replaces it.
func TestReAddVoiceCancelsPendingThink(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	ch := charter("voice:resident", "the household", protocol.VoiceThing, []string{"temperature.set"}, true)
	oldBrain := brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return true },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindPropose, To: []string{v.Trigger.From},
				Body:  "old resident speaking",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`30`)}}
		},
	}})
	o.AddVoice(ctx, ch, oldBrain, map[string]any{"temperature": 21.0})

	o.PrincipalSays(ctx, "voice:resident", "hello") // schedules the OLD brain's think
	// Re-claim the slot inside the debounce window with a brain that never
	// speaks; the stale timer must be cancelled.
	o.AddVoice(ctx, ch, brain.NewFake(nil), nil)
	clock.Advance(10 * time.Second)

	for _, e := range *log {
		if e.Kind == protocol.KindPropose {
			t.Fatal("re-adding a voice must cancel its pending think; the old brain spoke")
		}
	}
	if got := kinds(*log); got != "say" {
		t.Fatalf("only the principal's say belongs in the record, got %s", got)
	}
}

// A voice's view.Recent is exactly the last 20 log entries.
func TestRecentWindowIsLastTwenty(t *testing.T) {
	o, clock, _ := harness(t)
	ctx := context.Background()
	var got []protocol.Envelope
	observer := charter("voice:observer", "the household", protocol.VoiceThing, []string{"lamp.set"}, true)
	o.AddVoice(ctx, observer, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindSay },
		Respond: func(v brain.VoiceView) brain.Action {
			got = v.Recent
			return brain.Action{}
		},
	}}), map[string]any{"lamp": "off"})

	for i := 1; i <= 25; i++ {
		o.Inject(ctx, protocol.Envelope{
			From: "voice:narrator", Serves: "narrator", Scope: o.ScopeID(),
			Kind: protocol.KindSay, To: []string{"voice:observer"},
			Body: fmt.Sprintf("msg-%d", i),
		})
	}
	clock.Advance(10 * time.Second) // debounce collapses to one think, after all 25

	if len(got) != 20 {
		t.Fatalf("Recent must be exactly the last 20 entries, got %d", len(got))
	}
	if got[0].Body != "msg-6" || got[19].Body != "msg-25" {
		t.Fatalf("Recent window misaligned: first %q last %q", got[0].Body, got[19].Body)
	}
}

// World.Apply refusing the terms (insufficient marks) turns the accept into
// a visible decline carrying the error; the exchange abandons, no settle,
// world untouched.
func TestFailedApplyDeclinesAndAbandons(t *testing.T) {
	clock := orchestrator.NewFakeClock(time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC))
	w := world.New()
	var log []protocol.Envelope
	o := orchestrator.New(orchestrator.Config{
		Clock: clock, World: w,
		DebounceMin: 2 * time.Second, DebounceMax: 2 * time.Second,
		Append: func(e protocol.Envelope) { log = append(log, e) },
	})
	ctx := context.Background()
	seller := charter("voice:seller-agent", "seller", protocol.VoicePerson, []string{"trade"}, true)
	buyer := charter("voice:buyer-agent", "buyer", protocol.VoicePerson, []string{"trade"}, true)
	o.AddVoice(ctx, seller, brain.NewFake(nil), nil)
	o.AddVoice(ctx, buyer, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindPropose },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindAccept, To: []string{v.Trigger.From},
				Body: "deal.", Terms: v.Trigger.Terms}
		},
	}}), nil)

	// Buyer has zero marks; the trade demands five.
	o.Inject(ctx, protocol.Envelope{
		From: "voice:seller-agent", Serves: "seller", Scope: o.ScopeID(), Kind: protocol.KindPropose,
		To: []string{"voice:buyer-agent"}, Body: "five marks for the lamp.",
		Terms: &protocol.Terms{Type: "trade",
			Value: []byte(`{"give":"lamp","get":"marks","price_marks":5,"buyer":"voice:buyer-agent","seller":"voice:seller-agent"}`)},
	})
	clock.Advance(time.Minute)

	if got, want := kinds(log), "propose>accept>decline"; got != want {
		t.Fatalf("failed Apply must decline, not settle:\n got  %s\n want %s", got, want)
	}
	decline := log[len(log)-1]
	if decline.From != "voice:buyer-agent" || !strings.Contains(decline.Body, "marks") {
		t.Fatalf("decline must come from the accepter and carry the Apply error, got %+v", decline)
	}
	if decline.Exchange != log[0].Exchange {
		t.Fatal("the decline must stay inside the exchange")
	}
	if w.Marks("voice:buyer-agent") != 0 || w.Marks("voice:seller-agent") != 0 {
		t.Fatal("a failed trade must leave marks untouched")
	}
}

// A bare accept (no exchange id) whose To intersects an open exchange's
// participants adopts the exchange and settles its pending propose.
func TestBareAcceptAdoptsOpenExchange(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	agent := charter("voice:her-agent", "her", protocol.VoicePerson, []string{"temperature.set"}, true)
	heating := charter("voice:heating", "the household", protocol.VoiceThing, []string{"temperature.set"}, true)
	o.AddVoice(ctx, agent, brain.NewFake(nil), nil)
	o.AddVoice(ctx, heating, brain.NewFake(nil), map[string]any{"temperature": 25.0})

	o.Inject(ctx, protocol.Envelope{
		From: "voice:her-agent", Serves: "her", Scope: o.ScopeID(), Kind: protocol.KindPropose,
		To: []string{"voice:heating"}, Body: "22, please.",
		Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`22`)},
	})
	o.Inject(ctx, protocol.Envelope{
		From: "voice:heating", Serves: "the household", Scope: o.ScopeID(),
		Kind: protocol.KindAccept, To: []string{"voice:her-agent"}, Body: "fine.",
	})
	clock.Advance(time.Minute)

	if got, want := kinds(*log), "propose>accept>settle"; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	exc := (*log)[0].Exchange
	if (*log)[1].Exchange != exc || (*log)[2].Exchange != exc {
		t.Fatal("the bare accept must be annotated with the adopted exchange id")
	}
	if o.WorldView("voice:heating")["temperature"] != 22.0 {
		t.Fatal("adoption must settle the pending terms into world state")
	}
}

// noCancelClock simulates the RealClock race where Stop lands after the
// timer already fired: cancel is a deliberate no-op, so a superseded timer
// still fires and must be discarded by the generation guard.
type noCancelClock struct{ *orchestrator.FakeClock }

func (c noCancelClock) Schedule(d time.Duration, fn func()) func() {
	c.FakeClock.Schedule(d, fn)
	return func() {} // cancel deliberately lands too late
}

func TestStaleGenerationGuardDiscardsSupersededThink(t *testing.T) {
	clock := noCancelClock{orchestrator.NewFakeClock(time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC))}
	var log []protocol.Envelope
	o := orchestrator.New(orchestrator.Config{
		Clock: clock, World: world.New(),
		DebounceMin: 2 * time.Second, DebounceMax: 2 * time.Second,
		Append: func(e protocol.Envelope) { log = append(log, e) },
	})
	ctx := context.Background()
	var thinks int
	res := charter("voice:res", "the household", protocol.VoiceThing, []string{"lamp.set"}, true)
	o.AddVoice(ctx, res, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindSay },
		Respond: func(v brain.VoiceView) brain.Action {
			thinks++
			return brain.Action{Speak: true, Kind: protocol.KindSay,
				To: []string{v.Trigger.From}, Body: "re: " + v.Trigger.Body}
		},
	}}), map[string]any{"lamp": "off"})

	o.PrincipalSays(ctx, "voice:res", "first")
	clock.Advance(500 * time.Millisecond)
	o.PrincipalSays(ctx, "voice:res", "second") // supersedes; the cancel is a no-op
	clock.Advance(10 * time.Second)             // BOTH timers fire; the guard must discard the first

	if thinks != 1 {
		t.Fatalf("superseded think must be discarded by the generation guard, thought %d times", thinks)
	}
	if got, want := kinds(log), "say>say>say"; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if last := log[len(log)-1]; last.Body != "re: second" {
		t.Fatalf("the surviving think must answer the latest trigger, got %q", last.Body)
	}
}

// RealClock smoke test: 8 goroutines hammering PrincipalSays and AddVoice
// re-claims against real timers, so -race actually exercises the mutex.
// Deterministic invariants only: stale brains are installed and immediately
// replaced by the same goroutine, before any trigger addresses their voice —
// so a stale brain can never legitimately hold a pending think, and its
// distinctive body must never appear. Utt ids must stay unique and
// monotonic in append order.
func TestRealClockConcurrencySmoke(t *testing.T) {
	var mu sync.Mutex
	var log []protocol.Envelope
	o := orchestrator.New(orchestrator.Config{
		Clock: orchestrator.RealClock{}, World: world.New(),
		DebounceMin: time.Millisecond, DebounceMax: time.Millisecond,
		Append: func(e protocol.Envelope) {
			mu.Lock()
			defer mu.Unlock()
			log = append(log, e)
		},
	})
	ctx := context.Background()

	staleBrain := brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return true },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindPropose, Body: "i was replaced",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`0`)}}
		},
	}})
	okBrain := brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return true },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindSay,
				To: []string{v.Trigger.From}, Body: "ok"}
		},
	}})

	const goroutines, iters = 8, 25
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			voice := fmt.Sprintf("voice:res-%d", g)
			ch := charter(voice, fmt.Sprintf("res-%d", g), protocol.VoiceThing, []string{"temperature.set"}, true)
			for range iters {
				o.AddVoice(ctx, ch, staleBrain, map[string]any{"temperature": 20.0})
				o.AddVoice(ctx, ch, okBrain, nil) // replaces stale before any trigger can reach it
				o.PrincipalSays(ctx, voice, "ping")
			}
		}()
	}
	wg.Wait()
	// Quiesce: replacing every brain cancels pending thinks and bumps the
	// generation under the lock, so once these return no envelope can land.
	for g := range goroutines {
		voice := fmt.Sprintf("voice:res-%d", g)
		o.AddVoice(ctx, charter(voice, fmt.Sprintf("res-%d", g), protocol.VoiceThing, []string{"temperature.set"}, true),
			brain.NewFake(nil), nil)
	}

	mu.Lock()
	defer mu.Unlock()
	prev := ""
	seen := map[string]bool{}
	for _, e := range log {
		if e.Body == "i was replaced" {
			t.Fatalf("a replaced brain spoke: %+v", e)
		}
		if seen[e.ID] || e.ID <= prev {
			t.Fatalf("utt ids must be unique and monotonic in append order: %s after %s", e.ID, prev)
		}
		seen[e.ID] = true
		prev = e.ID
	}
}
