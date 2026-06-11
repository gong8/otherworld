package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
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
