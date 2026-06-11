// compose.go is the composition root's wiring: one server struct in which
// every package becomes the running world. Lock domains, in one place:
//
//   - Each orchestrator owns its scope's state (voices, exchanges, World)
//     under its own mutex. Its Append/OnDrop callbacks run UNDER that mutex:
//     they only enqueue/log, never block, never call back in.
//   - The store is hit by exactly one writer goroutine per scope (order), by
//     the replay/claim paths (pool-safe), and by the purge loop.
//   - server.mu guards the composition root's own maps: claims, the exchange
//     index, and the shadow marks ledger. Critical sections are tiny and
//     never call into an orchestrator.
//
// State reads for /v0/state: thing-state comes from Orchestrator.WorldView,
// which takes the orchestrator mutex (checked — safe from HTTP handlers).
// Marks have no such accessor, so the root keeps a SHADOW ledger, credited on
// claim and updated from trade settles in the append pipeline; /v0/state
// reads the shadow, never the orchestrator-owned World.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"time"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/gateway"
	"otherworld/fabric/internal/orchestrator"
	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/scenes"
	"otherworld/fabric/internal/store"
	"otherworld/fabric/internal/world"
)

// appendQueueSize is each scope's persist-then-broadcast buffer. Append runs
// under the orchestrator mutex, so an overflow drops (loudly) — never blocks.
const appendQueueSize = 1024

// claimNameRE is the claimable resident name shape (after lowercasing).
var claimNameRE = regexp.MustCompile(`^[a-z0-9-]{1,24}$`)

// scopeState is one scope's slice of the world: its orchestrator, its append
// queue, and the seed metadata the root needs at runtime.
type scopeState struct {
	orch    *orchestrator.Orchestrator
	appendQ chan protocol.Envelope
	murmurs []scenes.Murmur
	serves  map[string]string // seed voice → serves, for ambient murmurs
	things  []string          // seed thing voices, for /v0/state
	// personCap bounds claimed person voices (household 2, street 32).
	personCap int
	// showMarks: street state includes the marks ledger; household does not.
	showMarks bool
}

type exchangeInfo struct{ scope, proposer string }

type server struct {
	cfg    config
	store  *store.Store
	gw     *gateway.Gateway
	scopes map[string]*scopeState
	// runID namespaces per-boot identifiers: the orchestrator numbers
	// utterances from 1 each boot, but utterances.id is UNIQUE across boots —
	// the transcript accumulates; only the world is fresh.
	runID string

	ctx  context.Context // root lifetime: bounds tickers, loops, and injects
	stop chan struct{}   // closed by close(): writers drain their queues and exit
	wg   sync.WaitGroup

	mu        sync.Mutex
	claimed   map[string]string         // claimed agent voice → scope
	persons   map[string]int            // scope → claimed person count
	exchanges map[string]exchangeInfo   // exchange id → owner, from the append stream
	marks     map[string]map[string]int // scope → voice → marks (shadow ledger)
}

// newServer composes the world: store, two orchestrators (household, street),
// gateway, seeds, and the background loops. ctx bounds everything but the
// writers, which drain on close() so accepted utterances reach the store.
func newServer(ctx context.Context, cfg config) (*server, error) {
	st, err := store.Open(ctx, cfg.databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		st.Close()
		return nil, fmt.Errorf("run id: %w", err)
	}
	s := &server{
		cfg:       cfg,
		store:     st,
		scopes:    map[string]*scopeState{},
		runID:     hex.EncodeToString(nonce[:]),
		ctx:       ctx,
		stop:      make(chan struct{}),
		claimed:   map[string]string{},
		persons:   map[string]int{},
		exchanges: map[string]exchangeInfo{},
		marks:     map[string]map[string]int{},
	}
	s.gw = gateway.New(gateway.Config{
		OnPrincipalSay: s.onPrincipalSay,
		OnClaim:        s.onClaim,
		OnConsent:      s.onConsent,
		Replay:         s.replay,
		StateView:      s.stateView,
		Origins:        cfg.origins,
	})

	hhScope, hhSeeds := scenes.Household()
	stScope, stSeeds := scenes.Street()
	if err := s.addScope(ctx, hhScope, hhSeeds, 2, false); err != nil {
		st.Close()
		return nil, err
	}
	if err := s.addScope(ctx, stScope, stSeeds, 32, true); err != nil {
		st.Close()
		return nil, err
	}

	for _, sc := range s.scopes {
		s.wg.Add(2)
		go s.writer(sc)
		go s.ambient(sc)
	}
	s.wg.Add(1)
	go s.purgeLoop()
	return s, nil
}

