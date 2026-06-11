// Command fabricd runs the otherworld fabric: two scopes (household, street)
// of charter-bound voices over a Postgres record, served as a public
// WebSocket feed with token-gated private lines. See compose.go for the
// wiring; this file owns flags, env, and process lifecycle.
//
// internal/runtime is deliberately NOT wired in v1: it hosts async voice
// loops for when slow LLM brains arrive in Plan 3. Fake brains are instant
// and run under the orchestrator lock, so no mailboxes are needed yet.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// config is everything the composition needs beyond the listen address.
type config struct {
	databaseURL              string
	origins                  []string
	debounceMin, debounceMax time.Duration
	ambientMin, ambientMax   time.Duration
}

func defaultConfig(databaseURL string) config {
	return config{
		databaseURL: databaseURL,
		debounceMin: 1500 * time.Millisecond,
		debounceMax: 3000 * time.Millisecond,
		ambientMin:  60 * time.Second,
		ambientMax:  180 * time.Second,
	}
}

// applyEnv folds in the OW_* millisecond overrides (the e2e test shrinks the
// debounce to 10–20ms; ops may slow the murmurs down).
func (c *config) applyEnv() {
	envMS := func(name string, d *time.Duration) {
		v := os.Getenv(name)
		if v == "" {
			return
		}
		ms, err := strconv.Atoi(v)
		if err != nil || ms <= 0 {
			slog.Warn("ignoring bad env override", "name", name, "value", v)
			return
		}
		*d = time.Duration(ms) * time.Millisecond
	}
	envMS("OW_DEBOUNCE_MIN_MS", &c.debounceMin)
	envMS("OW_DEBOUNCE_MAX_MS", &c.debounceMax)
	envMS("OW_AMBIENT_MIN_MS", &c.ambientMin)
	envMS("OW_AMBIENT_MAX_MS", &c.ambientMax)
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	brains := flag.String("brains", "fake", "brain adapter: fake|bedrock")
	origins := flag.String("origins", "", "comma-separated browser origin allowlist for websocket upgrades (empty: dev mode, any origin)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if *brains != "fake" {
		if *brains == "bedrock" {
			slog.Error("bedrock brains arrive in plan 3")
		} else {
			slog.Error("unknown -brains adapter", "brains", *brains)
		}
		os.Exit(1)
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	cfg := defaultConfig(databaseURL)
	for _, o := range strings.Split(*origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.origins = append(cfg.origins, o)
		}
	}
	cfg.applyEnv()

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	s, err := newServer(ctx, cfg)
	if err != nil {
		slog.Error("boot failed", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{Addr: *addr, Handler: s.gw.Handler()}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	voices := 0
	scopeIDs := make([]string, 0, len(s.scopes))
	for scope, sc := range s.scopes {
		scopeIDs = append(scopeIDs, scope)
		voices += len(sc.serves)
	}
	slog.Info("fabricd up", "addr", *addr, "scopes", strings.Join(scopeIDs, ","), "voices", voices)

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
	case err := <-errCh:
		slog.Error("http server failed", "err", err)
	}

	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shCtx)
	cancel()
	stopSignals() // cancels the root ctx on the listen-failure path too
	s.close()     // writers drain, then the store closes
}
