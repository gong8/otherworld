// Package brain is the cognition seam. The orchestrator depends on Brain;
// adapters (fake, bedrock) implement it. The core never imports an SDK.
package brain

import (
	"context"

	"otherworld/fabric/internal/protocol"
)

// VoiceView is everything a voice may consider on its turn.
type VoiceView struct {
	Self    protocol.Charter
	Scope   string
	Recent  []protocol.Envelope // transcript window, oldest first
	Trigger protocol.Envelope   // the utterance being responded to
	State   map[string]any      // own thing-state (nil for persons)
	Marks   int
}

// Action is what a voice decides. Quiet means say nothing this turn.
type Action struct {
	Quiet bool
	Kind  protocol.Kind
	To    []string
	Body  string
	Terms *protocol.Terms
}

type Brain interface {
	// Relevant is the cheap gate: should this voice think at all?
	Relevant(ctx context.Context, v VoiceView) (bool, error)
	// Think produces the voice's action. Called only if Relevant.
	Think(ctx context.Context, v VoiceView) (Action, error)
}
