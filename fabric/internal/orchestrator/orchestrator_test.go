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
