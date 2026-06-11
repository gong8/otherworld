// Package gateway is the fabric's only ingress and egress. It serves the
// public WebSocket feed (law 5: there is no private agent-to-agent path —
// the feed carries everything appended to the record), token-gated private
// lines (the write path from a principal to their agent, plus the targeted
// stream of envelopes addressed to that principal's pseudo-voice:
// ask_principal prompts and agent replies), the claim and consent endpoints,
// and a read-only state view. The line's privacy is about authority, not
// secrecy: only the token holder may speak as the principal, but in v1 the
// principal's says land on the public record like everything else — visible
// causality is the demo's pedagogy.
//
// The gateway never imports the orchestrator; the composition root (Task 11)
// wires the Config callbacks. Broadcast is non-blocking by contract — see
// its doc comment.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"otherworld/fabric/internal/protocol"
)

const (
	// sendBuffer is each connection's outgoing queue depth. Broadcast never
	// blocks: a connection that falls sendBuffer envelopes behind is dropped.
	sendBuffer = 256
	// writeTimeout bounds one websocket write so a wedged socket cannot pin
	// its writer goroutine past it.
	writeTimeout = 10 * time.Second
)

// Frame is one feed/line message: the envelope plus its log sequence. The
// seq is the client's reconnect cursor (?after=); envelopes themselves are
// schema-frozen, so the cursor rides a wrapper frame.
type Frame struct {
	Seq int64             `json:"seq"`
	Env protocol.Envelope `json:"env"`
}

// Config wires the gateway to its consumer. Every callback may be nil: nil
// OnPrincipalSay drops line text, nil OnClaim refuses claims (501), nil
// OnConsent makes consent a no-op, nil Replay skips catch-up, nil StateView
// makes /v0/state a 404.
type Config struct {
	// OnPrincipalSay receives text spoken on a private line: the token
	// holder speaking, as the principal, to their claimed agent voice.
	OnPrincipalSay func(voice, text string)
	// OnClaim claims name within scope and returns the agent voice the
	// caller now speaks for. An error refuses the claim (409).
	OnClaim func(scope, name string) (voice string, err error)
	// OnConsent resolves an ask_principal. voice is the claimed AGENT voice
	// (never the principal pseudo-voice): the consumer injects the accept
	// From this voice with the exchange id.
	OnConsent func(exchange, voice string, approve bool)
	// Replay returns scope's frames after the given cursor, for feed
	// connections that ask to catch up before going live.
	Replay func(scope string, after int64) []Frame
	// StateView renders scope's world state for /v0/state.
	StateView func(scope string) any
	// Origins, when non-empty, is the browser origin allowlist for websocket
	// upgrades (websocket.AcceptOptions.OriginPatterns). Empty is dev mode:
	// any origin is accepted — tolerable only because the gateway carries no
	// cookies or ambient credentials (the feed is public; line and consent
	// are gated by capability tokens the caller must present explicitly) —
	// but production wiring (Task 11) should set it.
	Origins []string
}

// Gateway fans the record out to watchers and carries the few inbound paths
// (claims, consent, principal says) back to its consumer.
type Gateway struct {
	cfg    Config
	mux    *http.ServeMux
	accept *websocket.AcceptOptions

	mu     sync.Mutex
	feeds  map[string]map[*conn]struct{} // scope → feed connections
	lines  map[string]map[*conn]struct{} // principal pseudo-voice → line connections
	tokens map[string]string             // line token → claimed agent voice
}

func New(cfg Config) *Gateway {
	g := &Gateway{
		cfg: cfg,
		mux: http.NewServeMux(),
		// InsecureSkipVerify here disables the browser same-origin check
		// (NOT TLS verification) — dev mode, see Config.Origins.
		accept: &websocket.AcceptOptions{InsecureSkipVerify: true},
		feeds:  map[string]map[*conn]struct{}{},
		lines:  map[string]map[*conn]struct{}{},
		tokens: map[string]string{},
	}
	if len(cfg.Origins) > 0 {
		g.accept = &websocket.AcceptOptions{OriginPatterns: cfg.Origins}
	}
	g.mux.HandleFunc("GET /v0/feed", g.handleFeed)
	g.mux.HandleFunc("POST /v0/claim", g.handleClaim)
	g.mux.HandleFunc("GET /v0/line", g.handleLine)
	g.mux.HandleFunc("POST /v0/consent", g.handleConsent)
	g.mux.HandleFunc("GET /v0/state", g.handleState)
	return g
}

