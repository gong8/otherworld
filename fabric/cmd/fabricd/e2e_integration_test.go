//go:build integration

// The end-to-end run IS the two-minute demo, headless: boot the full server
// in-process against compose Postgres, then drive the demo script over real
// WebSocket clients — her cold beat, the his/her compromise beat (the marquee),
// the street trade with principal consent, the full-household refusal, and
// the viewers draining to zero.
//
// The store transcript accumulates across runs (only the world is fresh per
// boot), so claim names carry a per-run suffix and the replay assertion
// cursors from this run's first seq.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"otherworld/fabric/internal/gateway"
	"otherworld/fabric/internal/protocol"
)

func TestE2EDemoScript(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	// Fast debounce for the beats; ambient murmurs pushed out of the test
	// window so the asserted frame sequences stay deterministic.
	t.Setenv("OW_DEBOUNCE_MIN_MS", "10")
	t.Setenv("OW_DEBOUNCE_MAX_MS", "20")
	t.Setenv("OW_AMBIENT_MIN_MS", "600000")
	t.Setenv("OW_AMBIENT_MAX_MS", "600000")

	cfg := defaultConfig(dbURL)
	cfg.applyEnv()

	ctx, cancel := context.WithCancel(context.Background())
	s, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.gw.Handler())
	defer func() { // after the LIFO conn closes below: srv, then the world
		srv.Close()
		cancel()
		s.close()
	}()

	wsCtx, wsCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer wsCancel()

	nano := strconv.FormatInt(time.Now().UnixNano(), 36)
	her, him, visitor, extra := "her-"+nano, "him-"+nano, "visitor-"+nano, "extra-"+nano

	// ── Beat 1+2: her cold beat ──────────────────────────────────────────────
	feedHH := dialWS(t, wsCtx, srv, "/v0/feed?scope=scope:household")
	defer feedHH.CloseNow()
	herVoice, herToken := claimResident(t, srv, "scope:household", her)
	if herVoice != "voice:"+her+"-agent" {
		t.Fatalf("claimed voice %q", herVoice)
	}
	herLine := dialWS(t, wsCtx, srv, "/v0/line?token="+herToken)
	defer herLine.CloseNow()
	if err := herLine.Write(wsCtx, websocket.MessageText, []byte("i'm cold")); err != nil {
		t.Fatal(err)
	}

	cold := readFrames(t, wsCtx, feedHH, 4)
	assertKinds(t, "cold beat", cold, protocol.KindSay, protocol.KindPropose, protocol.KindAccept, protocol.KindSettle)
	assertMonotonic(t, "cold beat", 0, cold)
	if cold[1].Env.From != herVoice || !slices.Contains(cold[1].Env.To, "voice:heating") {
		t.Fatalf("propose routing: from %q to %v", cold[1].Env.From, cold[1].Env.To)
	}
	if got := termsTemp(t, cold[1].Env); got != 23.0 {
		t.Fatalf("propose temperature = %v, want 23", got)
	}
	if got := termsTemp(t, cold[3].Env); got != 23.0 {
		t.Fatalf("settle temperature = %v, want 23", got)
	}
	if got := heatingTemp(t, getState(t, srv, "scope:household")); got != 23.0 {
		t.Fatalf("state temperature = %v, want 23 after the cold beat", got)
	}

	// ── Beat 3: the compromise, live (the marquee) ───────────────────────────
	himVoice, himToken := claimResident(t, srv, "scope:household", him)
	himLine := dialWS(t, wsCtx, srv, "/v0/line?token="+himToken)
	defer himLine.CloseNow()
	if err := himLine.Write(wsCtx, websocket.MessageText, []byte("too hot in here")); err != nil {
		t.Fatal(err)
	}

	hot := readFrames(t, wsCtx, feedHH, 5)
	assertKinds(t, "compromise beat", hot,
		protocol.KindSay, protocol.KindPropose, protocol.KindPropose, protocol.KindAccept, protocol.KindSettle)
	assertMonotonic(t, "compromise beat", cold[3].Seq, hot)
	if hot[1].Env.From != himVoice || termsTemp(t, hot[1].Env) != 19.0 {
		t.Fatalf("his ask: from %q, temp %v; want %q asking 19", hot[1].Env.From, termsTemp(t, hot[1].Env), himVoice)
	}
	if hot[2].Env.From != "voice:heating" || !strings.Contains(hot[2].Env.Body, "hold the middle") {
		t.Fatalf("counter: from %q body %q, want heating holding the middle", hot[2].Env.From, hot[2].Env.Body)
	}
	if termsTemp(t, hot[2].Env) != 21.0 {
		t.Fatalf("counter temperature = %v, want 21", termsTemp(t, hot[2].Env))
	}
	if hot[3].Env.From != himVoice || hot[3].Env.Body != "fair enough." {
		t.Fatalf("accept: from %q body %q", hot[3].Env.From, hot[3].Env.Body)
	}
	if termsTemp(t, hot[4].Env) != 21.0 {
		t.Fatalf("settle temperature = %v, want 21", termsTemp(t, hot[4].Env))
	}
	if got := heatingTemp(t, getState(t, srv, "scope:household")); got != 21.0 {
		t.Fatalf("state temperature = %v, want 21 after the compromise", got)
	}

	// Replay: a reconnect cursored at this run's first seq replays exactly
	// this run's nine household frames, store-first order preserved.
	reFeed := dialWS(t, wsCtx, srv, fmt.Sprintf("/v0/feed?scope=scope:household&after=%d", cold[0].Seq-1))
	replayed := readFrames(t, wsCtx, reFeed, 9)
	assertKinds(t, "replay", replayed,
		protocol.KindSay, protocol.KindPropose, protocol.KindAccept, protocol.KindSettle,
		protocol.KindSay, protocol.KindPropose, protocol.KindPropose, protocol.KindAccept, protocol.KindSettle)
	assertMonotonic(t, "replay", cold[0].Seq-1, replayed)
	reFeed.Close(websocket.StatusNormalClosure, "")

	// ── Beat 4: the street trade, with consent on the private line ──────────
	feedST := dialWS(t, wsCtx, srv, "/v0/feed?scope=scope:street")
	defer feedST.CloseNow()
	visitorVoice, visitorToken := claimResident(t, srv, "scope:street", visitor)
	visitorLine := dialWS(t, wsCtx, srv, "/v0/line?token="+visitorToken)
	defer visitorLine.CloseNow()
	if err := visitorLine.Write(wsCtx, websocket.MessageText, []byte("find me something sweet")); err != nil {
		t.Fatal(err)
	}

	street := readFrames(t, wsCtx, feedST, 4)
	assertKinds(t, "street beat", street,
		protocol.KindSay, protocol.KindHail, protocol.KindPropose, protocol.KindAskPrincipal)
	assertMonotonic(t, "street beat", 0, street)
	if street[2].Env.From != "voice:corner-shop" || street[2].Env.Terms == nil || street[2].Env.Terms.Type != "trade" {
		t.Fatalf("shop bid: from %q terms %+v", street[2].Env.From, street[2].Env.Terms)
	}

	// The ask must ARRIVE ON THE PRIVATE LINE, not just the feed.
	ask := readFrame(t, wsCtx, visitorLine)
	if ask.Env.Kind != protocol.KindAskPrincipal {
		t.Fatalf("line got %s, want ask_principal", ask.Env.Kind)
	}
	if ask.Env.Exchange == "" || !strings.Contains(ask.Env.Body, "shall i?") {
		t.Fatalf("ask: exchange %q body %q", ask.Env.Exchange, ask.Env.Body)
	}

	consent(t, srv, visitorToken, ask.Env.Exchange, true)

	settled := readFrames(t, wsCtx, feedST, 2)
	assertKinds(t, "trade close", settled, protocol.KindAccept, protocol.KindSettle)
	if settled[0].Env.From != visitorVoice || settled[0].Env.Terms == nil {
		t.Fatalf("consent accept: from %q terms %+v", settled[0].Env.From, settled[0].Env.Terms)
	}
	if settled[1].Env.Terms == nil || settled[1].Env.Terms.Type != "trade" {
		t.Fatalf("trade settle terms: %+v", settled[1].Env.Terms)
	}

	stState := getState(t, srv, "scope:street")
	if got := marksOf(t, stState, visitorVoice); got != 97 {
		t.Fatalf("visitor marks = %v, want 97", got)
	}
	if got := marksOf(t, stState, "voice:corner-shop"); got != 3 {
		t.Fatalf("corner-shop marks = %v, want 3", got)
	}

	// ── Beat 5: the household is full ────────────────────────────────────────
	if status := claimStatus(t, srv, "scope:household", extra); status != http.StatusConflict {
		t.Fatalf("third household claim: status %d, want 409", status)
	}

	// ── Beat 6: everyone leaves ──────────────────────────────────────────────
	for _, c := range []*websocket.Conn{herLine, himLine, visitorLine, feedHH, feedST} {
		c.Close(websocket.StatusNormalClosure, "")
	}
	waitViewers(t, s, "scope:household", 0)
	waitViewers(t, s, "scope:street", 0)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func dialWS(t *testing.T, ctx context.Context, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	url := strings.Replace(srv.URL, "http", "ws", 1) + path
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	return conn
}

