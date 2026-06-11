package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"otherworld/fabric/internal/store"
)

func TestIntegrationAppendAndReplay(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := store.Open(ctx, url) // runs schema.sql idempotently
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Use per-run unique ids and scope to avoid UNIQUE constraint collisions on reruns.
	nano := time.Now().UnixNano()
	id1 := fmt.Sprintf("utt_T1_%d", nano)
	id2 := fmt.Sprintf("utt_T2_%d", nano)
	scope := fmt.Sprintf("scope:test:%d", nano)

	seq1, err := s.AppendUtterance(ctx, id1, time.Now().UTC(), scope, json.RawMessage(`{"k":1}`))
	if err != nil {
		t.Fatal(err)
	}
	seq2, err := s.AppendUtterance(ctx, id2, time.Now().UTC(), scope, json.RawMessage(`{"k":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if seq2 <= seq1 {
		t.Fatalf("seq not monotonic: %d then %d", seq1, seq2)
	}

	rows, err := s.ListUtterancesSince(ctx, scope, seq1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("replay-from-seq returned %d rows, want 1", len(rows))
	}

	if _, err := s.PurgeExpiredPresence(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}