// Handler returns the gateway's routes; the composition root mounts (and may
// wrap) it.
func (g *Gateway) Handler() http.Handler { return g.mux }

// Broadcast fans env — wrapped in a Frame carrying its log seq, the
// reconnect cursor — out to every feed connection watching env.Scope and to
// every private line whose principal pseudo-voice appears in env.To.
//
// Contract: Broadcast never blocks and performs no I/O. It is called from
// the orchestrator's Append path WITH the orchestrator mutex held, so it
// only enqueues onto per-connection buffers — each drained by that
// connection's own writer goroutine — and returns; it never writes a socket
// and never calls back into the orchestrator. A connection whose buffer is
// full is dropped: a slow reader cannot stall the world.
func (g *Gateway) Broadcast(seq int64, env protocol.Envelope) {
	data, err := json.Marshal(Frame{Seq: seq, Env: env})
	if err != nil {
		return // a Frame always marshals; nothing sane to do here anyway
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for c := range g.feeds[env.Scope] {
		c.enqueue(data)
	}
	for _, to := range env.To {
		for c := range g.lines[to] {
			c.enqueue(data)
		}
	}
}

// Revoke withdraws voice's claim: every token mapping to voice is deleted (a
// revoked token 401s on /v0/line and /v0/consent thereafter) and every
// private line registered under voice's principal pseudo-voice is dropped —
// its writer exits and disconnects the socket. Task 11 calls this on
// leave/slot-free.
func (g *Gateway) Revoke(voice string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for token, v := range g.tokens {
		if v == voice {
			delete(g.tokens, token)
		}
	}
	for c := range g.lines[principalFor(voice)] {
		c.drop() // the line handler unregisters on its way out
	}
}

// Viewers reports how many feed connections currently watch scope. The
// composition root uses it to let unwatched scopes sleep.
func (g *Gateway) Viewers(scope string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.feeds[scope])
}

// conn is one websocket subscriber: a buffered outgoing queue drained by a
// single writer goroutine, plus a drop signal for when the queue overflows.
// ws is assigned by the owning handler after registration and before the
// writer starts; Broadcast only ever touches the queue.
type conn struct {
	ws       *websocket.Conn
	out      chan []byte
	dropped  chan struct{}
	dropOnce sync.Once
}

func newConn(extra int) *conn {
	return &conn{out: make(chan []byte, sendBuffer+extra), dropped: make(chan struct{})}
}

// enqueue queues data without ever blocking; an overflowing conn is dropped.
func (c *conn) enqueue(data []byte) {
	select {
	case c.out <- data:
	default:
		c.drop()
	}
}

// drop signals the writer to abandon this connection.
func (c *conn) drop() { c.dropOnce.Do(func() { close(c.dropped) }) }

// writeLoop drains the queue onto the socket until ctx ends, the conn is
// dropped, or a write fails. It owns all data writes; closing the socket is
// the owning handler's job.
func (c *conn) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.dropped:
			return
		case data := <-c.out:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// register adds c to reg[key]; unregister removes it, pruning empty sets.
func (g *Gateway) register(reg map[string]map[*conn]struct{}, key string, c *conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	set := reg[key]
	if set == nil {
		set = map[*conn]struct{}{}
		reg[key] = set
	}
	set[c] = struct{}{}
}

func (g *Gateway) unregister(reg map[string]map[*conn]struct{}, key string, c *conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if set := reg[key]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(reg, key)
		}
	}
}

