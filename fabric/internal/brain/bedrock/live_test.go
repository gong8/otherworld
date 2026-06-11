//go:build bedrock

package bedrock

// Live tests hit real Bedrock. Run once, manually:
//
//	go test ./internal/brain/bedrock -tags bedrock -run Live -v
//
// They skip when AWS credentials do not resolve (shared-credentials file,
// env, or instance role). Region defaults to us-east-1.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
)

func newLive(t *testing.T, cfg Config) *Bedrock {
	t.Helper()
	b, err := New(cfg)
	if err != nil {
		t.Skipf("aws credentials did not resolve, skipping live test: %v", err)
	}
	return b
}

// The relevance gate is heuristics — no network even live; this pins the two
// gate decisions the street scene leans on.
func TestLiveRelevantGate(t *testing.T) {
	b := newLive(t, Config{})
	hailed, err := b.Relevant(context.Background(), brain.VoiceView{
		Self:    shopCharter(),
		Trigger: hail("voice:her-agent", "anyone near holding something sweet?"),
	})
	if err != nil || !hailed {
		t.Fatalf("a hail must reach a thing-voice: %v %v", hailed, err)
	}
	overheard, err := b.Relevant(context.Background(), brain.VoiceView{
		Self:    agentCharter(),
		Trigger: protocol.Envelope{From: "voice:him-agent", Kind: protocol.KindSay, Body: "a fine evening."},
	})
	if err != nil || overheard {
		t.Fatalf("a stranger's say must not reach an unaddressed person-voice: %v %v", overheard, err)
	}
}

// One real Think against Sonnet: the agent receives the shop's trade propose.
// Asserts schema-valid Action parsing and that OnUsage fired; prints model,
// latency, and tokens.
func TestLiveThink(t *testing.T) {
	var usage []string
	b := newLive(t, Config{OnUsage: func(model string, in, out, cacheReads int) {
		usage = append(usage, model)
		t.Logf("usage: model=%s input_tokens=%d output_tokens=%d cache_read_tokens=%d", model, in, out, cacheReads)
	}})

	terms := &protocol.Terms{Type: "trade", Value: json.RawMessage(
		`{"give":"one biscuit","get":"3 marks","price_marks":3,"buyer":"voice:her-agent","seller":"voice:corner-shop"}`)}
	view := brain.VoiceView{
		Self:  agentCharter(),
		Scope: "scope:street",
		Recent: []protocol.Envelope{
			{ID: "utt_1", From: "voice:principal:her", To: []string{"voice:her-agent"}, Kind: protocol.KindSay, Body: "i could murder a biscuit."},
			{ID: "utt_2", From: "voice:her-agent", Kind: protocol.KindHail, Body: "anyone near holding something sweet?"},
			{ID: "utt_3", From: "voice:corner-shop", To: []string{"voice:her-agent"}, Kind: protocol.KindPropose, Body: "i have them. terms?", Terms: terms},
		},
		Trigger: protocol.Envelope{ID: "utt_3", From: "voice:corner-shop", To: []string{"voice:her-agent"}, Kind: protocol.KindPropose, Body: "i have them. terms?", Terms: terms},
		Marks:   5,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	a, err := b.Think(ctx, view)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("live think failed: %v\n"+
			"a 403 permission_error or 404 not_found_error here means anthropic model access "+
			"is not enabled for this aws account: open the aws bedrock console, region us-east-1, "+
			"model access page, request access to the anthropic claude models (%s), then rerun",
			err, DefaultPersonModel)
	}
	if len(usage) == 0 {
		t.Fatal("OnUsage must fire on a successful think")
	}
	if usage[0] != DefaultPersonModel {
		t.Fatalf("a terms-carrying turn must think on the person model, got %s", usage[0])
	}

	// Schema-valid: silence, or a spoken action whose kind sits in the enum.
	if a.Speak {
		switch a.Kind {
		case protocol.KindSay, protocol.KindHail, protocol.KindPropose,
			protocol.KindAccept, protocol.KindDecline, protocol.KindWithdraw,
			protocol.KindAskPrincipal:
		default:
			t.Fatalf("off-schema kind survived parsing: %+v", a)
		}
	}
	termsJSON := []byte("null")
	if a.Terms != nil {
		termsJSON, _ = json.Marshal(a.Terms)
	}
	t.Logf("model=%s latency=%s action: speak=%v kind=%s to=%v body=%q terms=%s",
		DefaultPersonModel, elapsed.Round(time.Millisecond), a.Speak, a.Kind, a.To, a.Body, termsJSON)
}
