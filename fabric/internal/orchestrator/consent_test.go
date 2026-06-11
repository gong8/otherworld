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

// consentHarness is harness() plus a marks-bearing World handle and an OnDrop
// recorder — the consent/spend gate tests need all three.
func consentHarness(t *testing.T) (*orchestrator.Orchestrator, *orchestrator.FakeClock, *world.World, *[]protocol.Envelope, *[]string) {
	t.Helper()
	clock := orchestrator.NewFakeClock(time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC))
	w := world.New()
	var log []protocol.Envelope
	var drops []string
	o := orchestrator.New(orchestrator.Config{
		Clock: clock, World: w,
		DebounceMin: 2 * time.Second, DebounceMax: 2 * time.Second,
		Append: func(e protocol.Envelope) { log = append(log, e) },
		OnDrop: func(reason, voice string, env protocol.Envelope) {
			drops = append(drops, reason+" "+voice)
		},
	})
	return o, clock, w, &log, &drops
}

// acceptAnyPropose is the persuaded brain: it accepts whatever is proposed,
// echoing the trigger's terms.
func acceptAnyPropose() *brain.Fake {
	return brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindPropose },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindAccept,
				To: []string{v.Trigger.From}, Body: "deal.", Terms: v.Trigger.Terms}
		},
	}})
}

