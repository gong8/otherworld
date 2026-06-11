// Package bedrock implements brain.Brain on Claude models served through
// Amazon Bedrock (the modern Messages-API integration; model ids carry the
// "anthropic." prefix). Relevant is heuristics only — it runs at schedule
// time UNDER the orchestrator lock, so v1 makes no network call there: the
// plan allowed a tiny Haiku fallback, but the two scenes are fully covered by
// the heuristics and I/O under the lock would stall the whole scope on every
// trigger. GateModel is kept in Config for the day a networked gate moves off
// the lock. Think runs off the lock; seconds are safe there.
package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	awsbedrock "github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
)

// Default Bedrock model ids ("anthropic."-prefixed bare strings).
const (
	DefaultGateModel   = "anthropic.claude-haiku-4-5"
	DefaultThinkModel  = "anthropic.claude-haiku-4-5"
	DefaultPersonModel = "anthropic.claude-sonnet-4-6"
)

// maxTokens bounds one think's output. Actions are one or two sentences plus
// terms; 1024 is generous.
const maxTokens = 1024

type Config struct {
	// Region is the AWS region. Empty falls back to AWS_REGION /
	// AWS_DEFAULT_REGION, then us-east-1. Credentials resolve via the default
	// AWS chain (shared-credentials file included).
	Region string
	// GateModel is reserved for a networked relevance gate; v1 Relevant is
	// heuristic-only and never calls it. Defaults to claude-haiku-4-5.
	GateModel string
	// ThinkModel thinks for thing-voices. Defaults to claude-haiku-4-5.
	ThinkModel string
	// PersonModel thinks for person-voices and any turn whose trigger carries
	// terms (negotiation deserves the better model). Defaults to
	// claude-sonnet-4-6.
	PersonModel string
	// HTTPClient overrides the SDK's HTTP client; nil uses the default.
	// Tests inject a fake transport here.
	HTTPClient *http.Client
	// OnUsage, when set, observes per-think token usage (cache reads
	// included). Called from Think, off the orchestrator lock; nil is ok.
	OnUsage func(model string, inputTokens, outputTokens, cacheReadTokens int)
}

type Bedrock struct {
	cfg    Config
	client *awsbedrock.MantleClient
}

var _ brain.Brain = (*Bedrock)(nil)

// New builds the adapter. It resolves AWS credentials eagerly (a clear error
// at boot beats a silent voice at first think).
func New(cfg Config) (*Bedrock, error) {
	if cfg.GateModel == "" {
		cfg.GateModel = DefaultGateModel
	}
	if cfg.ThinkModel == "" {
		cfg.ThinkModel = DefaultThinkModel
	}
	if cfg.PersonModel == "" {
		cfg.PersonModel = DefaultPersonModel
	}
	region := cfg.Region
	if region == "" && os.Getenv("AWS_REGION") == "" && os.Getenv("AWS_DEFAULT_REGION") == "" {
		region = "us-east-1"
	}
	var opts []option.RequestOption
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	client, err := awsbedrock.NewMantleClient(context.Background(),
		awsbedrock.MantleClientConfig{AWSRegion: region}, opts...)
	if err != nil {
		return nil, fmt.Errorf("bedrock: %w", err)
	}
	return &Bedrock{cfg: cfg, client: client}, nil
}

// Relevant is the cheap gate, called under the orchestrator lock: pure
// heuristics, never I/O (see the package doc for why the Haiku fallback was
// skipped in v1).
//
//  1. addressed (trigger.To contains my voice) → relevant
//  2. my principal speaks (trigger.From is my principal pseudo-voice,
//     derived by the same rule as everywhere: "voice:her-agent" →
//     "voice:principal:her") → relevant
//  3. a hail → relevant for every thing-voice (the shop bids, the door
//     comments); person-voices ignore hails unless addressed
//  4. everything else → not relevant
func (b *Bedrock) Relevant(_ context.Context, v brain.VoiceView) (bool, error) {
	if slices.Contains(v.Trigger.To, v.Self.Voice) {
		return true, nil
	}
	if v.Trigger.From == principalOf(v.Self.Voice) {
		return true, nil
	}
	if v.Trigger.Kind == protocol.KindHail && v.Self.Kind == protocol.VoiceThing {
		return true, nil
	}
	return false, nil
}

// principalOf derives the principal pseudo-voice for an agent voice:
// "voice:her-agent" → "voice:principal:her" (the PrincipalSays rule).
func principalOf(voice string) string {
	bare := strings.TrimSuffix(strings.TrimPrefix(voice, "voice:"), "-agent")
	return "voice:principal:" + bare
}

