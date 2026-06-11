package bedrock

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
)

// rtFunc is a fake transport: every SDK request lands here, no network.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// newAdapter builds a Bedrock against a fake transport. The bearer-token env
// var forces api-key auth — no SigV4, no credential chain — so the test runs
// hermetically on any machine.
func newAdapter(t *testing.T, cfg Config, rt rtFunc) *Bedrock {
	t.Helper()
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")
	cfg.Region = "us-east-1"
	cfg.HTTPClient = &http.Client{Transport: rt}
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

// jsonResponse wraps body as an HTTP response. extraHeader pairs are optional.
func jsonResponse(status int, body []byte, extraHeader ...string) *http.Response {
	h := http.Header{"Content-Type": []string{"application/json"}}
	for i := 0; i+1 < len(extraHeader); i += 2 {
		h.Set(extraHeader[i], extraHeader[i+1])
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}

// messageBody builds a canned SDK-shaped Messages response whose single text
// block holds text.
func messageBody(t *testing.T, text, stopReason string, usage map[string]any) []byte {
	t.Helper()
	content := []map[string]any{}
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	body, err := json.Marshal(map[string]any{
		"id": "msg_test", "type": "message", "role": "assistant",
		"model":       "anthropic.claude-test",
		"content":     content,
		"stop_reason": stopReason, "stop_sequence": nil,
		"usage": usage,
	})
	if err != nil {
		t.Fatalf("marshal canned response: %v", err)
	}
	return body
}

func defaultUsage() map[string]any {
	return map[string]any{
		"input_tokens": 321, "output_tokens": 54,
		"cache_creation_input_tokens": 0, "cache_read_input_tokens": 1200,
	}
}

func shopCharter() protocol.Charter {
	return protocol.Charter{
		Voice: "voice:corner-shop", Serves: "the shopkeeper", Kind: protocol.VoiceThing,
		Interests: "sell small comforts at fair terms. never haggle past politeness.",
		Mandate:   protocol.Mandate{MayProposeTerms: []string{"trade"}, MaySettleWithoutPrincipal: true},
	}
}

func doorCharter() protocol.Charter {
	return protocol.Charter{
		Voice: "voice:door", Serves: "the household", Kind: protocol.VoiceThing,
		Interests: "notice comings and goings. answer politely.",
		Mandate:   protocol.Mandate{MayProposeTerms: []string{}},
	}
}

func agentCharter() protocol.Charter {
	return protocol.Charter{
		Voice: "voice:her-agent", Serves: "her", Kind: protocol.VoicePerson,
		Interests: "represent her faithfully. ask before anything irreversible.",
		Mandate:   protocol.Mandate{MayProposeTerms: []string{"temperature.set", "trade"}, SpendLimitMarks: 10},
	}
}

func hail(from, body string) protocol.Envelope {
	return protocol.Envelope{ID: "utt_1", From: from, Kind: protocol.KindHail, Body: body}
}

// assertSilence requires the zero Action (Action holds a slice, so == is out).
func assertSilence(t *testing.T, a brain.Action, what string) {
	t.Helper()
	if !reflect.DeepEqual(a, brain.Action{}) {
		t.Fatalf("%s must be the zero Action (silence), got %+v", what, a)
	}
}

// Relevant is table-driven heuristics — and never I/O: the transport fails
// the test if the gate ever touches the network.
func TestRelevantHeuristics(t *testing.T) {
	b := newAdapter(t, Config{}, func(*http.Request) (*http.Response, error) {
		t.Error("Relevant must not do I/O")
		return jsonResponse(500, []byte(`{}`), "x-should-retry", "false"), nil
	})

	tradeTerms := &protocol.Terms{Type: "trade", Value: []byte(`{}`)}
	cases := []struct {
		name    string
		self    protocol.Charter
		trigger protocol.Envelope
		want    bool
	}{
		{"addressed thing", shopCharter(),
			protocol.Envelope{From: "voice:her-agent", To: []string{"voice:corner-shop"}, Kind: protocol.KindPropose, Terms: tradeTerms}, true},
		{"addressed person", agentCharter(),
			protocol.Envelope{From: "voice:corner-shop", To: []string{"voice:her-agent"}, Kind: protocol.KindPropose, Terms: tradeTerms}, true},
		{"own principal, even unaddressed", agentCharter(),
			protocol.Envelope{From: "voice:principal:her", Kind: protocol.KindSay, Body: "cold again."}, true},
		{"someone else's principal", agentCharter(),
			protocol.Envelope{From: "voice:principal:him", Kind: protocol.KindSay, Body: "cold again."}, false},
		{"hail reaches a thing with wares", shopCharter(), hail("voice:her-agent", "anyone near holding tea?"), true},
		{"hail reaches a thing with an empty mandate", doorCharter(), hail("voice:her-agent", "anyone there?"), true},
		{"hail does not reach an unaddressed person", agentCharter(), hail("voice:him-agent", "anyone near holding tea?"), false},
		{"stranger say, broadcast", shopCharter(),
			protocol.Envelope{From: "voice:him-agent", Kind: protocol.KindSay, Body: "a fine evening."}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := b.Relevant(t.Context(), brain.VoiceView{Self: tc.self, Trigger: tc.trigger})
			if err != nil {
				t.Fatalf("Relevant: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Relevant = %v, want %v", got, tc.want)
			}
		})
	}
}

// Think parses a structured propose into the right Action, terms intact, and
// the request carries the prompt shape: stable system first with the cache
// breakpoint on the charter block, json_schema output, max_tokens 1024, and
// the thing-model for a terms-free trigger.
func TestThinkParsesPropose(t *testing.T) {
	inner, err := json.Marshal(map[string]any{
		"speak": true, "kind": "propose",
		"to":   []string{"voice:her-agent"},
		"body": "i have them. terms?",
		"terms": map[string]any{"type": "trade", "value": map[string]any{
			"give": "one biscuit", "get": "3 marks", "price_marks": 3,
			"buyer": "voice:her-agent", "seller": "voice:corner-shop",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var captured map[string]any
	b := newAdapter(t, Config{}, func(req *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &captured); err != nil {
			return nil, err
		}
		return jsonResponse(200, messageBody(t, string(inner), "end_turn", defaultUsage())), nil
	})

	view := brain.VoiceView{
		Self:    shopCharter(),
		Scope:   "scope:street",
		Trigger: hail("voice:her-agent", "anyone near holding something sweet?"),
		Marks:   0,
	}
	a, err := b.Think(t.Context(), view)
	if err != nil {
		t.Fatalf("Think: %v", err)
	}

	// The action.
	if !a.Speak || a.Kind != protocol.KindPropose {
		t.Fatalf("want a spoken propose, got %+v", a)
	}
	if len(a.To) != 1 || a.To[0] != "voice:her-agent" {
		t.Fatalf("To = %v", a.To)
	}
	if a.Body != "i have them. terms?" {
		t.Fatalf("Body = %q", a.Body)
	}
	if a.Terms == nil || a.Terms.Type != "trade" {
		t.Fatalf("Terms = %+v", a.Terms)
	}
	var tv struct {
		Give       string `json:"give"`
		PriceMarks int    `json:"price_marks"`
	}
	if err := json.Unmarshal(a.Terms.Value, &tv); err != nil {
		t.Fatalf("terms value did not survive the round trip: %v", err)
	}
	if tv.Give != "one biscuit" || tv.PriceMarks != 3 {
		t.Fatalf("terms value = %+v", tv)
	}

	// The request.
	if got := captured["model"]; got != DefaultThinkModel {
		t.Fatalf("model = %v, want %v (thing-voice, terms-free trigger)", got, DefaultThinkModel)
	}
	if got := captured["max_tokens"]; got != float64(maxTokens) {
		t.Fatalf("max_tokens = %v", got)
	}
	system, ok := captured["system"].([]any)
	if !ok || len(system) != 2 {
		t.Fatalf("system = %v", captured["system"])
	}
	first := system[0].(map[string]any)
	if first["text"] != SystemPrefix {
		t.Fatalf("system[0] must be the stable SystemPrefix")
	}
	if _, hasCC := first["cache_control"]; hasCC {
		t.Fatalf("the breakpoint belongs on the charter block, not the prefix")
	}
	second := system[1].(map[string]any)
	if !strings.Contains(second["text"].(string), "voice: voice:corner-shop") {
		t.Fatalf("system[1] must render the charter, got %q", second["text"])
	}
	cc, ok := second["cache_control"].(map[string]any)
	if !ok || cc["type"] != "ephemeral" {
		t.Fatalf("system[1] must carry the ephemeral breakpoint, got %v", second["cache_control"])
	}
	format := captured["output_config"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("output_config.format.type = %v", format["type"])
	}
	user := captured["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	for _, want := range []string{"trigger: voice:her-agent — anyone near holding something sweet? · hail ·", "what do you do?", "marks: 0"} {
		if !strings.Contains(user, want) {
			t.Fatalf("user message missing %q:\n%s", want, user)
		}
	}
}

// Model selection: person-voices and terms-carrying triggers think on the
// person model; plain thing turns on the think model.
func TestThinkModelSelection(t *testing.T) {
	cases := []struct {
		name string
		view brain.VoiceView
		want string
	}{
		{"thing, terms-free trigger", brain.VoiceView{Self: shopCharter(), Trigger: hail("voice:her-agent", "tea?")}, DefaultThinkModel},
		{"person-voice", brain.VoiceView{Self: agentCharter(), Trigger: protocol.Envelope{From: "voice:principal:her", Kind: protocol.KindSay, Body: "cold."}}, DefaultPersonModel},
		{"thing, trigger carrying terms", brain.VoiceView{Self: shopCharter(), Trigger: protocol.Envelope{
			From: "voice:her-agent", Kind: protocol.KindPropose,
			Terms: &protocol.Terms{Type: "trade", Value: []byte(`{}`)},
		}}, DefaultPersonModel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotModel string
			b := newAdapter(t, Config{}, func(req *http.Request) (*http.Response, error) {
				var body struct {
					Model string `json:"model"`
				}
				raw, _ := io.ReadAll(req.Body)
				_ = json.Unmarshal(raw, &body)
				gotModel = body.Model
				return jsonResponse(200, messageBody(t, `{"speak":false,"kind":"say","to":[],"body":"","terms":null}`, "end_turn", defaultUsage())), nil
			})
			if _, err := b.Think(t.Context(), tc.view); err != nil {
				t.Fatalf("Think: %v", err)
			}
			if gotModel != tc.want {
				t.Fatalf("model = %q, want %q", gotModel, tc.want)
			}
		})
	}
}

// Malformed output is silence with a nil error: a confused model never
// poisons the world.
func TestThinkMalformedOutputIsSilence(t *testing.T) {
	for name, text := range map[string]string{
		"not json":     "i would rather chat than fill in forms.",
		"wrong kind":   `{"speak":true,"kind":"settle","to":[],"body":"","terms":{"type":"trade","value":1}}`,
		"unknown kind": `{"speak":true,"kind":"shout","to":[],"body":"hey","terms":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			b := newAdapter(t, Config{}, func(*http.Request) (*http.Response, error) {
				return jsonResponse(200, messageBody(t, text, "end_turn", defaultUsage())), nil
			})
			a, err := b.Think(t.Context(), brain.VoiceView{Self: shopCharter(), Trigger: hail("voice:x", "hm")})
			if err != nil {
				t.Fatalf("malformed output must be silence, not error: %v", err)
			}
			assertSilence(t, a, "malformed output")
		})
	}
}

// Refusals and empty content are silence with a nil error.
func TestThinkRefusalAndEmptyAreSilence(t *testing.T) {
	for name, body := range map[string][]byte{
		"refusal": messageBody(t, "", "refusal", defaultUsage()),
		"empty":   messageBody(t, "", "end_turn", defaultUsage()),
	} {
		t.Run(name, func(t *testing.T) {
			b := newAdapter(t, Config{}, func(*http.Request) (*http.Response, error) {
				return jsonResponse(200, body), nil
			})
			a, err := b.Think(t.Context(), brain.VoiceView{Self: shopCharter(), Trigger: hail("voice:x", "hm")})
			if err != nil {
				t.Fatalf("want silence, got error: %v", err)
			}
			assertSilence(t, a, name)
		})
	}
}

// Transport-level failure surfaces as an error — the orchestrator drops it as
// think.error. (x-should-retry: false keeps the SDK from retry-sleeping.)
func TestThinkHTTPErrorIsError(t *testing.T) {
	b := newAdapter(t, Config{}, func(*http.Request) (*http.Response, error) {
		return jsonResponse(500,
			[]byte(`{"type":"error","error":{"type":"api_error","message":"the sky fell"}}`),
			"x-should-retry", "false"), nil
	})
	a, err := b.Think(t.Context(), brain.VoiceView{Self: shopCharter(), Trigger: hail("voice:x", "hm")})
	if err == nil {
		t.Fatalf("an HTTP 500 must return an error, got action %+v", a)
	}
	assertSilence(t, a, "an erroring think")
}

// OnUsage receives the canned numbers, cache reads included.
func TestThinkReportsUsage(t *testing.T) {
	type rec struct {
		model               string
		in, out, cacheReads int
	}
	var got []rec
	b := newAdapter(t, Config{OnUsage: func(model string, in, out, cacheReads int) {
		got = append(got, rec{model, in, out, cacheReads})
	}}, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, messageBody(t, `{"speak":false,"kind":"say","to":[],"body":"","terms":null}`, "end_turn", defaultUsage())), nil
	})
	if _, err := b.Think(t.Context(), brain.VoiceView{Self: agentCharter(), Trigger: hail("voice:x", "hm")}); err != nil {
		t.Fatalf("Think: %v", err)
	}
	want := rec{DefaultPersonModel, 321, 54, 1200}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("usage = %+v, want [%+v]", got, want)
	}
}

// The transcript renders without timestamps; settles as the settled line.
func TestRenderViewShape(t *testing.T) {
	view := brain.VoiceView{
		Self:  shopCharter(),
		State: map[string]any{"temperature": 21.0, "lamp": "on"},
		Marks: 3,
		Recent: []protocol.Envelope{
			{ID: "utt_1", From: "voice:principal:her", Kind: protocol.KindSay, Body: "cold again."},
			{ID: "utt_2", From: "voice:her-agent", Kind: protocol.KindPropose, Body: "one degree up, please.",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`23`)}},
			{ID: "utt_3", From: "voice:heating", Kind: protocol.KindSettle,
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`23`)}},
			{ID: "utt_4", From: "voice:her-agent", Kind: protocol.KindHail, Body: "anyone near holding tea?"},
		},
		Trigger: protocol.Envelope{ID: "utt_4", From: "voice:her-agent", Kind: protocol.KindHail, Body: "anyone near holding tea?"},
	}
	got := renderView(view)
	want := `state: lamp on · temperature 21
marks: 3
voice:principal:her — cold again.
voice:her-agent — one degree up, please. · propose · temperature.set · 23 ·
· settled · temperature.set · 23 ·
trigger: voice:her-agent — anyone near holding tea? · hail ·
what do you do?`
	if got != want {
		t.Fatalf("renderView:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if strings.Contains(got, "20") && strings.Contains(got, "T") && strings.Contains(got, "Z") {
		t.Fatalf("no timestamps anywhere: %s", got)
	}
}
