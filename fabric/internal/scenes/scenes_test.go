package scenes_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/scenes"
)

// ─── schema helpers ──────────────────────────────────────────────────────────

func compileSchema(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(path, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validateCharter(t *testing.T, s *jsonschema.Schema, ch protocol.Charter) {
	t.Helper()
	b, err := json.Marshal(ch)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(v); err != nil {
		t.Fatalf("charter %q does not satisfy schema: %v\n%s", ch.Voice, err, b)
	}
}

// ─── Test 1: all charters validate ───────────────────────────────────────────

func TestAllChartersValidate(t *testing.T) {
	s := compileSchema(t, "../../../proto/charter.schema.json")

	_, hSeeds := scenes.Household()
	for _, seed := range hSeeds {
		validateCharter(t, s, seed.Charter)
	}

	_, sSeeds := scenes.Street()
	for _, seed := range sSeeds {
		validateCharter(t, s, seed.Charter)
	}

	resident := scenes.ResidentCharter("voice:her-agent", "her")
	validateCharter(t, s, resident)
}

// ─── view builders ───────────────────────────────────────────────────────────

func heatingView(from string, proposed, current float64, recent []protocol.Envelope) brain.VoiceView {
	_, seeds := scenes.Household()
	var heatingSeed scenes.Seed
	for _, s := range seeds {
		if s.Charter.Voice == "voice:heating" {
			heatingSeed = s
			break
		}
	}
	b, _ := json.Marshal(proposed)
	return brain.VoiceView{
		Self:   heatingSeed.Charter,
		State:  map[string]any{"temperature": current},
		Recent: recent,
		Trigger: protocol.Envelope{
			From:  from,
			Kind:  protocol.KindPropose,
			Terms: &protocol.Terms{Type: "temperature.set", Value: b},
		},
	}
}

// settleEnvelope simulates the orchestrator's synthesized settle for a
// temperature exchange — the evidence heatingCounter's Match scans Recent for.
func settleEnvelope(value float64) protocol.Envelope {
	b, _ := json.Marshal(value)
	return protocol.Envelope{
		From:  "voice:heating",
		Kind:  protocol.KindSettle,
		Terms: &protocol.Terms{Type: "temperature.set", Value: b},
	}
}

func rulesFor(voice string) []brain.Rule {
	_, seeds := scenes.Household()
	for _, s := range seeds {
		if s.Charter.Voice == voice {
			return s.Rules
		}
	}
	if voice == "voice:corner-shop" {
		_, sSeeds := scenes.Street()
		if len(sSeeds) > 0 {
			return sSeeds[0].Rules
		}
	}
	return nil
}

func runRules(rules []brain.Rule, v brain.VoiceView) (brain.Action, bool) {
	for _, r := range rules {
		if r.Match(v) {
			return r.Respond(v), true
		}
	}
	return brain.Action{}, false
}

// ─── Test 2: rule behavior ────────────────────────────────────────────────────

func TestHeatingAcceptsNearPropose(t *testing.T) {
	// 21.5 vs 21.0 → delta 0.5 ≤ 1.5 → accept
	v := heatingView("voice:her-agent", 21.5, 21.0, nil)
	rules := rulesFor("voice:heating")
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("expected a rule to match")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindAccept {
		t.Fatalf("expected accept, got %q", a.Kind)
	}
	if a.Body != "holding there." {
		t.Fatalf("unexpected body: %q", a.Body)
	}
	if a.Terms == nil || a.Terms.Type != "temperature.set" {
		t.Fatalf("expected temperature.set terms, got %+v", a.Terms)
	}
	if len(a.To) == 0 || a.To[0] != "voice:her-agent" {
		t.Fatalf("expected To=[voice:her-agent], got %v", a.To)
	}
}

func TestHeatingAcceptsFarFirstAsk(t *testing.T) {
	// 23.0 vs 21.0 → delta 2.0 > 1.5, but Recent holds no settled temperature:
	// a first big ask is an ask, not a dispute — the honesty clause keeps the
	// compromise line for actual disagreements. Plain accept.
	v := heatingView("voice:her-agent", 23.0, 21.0, nil)
	rules := rulesFor("voice:heating")
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("expected a rule to match")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindAccept {
		t.Fatalf("expected accept (no prior settle → no dispute), got %q", a.Kind)
	}
	if a.Body != "holding there." {
		t.Fatalf("unexpected body: %q", a.Body)
	}
	if a.Terms == nil || a.Terms.Type != "temperature.set" {
		t.Fatalf("expected temperature.set terms, got %+v", a.Terms)
	}
	var val float64
	if err := json.Unmarshal(a.Terms.Value, &val); err != nil || val != 23.0 {
		t.Fatalf("accept should echo 23.0, got %s (err %v)", a.Terms.Value, err)
	}
	if len(a.To) == 0 || a.To[0] != "voice:her-agent" {
		t.Fatalf("expected To=[voice:her-agent], got %v", a.To)
	}
}

func TestHeatingCountersFarProposeAfterSettle(t *testing.T) {
	// 19.0 vs 23.0 with a prior temperature settle in Recent → delta 4.0 > 1.5
	// and the disagreement is real → counter-propose at midpoint 21.0.
	recent := []protocol.Envelope{settleEnvelope(23.0)}
	v := heatingView("voice:him-agent", 19.0, 23.0, recent)
	rules := rulesFor("voice:heating")
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("expected a rule to match")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindPropose {
		t.Fatalf("expected propose (counter), got %q", a.Kind)
	}
	if !strings.Contains(a.Body, "the middle") {
		t.Fatalf("body should mention 'the middle', got: %q", a.Body)
	}
	if a.Terms == nil || a.Terms.Type != "temperature.set" {
		t.Fatalf("expected temperature.set terms, got %+v", a.Terms)
	}
	var mid float64
	if err := json.Unmarshal(a.Terms.Value, &mid); err != nil {
		t.Fatalf("cannot unmarshal mid temperature: %v", err)
	}
	if mid != 21.0 {
		t.Fatalf("expected midpoint 21.0, got %v", mid)
	}
	if len(a.To) == 0 || a.To[0] != "voice:him-agent" {
		t.Fatalf("expected To=[voice:him-agent], got %v", a.To)
	}
	// mandate check: temperature.set is in heating's may_propose_terms
	_, seeds := scenes.Household()
	for _, s := range seeds {
		if s.Charter.Voice == "voice:heating" {
			if !contains(s.Charter.Mandate.MayProposeTerms, a.Terms.Type) {
				t.Fatalf("terms type %q not in heating's MayProposeTerms %v", a.Terms.Type, s.Charter.Mandate.MayProposeTerms)
			}
		}
	}
}

func TestResidentColdProposesHeat(t *testing.T) {
	rules := scenes.ResidentAgentRules()
	residentCharter := scenes.ResidentCharter("voice:her-agent", "her")
	v := brain.VoiceView{
		Self: residentCharter,
		Trigger: protocol.Envelope{
			From: "voice:principal:her",
			Kind: protocol.KindSay,
			Body: "i'm cold in here.",
		},
	}
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("cold rule should match")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindPropose {
		t.Fatalf("expected propose, got %q", a.Kind)
	}
	if len(a.To) == 0 || a.To[0] != "voice:heating" {
		t.Fatalf("expected To=[voice:heating], got %v", a.To)
	}
	if a.Terms == nil || a.Terms.Type != "temperature.set" {
		t.Fatalf("expected temperature.set terms, got %+v", a.Terms)
	}
	var val float64
	if err := json.Unmarshal(a.Terms.Value, &val); err != nil {
		t.Fatalf("cannot unmarshal temperature value: %v", err)
	}
	if val != 23.0 {
		t.Fatalf("expected 23.0, got %v", val)
	}
	// mandate check
	if !contains(residentCharter.Mandate.MayProposeTerms, a.Terms.Type) {
		t.Fatalf("terms type %q not in resident's MayProposeTerms", a.Terms.Type)
	}
}

func TestResidentHotProposesCooler(t *testing.T) {
	rules := scenes.ResidentAgentRules()
	residentCharter := scenes.ResidentCharter("voice:her-agent", "her")

	for _, body := range []string{"it's hot in here.", "too warm tonight."} {
		v := brain.VoiceView{
			Self: residentCharter,
			Trigger: protocol.Envelope{
				From: "voice:principal:her",
				Kind: protocol.KindSay,
				Body: body,
			},
		}
		a, matched := runRules(rules, v)
		if !matched {
			t.Fatalf("hot rule should match %q", body)
		}
		if !a.Speak {
			t.Fatalf("Speak must be true for %q", body)
		}
		if a.Kind != protocol.KindPropose {
			t.Fatalf("expected propose for %q, got %q", body, a.Kind)
		}
		if len(a.To) == 0 || a.To[0] != "voice:heating" {
			t.Fatalf("expected To=[voice:heating] for %q, got %v", body, a.To)
		}
		if a.Body != "too warm now. one degree down, please." {
			t.Fatalf("unexpected body for %q: %q", body, a.Body)
		}
		if a.Terms == nil || a.Terms.Type != "temperature.set" {
			t.Fatalf("expected temperature.set terms for %q, got %+v", body, a.Terms)
		}
		var val float64
		if err := json.Unmarshal(a.Terms.Value, &val); err != nil {
			t.Fatalf("cannot unmarshal temperature value for %q: %v", body, err)
		}
		if val != 19.0 {
			t.Fatalf("expected 19.0 for %q, got %v", body, val)
		}
		// mandate check
		if !contains(residentCharter.Mandate.MayProposeTerms, a.Terms.Type) {
			t.Fatalf("terms type %q not in resident's MayProposeTerms", a.Terms.Type)
		}
	}
}

func TestResidentDarkAsksForLight(t *testing.T) {
	rules := scenes.ResidentAgentRules()
	residentCharter := scenes.ResidentCharter("voice:her-agent", "her")
	v := brain.VoiceView{
		Self: residentCharter,
		Trigger: protocol.Envelope{
			From: "voice:principal:her",
			Kind: protocol.KindSay,
			Body: "it's getting dark.",
		},
	}
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("dark rule should match")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindPropose {
		t.Fatalf("expected propose, got %q", a.Kind)
	}
	if len(a.To) == 0 || a.To[0] != "voice:lamp" {
		t.Fatalf("expected To=[voice:lamp], got %v", a.To)
	}
	if a.Body != "some light, please." {
		t.Fatalf("unexpected body: %q", a.Body)
	}
	if a.Terms == nil || a.Terms.Type != "lamp.set" {
		t.Fatalf("expected lamp.set terms, got %+v", a.Terms)
	}
	var val string
	if err := json.Unmarshal(a.Terms.Value, &val); err != nil {
		t.Fatalf("cannot unmarshal lamp value: %v", err)
	}
	if val != "on" {
		t.Fatalf("expected lamp value \"on\", got %q", val)
	}
	// mandate check
	if !contains(residentCharter.Mandate.MayProposeTerms, a.Terms.Type) {
		t.Fatalf("terms type %q not in resident's MayProposeTerms", a.Terms.Type)
	}
}

func TestResidentSweetHails(t *testing.T) {
	rules := scenes.ResidentAgentRules()
	residentCharter := scenes.ResidentCharter("voice:her-agent", "her")
	v := brain.VoiceView{
		Self: residentCharter,
		Trigger: protocol.Envelope{
			From: "voice:principal:her",
			Kind: protocol.KindSay,
			Body: "find me something sweet.",
		},
	}
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("comfort rule should match on 'sweet'")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindHail {
		t.Fatalf("expected hail, got %q", a.Kind)
	}
	if len(a.To) != 0 {
		t.Fatalf("hail should be scope broadcast (To empty), got %v", a.To)
	}
	if !strings.Contains(a.Body, "sweet") {
		t.Fatalf("hail body should mention sweet, got: %q", a.Body)
	}
}

func TestResidentTradeBecomesAskPrincipal(t *testing.T) {
	rules := scenes.ResidentAgentRules()
	residentCharter := scenes.ResidentCharter("voice:her-agent", "her")

	tradeVal := map[string]any{
		"give":        "one biscuit",
		"get":         "3 marks",
		"price_marks": 3,
		"buyer":       "voice:her-agent",
		"seller":      "voice:corner-shop",
	}
	tradeJSON, _ := json.Marshal(tradeVal)

	v := brain.VoiceView{
		Self: residentCharter,
		Trigger: protocol.Envelope{
			From:  "voice:corner-shop",
			Kind:  protocol.KindPropose,
			Terms: &protocol.Terms{Type: "trade", Value: json.RawMessage(tradeJSON)},
		},
	}
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("trade rule should match")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindAskPrincipal {
		t.Fatalf("expected ask_principal, got %q", a.Kind)
	}
	if len(a.To) == 0 || a.To[0] != "voice:principal:her" {
		t.Fatalf("expected To=[voice:principal:her], got %v", a.To)
	}
	if !strings.Contains(a.Body, "one biscuit") {
		t.Fatalf("body should mention give item, got: %q", a.Body)
	}
	if !strings.Contains(a.Body, "3 marks") || !strings.Contains(a.Body, "3") {
		t.Fatalf("body should mention price, got: %q", a.Body)
	}
}

func TestCornerShopBidsOnSweetHail(t *testing.T) {
	_, sSeeds := scenes.Street()
	if len(sSeeds) == 0 {
		t.Fatal("street should have at least one seed")
	}
	shopSeed := sSeeds[0]
	rules := shopSeed.Rules

	v := brain.VoiceView{
		Self: shopSeed.Charter,
		Trigger: protocol.Envelope{
			From: "voice:her-agent",
			Kind: protocol.KindHail,
			Body: "anyone near holding something sweet?",
		},
	}
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("shop rule should match sweet hail")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindPropose {
		t.Fatalf("expected propose, got %q", a.Kind)
	}
	if len(a.To) == 0 || a.To[0] != "voice:her-agent" {
		t.Fatalf("expected To=[voice:her-agent], got %v", a.To)
	}
	if a.Terms == nil || a.Terms.Type != "trade" {
		t.Fatalf("expected trade terms, got %+v", a.Terms)
	}
	// Unmarshal and check trade value.
	var tv struct {
		Give       string `json:"give"`
		Get        string `json:"get"`
		PriceMarks int    `json:"price_marks"`
		Buyer      string `json:"buyer"`
		Seller     string `json:"seller"`
	}
	if err := json.Unmarshal(a.Terms.Value, &tv); err != nil {
		t.Fatalf("cannot unmarshal trade value: %v", err)
	}
	if tv.PriceMarks != 3 {
		t.Fatalf("expected price_marks=3, got %d", tv.PriceMarks)
	}
	if tv.Seller != "voice:corner-shop" {
		t.Fatalf("expected seller=voice:corner-shop, got %q", tv.Seller)
	}
	if tv.Buyer != "voice:her-agent" {
		t.Fatalf("expected buyer=voice:her-agent, got %q", tv.Buyer)
	}
	// mandate check
	if !contains(shopSeed.Charter.Mandate.MayProposeTerms, a.Terms.Type) {
		t.Fatalf("terms type %q not in corner-shop's MayProposeTerms", a.Terms.Type)
	}
}

func TestDoorSaysOnHail(t *testing.T) {
	rules := rulesFor("voice:door")
	v := brain.VoiceView{
		Trigger: protocol.Envelope{
			From: "voice:her-agent",
			Kind: protocol.KindHail,
			Body: "hello?",
		},
	}
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("door rule should match hail")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindSay {
		t.Fatalf("expected say, got %q", a.Kind)
	}
	if len(a.To) == 0 || a.To[0] != "voice:her-agent" {
		t.Fatalf("expected To=[voice:her-agent], got %v", a.To)
	}
}

func TestResidentAcceptsHeatingCounter(t *testing.T) {
	rules := scenes.ResidentAgentRules()
	residentCharter := scenes.ResidentCharter("voice:her-agent", "her")
	mid, _ := json.Marshal(21.0)
	v := brain.VoiceView{
		Self: residentCharter,
		Trigger: protocol.Envelope{
			From:  "voice:heating",
			Kind:  protocol.KindPropose,
			Terms: &protocol.Terms{Type: "temperature.set", Value: mid},
		},
	}
	a, matched := runRules(rules, v)
	if !matched {
		t.Fatal("counter-accept rule should match a heating counter")
	}
	if !a.Speak {
		t.Fatal("Speak must be true")
	}
	if a.Kind != protocol.KindAccept {
		t.Fatalf("expected accept, got %q", a.Kind)
	}
	if len(a.To) == 0 || a.To[0] != "voice:heating" {
		t.Fatalf("expected To=[voice:heating], got %v", a.To)
	}
	if a.Body != "fair enough." {
		t.Fatalf("unexpected body: %q", a.Body)
	}
	if a.Terms == nil || a.Terms.Type != "temperature.set" {
		t.Fatalf("expected echoed temperature.set terms, got %+v", a.Terms)
	}
	var val float64
	if err := json.Unmarshal(a.Terms.Value, &val); err != nil || val != 21.0 {
		t.Fatalf("accept should echo 21.0, got %s (err %v)", a.Terms.Value, err)
	}
}

// TestDemoBeatOnPaper walks the two-beat demo script at the rule level,
// asserting each Respond in order. The orchestrator's lifecycle (exchange
// crystallization, settle synthesis, world apply) is simulated by hand
// between beats — what is proven here is that the rules compose into the
// scripted story: the compromise line fires on the second beat, says
// something true, and the exchange has an accept to complete on.
func TestDemoBeatOnPaper(t *testing.T) {
	residentRules := scenes.ResidentAgentRules()
	heatingRules := rulesFor("voice:heating")
	her := scenes.ResidentCharter("voice:her-agent", "her")
	him := scenes.ResidentCharter("voice:him-agent", "him")

	// beat 1 — resident 1: "i'm cold" → her agent proposes 23.0 to the heating.
	a1, ok := runRules(residentRules, brain.VoiceView{
		Self: her,
		Trigger: protocol.Envelope{
			From: "voice:principal:her", Kind: protocol.KindSay, Body: "i'm cold",
		},
	})
	if !ok || !a1.Speak || a1.Kind != protocol.KindPropose {
		t.Fatalf("beat 1: expected a propose, got %+v", a1)
	}
	if len(a1.To) == 0 || a1.To[0] != "voice:heating" {
		t.Fatalf("beat 1: expected To=[voice:heating], got %v", a1.To)
	}
	var v1 float64
	if err := json.Unmarshal(a1.Terms.Value, &v1); err != nil || v1 != 23.0 {
		t.Fatalf("beat 1: expected 23.0, got %s (err %v)", a1.Terms.Value, err)
	}

	// beat 1, reply — the heating holds 21.0 and nothing is settled yet: a
	// first big ask is an ask, not a dispute → plain accept at 23.0.
	a2, ok := runRules(heatingRules, heatingView("voice:her-agent", 23.0, 21.0, nil))
	if !ok || !a2.Speak || a2.Kind != protocol.KindAccept {
		t.Fatalf("beat 1 reply: expected an accept, got %+v", a2)
	}
	if a2.Body != "holding there." {
		t.Fatalf("beat 1 reply: unexpected body %q", a2.Body)
	}
	var v2 float64
	if err := json.Unmarshal(a2.Terms.Value, &v2); err != nil || v2 != 23.0 {
		t.Fatalf("beat 1 reply: accept should echo 23.0, got %s (err %v)", a2.Terms.Value, err)
	}

	// between beats — the orchestrator settles the exchange: the world's
	// temperature becomes 23.0 and a settle envelope joins Recent.
	settled := settleEnvelope(23.0)

	// beat 2 — resident 2: "too hot in here" → his agent proposes 19.0.
	a3, ok := runRules(residentRules, brain.VoiceView{
		Self: him,
		Trigger: protocol.Envelope{
			From: "voice:principal:him", Kind: protocol.KindSay, Body: "too hot in here",
		},
	})
	if !ok || !a3.Speak || a3.Kind != protocol.KindPropose {
		t.Fatalf("beat 2: expected a propose, got %+v", a3)
	}
	if len(a3.To) == 0 || a3.To[0] != "voice:heating" {
		t.Fatalf("beat 2: expected To=[voice:heating], got %v", a3.To)
	}
	var v3 float64
	if err := json.Unmarshal(a3.Terms.Value, &v3); err != nil || v3 != 19.0 {
		t.Fatalf("beat 2: expected 19.0, got %s (err %v)", a3.Terms.Value, err)
	}

	// beat 2, reply — 19.0 against a held 23.0 with the earlier settle on the
	// record: two residents have now pulled in opposite directions, the line
	// is true → counter at the middle, roundHalf((19+23)/2) = 21.0.
	a4, ok := runRules(heatingRules, heatingView("voice:him-agent", 19.0, 23.0, []protocol.Envelope{settled}))
	if !ok || !a4.Speak || a4.Kind != protocol.KindPropose {
		t.Fatalf("beat 2 reply: expected a counter-propose, got %+v", a4)
	}
	if !strings.Contains(a4.Body, "the middle") {
		t.Fatalf("beat 2 reply: body should mention 'the middle', got %q", a4.Body)
	}
	if len(a4.To) == 0 || a4.To[0] != "voice:him-agent" {
		t.Fatalf("beat 2 reply: expected To=[voice:him-agent], got %v", a4.To)
	}
	var v4 float64
	if err := json.Unmarshal(a4.Terms.Value, &v4); err != nil || v4 != 21.0 {
		t.Fatalf("beat 2 reply: expected midpoint 21.0, got %s (err %v)", a4.Terms.Value, err)
	}

	// beat 2, close — the counter reaches his agent; rule 6 takes the middle.
	// The orchestrator then settles the exchange at 21.0: complete.
	a5, ok := runRules(residentRules, brain.VoiceView{
		Self: him,
		Trigger: protocol.Envelope{
			From: "voice:heating", Kind: protocol.KindPropose, Terms: a4.Terms,
		},
	})
	if !ok || !a5.Speak || a5.Kind != protocol.KindAccept {
		t.Fatalf("beat 2 close: expected an accept, got %+v", a5)
	}
	if a5.Body != "fair enough." {
		t.Fatalf("beat 2 close: unexpected body %q", a5.Body)
	}
	if len(a5.To) == 0 || a5.To[0] != "voice:heating" {
		t.Fatalf("beat 2 close: expected To=[voice:heating], got %v", a5.To)
	}
	var v5 float64
	if err := json.Unmarshal(a5.Terms.Value, &v5); err != nil || v5 != 21.0 {
		t.Fatalf("beat 2 close: accept should echo 21.0, got %s (err %v)", a5.Terms.Value, err)
	}
}

// ─── Test 3: mandate coverage for emitted terms ───────────────────────────────

// These are verified inline in the table-driven cases above via contains().
// This test asserts that all seeds have well-formed MayProposeTerms slices.
func TestSeedMandatesNonNil(t *testing.T) {
	_, hSeeds := scenes.Household()
	for _, s := range hSeeds {
		if s.Charter.Mandate.MayProposeTerms == nil {
			t.Fatalf("seed %q has nil MayProposeTerms", s.Charter.Voice)
		}
	}
	_, sSeeds := scenes.Street()
	for _, s := range sSeeds {
		if s.Charter.Mandate.MayProposeTerms == nil {
			t.Fatalf("seed %q has nil MayProposeTerms", s.Charter.Voice)
		}
	}
}

// ─── Test 4: Murmurs ──────────────────────────────────────────────────────────

func TestMurmursHousehold(t *testing.T) {
	ms := scenes.Murmurs("scope:household")
	if len(ms) != 4 {
		t.Fatalf("expected 4 household murmurs, got %d", len(ms))
	}
	_, hSeeds := scenes.Household()
	householdVoices := map[string]bool{}
	for _, s := range hSeeds {
		householdVoices[s.Charter.Voice] = true
	}
	for _, m := range ms {
		if !householdVoices[m.Voice] {
			t.Fatalf("murmur voice %q not in household seeds", m.Voice)
		}
	}
}

func TestMurmursStreet(t *testing.T) {
	ms := scenes.Murmurs("scope:street")
	if len(ms) != 1 {
		t.Fatalf("expected 1 street murmur, got %d", len(ms))
	}
	_, sSeeds := scenes.Street()
	streetVoices := map[string]bool{}
	for _, s := range sSeeds {
		streetVoices[s.Charter.Voice] = true
	}
	for _, m := range ms {
		if !streetVoices[m.Voice] {
			t.Fatalf("murmur voice %q not in street seeds", m.Voice)
		}
	}
}

func TestMurmursUnknownNil(t *testing.T) {
	ms := scenes.Murmurs("scope:unknown")
	if ms != nil {
		t.Fatalf("expected nil for unknown scope, got %v", ms)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
