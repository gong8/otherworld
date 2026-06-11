// Package runtime gives each voice a mailbox and a goroutine, supervised:
// a panic kills one turn, not the voice — the loop continues, and the crash
// hook lets the orchestrator mark in-flight exchanges interrupted (never
// silent loss).
package runtime

import (
	"context"
	"sync"

	"otherworld/fabric/internal/protocol"
)

// Handler is the function invoked for every envelope delivered to a voice.
type Handler func(ctx context.Context, env protocol.Envelope)

// Runtime manages a set of voice mailboxes, each served by a goroutine.
// The zero value is not valid; use New or NewWithCrashHook.
type Runtime struct {
	mu        sync.RWMutex
	mailboxes map[string]chan protocol.Envelope
	crashHook func(voice string)
}

// New returns a Runtime with a no-op crash hook.
func New() *Runtime { return NewWithCrashHook(func(string) {}) }

// NewWithCrashHook returns a Runtime that calls hook(voice) whenever a
// handler panics. The hook is called synchronously inside the goroutine that
// caught the panic, so it must not block.
func NewWithCrashHook(hook func(voice string)) *Runtime {
	return &Runtime{mailboxes: map[string]chan protocol.Envelope{}, crashHook: hook}
}

// Spawn registers voice and starts its dispatch goroutine. If the voice is
// already registered, Spawn is a no-op. The goroutine runs until ctx is
// canceled. The mailbox is NOT removed from the map when the context is
// canceled — callers that want Deliver to return false must also call Despawn.
func (r *Runtime) Spawn(ctx context.Context, voice string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.mailboxes[voice]; exists {
		return
	}
	mb := make(chan protocol.Envelope, 64)
	r.mailboxes[voice] = mb
	go r.loop(ctx, voice, mb, h)
}

// loop is the per-voice dispatch goroutine. It exits when ctx is done.
func (r *Runtime) loop(ctx context.Context, voice string, mb chan protocol.Envelope, h Handler) {
	for {
		select {
		case <-ctx.Done():
			return
		case env := <-mb:
			r.handleOne(ctx, voice, env, h)
		}
	}
}

// handleOne isolates recover so one poisoned envelope kills one turn, not
// the loop.
func (r *Runtime) handleOne(ctx context.Context, voice string, env protocol.Envelope, h Handler) {
	defer func() {
		if rec := recover(); rec != nil {
			r.crashHook(voice)
		}
	}()
	h(ctx, env)
}

// Deliver enqueues env on the named voice's mailbox. Returns false if the
// voice is not registered or its mailbox is full (backpressure; caller
// decides how to handle). Never blocks.
func (r *Runtime) Deliver(voice string, env protocol.Envelope) bool {
	r.mu.RLock()
	mb, ok := r.mailboxes[voice]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case mb <- env:
		return true
	default:
		return false // mailbox full: backpressure, caller decides
	}
}

// Despawn removes voice from the mailbox map so that subsequent Deliver calls
// return false. The dispatch goroutine is NOT stopped here — it will exit
// when its context is canceled. The composition root is expected to cancel the
// per-voice context AND call Despawn so that both the goroutine and the map
// entry are cleaned up.
func (r *Runtime) Despawn(voice string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mailboxes, voice)
}