func claimResident(t *testing.T, srv *httptest.Server, scope, name string) (voice, token string) {
	t.Helper()
	resp := postClaim(t, srv, scope, name)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("claim %s in %s: status %d", name, scope, resp.StatusCode)
	}
	var out struct{ Voice, Token string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Voice, out.Token
}

func claimStatus(t *testing.T, srv *httptest.Server, scope, name string) int {
	t.Helper()
	resp := postClaim(t, srv, scope, name)
	resp.Body.Close()
	return resp.StatusCode
}

func postClaim(t *testing.T, srv *httptest.Server, scope, name string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"scope": scope, "name": name})
	resp, err := http.Post(srv.URL+"/v0/claim", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func consent(t *testing.T, srv *httptest.Server, token, exchange string, approve bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"token": token, "exchange": exchange, "approve": approve})
	resp, err := http.Post(srv.URL+"/v0/consent", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("consent: status %d, want 204", resp.StatusCode)
	}
}

func readFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) gateway.Frame {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var frame gateway.Frame
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	return frame
}

func readFrames(t *testing.T, ctx context.Context, conn *websocket.Conn, n int) []gateway.Frame {
	t.Helper()
	frames := make([]gateway.Frame, 0, n)
	for len(frames) < n {
		frames = append(frames, readFrame(t, ctx, conn))
	}
	return frames
}