// wireAction is the structured-output shape; actionSchema mirrors it.
type wireAction struct {
	Speak bool       `json:"speak"`
	Kind  string     `json:"kind"`
	To    []string   `json:"to"`
	Body  string     `json:"body"`
	Terms *wireTerms `json:"terms"`
}

type wireTerms struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// actionSchema mirrors brain.Action for output_config.format. settle is
// excluded from the kind enum on purpose: settles are synthesized by the
// lifecycle only, and the orchestrator's gate would drop a spoken one anyway.
var actionSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []string{"speak", "kind", "to", "body", "terms"},
	"properties": map[string]any{
		"speak": map[string]any{
			"type":        "boolean",
			"description": "false is silence; the other fields are ignored then",
		},
		"kind": map[string]any{
			"type": "string",
			"enum": []string{"say", "hail", "propose", "accept", "decline", "withdraw", "ask_principal"},
		},
		"to": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "voice ids to address; empty addresses the whole scope",
		},
		"body": map[string]any{
			"type":        "string",
			"description": "one or two sentences, lowercase, calm",
		},
		"terms": map[string]any{
			"description": "terms carried by a propose or accept; null otherwise",
			"anyOf": []any{
				map[string]any{"type": "null"},
				map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"type", "value"},
					"properties": map[string]any{
						"type": map[string]any{
							"type":        "string",
							"description": "a term type from your mandate",
						},
						"value": map[string]any{
							"description": "the value for that term type — the shape the record shows for it: a number, a string, or an object",
						},
					},
				},
			},
		},
	},
}

// Think builds the prompt, calls the model, and parses the structured output
// into a brain.Action. Malformed, refused, or empty output is silence with a
// nil error — a confused model must never poison the world; transport and API
// errors return as errors, which the orchestrator drops as think.error. A
// panic anywhere inside is recovered into an error (belt for the brain.Brain
// no-panic contract).
func (b *Bedrock) Think(ctx context.Context, v brain.VoiceView) (a brain.Action, err error) {
	defer func() {
		if r := recover(); r != nil {
			a, err = brain.Action{}, fmt.Errorf("bedrock: think panic: %v", r)
		}
	}()

	model := b.cfg.ThinkModel
	if v.Self.Kind == protocol.VoicePerson || v.Trigger.Terms != nil {
		model = b.cfg.PersonModel
	}

	// System: stable first. The ephemeral breakpoint sits on the charter
	// block — the last stable block — so prefix and charter cache together,
	// one entry per voice. Everything volatile lives in the user message.
	msg, err := b.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: SystemPrefix},
			{Text: renderCharter(v.Self), CacheControl: anthropic.NewCacheControlEphemeralParam()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(renderView(v))),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: actionSchema},
		},
	})
	if err != nil {
		return brain.Action{}, fmt.Errorf("bedrock: think: %w", err)
	}

	if b.cfg.OnUsage != nil {
		b.cfg.OnUsage(model,
			int(msg.Usage.InputTokens),
			int(msg.Usage.OutputTokens),
			int(msg.Usage.CacheReadInputTokens))
	}

	if msg.StopReason == anthropic.StopReasonRefusal {
		return brain.Action{}, nil // a refusal is silence
	}
	var text string
	for _, blk := range msg.Content {
		if blk.Type == "text" {
			text = blk.Text
			break
		}
	}
	if strings.TrimSpace(text) == "" {
		return brain.Action{}, nil // nothing said is silence
	}
	var w wireAction
	if err := json.Unmarshal([]byte(text), &w); err != nil {
		return brain.Action{}, nil // malformed output is silence, never poison
	}
	return w.toAction(), nil
}

// toAction converts the wire shape into a brain.Action, re-marshaling the
// terms value to RawMessage. Anything off-protocol degrades to silence.
func (w wireAction) toAction() brain.Action {
	if !w.Speak {
		return brain.Action{}
	}
	kind := protocol.Kind(w.Kind)
	switch kind {
	case protocol.KindSay, protocol.KindHail, protocol.KindPropose,
		protocol.KindAccept, protocol.KindDecline, protocol.KindWithdraw,
		protocol.KindAskPrincipal:
	default:
		return brain.Action{} // settle and the unknown both die here
	}
	a := brain.Action{Speak: true, Kind: kind, To: w.To, Body: w.Body}
	if w.Terms != nil {
		raw, err := json.Marshal(w.Terms.Value)
		if err != nil {
			return brain.Action{}
		}
		a.Terms = &protocol.Terms{Type: w.Terms.Type, Value: raw}
	}
	return a
}
