// Package store wraps sqlc-generated queries behind a small surface.
package store

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"otherworld/fabric/internal/store/storegen"
)

//go:embed schema.sql
var schemaSQL string

// Store holds a pgxpool and sqlc-generated Queries.
type Store struct {
	pool *pgxpool.Pool
	q    *storegen.Queries
}

// Open connects to the database, runs schema.sql idempotently, and returns a Store.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, q: storegen.New(pool)}, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// toTz converts a time.Time to pgtype.Timestamptz.
func toTz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// AppendUtterance inserts an utterance and returns its assigned seq.
func (s *Store) AppendUtterance(ctx context.Context, id string, ts time.Time, scope string, payload json.RawMessage) (int64, error) {
	return s.q.AppendUtterance(ctx, storegen.AppendUtteranceParams{
		ID:      id,
		Ts:      toTz(ts),
		Scope:   scope,
		Payload: []byte(payload),
	})
}

// UtteranceRow is a single row from ListUtterancesSince.
type UtteranceRow struct {
	Seq     int64
	Payload json.RawMessage
}

// ListUtterancesSince returns utterances in scope with seq > after, up to limit rows.
func (s *Store) ListUtterancesSince(ctx context.Context, scope string, after int64, limit int32) ([]UtteranceRow, error) {
	rows, err := s.q.ListUtterancesSince(ctx, storegen.ListUtterancesSinceParams{
		Scope: scope,
		Seq:   after,
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]UtteranceRow, len(rows))
	for i, r := range rows {
		out[i] = UtteranceRow{Seq: r.Seq, Payload: json.RawMessage(r.Payload)}
	}
	return out, nil
}

// UpsertVoice inserts or updates a voice record.
func (s *Store) UpsertVoice(ctx context.Context, id, scope string, charter json.RawMessage) error {
	return s.q.UpsertVoice(ctx, storegen.UpsertVoiceParams{
		ID:      id,
		Scope:   scope,
		Charter: []byte(charter),
	})
}

// OpenExchange inserts a new exchange in state 'open'.
func (s *Store) OpenExchange(ctx context.Context, id, scope string, participants []string, at time.Time) error {
	return s.q.OpenExchange(ctx, storegen.OpenExchangeParams{
		ID:           id,
		Scope:        scope,
		Participants: participants,
		OpenedAt:     toTz(at),
	})
}

// CloseExchange updates an exchange's state and closed_at.
func (s *Store) CloseExchange(ctx context.Context, id, state string, at time.Time) error {
	return s.q.CloseExchange(ctx, storegen.CloseExchangeParams{
		ID:       id,
		State:    state,
		ClosedAt: toTz(at),
	})
}

// InsertSettlement inserts a settlement record.
func (s *Store) InsertSettlement(ctx context.Context, id, exchangeID, scope string, terms json.RawMessage, parties []string, ts time.Time) error {
	return s.q.InsertSettlement(ctx, storegen.InsertSettlementParams{
		ID:         id,
		ExchangeID: exchangeID,
		Scope:      scope,
		Terms:      []byte(terms),
		Parties:    parties,
		Ts:         toTz(ts),
	})
}

// SettlementRow is a single row from ListSettlements.
type SettlementRow struct {
	ID         string
	ExchangeID string
	Terms      json.RawMessage
	Parties    []string
	Ts         time.Time
}

// ListSettlements returns settlements in scope ordered by ts desc, up to limit rows.
func (s *Store) ListSettlements(ctx context.Context, scope string, limit int32) ([]SettlementRow, error) {
	rows, err := s.q.ListSettlements(ctx, storegen.ListSettlementsParams{
		Scope: scope,
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SettlementRow, len(rows))
	for i, r := range rows {
		out[i] = SettlementRow{
			ID:         r.ID,
			ExchangeID: r.ExchangeID,
			Terms:      json.RawMessage(r.Terms),
			Parties:    r.Parties,
			Ts:         r.Ts.Time,
		}
	}
	return out, nil
}

// InsertPresence inserts a presence event with a TTL expiry (law 7: the door forgets).
func (s *Store) InsertPresence(ctx context.Context, id, scope, voice, event string, ts, expiresAt time.Time) error {
	return s.q.InsertPresence(ctx, storegen.InsertPresenceParams{
		ID:        id,
		Scope:     scope,
		Voice:     voice,
		Event:     event,
		Ts:        toTz(ts),
		ExpiresAt: toTz(expiresAt),
	})
}

// PurgeExpiredPresence deletes presence events with expires_at < now, returning the count deleted.
func (s *Store) PurgeExpiredPresence(ctx context.Context, now time.Time) (int64, error) {
	return s.q.PurgeExpiredPresence(ctx, toTz(now))
}

// WipeRecord deletes all rows from the four record tables in FK-safe order:
// settlements first (references exchanges), then exchanges, utterances, and
// presence_events. Runs in a single transaction.
//
// Dev sandbox only. The durable record is a production property; this method
// must never be called outside of fresh-mode startup.
func (s *Store) WipeRecord(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, stmt := range []string{
		"DELETE FROM settlements",
		"DELETE FROM exchanges",
		"DELETE FROM utterances",
		"DELETE FROM presence_events",
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("wipe: %s: %w", stmt, err)
		}
	}
	return tx.Commit(ctx)
}
