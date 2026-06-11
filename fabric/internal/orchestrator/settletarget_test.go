package orchestrator_test

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/orchestrator"
	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/world"
)

// TestSettleTargetMatchesLifecycle is the DRIFT TRIPWIRE [B4 review]: the
// consent and spend gates fire when settleTarget says an accept would settle,
// while the lifecycle decides settlement for real — if the two rule sets ever
// drift apart, a gated accept could settle ungated or a free accept be gated.
// Seeded, deterministic, property-style: each iteration builds the SAME
// exchange state on two cloned orchestrators, asks settleTarget on one and
// injects the accept for real on the other, and asserts agreement
// (settleTarget non-nil ⟺ a settle lands). Terms are temperature.set on a
// registered thing so World.Apply always succeeds — the property under test
// is the adoption+matching rule, not the reducer.
func TestSettleTargetMatchesLifecycle(t *testing.T) {
	const a, b, c = "voice:alpha-agent", "voice:heating", "voice:outsider-agent"
	rng := rand.New(rand.NewPCG(0xB5, 0x715E))
	temp := &protocol.Terms{Type: "temperature.set", Value: []byte(`22`)}
	pick := func(ss ...string) string { return ss[rng.IntN(len(ss))] }

	for i := 0; i < 400; i++ {
		// Two clones: identical seeded state, built by the same script.
		var orchs [2]*orchestrator.Orchestrator
		var logs [2]*[]protocol.Envelope
		for j := range orchs {
			log := &[]protocol.Envelope{}
			o := orchestrator.New(orchestrator.Config{
				Clock: orchestrator.NewFakeClock(time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC)),
				World: world.New(), DebounceMin: time.Second, DebounceMax: time.Second,
				Append: func(e protocol.Envelope) { *log = append(*log, e) },
			})
			ctx := context.Background()
			o.AddVoice(ctx, charter(a, "alpha", protocol.VoicePerson, []string{"temperature.set"}, true), brain.NewFake(nil), nil)
			o.AddVoice(ctx, charter(b, "the household", protocol.VoiceThing, []string{"temperature.set"}, true), brain.NewFake(nil), map[string]any{"temperature": 21.0})
			o.AddVoice(ctx, charter(c, "outsider", protocol.VoicePerson, []string{"temperature.set"}, true), brain.NewFake(nil), nil)
			orchs[j], logs[j] = o, log
		}
		script := func(env protocol.Envelope) {
			for _, o := range orchs {
				o.Inject(context.Background(), env)
			}
		}

		var excID string
		if rng.IntN(4) > 0 { // usually an exchange exists
			var terms *protocol.Terms
			if rng.IntN(4) > 0 { // usually the pending propose carries terms
				terms = temp
			}
			script(protocol.Envelope{From: a, Serves: "alpha", Kind: protocol.KindPropose, To: []string{b}, Terms: terms})
			excID = (*logs[0])[len(*logs[0])-1].Exchange
			if rng.IntN(4) == 0 { // sometimes a counter-offer flips the proposer
				script(protocol.Envelope{From: b, Serves: "the household", Kind: protocol.KindPropose, To: []string{a}, Exchange: excID, Terms: temp})
			}
			if rng.IntN(4) == 0 { // sometimes the exchange is already closed
				script(protocol.Envelope{From: a, Serves: "alpha", Kind: protocol.KindWithdraw, To: []string{b}, Exchange: excID})
			}
		}

		accept := protocol.Envelope{
			From: pick(a, b, c), Serves: "x", Kind: protocol.KindAccept,
			Exchange: pick("", excID, "exc_00000000000000000000000bogus"),
		}
		switch rng.IntN(4) {
		case 0:
			accept.To = []string{a}
		case 1:
			accept.To = []string{b}
		case 2:
			accept.To = []string{c}
		}
		if rng.IntN(2) == 0 {
			accept.Terms = temp // the accept's own terms are decoration either way
		}

		predicted := orchs[0].WouldSettle(accept)
		before := len(*logs[1])
		orchs[1].Inject(context.Background(), accept)
		settled := false
		for _, e := range (*logs[1])[before:] {
			if e.Kind == protocol.KindSettle {
				settled = true
			}
		}
		if predicted != settled {
			t.Fatalf("iter %d: settleTarget predicted %v, lifecycle settled %v\naccept: %+v\nexchange: %q",
				i, predicted, settled, accept, excID)
		}
	}
}
