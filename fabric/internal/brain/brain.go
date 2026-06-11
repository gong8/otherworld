// Package brain is the cognition seam. The orchestrator depends on Brain;
// adapters (fake, bedrock) implement it. The core never imports an SDK.
package brain

import (
	"context"

	"otherworld/fabric/internal/protocol"
)

// VoiceView is everything a voice may consider on its turn.
// Recent/Trigger share Terms and To backing with the record; treat as
// read-only.
type VoiceView struct {
	Self    protocol.Charter
	Scope   string
	Recent  []protocol.Envelope // transcript window, oldest first
	Trigger protocol.Envelope   // the utterance being responded to
	State   map[string]any      // own thing-state (nil for persons)
	Marks   int
}

// Action is what a voice decides. The zero value is silence: a voice speaks
// only by setting Speak.
type Action struct {
	Speak bool
	Kind  protocol.Kind
	To    []string
	Body  string
	Terms *protocol.Terms
}

// Brain is implemented by fake (scripted) and bedrock (LLM-backed) adapters.
//
// Implementations must not panic: a panic in Think crashes the voice's timer
// goroutine. The orchestrator recovers a Think panic as a belt (it reads as a
// brain error and drops as think.error), but a panic in Relevant — called
// under the orchestrator lock — is fatal to the scope.
type Brain interface {
	// Relevant is the cheap gate: should this voice think at all?
	// Implementations may ignore Recent to reduce cost.
	Relevant(ctx context.Context, v VoiceView) (bool, error)
	// Think produces the voice's action. Called only if Relevant.
	Think(ctx context.Context, v VoiceView) (Action, error)
}
