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

func heatingView(triggerTermsValue float64) brain.VoiceView {
	_, seeds := scenes.Household()
	var heatingSeed scenes.Seed
	for _, s := range seeds {
		if s.Charter.Voice == "voice:heating" {
			heatingSeed = s
			break
		}
	}
	b, _ := json.Marshal(triggerTermsValue)
	return brain.VoiceView{
		Self:  heatingSeed.Charter,
		State: map[string]any{"temperature": 21.0},
		Trigger: protocol.Envelope{
			From:  "voice:her-agent",
			Kind:  protocol.KindPropose,
			Terms: &protocol.Terms{Type: "temperature.set", Value: b},
		},
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
	v := heatingView(21.5)
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
	if a.Terms == nil || a.Terms.Type != "temperature.set" {
		t.Fatalf("expected temperature.set terms, got %+v", a.Terms)
	}
	if len(a.To) == 0 || a.To[0] != "voice:her-agent" {
		t.Fatalf("expected To=[voice:her-agent], got %v", a.To)
	}
}

func TestHeatingCountersFarPropose(t *testing.T) {
	// 25.0 vs 21.0 → delta 4.0 > 1.5 → counter-propose at midpoint 23.0
	v := heatingView(25.0)
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
	if mid != 23.0 {
		t.Fatalf("expected midpoint 23.0, got %v", mid)
	}
	if len(a.To) == 0 || a.To[0] != "voice:her-agent" {
		t.Fatalf("expected To=[voice:her-agent], got %v", a.To)
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
	if val != 22.0 {
		t.Fatalf("expected 22.0, got %v", val)
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
		if val != 20.5 {
			t.Fatalf("expected 20.5 for %q, got %v", body, val)
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
