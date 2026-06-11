// Package orchestrator is the deterministic heart of the fabric. It routes
// utterances to present voices, gates thinking on relevance, debounces
// thinks, runs the exchange lifecycle (crystallize → settle/withdraw/
// abandon), enforces mandates (law 4: nothing is proposed or settled outside
// a voice's charter), applies settled terms to the world (law 6), caps turns,
// and appends every surviving envelope to the event log. It is brain-free:
// all cognition arrives through the brain.Brain seam, so everything here is
// plain logic, fully testable with fake clocks and fake brains.
//
// # Concurrency model
//
// A single mutex guards all orchestrator state. Every entry point
// (PrincipalSays, Inject, AddVoice, WorldView) takes the lock; Clock.Schedule
// callbacks re-enter through the same lock. The lock is never held across a
// brain.Think call: think snapshots the VoiceView and the voice's generation
// under the lock, releases it for the duration of Think, then reacquires and
// re-validates (generation, exchange gate) before any effect lands — a slow
// brain never freezes the scope. One think executes at a time per timer
// goroutine; a think superseded while in flight (a newer trigger or a
// re-AddVoice bumped the generation) is discarded silently on reacquire.
// Relevant, by contrast, runs at schedule time under the lock by design — it
// must stay cheap (the bedrock adapter honors this with heuristics). Because
// all timer activity in tests happens on the goroutine driving
// FakeClock.Advance, the FakeClock needs no mutex of its own, and releasing
// the lock mid-callback admits no interleaving there (one goroutine drives
// everything), so golden tests stay deterministic. With RealClock, a timer
// that has already fired when its cancel races Stop is discarded by the same
// per-voice generation counter inside the callback.
package orchestrator

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/protocol/termschema"
	"otherworld/fabric/internal/world"
)

// recentWindow is how many trailing log entries a voice sees in VoiceView.
const recentWindow = 20

type Config struct {
	Clock Clock
	World *world.World
	// TurnCap is the maximum number of turns an exchange may take before the
	// next reply is forced into a visible withdraw. Defaults to 12.
	TurnCap     int
	DebounceMin time.Duration
	DebounceMax time.Duration
	// Append is the event log sink, called exactly once per recorded
	// envelope, synchronously and WITH the orchestrator mutex held: calling
	// back into the Orchestrator deadlocks (the mutex is not reentrant), and
	// blocking here stalls the whole scope — the callback owns any buffering
	// it needs. Task 9's Broadcast must hand off to per-conn writer
	// goroutines, never write sockets synchronously.
	Append func(protocol.Envelope)
	// OnDrop, when set, observes envelopes the orchestrator silently
	// discards. Reasons: "settle.external", "relevant.error", "think.error",
	// "settle.spoken", "mandate". voice is the acting/failing voice: the
	// thinking voice at the relevant.error/think.error/mandate/settle.spoken
	// sites, env.From at settle.external. Called with the orchestrator mutex
	// held: it must not call back into the Orchestrator and must not block.
	// Zero cost when nil.
	OnDrop func(reason, voice string, env protocol.Envelope)
	// Terms, when non-nil, validates propose payloads against the proto/terms
	// schema registry before they reach the record. nil disables payload
	// validation — unit-test convenience; fabricd always sets it.
	Terms *termschema.Registry
	// Scope identifies this orchestrator's scope. Defaults to "scope:test".
	Scope string
	// RunID, when set, namespaces exchange ids: "exc_<RunID>_%026d". This
	// keeps exchange ids unique across boots in the accumulated transcript.
	// Utterance ids are left as-is ("utt_%026d") — the composition root
	// already rewrites those with its own run+scope prefix.
	RunID string
}

type Orchestrator struct {
	mu     sync.Mutex
	cfg    Config
	voices map[string]*voiceEntry
	order  []string // registration order, for deterministic broadcast
	recent []protocol.Envelope
	// exchanges: closed exchanges are tombstones — think consults them to
	// drop replies into abandoned exchanges. Reaping needs a grace period
	// ≥ one debounce window or replies into abandoned exchanges reach the
	// record as dangling-id envelopes.
	exchanges map[string]*exchange
	// excOrder lists exchanges in creation order for deterministic adoption;
	// closed ids are compacted out during the adoption scan.
	excOrder []string
	uttSeq   uint64
	excSeq   uint64
	rng      *rand.Rand
}