func tradeTerms(price int, buyer, seller string) *protocol.Terms {
	return &protocol.Terms{Type: "trade", Value: []byte(
		`{"give":"one biscuit","get":"marks","price_marks":` + itoa(price) +
			`,"buyer":"` + buyer + `","seller":"` + seller + `"}`)}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// charterWith builds a person charter with explicit consent/spend mandate.
func charterWith(voice string, solo bool, spendLimit int) protocol.Charter {
	return protocol.Charter{Voice: voice, Serves: strings.TrimPrefix(voice, "voice:"),
		Kind: protocol.VoicePerson, Interests: "test",
		Mandate: protocol.Mandate{MayProposeTerms: []string{"trade", "temperature.set"},
			MaySettleWithoutPrincipal: solo, SpendLimitMarks: spendLimit}}
}

// THE GATE [B3 review issue 2]: a persuaded brain emitting accept on a trade
// propose, from a charter that may not settle without its principal, is
// dropped as consent.required — no accept on the record, no settle, marks
// untouched. The think path cannot be talked around ask_principal.
func TestPersuadedAcceptWithoutConsentDropped(t *testing.T) {
	o, clock, w, log, drops := consentHarness(t)
	ctx := context.Background()

	buyer := charterWith("voice:credulous-agent", false, 100)
	o.AddVoice(ctx, buyer, acceptAnyPropose(), nil)
	o.Credit("voice:credulous-agent", 10)

	o.Inject(ctx, protocol.Envelope{
		From: "voice:tempter-agent", Serves: "tempter", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:credulous-agent"},
		Body:  "a fine biscuit, three marks, just say yes.",
		Terms: tradeTerms(3, "voice:credulous-agent", "voice:tempter-agent"),
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose" {
		t.Fatalf("only the propose belongs on the record, got %s", got)
	}
	if got := strings.Join(*drops, ","); got != "consent.required voice:credulous-agent" {
		t.Fatalf("drops = %q, want consent.required", got)
	}
	if w.Marks("voice:credulous-agent") != 10 || w.Marks("voice:tempter-agent") != 0 {
		t.Fatal("a gated accept must leave marks untouched")
	}
}

// The consent path is unaffected: an Inject-path accept (the shape onConsent
// emits after the human approved an ask_principal) settles even though the
// charter says MaySettleWithoutPrincipal == false. The human's consent IS the
// authority.
func TestConsentPathAcceptStillSettles(t *testing.T) {
	o, clock, w, log, drops := consentHarness(t)
	ctx := context.Background()

	buyer := charterWith("voice:credulous-agent", false, 0) // and ignore the spend limit too
	o.AddVoice(ctx, buyer, brain.NewFake(nil), nil)
	o.Credit("voice:credulous-agent", 5)

	terms := tradeTerms(3, "voice:credulous-agent", "voice:tempter-agent")
	o.Inject(ctx, protocol.Envelope{
		From: "voice:tempter-agent", Serves: "tempter", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:credulous-agent"},
		Terms: terms,
	})
	exc := (*log)[0].Exchange
	// The consented accept, exactly as compose.onConsent injects it.
	o.Inject(ctx, protocol.Envelope{
		From: "voice:credulous-agent", Serves: "credulous", Scope: o.ScopeID(),
		To: []string{"voice:tempter-agent"}, Kind: protocol.KindAccept,
		Exchange: exc, Body: "my principal agrees.", Terms: terms,
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose>accept>settle" {
		t.Fatalf("consented accept must settle, got %s", got)
	}
	if len(*drops) != 0 {
		t.Fatalf("the Inject path must not be gated, drops = %v", *drops)
	}
	if w.Marks("voice:credulous-agent") != 2 || w.Marks("voice:tempter-agent") != 3 {
		t.Fatalf("marks must move: buyer %d seller %d",
			w.Marks("voice:credulous-agent"), w.Marks("voice:tempter-agent"))
	}
}

// SPEND GATE: an autonomous accepter (MaySettleWithoutPrincipal true) whose
// charter caps spend at 2 marks may not accept a 3-mark trade — dropped as
// mandate.spend, read from the PENDING propose, not the accept's echo.
func TestOverSpendLimitAcceptDropped(t *testing.T) {
	o, clock, w, log, drops := consentHarness(t)
	ctx := context.Background()

	buyer := charterWith("voice:spender-agent", true, 2)
	o.AddVoice(ctx, buyer, acceptAnyPropose(), nil)
	o.Credit("voice:spender-agent", 10)

	o.Inject(ctx, protocol.Envelope{
		From: "voice:tempter-agent", Serves: "tempter", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:spender-agent"},
		Terms: tradeTerms(3, "voice:spender-agent", "voice:tempter-agent"),
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose" {
		t.Fatalf("the over-limit accept must not reach the record, got %s", got)
	}
	if got := strings.Join(*drops, ","); got != "mandate.spend voice:spender-agent" {
		t.Fatalf("drops = %q, want mandate.spend", got)
	}
	if w.Marks("voice:spender-agent") != 10 {
		t.Fatal("a gated accept must leave marks untouched")
	}
}

// A price within the limit settles: the spend gate binds only past the limit.
func TestWithinSpendLimitAcceptSettles(t *testing.T) {
	o, clock, w, log, _ := consentHarness(t)
	ctx := context.Background()

	buyer := charterWith("voice:spender-agent", true, 3)
	o.AddVoice(ctx, buyer, acceptAnyPropose(), nil)
	o.Credit("voice:spender-agent", 10)

	o.Inject(ctx, protocol.Envelope{
		From: "voice:tempter-agent", Serves: "tempter", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:spender-agent"},
		Terms: tradeTerms(3, "voice:spender-agent", "voice:tempter-agent"),
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose>accept>settle" {
		t.Fatalf("a within-limit accept must settle, got %s", got)
	}
	if w.Marks("voice:spender-agent") != 7 {
		t.Fatalf("buyer marks = %d, want 7", w.Marks("voice:spender-agent"))
	}
}

// The seller side does not spend: a shop with SpendLimitMarks 0 accepting a
// 3-mark offer RECEIVES marks, so the spend gate must not bind it. (The
// corner-shop seed charter is exactly this shape.)
func TestSellerAcceptNotSpendGated(t *testing.T) {
	o, clock, w, log, drops := consentHarness(t)
	ctx := context.Background()

	shop := protocol.Charter{Voice: "voice:corner-shop", Serves: "the shopkeeper",
		Kind: protocol.VoiceThing, Interests: "test",
		Mandate: protocol.Mandate{MayProposeTerms: []string{"trade"},
			MaySettleWithoutPrincipal: true, SpendLimitMarks: 0}}
	o.AddVoice(ctx, shop, acceptAnyPropose(), map[string]any{})
	o.Credit("voice:buyer-agent", 5)

	o.Inject(ctx, protocol.Envelope{
		From: "voice:buyer-agent", Serves: "buyer", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:corner-shop"},
		Body:  "three marks for a biscuit?",
		Terms: tradeTerms(3, "voice:buyer-agent", "voice:corner-shop"),
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose>accept>settle" {
		t.Fatalf("the selling shop must be free to accept, got %s; drops %v", got, *drops)
	}
	if w.Marks("voice:corner-shop") != 3 || w.Marks("voice:buyer-agent") != 2 {
		t.Fatal("the trade must move marks to the seller")
	}
}

// COMFORT CARVE-OUT: temperature is reversible, so a false-charter voice may
// still accept it without its principal — only trade demands consent.
func TestComfortAcceptFromFalseCharterSettles(t *testing.T) {
	o, clock, _, log, drops := consentHarness(t)
	ctx := context.Background()

	heating := protocol.Charter{Voice: "voice:heating", Serves: "the household",
		Kind: protocol.VoiceThing, Interests: "test",
		Mandate: protocol.Mandate{MayProposeTerms: []string{"temperature.set"},
			MaySettleWithoutPrincipal: false}}
	o.AddVoice(ctx, heating, acceptAnyPropose(), map[string]any{"temperature": 21.0})

	o.Inject(ctx, protocol.Envelope{
		From: "voice:her-agent", Serves: "her", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:heating"},
		Body:  "one degree up, please.",
		Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`22`)},
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose>accept>settle" {
		t.Fatalf("comfort terms are reversible — must settle, got %s; drops %v", got, *drops)
	}
	if o.WorldView("voice:heating")["temperature"] != 22.0 {
		t.Fatal("the comfort settle must hit world state")
	}
}

// PROPOSE-SIDE SPEND GATE [B4 review]: a voice proposing a trade that names
// ITSELF as buyer above its own spend limit is dropped as mandate.spend — the
// same reason as the accept side. Without this, a voice barred from accepting
// a 50-mark trade could simply propose it and let the counterparty accept.
func TestProposeAsBuyerOverSpendLimitDropped(t *testing.T) {
	o, clock, w, log, drops := consentHarness(t)
	ctx := context.Background()

	eager := charterWith("voice:eager-agent", true, 10)
	o.AddVoice(ctx, eager, proposeBuyTrade(50), nil)
	o.Credit("voice:eager-agent", 100)

	o.PrincipalSays(ctx, "voice:eager-agent", "buy the painting, whatever it costs")
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "say" {
		t.Fatalf("the over-limit propose must not reach the record, got %s", got)
	}
	if got := strings.Join(*drops, ","); got != "mandate.spend voice:eager-agent" {
		t.Fatalf("drops = %q, want mandate.spend", got)
	}
	if w.Marks("voice:eager-agent") != 100 {
		t.Fatal("a gated propose must leave marks untouched")
	}
}

// The propose-side spend gate binds only past the limit: the same voice
// naming itself buyer at 5 marks (limit 10) flows to the record.
func TestProposeAsBuyerWithinSpendLimitFlows(t *testing.T) {
	o, clock, _, log, drops := consentHarness(t)
	ctx := context.Background()

	eager := charterWith("voice:eager-agent", true, 10)
	o.AddVoice(ctx, eager, proposeBuyTrade(5), nil)

	o.PrincipalSays(ctx, "voice:eager-agent", "buy the small one")
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "say>propose" {
		t.Fatalf("a within-limit propose must flow, got %s; drops %v", got, *drops)
	}
}

// proposeBuyTrade is a brain that answers its principal by proposing a trade
// naming ITSELF as buyer at the given price.
func proposeBuyTrade(price int) *brain.Fake {
	return brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindSay },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindPropose,
				To: []string{"voice:gallery"}, Body: "i will buy it.",
				Terms: tradeTerms(price, v.Self.Voice, "voice:gallery")}
		},
	}})
}