func assertKinds(t *testing.T, beat string, frames []gateway.Frame, want ...protocol.Kind) {
	t.Helper()
	var got, expect []string
	for _, f := range frames {
		got = append(got, string(f.Env.Kind))
	}
	for _, k := range want {
		expect = append(expect, string(k))
	}
	if g, w := strings.Join(got, ">"), strings.Join(expect, ">"); g != w {
		t.Fatalf("%s: kinds %s, want %s", beat, g, w)
	}
}

func assertMonotonic(t *testing.T, beat string, after int64, frames []gateway.Frame) {
	t.Helper()
	prev := after
	for i, f := range frames {
		if f.Seq <= prev {
			t.Fatalf("%s: frame %d seq %d not monotonic after %d", beat, i, f.Seq, prev)
		}
		prev = f.Seq
	}
}

// termsTemp unwraps a temperature.set terms value.
func termsTemp(t *testing.T, env protocol.Envelope) float64 {
	t.Helper()
	if env.Terms == nil || env.Terms.Type != "temperature.set" {
		t.Fatalf("terms = %+v, want temperature.set", env.Terms)
	}
	var v float64
	if err := json.Unmarshal(env.Terms.Value, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func getState(t *testing.T, srv *httptest.Server, scope string) map[string]any {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v0/state?scope=" + scope)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state %s: status %d", scope, resp.StatusCode)
	}
	var state map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func heatingTemp(t *testing.T, state map[string]any) float64 {
	t.Helper()
	things, _ := state["things"].(map[string]any)
	heating, _ := things["heating"].(map[string]any)
	v, ok := heating["temperature"].(float64)
	if !ok {
		t.Fatalf("no heating temperature in state: %v", state)
	}
	return v
}

func marksOf(t *testing.T, state map[string]any, voice string) float64 {
	t.Helper()
	marks, _ := state["marks"].(map[string]any)
	v, ok := marks[voice].(float64)
	if !ok {
		t.Fatalf("no marks for %s in state: %v", voice, state)
	}
	return v
}

func waitViewers(t *testing.T, s *server, scope string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.gw.Viewers(scope) != want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := s.gw.Viewers(scope); got != want {
		t.Fatalf("viewers(%s) = %d, want %d", scope, got, want)
	}
}