type voiceEntry struct {
	charter protocol.Charter
	brain   brain.Brain
	cancel  func() // cancels the one pending think; nil if none
	gen     uint64 // think generation; a stale RealClock fire is discarded
}

type exchange struct {
	id           string
	participants []string
	turns        int
	pending      *protocol.Envelope // latest pending propose (counter-offers replace it)
	closed       bool
	outcome      string // "settled" or "abandoned"
}

func New(cfg Config) *Orchestrator {
	if cfg.Clock == nil {
		panic("orchestrator: Config.Clock is required")
	}
	if cfg.TurnCap <= 0 {
		cfg.TurnCap = 12
	}
	if cfg.Scope == "" {
		cfg.Scope = "scope:test"
	}
	if cfg.DebounceMax < cfg.DebounceMin {
		cfg.DebounceMax = cfg.DebounceMin
	}
	return &Orchestrator{
		cfg:       cfg,
		voices:    map[string]*voiceEntry{},
		exchanges: map[string]*exchange{},
		// Seeded per-orchestrator; only consulted when DebounceMax > Min, so
		// tests with Min == Max are fully deterministic.
		rng: rand.New(rand.NewPCG(uint64(cfg.Clock.Now().UnixNano()), 0x6f74686572776f72)),
	}
}

func (o *Orchestrator) ScopeID() string { return o.cfg.Scope } // immutable after New

// AddVoice registers a voice. Things with initial state are registered in the
// world so settled terms have a body to land on.
func (o *Orchestrator) AddVoice(ctx context.Context, ch protocol.Charter, b brain.Brain, initState map[string]any) {
	_ = ctx
	o.mu.Lock()
	defer o.mu.Unlock()
	if old, exists := o.voices[ch.Voice]; exists {
		// Re-claim of a resident slot: a stale timer must never fire the old
		// charter/brain. Cancel the pending think and bump the generation so
		// an in-flight RealClock fire discards itself too.
		if old.cancel != nil {
			old.cancel()
			old.cancel = nil
		}
		old.gen++
	} else {
		o.order = append(o.order, ch.Voice)
	}
	o.voices[ch.Voice] = &voiceEntry{charter: ch, brain: b}
	if ch.Kind == protocol.VoiceThing && initState != nil {
		o.cfg.World.Register(ch.Voice, world.ThingState(initState))
	}
}

// WorldView returns a copy of the voice's thing-state (nil for persons).
func (o *Orchestrator) WorldView(voice string) map[string]any {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cfg.World.View(voice)
}

// Pending returns a copy of the terms on exchangeID's pending propose: nil,
// false when the exchange is unknown, closed, or has no pending propose with
// terms. The consent path (Task 11) needs these to construct the accept — the
// lifecycle requires accepts to carry terms before they can settle.
func (o *Orchestrator) Pending(exchangeID string) (*protocol.Terms, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	ex := o.exchanges[exchangeID]
	if ex == nil || ex.closed || ex.pending == nil || ex.pending.Terms == nil {
		return nil, false
	}
	cp := *ex.pending.Terms
	cp.Value = slices.Clone(cp.Value)
	return &cp, true
}

// Credit adjusts voice's marks balance under the orchestrator mutex. The
// World lives in the orchestrator's lock domain (brains read Marks during
// thinks), so runtime credits — claims granting a resident their stake — must
// come through here, never via World.Credit directly.
func (o *Orchestrator) Credit(voice string, n int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cfg.World.Credit(voice, n)
}

// PrincipalSays is the private line: the principal behind agentVoice speaks
// to their agent. "voice:her-agent" → principal "her" (strip the "voice:"
// prefix and a trailing "-agent" suffix; without the suffix, the bare name).
func (o *Orchestrator) PrincipalSays(ctx context.Context, agentVoice, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	p := strings.TrimSuffix(strings.TrimPrefix(agentVoice, "voice:"), "-agent")
	o.inject(ctx, protocol.Envelope{
		From:   "voice:principal:" + p,
		Serves: p,
		Scope:  o.cfg.Scope,
		To:     []string{agentVoice},
		Kind:   protocol.KindSay,
		Body:   text,
	})
}