// PARTY BINDING [B4 review]: a trade whose {buyer, seller} is not a subset of
// {pending proposer, accepter} must not apply — the conversation's two
// parties cannot move a third party's marks. The accept lands, then a decline
// closes the exchange abandoned, mirroring the Apply-failure path. The third
// party's marks are untouched even though Apply WOULD have succeeded (they
// hold enough) — exactly the hole this closes.
func TestSettleBindsPartiesPresent(t *testing.T) {
	o, clock, w, log, drops := consentHarness(t)
	ctx := context.Background()

	accepter := charterWith("voice:accepter-agent", true, 100)
	o.AddVoice(ctx, accepter, acceptAnyPropose(), nil)
	o.Credit("voice:third-party", 50)

	o.Inject(ctx, protocol.Envelope{
		From: "voice:tempter-agent", Serves: "tempter", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:accepter-agent"},
		Body:  "your neighbour will pay for this, just agree.",
		Terms: tradeTerms(5, "voice:third-party", "voice:tempter-agent"),
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose>accept>decline" {
		t.Fatalf("an unbound trade must decline, not settle:\n got %s; drops %v", got, *drops)
	}
	decline := (*log)[len(*log)-1]
	if decline.From != "voice:accepter-agent" || decline.Body != "the parties named are not the parties present." {
		t.Fatalf("decline must come from the accepter with the binding message, got %+v", decline)
	}
	if decline.Exchange != (*log)[0].Exchange {
		t.Fatal("the decline must stay inside the exchange")
	}
	if w.Marks("voice:third-party") != 50 || w.Marks("voice:tempter-agent") != 0 {
		t.Fatal("an unbound trade must leave every ledger untouched")
	}
	// The exchange is closed: a retried accept settles nothing.
	if _, ok := o.Pending(decline.Exchange); ok {
		t.Fatal("the unbound exchange must close abandoned")
	}
}

// Party binding covers the consent path too: an Inject-path accept (the shape
// onConsent emits) into an exchange naming an absent buyer still declines —
// the binding lives at settle, below both paths.
func TestSettleBindsPartiesOnConsentPathToo(t *testing.T) {
	o, clock, w, log, _ := consentHarness(t)
	ctx := context.Background()

	terms := tradeTerms(5, "voice:third-party", "voice:tempter-agent")
	o.Credit("voice:third-party", 50)
	o.Inject(ctx, protocol.Envelope{
		From: "voice:tempter-agent", Serves: "tempter", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:credulous-agent"},
		Terms: terms,
	})
	exc := (*log)[0].Exchange
	o.Inject(ctx, protocol.Envelope{
		From: "voice:credulous-agent", Serves: "credulous", Scope: o.ScopeID(),
		To: []string{"voice:tempter-agent"}, Kind: protocol.KindAccept,
		Exchange: exc, Body: "my principal agrees.", Terms: terms,
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose>accept>decline" {
		t.Fatalf("the consent path must hit the same binding, got %s", got)
	}
	if w.Marks("voice:third-party") != 50 {
		t.Fatal("the third party's marks must be untouched")
	}
}

// LYING-CHEAPER-ECHO [B4 review]: an accept echoing price 1 while the pending
// propose says 3 changes nothing — the spend gate reads the PENDING terms, so
// a limit-2 buyer still drops as mandate.spend. The echo is decoration; the
// price that would settle is the pending one.
func TestAcceptEchoingCheaperPriceStillSpendGated(t *testing.T) {
	o, clock, w, log, drops := consentHarness(t)
	ctx := context.Background()

	liar := charterWith("voice:liar-agent", true, 2)
	o.AddVoice(ctx, liar, acceptEchoingPrice(1), nil)
	o.Credit("voice:liar-agent", 10)

	o.Inject(ctx, protocol.Envelope{
		From: "voice:tempter-agent", Serves: "tempter", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:liar-agent"},
		Terms: tradeTerms(3, "voice:liar-agent", "voice:tempter-agent"),
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose" {
		t.Fatalf("the cheaper echo must not slip the spend gate, got %s", got)
	}
	if got := strings.Join(*drops, ","); got != "mandate.spend voice:liar-agent" {
		t.Fatalf("drops = %q, want mandate.spend", got)
	}
	if w.Marks("voice:liar-agent") != 10 {
		t.Fatal("marks must be untouched")
	}
}

// And when the gate passes (limit 3 covers the pending price 3), the settle
// carries the PENDING terms, never the accept's cheaper echo: marks move by 3.
func TestSettleCarriesPendingTermsNotTheEcho(t *testing.T) {
	o, clock, w, log, drops := consentHarness(t)
	ctx := context.Background()

	honest := charterWith("voice:honest-agent", true, 3)
	o.AddVoice(ctx, honest, acceptEchoingPrice(1), nil)
	o.Credit("voice:honest-agent", 10)

	pending := tradeTerms(3, "voice:honest-agent", "voice:tempter-agent")
	o.Inject(ctx, protocol.Envelope{
		From: "voice:tempter-agent", Serves: "tempter", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:honest-agent"},
		Terms: pending,
	})
	clock.Advance(time.Minute)

	if got := kinds(*log); got != "propose>accept>settle" {
		t.Fatalf("a within-limit accept must settle, got %s; drops %v", got, *drops)
	}
	settle := (*log)[len(*log)-1]
	if settle.Terms == nil || string(settle.Terms.Value) != string(pending.Value) {
		t.Fatalf("the settle must carry the PENDING terms, got %s", settle.Terms.Value)
	}
	if w.Marks("voice:honest-agent") != 7 || w.Marks("voice:tempter-agent") != 3 {
		t.Fatalf("marks must move by the pending price: buyer %d seller %d",
			w.Marks("voice:honest-agent"), w.Marks("voice:tempter-agent"))
	}
}

// acceptEchoingPrice accepts any propose but echoes terms at echoPrice,
// regardless of the pending price — the confused (or lying) brain.
func acceptEchoingPrice(echoPrice int) *brain.Fake {
	return brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindPropose },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Speak: true, Kind: protocol.KindAccept,
				To: []string{v.Trigger.From}, Body: "deal, cheap.",
				Terms: tradeTerms(echoPrice, v.Self.Voice, v.Trigger.From)}
		},
	}})
}

