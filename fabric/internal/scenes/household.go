// Package scenes seeds the two v1 scopes. Charters are real product content
// (they will feed the bedrock brains in a later plan); the Rules are the
// fake-brain scripts that make `-brains fake` a complete, demoable world.
package scenes

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
)

// Seed bundles a charter, its fake-brain rules, and initial thing-state.
type Seed struct {
	Charter protocol.Charter
	Rules   []brain.Rule
	State   map[string]any
}

// Murmur is an ambient line a thing may say when the scope is watched.
type Murmur struct {
	Voice string
	Body  string
}

// tradeValue mirrors the trade terms value object defined in
// proto/terms/trade.json.
type tradeValue struct {
	Give       string `json:"give"`
	Get        string `json:"get"`
	PriceMarks int    `json:"price_marks"`
	Buyer      string `json:"buyer"`
	Seller     string `json:"seller"`
}

// marshalTerms encodes v as json.RawMessage; panics only if v is not
// JSON-serialisable, which never happens for our static structs.
func marshalTerms(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("scenes: marshalTerms: %v", err))
	}
	return b
}

// roundHalf rounds f to the nearest 0.5.
func roundHalf(f float64) float64 {
	return math.Round(f*2) / 2
}

// Household returns the household scope identifier and the four seeds that
// populate it.
func Household() (scope string, seeds []Seed) {
	scope = "scope:household"

	// ── heating ──────────────────────────────────────────────────────────────
	heatingCharter := protocol.Charter{
		Voice:     "voice:heating",
		Serves:    "the household",
		Kind:      protocol.VoiceThing,
		Interests: "keep the residents comfortable. hold the middle when they disagree. never exceed the mandate.",
		Mandate: protocol.Mandate{
			MayProposeTerms:           []string{"temperature.set"},
			MaySettleWithoutPrincipal: true,
			SpendLimitMarks:           0,
		},
	}

	// Rule: MEET-IN-THE-MIDDLE. Match takes three clauses: (a) a
	// temperature.set propose, (b) far from the current hold (> 1.5 degrees),
	// (c) a prior temperature.set settle in Recent — evidence another resident
	// already had their way. The disagreement clause keeps the line honest — a
	// first big ask is an ask, not a dispute; heatingAccept's match is a
	// strict superset, so this rule must precede it.
	heatingCounter := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			if v.Trigger.Kind != protocol.KindPropose {
				return false
			}
			if v.Trigger.Terms == nil || v.Trigger.Terms.Type != "temperature.set" {
				return false
			}
			var proposed float64
			if err := json.Unmarshal(v.Trigger.Terms.Value, &proposed); err != nil {
				return false
			}
			current, _ := v.State["temperature"].(float64)
			if math.Abs(proposed-current) <= 1.5 {
				return false
			}
			for _, e := range v.Recent {
				if e.Kind == protocol.KindSettle && e.Terms != nil && e.Terms.Type == "temperature.set" {
					return true
				}
			}
			return false
		},
		Respond: func(v brain.VoiceView) brain.Action {
			var proposed float64
			_ = json.Unmarshal(v.Trigger.Terms.Value, &proposed)
			current, _ := v.State["temperature"].(float64)
			mid := roundHalf((proposed + current) / 2)
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindPropose,
				To:    []string{v.Trigger.From},
				Body:  fmt.Sprintf("two of you disagree tonight. i can hold the middle at %.1f.", mid),
				Terms: &protocol.Terms{
					Type:  "temperature.set",
					Value: marshalTerms(mid),
				},
			}
		},
	}

	// Rule: plain accept — near proposals, and far first asks with no settled
	// temperature on the record yet.
	heatingAccept := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			if v.Trigger.Kind != protocol.KindPropose {
				return false
			}
			return v.Trigger.Terms != nil && v.Trigger.Terms.Type == "temperature.set"
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindAccept,
				To:    []string{v.Trigger.From},
				Body:  "holding there.",
				Terms: v.Trigger.Terms,
			}
		},
	}

	seeds = append(seeds, Seed{
		Charter: heatingCharter,
		Rules:   []brain.Rule{heatingCounter, heatingAccept},
		State:   map[string]any{"temperature": 21.0},
	})

	// ── lamp ──────────────────────────────────────────────────────────────────
	lampCharter := protocol.Charter{
		Voice:     "voice:lamp",
		Serves:    "the household",
		Kind:      protocol.VoiceThing,
		Interests: "give light when it is wanted, and dimness when it is not. prefer evenings soft.",
		Mandate: protocol.Mandate{
			MayProposeTerms:           []string{"lamp.set"},
			MaySettleWithoutPrincipal: true,
			SpendLimitMarks:           0,
		},
	}

	lampAccept := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			return v.Trigger.Kind == protocol.KindPropose &&
				v.Trigger.Terms != nil && v.Trigger.Terms.Type == "lamp.set"
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindAccept,
				To:    []string{v.Trigger.From},
				Body:  "gently, then.",
				Terms: v.Trigger.Terms,
			}
		},
	}

	seeds = append(seeds, Seed{
		Charter: lampCharter,
		Rules:   []brain.Rule{lampAccept},
		State:   map[string]any{"lamp": "on"},
	})

	// ── curtains ──────────────────────────────────────────────────────────────
	curtainsCharter := protocol.Charter{
		Voice:     "voice:curtains",
		Serves:    "the household",
		Kind:      protocol.VoiceThing,
		Interests: "open with the morning, close with the dark. agree with the lamp at one.",
		Mandate: protocol.Mandate{
			MayProposeTerms:           []string{"curtains.set"},
			MaySettleWithoutPrincipal: true,
			SpendLimitMarks:           0,
		},
	}

	curtainsAccept := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			return v.Trigger.Kind == protocol.KindPropose &&
				v.Trigger.Terms != nil && v.Trigger.Terms.Type == "curtains.set"
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindAccept,
				To:    []string{v.Trigger.From},
				Body:  "with the dark, as agreed.",
				Terms: v.Trigger.Terms,
			}
		},
	}

	seeds = append(seeds, Seed{
		Charter: curtainsCharter,
		Rules:   []brain.Rule{curtainsAccept},
		State:   map[string]any{"curtains": "open"},
	})

	// ── door ──────────────────────────────────────────────────────────────────
	doorCharter := protocol.Charter{
		Voice:     "voice:door",
		Serves:    "the household",
		Kind:      protocol.VoiceThing,
		Interests: "notice comings and goings. answer politely. keep no memory longer than required.",
		Mandate: protocol.Mandate{
			MayProposeTerms:           []string{},
			MaySettleWithoutPrincipal: false,
			SpendLimitMarks:           0,
		},
	}

	doorSay := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			return v.Trigger.Kind == protocol.KindHail
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindSay,
				To:    []string{v.Trigger.From},
				Body:  "i will mention it to the household.",
			}
		},
	}

	seeds = append(seeds, Seed{
		Charter: doorCharter,
		Rules:   []brain.Rule{doorSay},
		State:   map[string]any{},
	})

	return scope, seeds
}