// Inject records an envelope and routes it. The envelope's ID, TS and V are
// assigned here; lifecycle bookkeeping may annotate it with an exchange id.
//
// External settle envelopes are dropped — no Append, no routing. A settle on
// the record MUST mean the world changed (law 6), so settles exist only via
// the internal accept→synthesis path: settleExchange calls the private
// inject funnel directly, which carries no such filter.
func (o *Orchestrator) Inject(ctx context.Context, env protocol.Envelope) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if env.Kind == protocol.KindSettle {
		o.drop("settle.external", env.From, env)
		return
	}
	o.inject(ctx, env)
}

// drop reports a silently discarded envelope to OnDrop, if set. Lock held.
func (o *Orchestrator) drop(reason, voice string, env protocol.Envelope) {
	if o.cfg.OnDrop != nil {
		o.cfg.OnDrop(reason, voice, env)
	}
}

// inject is the single funnel for every envelope: identity, lifecycle,
// exactly one Append, settlement synthesis, then routing. Lock held;
// re-entrant via synthesized envelopes (settle, decline, withdraw).
func (o *Orchestrator) inject(ctx context.Context, env protocol.Envelope) {
	o.uttSeq++
	env.V = 0
	env.ID = fmt.Sprintf("utt_%026d", o.uttSeq)
	env.TS = o.cfg.Clock.Now()
	if env.Scope == "" {
		env.Scope = o.cfg.Scope
	}

	settling := o.lifecycle(&env)

	o.recent = append(o.recent, env)
	if len(o.recent) > recentWindow {
		o.recent = o.recent[len(o.recent)-recentWindow:]
	}
	if o.cfg.Append != nil {
		o.cfg.Append(env)
	}

	if settling != nil {
		o.settleExchange(ctx, settling, env)
	}

	o.route(ctx, env)
}

// lifecycle runs exchange bookkeeping before Append, possibly annotating env
// with an exchange id. It returns a non-nil exchange when env is an accept
// matching that exchange's pending propose — settlement happens after the
// accept itself is appended.
func (o *Orchestrator) lifecycle(env *protocol.Envelope) *exchange {
	// Adoption: an accept/decline without an exchange id whose To intersects
	// an open exchange's participants inherits that exchange (first open
	// exchange in creation order wins). The scan compacts closed ids out of
	// excOrder as it goes; the exchanges map keeps them as tombstones.
	if env.Exchange == "" && (env.Kind == protocol.KindAccept || env.Kind == protocol.KindDecline) {
		open := o.excOrder[:0]
		for _, id := range o.excOrder {
			ex := o.exchanges[id]
			if ex.closed {
				continue
			}
			open = append(open, id)
			if env.Exchange == "" && intersects(env.To, ex.participants) {
				env.Exchange = id
			}
		}
		o.excOrder = open
	}

	if env.Kind == protocol.KindPropose && env.Exchange == "" {
		// A bare propose crystallizes a new exchange.
		o.excSeq++
		var id string
		if o.cfg.RunID != "" {
			id = fmt.Sprintf("exc_%s_%026d", o.cfg.RunID, o.excSeq)
		} else {
			id = fmt.Sprintf("exc_%026d", o.excSeq)
		}
		env.Exchange = id
		cp := *env
		ex := &exchange{id: id, participants: union(env.From, env.To), turns: 1, pending: &cp}
		o.exchanges[id] = ex
		o.excOrder = append(o.excOrder, id)
		return nil
	}

	if env.Exchange == "" {
		return nil
	}
	ex := o.exchanges[env.Exchange]
	if ex == nil || ex.closed {
		return nil // unknown or closed exchange: record only, no bookkeeping
	}
	ex.turns++
	switch env.Kind {
	case protocol.KindPropose:
		// Counter-offer: replaces the pending propose.
		cp := *env
		ex.pending = &cp
	case protocol.KindAccept:
		// An accept matches the pending propose when a participant other
		// than the pending proposer speaks it. (Adopted outsiders are
		// recorded against the exchange but cannot settle it.)
		if ex.pending != nil && ex.pending.Terms != nil &&
			env.From != ex.pending.From && slices.Contains(ex.participants, env.From) {
			return ex
		}
	case protocol.KindWithdraw:
		ex.closed, ex.outcome = true, "abandoned"
	}
	return nil
}