// addScope builds one scope: a World, an orchestrator over it, and the seeds.
// Every seed is credited 0 marks (the ledger knows it; the corner shop
// accumulates from there) and upserted into the voices table.
func (s *server) addScope(ctx context.Context, scope string, seeds []scenes.Seed, personCap int, showMarks bool) error {
	sc := &scopeState{
		appendQ:   make(chan protocol.Envelope, appendQueueSize),
		murmurs:   scenes.Murmurs(scope),
		serves:    map[string]string{},
		personCap: personCap,
		showMarks: showMarks,
	}
	sc.orch = orchestrator.New(orchestrator.Config{
		Clock:       orchestrator.RealClock{},
		World:       world.New(),
		TurnCap:     12,
		DebounceMin: s.cfg.debounceMin,
		DebounceMax: s.cfg.debounceMax,
		Scope:       scope,
		// Append runs under the orchestrator mutex: enqueue only, never
		// block. A full queue drops the envelope — loudly; the writer
		// goroutine owns persist-then-broadcast order.
		Append: func(env protocol.Envelope) {
			select {
			case sc.appendQ <- env:
			default:
				slog.Error("append queue full: envelope dropped",
					"scope", env.Scope, "id", env.ID, "kind", env.Kind, "from", env.From)
			}
		},
		OnDrop: func(reason, voice string, env protocol.Envelope) {
			slog.Warn("orchestrator drop", "reason", reason, "voice", voice, "kind", env.Kind, "scope", scope)
		},
	})
	s.marks[scope] = map[string]int{}
	for _, seed := range seeds {
		sc.orch.AddVoice(ctx, seed.Charter, brain.NewFake(seed.Rules), seed.State)
		sc.orch.Credit(seed.Charter.Voice, 0)
		s.marks[scope][seed.Charter.Voice] = 0
		sc.serves[seed.Charter.Voice] = seed.Charter.Serves
		if seed.Charter.Kind == protocol.VoiceThing {
			sc.things = append(sc.things, seed.Charter.Voice)
		}
		data, err := json.Marshal(seed.Charter)
		if err == nil {
			err = s.store.UpsertVoice(ctx, seed.Charter.Voice, scope, data)
		}
		if err != nil {
			return fmt.Errorf("seed %s: %w", seed.Charter.Voice, err)
		}
	}
	s.scopes[scope] = sc
	return nil
}

// close drains and stops the writers, then closes the store. The caller must
// cancel the root ctx FIRST (the ambient and purge loops exit on it) or the
// wait below never returns.
func (s *server) close() {
	close(s.stop)
	s.wg.Wait()
	s.store.Close()
}

// ── the persist-then-broadcast pipeline ──────────────────────────────────────

// writer is scope's single pipeline goroutine: store first, broadcast second,
// in append order. On stop it drains what the orchestrator already accepted,
// then exits. The queue is never closed — a debounced think's timer may fire
// during shutdown and Append once more; that envelope parks in the buffer.
func (s *server) writer(sc *scopeState) {
	defer s.wg.Done()
	for {
		select {
		case env := <-sc.appendQ:
			s.record(env)
		case <-s.stop:
			for {
				select {
				case env := <-sc.appendQ:
					s.record(env)
				default:
					return
				}
			}
		}
	}
}