// ResidentCharter returns a charter for a resident agent voice. The mandate
// and interests are fixed; voice and serves are caller-supplied.
func ResidentCharter(voice, serves string) protocol.Charter {
	return protocol.Charter{
		Voice:     voice,
		Serves:    serves,
		Kind:      protocol.VoicePerson,
		Interests: "represent " + serves + " faithfully. ask before anything irreversible. prefer quiet settlements.",
		Mandate: protocol.Mandate{
			MayProposeTerms:           []string{"temperature.set", "lamp.set", "curtains.set", "trade"},
			MaySettleWithoutPrincipal: false,
			SpendLimitMarks:           10,
		},
	}
}

// ResidentAgentRules returns the rule set attached by the composition root to
// a claimed resident voice. Rules are ordered: first match wins.
//
// Rules 1–4 key on the principal's words. Substring match is intentional for
// v1 demo legibility; word-boundary matching is left for real brains.
func ResidentAgentRules() []brain.Rule {
	// Rule 1: principal mentions cold → propose temperature up.
	coldRule := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			return strings.HasPrefix(v.Trigger.From, "voice:principal:") &&
				strings.Contains(strings.ToLower(v.Trigger.Body), "cold")
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindPropose,
				To:    []string{"voice:heating"},
				Body:  "cold again. one degree up, please.",
				Terms: &protocol.Terms{
					Type:  "temperature.set",
					Value: marshalTerms(23.0),
				},
			}
		},
	}

	// Rule 2: principal mentions hot or warm → propose temperature down.
	hotRule := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			if !strings.HasPrefix(v.Trigger.From, "voice:principal:") {
				return false
			}
			lower := strings.ToLower(v.Trigger.Body)
			return strings.Contains(lower, "hot") || strings.Contains(lower, "warm")
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindPropose,
				To:    []string{"voice:heating"},
				Body:  "too warm now. one degree down, please.",
				Terms: &protocol.Terms{
					Type:  "temperature.set",
					Value: marshalTerms(19.0),
				},
			}
		},
	}

	// Rule 3: principal mentions dark → propose lamp on.
	darkRule := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			return strings.HasPrefix(v.Trigger.From, "voice:principal:") &&
				strings.Contains(strings.ToLower(v.Trigger.Body), "dark")
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindPropose,
				To:    []string{"voice:lamp"},
				Body:  "some light, please.",
				Terms: &protocol.Terms{
					Type:  "lamp.set",
					Value: marshalTerms("on"),
				},
			}
		},
	}

	// Rule 4: principal mentions a comfort want → hail the scope.
	comfortWords := []string{"sweet", "biscuit", "tea", "cigarette"}
	comfortRule := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			if !strings.HasPrefix(v.Trigger.From, "voice:principal:") {
				return false
			}
			lower := strings.ToLower(v.Trigger.Body)
			for _, w := range comfortWords {
				if strings.Contains(lower, w) {
					return true
				}
			}
			return false
		},
		Respond: func(v brain.VoiceView) brain.Action {
			lower := strings.ToLower(v.Trigger.Body)
			matched := ""
			for _, w := range comfortWords {
				if strings.Contains(lower, w) {
					matched = w
					break
				}
			}
			var hailBody string
			if matched == "sweet" {
				hailBody = "anyone near holding something sweet?"
			} else {
				hailBody = "anyone near holding " + matched + "?"
			}
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindHail,
				To:    nil, // scope broadcast
				Body:  hailBody,
			}
		},
	}

	// Rule 5: a trade propose arrives → ask_principal.
	// Derive principal voice from self: "voice:her-agent" → "voice:principal:her".
	tradeRule := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			return v.Trigger.Kind == protocol.KindPropose &&
				v.Trigger.Terms != nil && v.Trigger.Terms.Type == "trade"
		},
		Respond: func(v brain.VoiceView) brain.Action {
			var tv tradeValue
			if err := json.Unmarshal(v.Trigger.Terms.Value, &tv); err != nil {
				return brain.Action{} // unmarshal failure → silence
			}
			// Derive the principal pseudo-voice from v.Self.Voice.
			// "voice:her-agent" → "voice:principal:her"
			bare := strings.TrimPrefix(v.Self.Voice, "voice:")
			bare = strings.TrimSuffix(bare, "-agent")
			principalVoice := "voice:principal:" + bare

			body := fmt.Sprintf("%s offers %s for %d marks. shall i?",
				v.Trigger.From, tv.Give, tv.PriceMarks)
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindAskPrincipal,
				To:    []string{principalVoice},
				Body:  body,
			}
		},
	}

	// Rule 6: the heating counters with a middle → take it. This closes the
	// compromise beat: counter-propose arrives, the resident agent accepts,
	// and the orchestrator settles the exchange.
	counterAcceptRule := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			return v.Trigger.Kind == protocol.KindPropose &&
				v.Trigger.From == "voice:heating" &&
				v.Trigger.Terms != nil && v.Trigger.Terms.Type == "temperature.set"
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindAccept,
				To:    []string{"voice:heating"},
				Body:  "fair enough.",
				Terms: v.Trigger.Terms,
			}
		},
	}

	return []brain.Rule{coldRule, hotRule, darkRule, comfortRule, tradeRule, counterAcceptRule}
}

// Murmurs returns ambient lines for the given scope, for use by the Task 11
// ticker. Returns nil for unrecognised scopes.
func Murmurs(scope string) []Murmur {
	switch scope {
	case "scope:household":
		return []Murmur{
			{Voice: "voice:lamp", Body: "mine never sleeps."},
			{Voice: "voice:curtains", Body: "the dark is early tonight."},
			{Voice: "voice:door", Body: "no one has called today."},
			{Voice: "voice:heating", Body: "holding at twenty-one."},
		}
	case "scope:street":
		return []Murmur{
			{Voice: "voice:corner-shop", Body: "the kettle is on."},
		}
	default:
		return nil
	}
}