// settleExchange runs after an accept matching ex's pending propose was
// appended: apply the terms to the world, then synthesize a settle — or, if
// Apply refuses, a decline carrying the error — and close the exchange.
//
// World.Apply owner rule: the first exchange participant (in From-then-To
// order at exchange open) whose charter Kind == VoiceThing — that is the
// thing whose body the terms change; if no participant is a thing, the
// settle's From. With more than one thing participant, the first in
// participant order wins.
func (o *Orchestrator) settleExchange(ctx context.Context, ex *exchange, accept protocol.Envelope) {
	owner := accept.From
	for _, p := range ex.participants {
		if ve := o.voices[p]; ve != nil && ve.charter.Kind == protocol.VoiceThing {
			owner = p
			break
		}
	}
	if err := o.cfg.World.Apply(owner, *ex.pending.Terms); err != nil {
		ex.closed, ex.outcome = true, "abandoned"
		o.inject(ctx, protocol.Envelope{
			From: accept.From, Serves: accept.Serves, Scope: o.cfg.Scope,
			To: []string{ex.pending.From}, Kind: protocol.KindDecline,
			Exchange: ex.id, Body: err.Error(),
		})
		return
	}
	ex.closed, ex.outcome = true, "settled" // closed before inject: its lifecycle is a no-op
	o.inject(ctx, protocol.Envelope{
		From: accept.From, Serves: accept.Serves, Scope: o.cfg.Scope,
		To: others(ex.participants, accept.From), Kind: protocol.KindSettle,
		Exchange: ex.id, Terms: ex.pending.Terms,
	})
}

// route delivers env into thinks. Addressed envelopes go to their To list;
// unaddressed ones broadcast to every voice in the scope except the sender.
// Principal pseudo-voices never receive (they are not registered voices).
// ask_principal routes only to the named principal — a pseudo-voice here —
// so it reaches no brain; the gateway consumes it from the log (Task 9/11).
func (o *Orchestrator) route(ctx context.Context, env protocol.Envelope) {
	if env.Kind == protocol.KindAskPrincipal {
		return
	}
	targets := env.To
	if len(targets) == 0 {
		targets = o.order
	}
	for _, name := range targets {
		if name == env.From {
			continue
		}
		if ve := o.voices[name]; ve != nil {
			o.scheduleThink(ctx, ve, env)
		}
	}
}

// scheduleThink gates on relevance now, then debounces the think. Only one
// think may be pending per voice: a newer trigger replaces the older one.
// Note that Relevant runs at schedule time — under the lock, so it must stay
// cheap — and therefore an irrelevant trigger does NOT displace a pending
// think; moving Relevant to fire time would change that semantics.
func (o *Orchestrator) scheduleThink(ctx context.Context, ve *voiceEntry, trigger protocol.Envelope) {
	rel, err := ve.brain.Relevant(ctx, o.view(ve, trigger))
	if err != nil {
		o.drop("relevant.error", ve.charter.Voice, trigger)
		return // a brain error reads as "not relevant"
	}
	if !rel {
		return
	}
	// Detach the context: a debounced think outlives its triggering call.
	// With RealClock and the Task 9 gateway, request-scoped contexts are
	// canceled long before the timer fires — every think would then fail and
	// read as silence. Values survive; cancellation does not propagate.
	ctx = context.WithoutCancel(ctx)
	if ve.cancel != nil {
		ve.cancel()
		ve.cancel = nil
	}
	ve.gen++
	gen := ve.gen
	d := o.cfg.DebounceMin
	if spread := o.cfg.DebounceMax - o.cfg.DebounceMin; spread > 0 {
		d += time.Duration(o.rng.Int64N(int64(spread) + 1))
	}
	ve.cancel = o.cfg.Clock.Schedule(d, func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		if ve.gen != gen {
			return // superseded; this timer fired before its cancel landed
		}
		ve.cancel = nil
		o.think(ctx, ve, trigger)
	})
}

// view builds what the voice may consider on this turn.
func (o *Orchestrator) view(ve *voiceEntry, trigger protocol.Envelope) brain.VoiceView {
	recent := make([]protocol.Envelope, len(o.recent))
	copy(recent, o.recent)
	return brain.VoiceView{
		Self:    ve.charter,
		Scope:   o.cfg.Scope,
		Recent:  recent,
		Trigger: trigger,
		State:   o.cfg.World.View(ve.charter.Voice), // nil for persons
		Marks:   o.cfg.World.Marks(ve.charter.Voice),
	}
}

