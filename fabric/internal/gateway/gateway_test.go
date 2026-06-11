package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"otherworld/fabric/internal/gateway"
	"otherworld/fabric/internal/protocol"
)

// TestFeedStreamsAppendedEnvelopes is plan-pinned, adapted (sanctioned) to
// the Frame wire shape: Broadcast takes the log seq and the feed carries
// {"seq":...,"env":{...}}.
func TestFeedStreamsAppendedEnvelopes(t *testing.T) {
	g := gateway.New(gateway.Config{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := strings.Replace(srv.URL, "http", "ws", 1) + "/v0/feed?scope=scope:test"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	g.Broadcast(7, protocol.Envelope{ID: "utt_X", Scope: "scope:test", Kind: protocol.KindSay, Body: "hello"})

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frame gateway.Frame
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Env.ID != "utt_X" {
		t.Fatalf("got %q", frame.Env.ID)
	}
	if frame.Seq != 7 {
		t.Fatalf("seq = %d, want 7", frame.Seq)
	}
}

func TestViewerCountTracksConnections(t *testing.T) {
	g := gateway.New(gateway.Config{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	if g.Viewers("scope:test") != 0 {
		t.Fatal("expected 0 viewers")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := strings.Replace(srv.URL, "http", "ws", 1) + "/v0/feed?scope=scope:test"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for g.Viewers("scope:test") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if g.Viewers("scope:test") != 1 {
		t.Fatal("viewer not counted")
	}
}

// --- helpers ---

// dialWS dials a websocket path on srv. The caller owns closing the conn —
// with defer, AFTER the deferred srv.Close, so the client side drops first
// and the server handler can exit.
func dialWS(t *testing.T, ctx context.Context, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	url := strings.Replace(srv.URL, "http", "ws", 1) + path
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	return conn
}

// claim POSTs /v0/claim and returns the granted voice and token.
func claim(t *testing.T, srv *httptest.Server, scope, name string) (voice, token string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"scope": scope, "name": name})
	resp, err := http.Post(srv.URL+"/v0/claim", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("claim: status %d", resp.StatusCode)
	}
	var out struct{ Voice, Token string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Voice, out.Token
}

func readFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) gateway.Frame {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frame gateway.Frame
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	return frame
}

func waitViewers(t *testing.T, g *gateway.Gateway, scope string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for g.Viewers(scope) != want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := g.Viewers(scope); got != want {
		t.Fatalf("viewers(%s) = %d, want %d", scope, got, want)
	}
}

// --- own tests ---

func TestViewerCountDecrementsAfterDisconnect(t *testing.T) {
	g := gateway.New(gateway.Config{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialWS(t, ctx, srv, "/v0/feed?scope=scope:test")
	defer conn.CloseNow()
	waitViewers(t, g, "scope:test", 1)
	conn.Close(websocket.StatusNormalClosure, "")
	waitViewers(t, g, "scope:test", 0)
}

// TestFeedScopeIsolation: a feed only carries its own scope. Per-conn
// delivery is ordered, so if scope:a traffic had leaked onto the scope:b
// feed it would arrive before the scope:b marker.
func TestFeedScopeIsolation(t *testing.T) {
	g := gateway.New(gateway.Config{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connA := dialWS(t, ctx, srv, "/v0/feed?scope=scope:a")
	defer connA.CloseNow()
	connB := dialWS(t, ctx, srv, "/v0/feed?scope=scope:b")
	defer connB.CloseNow()

	g.Broadcast(1, protocol.Envelope{ID: "utt_a", Scope: "scope:a", Kind: protocol.KindSay})
	g.Broadcast(2, protocol.Envelope{ID: "utt_b", Scope: "scope:b", Kind: protocol.KindSay})

	if f := readFrame(t, ctx, connA); f.Env.ID != "utt_a" {
		t.Fatalf("scope:a feed got %q, want utt_a", f.Env.ID)
	}
	if f := readFrame(t, ctx, connB); f.Env.ID != "utt_b" {
		t.Fatalf("scope:b feed got %q, want utt_b", f.Env.ID)
	}
}

func TestReplayThenLiveOrdering(t *testing.T) {
	g := gateway.New(gateway.Config{
		Replay: func(scope string, after int64) []gateway.Frame {
			if scope != "scope:test" || after != 0 {
				t.Errorf("Replay(%q, %d), want (scope:test, 0)", scope, after)
			}
			return []gateway.Frame{
				{Seq: 1, Env: protocol.Envelope{ID: "utt_1", Scope: scope, Kind: protocol.KindSay}},
				{Seq: 2, Env: protocol.Envelope{ID: "utt_2", Scope: scope, Kind: protocol.KindSay}},
			}
		},
	})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialWS(t, ctx, srv, "/v0/feed?scope=scope:test&after=0")
	defer conn.CloseNow()

	g.Broadcast(3, protocol.Envelope{ID: "utt_3", Scope: "scope:test", Kind: protocol.KindSay})

	prev := int64(0) // seqs must stay monotonic across the replay→live boundary
	for i, want := range []string{"utt_1", "utt_2", "utt_3"} {
		f := readFrame(t, ctx, conn)
		if f.Env.ID != want {
			t.Fatalf("message %d: got %q, want %q", i, f.Env.ID, want)
		}
		if f.Seq <= prev {
			t.Fatalf("message %d: seq %d not monotonic after %d", i, f.Seq, prev)
		}
		prev = f.Seq
	}
}

func TestClaimLineRoundTrip(t *testing.T) {
	says := make(chan [2]string, 1)
	g := gateway.New(gateway.Config{
		OnClaim:        func(scope, name string) (string, error) { return "voice:" + name + "-agent", nil },
		OnPrincipalSay: func(voice, text string) { says <- [2]string{voice, text} },
	})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	voice, token := claim(t, srv, "scope:test", "her")
	if voice != "voice:her-agent" {
		t.Fatalf("claimed voice %q, want voice:her-agent", voice)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(token) {
		t.Fatalf("token %q is not 32 hex chars", token)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialWS(t, ctx, srv, "/v0/line?token="+token)
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte("i'm cold")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-says:
		if got[0] != "voice:her-agent" || got[1] != "i'm cold" {
			t.Fatalf("OnPrincipalSay got %q", got)
		}
	case <-ctx.Done():
		t.Fatal("OnPrincipalSay never called")
	}
}

// TestAskPrincipalReachesOnlyItsLine: an envelope addressed to a principal
// pseudo-voice lands on that principal's line and no other. The rival line
// must see its own marker FIRST — per-line delivery is ordered, so a leaked
// utt_ask would have arrived before it.
func TestAskPrincipalReachesOnlyItsLine(t *testing.T) {
	g := gateway.New(gateway.Config{
		OnClaim: func(scope, name string) (string, error) { return "voice:" + name + "-agent", nil },
	})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, herToken := claim(t, srv, "scope:test", "her")
	_, rivalToken := claim(t, srv, "scope:test", "rival")
	herLine := dialWS(t, ctx, srv, "/v0/line?token="+herToken)
	defer herLine.CloseNow()
	rivalLine := dialWS(t, ctx, srv, "/v0/line?token="+rivalToken)
	defer rivalLine.CloseNow()

	g.Broadcast(1, protocol.Envelope{
		ID: "utt_ask", Scope: "scope:test", Kind: protocol.KindAskPrincipal,
		From: "voice:her-agent", To: []string{"voice:principal:her"}, Body: "may I accept?",
	})
	g.Broadcast(2, protocol.Envelope{
		ID: "utt_marker", Scope: "scope:test", Kind: protocol.KindSay,
		To: []string{"voice:principal:rival"},
	})

	if f := readFrame(t, ctx, herLine); f.Env.ID != "utt_ask" {
		t.Fatalf("her line got %q, want utt_ask", f.Env.ID)
	}
	if f := readFrame(t, ctx, rivalLine); f.Env.ID != "utt_marker" {
		t.Fatalf("rival line got %q, want utt_marker (utt_ask leaked?)", f.Env.ID)
	}
}

func TestConsentDeliversClaimedAgentVoice(t *testing.T) {
	type consent struct {
		exchange, voice string
		approve         bool
	}
	consents := make(chan consent, 1)
	g := gateway.New(gateway.Config{
		OnClaim: func(scope, name string) (string, error) { return "voice:her-agent", nil },
		OnConsent: func(exchange, voice string, approve bool) {
			consents <- consent{exchange: exchange, voice: voice, approve: approve}
		},
	})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	_, token := claim(t, srv, "scope:test", "her")
	body, _ := json.Marshal(map[string]any{"token": token, "exchange": "exc_7", "approve": true})
	resp, err := http.Post(srv.URL+"/v0/consent", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("consent: status %d, want 204", resp.StatusCode)
	}
	select {
	case c := <-consents:
		if c.exchange != "exc_7" || c.voice != "voice:her-agent" || !c.approve {
			t.Fatalf("OnConsent got %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnConsent never called")
	}
}

func TestUnknownTokenRejected(t *testing.T) {
	g := gateway.New(gateway.Config{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := strings.Replace(srv.URL, "http", "ws", 1) + "/v0/line?token=" + strings.Repeat("ab", 16)
	if conn, resp, err := websocket.Dial(ctx, url, nil); err == nil {
		conn.CloseNow()
		t.Fatal("line dial with unknown token succeeded")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("line dial: %v, want HTTP 401", err)
	}

	body, _ := json.Marshal(map[string]any{"token": "nope", "exchange": "exc_1", "approve": true})
	resp, err := http.Post(srv.URL+"/v0/consent", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("consent: status %d, want 401", resp.StatusCode)
	}
}

func TestClaimErrors(t *testing.T) {
	g := gateway.New(gateway.Config{
		OnClaim: func(scope, name string) (string, error) { return "", errors.New("name taken") },
	})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v0/claim", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad body: status %d, want 400", resp.StatusCode)
	}

	body, _ := json.Marshal(map[string]string{"scope": "scope:test", "name": "her"})
	resp, err = http.Post(srv.URL+"/v0/claim", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("refused claim: status %d, want 409", resp.StatusCode)
	}

	// Nil OnClaim is guarded, not a panic.
	bare := gateway.New(gateway.Config{})
	srv2 := httptest.NewServer(bare.Handler())
	defer srv2.Close()
	resp, err = http.Post(srv2.URL+"/v0/claim", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("nil OnClaim: status %d, want 501", resp.StatusCode)
	}
}

func TestStateServesViewJSON(t *testing.T) {
	g := gateway.New(gateway.Config{
		StateView: func(scope string) any { return map[string]string{"scope": scope, "lamp": "on"} },
	})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v0/state?scope=scope:test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["scope"] != "scope:test" || got["lamp"] != "on" {
		t.Fatalf("state view: %v", got)
	}
}

func TestStateWithoutViewIs404(t *testing.T) {
	g := gateway.New(gateway.Config{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v0/state?scope=scope:test")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
}

// TestNilCallbacksAreGuarded: a line message with no OnPrincipalSay wired is
// dropped, not a panic; a consent with no OnConsent is a 204 no-op. The
// graceful close handshake after the text proves the read loop survived it.
func TestNilCallbacksAreGuarded(t *testing.T) {
	g := gateway.New(gateway.Config{
		OnClaim: func(scope, name string) (string, error) { return "voice:her-agent", nil },
	})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, token := claim(t, srv, "scope:test", "her")
	conn := dialWS(t, ctx, srv, "/v0/line?token="+token)
	if err := conn.Write(ctx, websocket.MessageText, []byte("anyone there?")); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("close handshake after nil-OnPrincipalSay text: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"token": token, "exchange": "exc_1", "approve": false})
	resp, err := http.Post(srv.URL+"/v0/consent", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("nil OnConsent: status %d, want 204", resp.StatusCode)
	}
}

// TestRevokeKillsTokensAndDisconnectsLine: Revoke deletes every token for
// the voice and drops the lines under its principal — the open line is
// closed from the server side, and the old token 401s on /v0/line and
// /v0/consent thereafter.
func TestRevokeKillsTokensAndDisconnectsLine(t *testing.T) {
	g := gateway.New(gateway.Config{
		OnClaim: func(scope, name string) (string, error) { return "voice:her-agent", nil },
	})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, token := claim(t, srv, "scope:test", "her")
	conn := dialWS(t, ctx, srv, "/v0/line?token="+token)
	defer conn.CloseNow()

	g.Revoke("voice:her-agent")

	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("revoked line still delivered a message")
	}

	url := strings.Replace(srv.URL, "http", "ws", 1) + "/v0/line?token=" + token
	if c2, resp, err := websocket.Dial(ctx, url, nil); err == nil {
		c2.CloseNow()
		t.Fatal("line dial with revoked token succeeded")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("line dial: %v, want HTTP 401", err)
	}

	body, _ := json.Marshal(map[string]any{"token": token, "exchange": "exc_1", "approve": true})
	resp, err := http.Post(srv.URL+"/v0/consent", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("consent with revoked token: status %d, want 401", resp.StatusCode)
	}
}

// TestOriginAllowlist: with Config.Origins set, only listed browser origins
// may upgrade; without it (dev mode) any origin is accepted.
func TestOriginAllowlist(t *testing.T) {
	g := gateway.New(gateway.Config{Origins: []string{"allowed.example"}})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := strings.Replace(srv.URL, "http", "ws", 1) + "/v0/feed?scope=scope:test"

	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://allowed.example"}},
	})
	if err != nil {
		t.Fatalf("allowed origin rejected: %v", err)
	}
	conn.CloseNow()

	if conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}},
	}); err == nil {
		conn.CloseNow()
		t.Fatal("disallowed origin accepted")
	}
}

// TestSlowConsumerDoesNotBlockBroadcast: a feed conn that never reads must
// not stall Broadcast — it is called from the orchestrator's Append path
// with the orchestrator mutex held. The conn may be dropped; the world must
// not wait for it.
func TestSlowConsumerDoesNotBlockBroadcast(t *testing.T) {
	g := gateway.New(gateway.Config{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := dialWS(t, ctx, srv, "/v0/feed?scope=scope:test")
	defer conn.CloseNow() // runs before srv.Close, releasing the handler
	waitViewers(t, g, "scope:test", 1)

	body := strings.Repeat("x", 1024)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		g.Broadcast(int64(i+1), protocol.Envelope{
			ID: fmt.Sprintf("utt_%d", i), Scope: "scope:test",
			Kind: protocol.KindSay, Body: body,
		})
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("1000 broadcasts to a dead reader took %v", elapsed)
	}
}