// handleFeed is GET /v0/feed?scope=...&after=... — the public record, live.
func (g *Gateway) handleFeed(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		http.Error(w, "missing scope", http.StatusBadRequest)
		return
	}
	// Replay runs before the upgrade and outside the registry lock: it may
	// hit a store, and Broadcast must never wait on it. Envelopes appended
	// between this snapshot and registration below are not delivered — the
	// client's after cursor covers them on its next connect.
	var backlog [][]byte
	if after := r.URL.Query().Get("after"); after != "" && g.cfg.Replay != nil {
		cursor, err := strconv.ParseInt(after, 10, 64)
		if err != nil {
			http.Error(w, "bad after", http.StatusBadRequest)
			return
		}
		for _, frame := range g.cfg.Replay(scope, cursor) {
			if data, err := json.Marshal(frame); err == nil {
				backlog = append(backlog, data)
			}
		}
	}

	// Queue the backlog, then register, then upgrade — in that order. The
	// single per-conn queue makes replay precede live traffic, and because
	// registration precedes the handshake response, a completed client dial
	// guarantees the viewer is counted and no later Broadcast misses it.
	c := newConn(len(backlog))
	for _, data := range backlog {
		c.out <- data // capacity covers the whole backlog; never blocks
	}
	g.register(g.feeds, scope, c)
	defer g.unregister(g.feeds, scope, c)

	ws, err := websocket.Accept(w, r, g.accept)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	c.ws = ws

	// The feed is server→client only: CloseRead pumps control frames and
	// cancels the context when the client goes away.
	c.writeLoop(ws.CloseRead(r.Context()))
}

// handleLine is GET /v0/line?token=... — the private line. In: plain text
// spoken as the principal. Out: envelopes addressed to the principal
// pseudo-voice behind the claimed agent voice.
func (g *Gateway) handleLine(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	voice, ok := g.tokens[r.URL.Query().Get("token")]
	g.mu.Unlock()
	if !ok {
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}
	principal := principalFor(voice)

	// Same ordering as the feed: registered before the handshake response,
	// so once a dial completes no ask_principal can slip past this line.
	c := newConn(0)
	g.register(g.lines, principal, c)
	defer g.unregister(g.lines, principal, c)

	ws, err := websocket.Accept(w, r, g.accept)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	c.ws = ws

	// Writer: targeted envelopes out. CloseNow on exit kicks the read loop
	// below loose when the conn is dropped for slow reading; when the read
	// loop exits first, the handler's return cancels r.Context() and ends
	// the writer — neither side leaks.
	go func() {
		c.writeLoop(r.Context())
		ws.CloseNow()
	}()

	// Reader: plain text in, spoken as the principal by the token holder.
	for {
		typ, data, err := ws.Read(r.Context())
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		if g.cfg.OnPrincipalSay != nil {
			g.cfg.OnPrincipalSay(voice, string(data))
		}
	}
}

// handleClaim is POST /v0/claim {"scope","name"} → {"voice","token"}.
func (g *Gateway) handleClaim(w http.ResponseWriter, r *http.Request) {
	if g.cfg.OnClaim == nil {
		http.Error(w, "claims not wired", http.StatusNotImplemented)
		return
	}
	var req struct {
		Scope string `json:"scope"`
		Name  string `json:"name"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	voice, err := g.cfg.OnClaim(req.Scope, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	token, err := newToken()
	if err != nil {
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}
	g.mu.Lock()
	g.tokens[token] = voice
	g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"voice": voice, "token": token})
}

// handleConsent is POST /v0/consent {"token","exchange","approve"} → 204.
func (g *Gateway) handleConsent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Exchange string `json:"exchange"`
		Approve  bool   `json:"approve"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	g.mu.Lock()
	voice, ok := g.tokens[req.Token]
	g.mu.Unlock()
	if !ok {
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}
	if g.cfg.OnConsent != nil {
		g.cfg.OnConsent(req.Exchange, voice, req.Approve)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleState is GET /v0/state?scope=... → the scope's world state as JSON.
func (g *Gateway) handleState(w http.ResponseWriter, r *http.Request) {
	if g.cfg.StateView == nil {
		http.Error(w, "no state view", http.StatusNotFound)
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		http.Error(w, "missing scope", http.StatusBadRequest)
		return
	}
	data, err := json.Marshal(g.cfg.StateView(scope))
	if err != nil {
		http.Error(w, "state not serializable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// principalFor derives the principal pseudo-voice behind an agent voice:
// "voice:her-agent" → "voice:principal:her" — strip the "voice:" prefix and
// a trailing "-agent" suffix, the same rule as orchestrator.PrincipalSays.
func principalFor(agentVoice string) string {
	return "voice:principal:" + strings.TrimSuffix(strings.TrimPrefix(agentVoice, "voice:"), "-agent")
}

// newToken returns 32 hex chars (128 bits) from crypto/rand.
func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
