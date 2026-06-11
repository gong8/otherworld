-- name: AppendUtterance :one
INSERT INTO utterances (id, ts, scope, payload) VALUES ($1, $2, $3, $4) RETURNING seq;

-- name: ListUtterancesSince :many
SELECT seq, payload FROM utterances WHERE scope = $1 AND seq > $2 ORDER BY seq ASC LIMIT $3;

-- name: UpsertVoice :exec
INSERT INTO voices (id, scope, charter) VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET charter = EXCLUDED.charter, scope = EXCLUDED.scope;

-- name: OpenExchange :exec
INSERT INTO exchanges (id, scope, state, participants, opened_at) VALUES ($1, $2, 'open', $3, $4);

-- name: CloseExchange :exec
UPDATE exchanges SET state = $2, closed_at = $3 WHERE id = $1;

-- name: InsertSettlement :exec
INSERT INTO settlements (id, exchange_id, scope, terms, parties, ts) VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListSettlements :many
SELECT id, exchange_id, terms, parties, ts FROM settlements WHERE scope = $1 ORDER BY ts DESC LIMIT $2;

-- name: InsertPresence :exec
INSERT INTO presence_events (id, scope, voice, event, ts, expires_at) VALUES ($1, $2, $3, $4, $5, $6);

-- name: PurgeExpiredPresence :execrows
DELETE FROM presence_events WHERE expires_at < $1;