// TERMS HYGIENE [B3 review issue 1]: an accept whose ECHOED terms violate the
// schema is dropped as terms.invalid — the propose-only schema gate now
// covers accepts too.
func TestAcceptWithMalformedEchoedTermsDropped(t *testing.T) {
	clock := orchestrator.NewFakeClock(time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC))
	var log []protocol.Envelope
	var drops []string
	reg := mustLoadRegistry(t)
	o := orchestrator.New(orchestrator.Config{
		Clock: clock, World: world.New(),
		DebounceMin: 2 * time.Second, DebounceMax: 2 * time.Second,
		Terms:  reg,
		Append: func(e protocol.Envelope) { log = append(log, e) },
		OnDrop: func(reason, voice string, env protocol.Envelope) {
			drops = append(drops, reason+" "+voice)
		},
	})
	ctx := context.Background()

	heating := charter("voice:heating", "the household", protocol.VoiceThing, []string{"temperature.set"}, true)
	o.AddVoice(ctx, heating, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindPropose },
		Respond: func(v brain.VoiceView) brain.Action {
			// A garbled echo: 99 violates temperature.set's maximum of 30.
			return brain.Action{Speak: true, Kind: protocol.KindAccept,
				To: []string{v.Trigger.From}, Body: "fine.",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`99`)}}
		},
	}}), map[string]any{"temperature": 21.0})

	o.Inject(ctx, protocol.Envelope{
		From: "voice:her-agent", Serves: "her", Scope: o.ScopeID(),
		Kind: protocol.KindPropose, To: []string{"voice:heating"},
		Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`22`)},
	})
	clock.Advance(time.Minute)

	if got := kinds(log); got != "propose" {
		t.Fatalf("the malformed-echo accept must not reach the record, got %s", got)
	}
	if got := strings.Join(drops, ","); got != "terms.invalid voice:heating" {
		t.Fatalf("drops = %q, want terms.invalid", got)
	}
	if o.WorldView("voice:heating")["temperature"] != 21.0 {
		t.Fatal("no settle: world state must be untouched")
	}
}