// exchangeGate vets trigger's exchange and reports whether the think may
// proceed. Lock held. think runs it twice — before releasing the lock for
// the brain call and again after reacquiring, because the exchange may close
// or reach the cap while the voice thinks. A reply into a closed-abandoned
// exchange is swallowed silently; a reply that would exceed the turn cap
// becomes a visible withdraw, emitted here under the lock as ever.
func (o *Orchestrator) exchangeGate(ctx context.Context, ve *voiceEntry, trigger protocol.Envelope) bool {
	ex := o.exchanges[trigger.Exchange]
	if trigger.Exchange == "" || ex == nil {
		return true
	}
	if ex.closed && ex.outcome == "abandoned" {
		return false // dead exchange: no further replies into it
	}
	if !ex.closed && ex.turns >= o.cfg.TurnCap {
		// Turn cap: the reply that would exceed the cap becomes a
		// visible withdraw; lifecycle closes the exchange as abandoned.
		o.inject(ctx, protocol.Envelope{
			From: ve.charter.Voice, Serves: ve.charter.Serves, Scope: o.cfg.Scope,
			To:   others(ex.participants, ve.charter.Voice),
			Kind: protocol.KindWithdraw, Exchange: ex.id, Body: "turn cap reached",
		})
		return false
	}
	return true
}

// think fires a debounced think: exchange gate, brain call, speak gate,
// mandate gate, then the surviving action becomes an envelope. The mutex is
// held on entry and on return (the timer callback owns lock/unlock), but
// never across the brain call:
//
//	phase 1 (locked):   exchange gate; snapshot VoiceView and generation.
//	phase 2 (unlocked): ve.brain.Think — the only code that runs off the lock.
//	phase 3 (relocked): discard if superseded (gen mismatch covers newer
//	  triggers and re-AddVoice); think.error OnDrop, under the lock per its
//	  contract; exchange gate re-run (the exchange may have closed or capped
//	  while we thought); speak, settle.spoken, mandate and terms gates; inject.
func (o *Orchestrator) think(ctx context.Context, ve *voiceEntry, trigger protocol.Envelope) {
	if !o.exchangeGate(ctx, ve, trigger) {
		return
	}
	view := o.view(ve, trigger) // value snapshot, safe off the lock
	gen := ve.gen
	o.mu.Unlock()
	a, err := ve.brain.Think(ctx, view)
	o.mu.Lock()
	if ve.gen != gen {
		return // superseded while thinking: the result is stale, discard silently
	}
	if err != nil {
		o.drop("think.error", ve.charter.Voice, trigger)
		return // errors are silence
	}
	if !o.exchangeGate(ctx, ve, trigger) {
		return // the exchange closed or capped while we thought
	}
	if !a.Speak {
		return // the zero value is silence
	}
	env := protocol.Envelope{
		From: ve.charter.Voice, Serves: ve.charter.Serves, Scope: o.cfg.Scope,
		To: a.To, Kind: a.Kind, Body: a.Body, Terms: a.Terms,
		Exchange: trigger.Exchange, // replies inherit the trigger's exchange
	}
	// MANDATE GATE (law 4): a propose whose terms are missing or outside the
	// charter dies here, silently — it never reaches the record. A settle is
	// dropped unconditionally, mandate or not: settles are synthesized by
	// the lifecycle exclusively; a spoken settle would let a voice lie about
	// state (law 6).
	if a.Kind == protocol.KindSettle {
		o.drop("settle.spoken", ve.charter.Voice, env)
		return
	}
	if a.Kind == protocol.KindPropose {
		if a.Terms == nil || !slices.Contains(ve.charter.Mandate.MayProposeTerms, a.Terms.Type) {
			o.drop("mandate", ve.charter.Voice, env)
			return
		}
		// SCHEMA GATE (law 6, defense-in-depth): validate the payload against
		// the proto/terms registry. Order: mandate first (cheaper), schema
		// second. nil registry disables validation (unit-test convenience).
		if o.cfg.Terms != nil {
			if err := o.cfg.Terms.Validate(*a.Terms); err != nil {
				o.drop("terms.invalid", ve.charter.Voice, env)
				return
			}
		}
	}
	o.inject(ctx, env)
}

func union(from string, to []string) []string {
	out := []string{from}
	for _, t := range to {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

func others(all []string, except string) []string {
	var out []string
	for _, p := range all {
		if p != except {
			out = append(out, p)
		}
	}
	return out
}

func intersects(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}