// record persists one envelope, then broadcasts it with its real seq. On
// store failure the broadcast is SKIPPED: the record is the product; the feed
// never shows what isn't recorded.
func (s *server) record(env protocol.Envelope) {
	// Globally unique utterance id: each orchestrator numbers utterances
	// from 1 each boot, so prefix the ordinal with the run id AND the scope
	// — the accumulated transcript never collides across boots or scopes.
	env.ID = fmt.Sprintf("utt_%s_%s_%s",
		s.runID, strings.TrimPrefix(env.Scope, "scope:"), strings.TrimPrefix(env.ID, "utt_"))
	s.index(env)
	payload, err := json.Marshal(env)
	if err != nil {
		slog.Error("append: marshal failed", "id", env.ID, "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	seq, err := s.store.AppendUtterance(ctx, env.ID, env.TS, env.Scope, payload)
	cancel()
	if err != nil {
		slog.Error("append: store failed; envelope NOT broadcast",
			"id", env.ID, "kind", env.Kind, "scope", env.Scope, "err", err)
		return
	}
	s.gw.Broadcast(seq, env)
}

// index maintains the root's view of the append stream: which scope owns each
// exchange and who proposed last (the consent path needs both), plus the
// shadow marks ledger trade settles move. Runs on the writer goroutine, so a
// propose is always indexed before the ask_principal that follows it reaches
// any private line.
func (s *server) index(env protocol.Envelope) {
	switch env.Kind {
	case protocol.KindPropose:
		if env.Exchange != "" {
			s.mu.Lock()
			s.exchanges[env.Exchange] = exchangeInfo{scope: env.Scope, proposer: env.From}
			s.mu.Unlock()
		}
	case protocol.KindSettle, protocol.KindWithdraw:
		if env.Kind == protocol.KindSettle && env.Terms != nil && env.Terms.Type == "trade" {
			var v struct {
				PriceMarks int    `json:"price_marks"`
				Buyer      string `json:"buyer"`
				Seller     string `json:"seller"`
			}
			if err := json.Unmarshal(env.Terms.Value, &v); err == nil {
				s.mu.Lock()
				if ledger := s.marks[env.Scope]; ledger != nil {
					ledger[v.Buyer] -= v.PriceMarks
					ledger[v.Seller] += v.PriceMarks
				}
				s.mu.Unlock()
			}
		}
		s.mu.Lock()
		delete(s.exchanges, env.Exchange) // closed: the index forgets it
		s.mu.Unlock()
	}
}

// ── gateway callbacks ────────────────────────────────────────────────────────

// onClaim grants name a resident agent voice in scope: charter + fake brain
// on the scope's orchestrator, 100 marks, a voices row, and a presence row
// with a 24h TTL (law 7: the door forgets). Slot-free/leave is NOT in v1;
// gateway.Revoke is the seam when it arrives.
func (s *server) onClaim(scope, name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !claimNameRE.MatchString(name) {
		return "", fmt.Errorf("name must be 1-24 chars: lowercase letters, digits, hyphens")
	}
	sc := s.scopes[scope]
	if sc == nil {
		return "", fmt.Errorf("unknown scope %q", scope)
	}
	voice := "voice:" + name + "-agent"

	s.mu.Lock()
	if _, taken := s.claimed[voice]; taken {
		s.mu.Unlock()
		return "", fmt.Errorf("name %q is taken", name)
	}
	if s.persons[scope] >= sc.personCap {
		s.mu.Unlock()
		return "", fmt.Errorf("%s is full", scope)
	}
	s.claimed[voice] = scope
	s.persons[scope]++
	s.marks[scope][voice] += 100
	s.mu.Unlock()

	charter := scenes.ResidentCharter(voice, name)
	sc.orch.AddVoice(s.ctx, charter, brain.NewFake(scenes.ResidentAgentRules()), nil)
	sc.orch.Credit(voice, 100)

	// Store failures here are logged, not fatal: the claim is live in-memory
	// and the transcript (the product) flows through the writer regardless.
	if data, err := json.Marshal(charter); err == nil {
		if err := s.store.UpsertVoice(s.ctx, voice, scope, data); err != nil {
			slog.Error("claim: upsert voice failed", "voice", voice, "err", err)
		}
	}
	now := time.Now().UTC()
	prsID := fmt.Sprintf("prs_%s_%s", s.runID, name)
	if err := s.store.InsertPresence(s.ctx, prsID, scope, voice, "entered", now, now.Add(24*time.Hour)); err != nil {
		slog.Error("claim: insert presence failed", "voice", voice, "err", err)
	}
	slog.Info("claim", "scope", scope, "name", name, "voice", voice)
	return voice, nil
}

// onPrincipalSay routes line text to the orchestrator owning the claimed
// voice's scope.
func (s *server) onPrincipalSay(voice, text string) {
	s.mu.Lock()
	scope, ok := s.claimed[voice]
	s.mu.Unlock()
	if !ok {
		slog.Warn("line: say from unclaimed voice", "voice", voice)
		return
	}
	s.scopes[scope].orch.PrincipalSays(s.ctx, voice, text)
}

// onConsent resolves an ask_principal. Approve injects an accept From the
// agent voice carrying the exchange's pending terms (the lifecycle requires
// accepts to carry terms to settle); refuse injects a decline. To is the
// pending proposer — the other exchange party — known from the append index.
func (s *server) onConsent(exchangeID, voice string, approve bool) {
	s.mu.Lock()
	info, ok := s.exchanges[exchangeID]
	s.mu.Unlock()
	if !ok {
		slog.Warn("consent: unknown exchange", "exchange", exchangeID, "voice", voice)
		return
	}
	sc := s.scopes[info.scope]
	if sc == nil {
		return // unreachable: the index only ever holds known scopes
	}
	var to []string
	if info.proposer != "" && info.proposer != voice {
		to = []string{info.proposer}
	}
	serves := strings.TrimSuffix(strings.TrimPrefix(voice, "voice:"), "-agent")
	if !approve {
		sc.orch.Inject(s.ctx, protocol.Envelope{
			From: voice, Serves: serves, Scope: info.scope, To: to,
			Kind: protocol.KindDecline, Exchange: exchangeID, Body: "my principal declines.",
		})
		return
	}
	terms, ok := sc.orch.Pending(exchangeID)
	if !ok {
		slog.Warn("consent: no pending terms", "exchange", exchangeID, "voice", voice)
		return
	}
	sc.orch.Inject(s.ctx, protocol.Envelope{
		From: voice, Serves: serves, Scope: info.scope, To: to,
		Kind: protocol.KindAccept, Exchange: exchangeID,
		Body: "my principal agrees.", Terms: terms,
	})
}

// replay serves a feed's catch-up: scope's stored utterances after the
// cursor, re-wrapped as frames with their real seqs.
func (s *server) replay(scope string, after int64) []gateway.Frame {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.store.ListUtterancesSince(ctx, scope, after, 200)
	if err != nil {
		slog.Error("replay failed", "scope", scope, "err", err)
		return nil
	}
	frames := make([]gateway.Frame, 0, len(rows))
	for _, r := range rows {
		var env protocol.Envelope
		if err := json.Unmarshal(r.Payload, &env); err != nil {
			slog.Error("replay: bad payload", "seq", r.Seq, "err", err)
			continue
		}
		frames = append(frames, gateway.Frame{Seq: r.Seq, Env: env})
	}
	return frames
}

// stateView renders scope for /v0/state. Thing-state reads go through
// Orchestrator.WorldView (which takes the orchestrator mutex — safe from
// handlers, the simplest correct path); marks come from the shadow ledger.
func (s *server) stateView(scope string) any {
	sc := s.scopes[scope]
	if sc == nil {
		return map[string]any{"scope": scope}
	}
	things := map[string]any{}
	for _, voice := range sc.things {
		if st := sc.orch.WorldView(voice); st != nil {
			things[strings.TrimPrefix(voice, "voice:")] = st
		}
	}
	out := map[string]any{"scope": scope, "things": things}
	if sc.showMarks {
		s.mu.Lock()
		marks := make(map[string]int, len(s.marks[scope]))
		for v, m := range s.marks[scope] {
			marks[v] = m
		}
		s.mu.Unlock()
		out["marks"] = marks
	}
	return out
}

// ── background loops ─────────────────────────────────────────────────────────

// ambient murmurs into scope on a jittered interval — but only while someone
// watches (Viewers > 0): unwatched scopes sleep.
func (s *server) ambient(sc *scopeState) {
	defer s.wg.Done()
	if len(sc.murmurs) == 0 {
		return
	}
	for {
		d := s.cfg.ambientMin
		if spread := s.cfg.ambientMax - s.cfg.ambientMin; spread > 0 {
			d += mrand.N(spread + 1)
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(d):
		}
		scope := sc.orch.ScopeID()
		if s.gw.Viewers(scope) == 0 {
			continue
		}
		m := sc.murmurs[mrand.IntN(len(sc.murmurs))]
		sc.orch.Inject(s.ctx, protocol.Envelope{
			From: m.Voice, Serves: sc.serves[m.Voice], Scope: scope,
			Kind: protocol.KindSay, Body: m.Body,
		})
	}
}

// purgeLoop forgets expired presence every minute (law 7).
func (s *server) purgeLoop() {
	defer s.wg.Done()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if _, err := s.store.PurgeExpiredPresence(ctx, time.Now().UTC()); err != nil {
				slog.Error("presence purge failed", "err", err)
			}
			cancel()
		}
	}
}
