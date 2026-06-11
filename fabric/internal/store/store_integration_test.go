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

	// Use per-run unique ids and scopes to avoid UNIQUE constraint collisions on reruns.
	nano := time.Now().UnixNano()
	id1 := fmt.Sprintf("utt_T1_%d", nano)
	id2 := fmt.Sprintf("utt_T2_%d", nano)
	id3 := fmt.Sprintf("utt_T3_%d", nano)
	scope := fmt.Sprintf("scope:test:%d", nano)
	otherScope := fmt.Sprintf("scope:other:%d", nano)

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

	// Scope isolation: an utterance in another scope must not leak into replay.
	if _, err := s.AppendUtterance(ctx, id3, time.Now().UTC(), otherScope, json.RawMessage(`{"k":3}`)); err != nil {
		t.Fatal(err)
	}

	rows, err := s.ListUtterancesSince(ctx, scope, seq1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("replay-from-seq returned %d rows, want 1", len(rows))
	}

	// Payload round-trip: the replayed row must carry the exact JSON we appended.
	var got struct {
		K int `json:"k"`
	}
	if err := json.Unmarshal(rows[0].Payload, &got); err != nil {
		t.Fatalf("payload did not unmarshal: %v", err)
	}
	if got.K != 2 {
		t.Fatalf("replayed payload k = %d, want 2", got.K)
	}

	// Purge semantics (law 7: the door forgets). Pre-purge clears any expired
	// leftovers from prior runs so the count assertion below is exact.
	now := time.Now().UTC()
	if _, err := s.PurgeExpiredPresence(ctx, now); err != nil {
		t.Fatal(err)
	}
	expiredID := fmt.Sprintf("prs_expired_%d", nano)
	liveID := fmt.Sprintf("prs_live_%d", nano)
	if err := s.InsertPresence(ctx, expiredID, scope, "voice:test", "entered", now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertPresence(ctx, liveID, scope, "voice:test", "left", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	purged, err := s.PurgeExpiredPresence(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if purged != 1 {
		t.Fatalf("purge deleted %d rows, want exactly 1 (expired only)", purged)
	}
}
