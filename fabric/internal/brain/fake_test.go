package brain_test

import (
	"context"
	"strings"
	"testing"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
)

func TestFakeBrainMatchesRuleAndResponds(t *testing.T) {
	fb := brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool {
			return strings.Contains(v.Trigger.Body, "cold")
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Kind: protocol.KindPropose, Body: "one degree, then.",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`21.5`)}}
		},
	}})
	view := brain.VoiceView{Trigger: protocol.Envelope{Body: "she is cold again."}}
	ok, err := fb.Relevant(context.Background(), view)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("rule should match")
	}
	a, err := fb.Think(context.Background(), view)
	if err != nil {
		t.Fatal(err)
	}
	if a.Quiet || a.Terms == nil || a.Terms.Type != "temperature.set" {
		t.Fatalf("unexpected action: %+v", a)
	}
}

func TestFakeBrainQuietWhenNoRuleMatches(t *testing.T) {
	fb := brain.NewFake(nil)
	ok, err := fb.Relevant(context.Background(), brain.VoiceView{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("no rules → not relevant")
	}
	a, err := fb.Think(context.Background(), brain.VoiceView{})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Quiet {
		t.Fatal("no rules → quiet")
	}
}
