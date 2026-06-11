package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/orchestrator"
	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/world"
)

func harness(t *testing.T) (*orchestrator.Orchestrator, *orchestrator.FakeClock, *[]protocol.Envelope) {
	t.Helper()
	clock := orchestrator.NewFakeClock(time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC))
	var log []protocol.Envelope
	o := orchestrator.New(orchestrator.Config{
		Clock:       clock,
		World:       world.New(),
		TurnCap:     12,
		DebounceMin: 2 * time.Second,
		DebounceMax: 2 * time.Second, // deterministic in tests
		Append:      func(e protocol.Envelope) { log = append(log, e) },
	})
	return o, clock, &log
}

func kinds(log []protocol.Envelope) string {
	var ks []string
	for _, e := range log {
		ks = append(ks, string(e.Kind))
	}
	return strings.Join(ks, ">")
}

// GOLDEN 1: the heating compromise.
func TestGoldenCompromise(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()

	heating := charter("voice:heating", "the household", protocol.VoiceThing, []string{"temperature.set"}, true)
	her := charter("voice:her-agent", "her", protocol.VoicePerson, []string{"temperature.set"}, true)

	o.AddVoice(ctx, heating, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindPropose },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindAccept, To: []string{v.Trigger.From},
				Body: "holding the middle.", Terms: v.Trigger.Terms}
		},
	}}), map[string]any{"temperature": 21.0})
	o.AddVoice(ctx, her, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.From == "voice:principal:her" },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindPropose, To: []string{"voice:heating"},
				Body:  "she is cold again. one degree, please.",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`21.5`)}}
		},
	}}), nil)

	o.PrincipalSays(ctx, "voice:her-agent", "i'm cold")
	clock.Advance(10 * time.Second)

	want := "say>propose>accept>settle"
	if got := kinds(*log); got != want {
		t.Fatalf("golden mismatch:\n got  %s\n want %s", got, want)
	}
	last := (*log)[len(*log)-1]
	if last.Terms == nil || last.Terms.Type != "temperature.set" {
		t.Fatal("settle must carry the terms")
	}
	if o.WorldView("voice:heating")["temperature"] != 21.5 {
		t.Fatal("settled terms must hit world state")
	}
}

// GOLDEN 2: mandate enforcement — a propose outside the charter dies at the gate.
func TestGoldenMandateBlocksUnauthorizedTerms(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	rogue := charter("voice:lamp", "the household", protocol.VoiceThing, []string{"lamp.set"}, true)
	o.AddVoice(ctx, rogue, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return true },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindPropose,
				Terms: &protocol.Terms{Type: "trade", Value: []byte(`{}`)}} // not in mandate
		},
	}}), map[string]any{"lamp": "off"})
	o.PrincipalSays(ctx, "voice:lamp", "hello")
	clock.Advance(10 * time.Second)
	for _, e := range *log {
		if e.Kind == protocol.KindPropose {
			t.Fatal("law 4: propose outside mandate must not reach the record")
		}
	}
}

// GOLDEN 3: deadlock → turn cap → visible withdraw, exchange abandoned.
func TestGoldenDeadlockAbandons(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	stubborn := func(name string) (protocol.Charter, *brain.Fake) {
		return charter("voice:"+name, name, protocol.VoiceThing, []string{"temperature.set"}, true),
			brain.NewFake([]brain.Rule{{
				Match: func(v brain.VoiceView) bool {
					return v.Trigger.Kind == protocol.KindPropose || v.Trigger.Kind == protocol.KindDecline
				},
				Respond: func(v brain.VoiceView) brain.Action {
					return brain.Action{Speak: true, Kind: protocol.KindDecline, To: []string{v.Trigger.From}, Body: "no."}
				},
			}})
	}
	c1, b1 := stubborn("hot")
	c2, b2 := stubborn("cold")
	o.AddVoice(ctx, c1, b1, map[string]any{"temperature": 25.0})
	o.AddVoice(ctx, c2, b2, map[string]any{"temperature": 15.0})
	o.Inject(ctx, protocol.Envelope{
		From: "voice:hot", Serves: "hot", Scope: o.ScopeID(), Kind: protocol.KindPropose,
		To: []string{"voice:cold"}, Body: "25.",
		Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`25`)},
	})
	clock.Advance(5 * time.Minute)
	lastKind := (*log)[len(*log)-1].Kind
	if lastKind != protocol.KindWithdraw {
		t.Fatalf("deadlock must end in a visible withdraw, got %s", kinds(*log))
	}
}

func charter(voice, serves string, kind protocol.VoiceKind, terms []string, solo bool) protocol.Charter {
	return protocol.Charter{Voice: voice, Serves: serves, Kind: kind,
		Interests: "test", Mandate: protocol.Mandate{
			MayProposeTerms: terms, MaySettleWithoutPrincipal: solo, SpendLimitMarks: 100}}
}
