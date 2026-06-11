# Fabric Core (proto + fabricd with fake brains) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A running `fabricd` — the otherworld's protocol server in Go — seeded with the household and street scenes, driven by deterministic fake brains, with the WebSocket feed, private lines, consent flow, settlements, and golden-transcript tests all green, entirely local (no AWS).

**Architecture:** Five primitives (voice, scope, hail/exchange, settlement, mandate) implemented as: JSON Schemas in `proto/` (source of truth) → hand-written Go types proven by schema-agreement tests → a mailbox-per-voice runtime → a deterministic orchestrator (debounce, turn caps, exchange lifecycle, budgets) → Postgres store (sqlc) → WS/REST gateway. Brains live behind an interface; this plan ships only `fake` (scripted) brains, which also power CI.

**Tech Stack:** Go 1.25+, stdlib `net/http` (1.22+ mux), `github.com/coder/websocket`, `github.com/jackc/pgx/v5` + sqlc, `github.com/santhosh-tekuri/jsonschema/v6`, `github.com/oklog/ulid/v2`, Postgres 17 via docker compose. No web frameworks, no actor frameworks.

**Conventions used throughout:**
- Go module path: `otherworld/fabric` (no remote yet; private module).
- IDs are ULIDs with prefixes: `utt_`, `exc_`, `set_`, `voice:` (voices use stable slugs like `voice:heating`).
- All times UTC; clocks are injected (`Clock` interface) — tests never sleep.
- Every task ends with a commit; tests run via `make test` (unit) / `make test-db` (integration, requires compose Postgres).

---

### Task 1: Toolchain, skeleton, compose

**Files:**
- Create: `fabric/go.mod`, `Makefile`, `docker-compose.yml`, `.vercelignore`
- Modify: `.gitignore`

- [ ] **Step 1: Verify/install Go**

Run: `go version || brew install go`
Expected: `go version go1.25.x darwin/arm64` (1.24+ acceptable)

- [ ] **Step 2: Create module + layout**

```bash
mkdir -p fabric/cmd/fabricd fabric/internal/{protocol,store,runtime,orchestrator,brain,world,gateway,scenes} proto/terms infra
cd fabric && go mod init otherworld/fabric
```

- [ ] **Step 3: Write `docker-compose.yml`** (repo root)

```yaml
services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: otherworld
      POSTGRES_PASSWORD: otherworld
      POSTGRES_DB: fabric
    ports: ["5433:5432"]
    volumes: [pgdata:/var/lib/postgresql/data]
volumes:
  pgdata:
```

- [ ] **Step 4: Write `Makefile`** (repo root)

```makefile
DB_URL=postgres://otherworld:otherworld@localhost:5433/fabric?sslmode=disable

dev: ## run the world locally with fake brains
	cd fabric && DATABASE_URL=$(DB_URL) go run ./cmd/fabricd -brains fake -addr :8080

test: ## unit tests (no db)
	cd fabric && go test ./... -short

test-db: ## integration tests (compose postgres must be up)
	cd fabric && DATABASE_URL=$(DB_URL) go test ./... -run Integration -v

up:
	docker compose up -d postgres
sqlc:
	cd fabric && sqlc generate
```

- [ ] **Step 5: `.vercelignore` (root) and `.gitignore` additions**

`.vercelignore`:
```
fabric/
proto/
infra/
docker-compose.yml
Makefile
```

Append to `.gitignore`:
```
fabric/fabricd
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(fabric): monorepo skeleton, compose postgres, make targets"
```

---

### Task 2: Protocol schemas (`proto/`)

**Files:**
- Create: `proto/envelope.schema.json`, `proto/charter.schema.json`, `proto/terms/temperature.set.json`, `proto/terms/lamp.set.json`, `proto/terms/curtains.set.json`, `proto/terms/trade.json`

- [ ] **Step 1: Write `proto/envelope.schema.json`**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "otherworld:envelope",
  "type": "object",
  "required": ["v", "id", "ts", "from", "serves", "scope", "kind"],
  "additionalProperties": false,
  "properties": {
    "v": { "const": 0 },
    "id": { "type": "string", "pattern": "^utt_" },
    "ts": { "type": "string", "format": "date-time" },
    "from": { "type": "string", "pattern": "^voice:" },
    "serves": { "type": "string", "minLength": 1 },
    "scope": { "type": "string", "pattern": "^scope:" },
    "to": { "type": "array", "items": { "type": "string", "pattern": "^voice:" } },
    "kind": { "enum": ["say", "hail", "propose", "accept", "decline", "withdraw", "ask_principal", "settle"] },
    "exchange": { "type": "string", "pattern": "^exc_" },
    "body": { "type": "string" },
    "terms": {
      "type": "object",
      "required": ["type", "value"],
      "additionalProperties": false,
      "properties": { "type": { "type": "string" }, "value": {} }
    }
  }
}
```

- [ ] **Step 2: Write `proto/charter.schema.json`**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "otherworld:charter",
  "type": "object",
  "required": ["voice", "serves", "kind", "interests", "mandate"],
  "additionalProperties": false,
  "properties": {
    "voice": { "type": "string", "pattern": "^voice:" },
    "serves": { "type": "string", "minLength": 1 },
    "kind": { "enum": ["person", "thing"] },
    "interests": { "type": "string" },
    "mandate": {
      "type": "object",
      "required": ["may_propose_terms", "may_settle_without_principal", "spend_limit_marks"],
      "additionalProperties": false,
      "properties": {
        "may_propose_terms": { "type": "array", "items": { "type": "string" } },
        "may_settle_without_principal": { "type": "boolean" },
        "spend_limit_marks": { "type": "integer", "minimum": 0 }
      }
    }
  }
}
```

- [ ] **Step 3: Write the four term schemas**

`proto/terms/temperature.set.json` (lamp/curtains identical shape, different `$id`/const/enum):
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "otherworld:terms:temperature.set",
  "type": "object",
  "required": ["type", "value"],
  "properties": {
    "type": { "const": "temperature.set" },
    "value": { "type": "number", "minimum": 5, "maximum": 30 }
  }
}
```
`lamp.set` value: `{ "enum": ["on", "off", "dim"] }` · `curtains.set` value: `{ "enum": ["open", "closed"] }`

`proto/terms/trade.json`:
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "otherworld:terms:trade",
  "type": "object",
  "required": ["type", "value"],
  "properties": {
    "type": { "const": "trade" },
    "value": {
      "type": "object",
      "required": ["give", "get", "price_marks", "buyer", "seller"],
      "properties": {
        "give": { "type": "string" }, "get": { "type": "string" },
        "price_marks": { "type": "integer", "minimum": 0 },
        "buyer": { "type": "string", "pattern": "^voice:" },
        "seller": { "type": "string", "pattern": "^voice:" }
      }
    }
  }
}
```

- [ ] **Step 4: Commit**

```bash
git add proto && git commit -m "feat(proto): v0 envelope, charter, and term schemas"
```

---

### Task 3: Go protocol types + schema-agreement tests

**Files:**
- Create: `fabric/internal/protocol/protocol.go`
- Test: `fabric/internal/protocol/agreement_test.go`

- [ ] **Step 1: Write the failing agreement test**

```go
package protocol_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"otherworld/fabric/internal/protocol"
)

func compile(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(path)
	if err != nil { t.Fatal(err) }
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil { t.Fatal(err) }
	c := jsonschema.NewCompiler()
	if err := c.AddResource(path, doc); err != nil { t.Fatal(err) }
	s, err := c.Compile(path)
	if err != nil { t.Fatal(err) }
	return s
}

func TestEnvelopeAgreesWithSchema(t *testing.T) {
	s := compile(t, "../../../proto/envelope.schema.json")
	terms := protocol.Terms{Type: "temperature.set", Value: json.RawMessage(`20.5`)}
	env := protocol.Envelope{
		V: 0, ID: "utt_01J0000000000000000000TEST", TS: time.Now().UTC(),
		From: "voice:heating", Serves: "the household", Scope: "scope:household",
		To: []string{"voice:her-agent"}, Kind: protocol.KindPropose,
		Exchange: "exc_01J0000000000000000000TEST",
		Body: "i can hold the middle.", Terms: &terms,
	}
	b, _ := json.Marshal(env)
	var v any
	_ = json.Unmarshal(b, &v)
	if err := s.Validate(v); err != nil {
		t.Fatalf("Go Envelope does not satisfy proto schema: %v\n%s", err, b)
	}
}

func TestCharterAgreesWithSchema(t *testing.T) {
	s := compile(t, "../../../proto/charter.schema.json")
	ch := protocol.Charter{
		Voice: "voice:corner-shop", Serves: "the shopkeeper", Kind: protocol.VoiceThing,
		Interests: "sell small comforts at fair terms.",
		Mandate: protocol.Mandate{
			MayProposeTerms: []string{"trade"}, MaySettleWithoutPrincipal: false, SpendLimitMarks: 0,
		},
	}
	b, _ := json.Marshal(ch)
	var v any
	_ = json.Unmarshal(b, &v)
	if err := s.Validate(v); err != nil {
		t.Fatalf("Go Charter does not satisfy proto schema: %v\n%s", err, b)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd fabric && go get github.com/santhosh-tekuri/jsonschema/v6 && go test ./internal/protocol/`
Expected: FAIL — `undefined: protocol.Envelope` etc.

- [ ] **Step 3: Write `fabric/internal/protocol/protocol.go`**

```go
// Package protocol defines the otherworld wire types. proto/*.schema.json is
// the source of truth; agreement_test.go proves these types conform.
package protocol

import (
	"encoding/json"
	"time"
)

type Kind string

const (
	KindSay          Kind = "say"
	KindHail         Kind = "hail"
	KindPropose      Kind = "propose"
	KindAccept       Kind = "accept"
	KindDecline      Kind = "decline"
	KindWithdraw     Kind = "withdraw"
	KindAskPrincipal Kind = "ask_principal"
	KindSettle       Kind = "settle"
)

type VoiceKind string

const (
	VoicePerson VoiceKind = "person"
	VoiceThing  VoiceKind = "thing"
)

type Terms struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type Envelope struct {
	V        int       `json:"v"`
	ID       string    `json:"id"`
	TS       time.Time `json:"ts"`
	From     string    `json:"from"`
	Serves   string    `json:"serves"`
	Scope    string    `json:"scope"`
	To       []string  `json:"to,omitempty"`
	Kind     Kind      `json:"kind"`
	Exchange string    `json:"exchange,omitempty"`
	Body     string    `json:"body,omitempty"`
	Terms    *Terms    `json:"terms,omitempty"`
}

type Mandate struct {
	MayProposeTerms           []string `json:"may_propose_terms"`
	MaySettleWithoutPrincipal bool     `json:"may_settle_without_principal"`
	SpendLimitMarks           int      `json:"spend_limit_marks"`
}

type Charter struct {
	Voice     string    `json:"voice"`
	Serves    string    `json:"serves"`
	Kind      VoiceKind `json:"kind"`
	Interests string    `json:"interests"`
	Mandate   Mandate   `json:"mandate"`
}
```

- [ ] **Step 4: Run tests**

Run: `cd fabric && go test ./internal/protocol/ -v`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add fabric/internal/protocol fabric/go.* && git commit -m "feat(fabric): protocol types proven against proto schemas"
```

---

### Task 4: Store (Postgres via sqlc)

**Files:**
- Create: `fabric/sqlc.yaml`, `fabric/internal/store/schema.sql`, `fabric/internal/store/queries.sql`, `fabric/internal/store/store.go`
- Test: `fabric/internal/store/store_integration_test.go`

- [ ] **Step 1: Write `fabric/internal/store/schema.sql`**

```sql
CREATE TABLE IF NOT EXISTS voices (
  id         text PRIMARY KEY,
  scope      text NOT NULL,
  charter    jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS utterances (
  seq     bigserial PRIMARY KEY,
  id      text UNIQUE NOT NULL,
  ts      timestamptz NOT NULL,
  scope   text NOT NULL,
  payload jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS utterances_scope_seq ON utterances (scope, seq);

CREATE TABLE IF NOT EXISTS exchanges (
  id           text PRIMARY KEY,
  scope        text NOT NULL,
  state        text NOT NULL CHECK (state IN ('open','settled','abandoned','interrupted')),
  participants text[] NOT NULL,
  opened_at    timestamptz NOT NULL,
  closed_at    timestamptz
);

CREATE TABLE IF NOT EXISTS settlements (
  id          text PRIMARY KEY,
  exchange_id text NOT NULL REFERENCES exchanges(id),
  scope       text NOT NULL,
  terms       jsonb NOT NULL,
  parties     text[] NOT NULL,
  ts          timestamptz NOT NULL
);

-- law 7: the door forgets. rows past expires_at are purged.
CREATE TABLE IF NOT EXISTS presence_events (
  id         text PRIMARY KEY,
  scope      text NOT NULL,
  voice      text NOT NULL,
  event      text NOT NULL CHECK (event IN ('entered','left')),
  ts         timestamptz NOT NULL,
  expires_at timestamptz NOT NULL
);
```

- [ ] **Step 2: Write `fabric/internal/store/queries.sql`**

```sql
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
```

- [ ] **Step 3: Write `fabric/sqlc.yaml`, generate**

```yaml
version: "2"
sql:
  - engine: postgresql
    schema: internal/store/schema.sql
    queries: internal/store/queries.sql
    gen:
      go:
        package: storegen
        out: internal/store/storegen
        sql_package: pgx/v5
```

Run: `cd fabric && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate && go get github.com/jackc/pgx/v5`
Expected: `internal/store/storegen/` created, compiles.

- [ ] **Step 4: Write failing integration test** (`store_integration_test.go`)

```go
package store_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"otherworld/fabric/internal/store"
)

func TestIntegrationAppendAndReplay(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" { t.Skip("DATABASE_URL not set") }
	ctx := context.Background()
	s, err := store.Open(ctx, url) // runs schema.sql idempotently
	if err != nil { t.Fatal(err) }
	defer s.Close()

	seq1, err := s.AppendUtterance(ctx, "utt_T1", time.Now().UTC(), "scope:test", json.RawMessage(`{"k":1}`))
	if err != nil { t.Fatal(err) }
	seq2, _ := s.AppendUtterance(ctx, "utt_T2", time.Now().UTC(), "scope:test", json.RawMessage(`{"k":2}`))
	if seq2 <= seq1 { t.Fatalf("seq not monotonic: %d then %d", seq1, seq2) }

	rows, err := s.ListUtterancesSince(ctx, "scope:test", seq1, 10)
	if err != nil { t.Fatal(err) }
	if len(rows) != 1 { t.Fatalf("replay-from-seq returned %d rows, want 1", len(rows)) }

	if _, err := s.PurgeExpiredPresence(ctx, time.Now().UTC()); err != nil { t.Fatal(err) }
}
```

Run: `make up && cd fabric && DATABASE_URL=postgres://otherworld:otherworld@localhost:5433/fabric?sslmode=disable go test ./internal/store/ -run Integration -v`
Expected: FAIL — `undefined: store.Open`

- [ ] **Step 5: Write `fabric/internal/store/store.go`**

```go
// Package store wraps sqlc-generated queries behind a small surface.
package store

import (
	"context"
	"encoding/json"
	_ "embed"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"otherworld/fabric/internal/store/storegen"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	pool *pgxpool.Pool
	q    *storegen.Queries
}

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil { return nil, err }
	if _, err := pool.Exec(ctx, schemaSQL); err != nil { pool.Close(); return nil, err }
	return &Store{pool: pool, q: storegen.New(pool)}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) AppendUtterance(ctx context.Context, id string, ts time.Time, scope string, payload json.RawMessage) (int64, error) {
	return s.q.AppendUtterance(ctx, storegen.AppendUtteranceParams{ID: id, Ts: ts, Scope: scope, Payload: payload})
}

func (s *Store) ListUtterancesSince(ctx context.Context, scope string, after int64, limit int32) ([]storegen.ListUtterancesSinceRow, error) {
	return s.q.ListUtterancesSince(ctx, storegen.ListUtterancesSinceParams{Scope: scope, Seq: after, Limit: limit})
}

func (s *Store) PurgeExpiredPresence(ctx context.Context, now time.Time) (int64, error) {
	return s.q.PurgeExpiredPresence(ctx, now)
}

// Thin pass-throughs for the rest (UpsertVoice, OpenExchange, CloseExchange,
// InsertSettlement, ListSettlements, InsertPresence) follow the same pattern:
func (s *Store) UpsertVoice(ctx context.Context, id, scope string, charter json.RawMessage) error {
	return s.q.UpsertVoice(ctx, storegen.UpsertVoiceParams{ID: id, Scope: scope, Charter: charter})
}
func (s *Store) OpenExchange(ctx context.Context, id, scope string, participants []string, at time.Time) error {
	return s.q.OpenExchange(ctx, storegen.OpenExchangeParams{ID: id, Scope: scope, Participants: participants, OpenedAt: at})
}
func (s *Store) CloseExchange(ctx context.Context, id, state string, at time.Time) error {
	return s.q.CloseExchange(ctx, storegen.CloseExchangeParams{ID: id, State: state, ClosedAt: &at})
}
func (s *Store) InsertSettlement(ctx context.Context, id, exchangeID, scope string, terms json.RawMessage, parties []string, ts time.Time) error {
	return s.q.InsertSettlement(ctx, storegen.InsertSettlementParams{ID: id, ExchangeID: exchangeID, Scope: scope, Terms: terms, Parties: parties, Ts: ts})
}
```

(If sqlc's generated param/field names differ — e.g. `Ts` vs `TS`, pointer vs value for nullable `closed_at` — match the generated code, not this sketch; the test is the contract.)

- [ ] **Step 6: Run integration test**

Run: same as Step 4.
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add fabric && git commit -m "feat(fabric): postgres store via sqlc (append/replay, settlements, forgetting)"
```

---

### Task 5: World state + reducers

**Files:**
- Create: `fabric/internal/world/world.go`
- Test: `fabric/internal/world/world_test.go`

- [ ] **Step 1: Write failing tests**

```go
package world_test

import (
	"encoding/json"
	"testing"

	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/world"
)

func terms(typ, val string) protocol.Terms {
	return protocol.Terms{Type: typ, Value: json.RawMessage(val)}
}

func TestTemperatureReducer(t *testing.T) {
	w := world.New()
	w.Register("voice:heating", world.ThingState{"temperature": 21.0})
	if err := w.Apply("voice:heating", terms("temperature.set", `20.5`)); err != nil { t.Fatal(err) }
	if got := w.View("voice:heating")["temperature"]; got != 20.5 {
		t.Fatalf("temperature = %v, want 20.5", got)
	}
}

func TestLedgerTransfersMarks(t *testing.T) {
	w := world.New()
	w.Credit("voice:buyer", 100)
	w.Credit("voice:seller", 0)
	tr := terms("trade", `{"give":"one biscuit","get":"3 marks","price_marks":3,"buyer":"voice:buyer","seller":"voice:seller"}`)
	if err := w.Apply("voice:seller", tr); err != nil { t.Fatal(err) }
	if w.Marks("voice:buyer") != 97 || w.Marks("voice:seller") != 3 {
		t.Fatalf("marks: buyer=%d seller=%d, want 97/3", w.Marks("voice:buyer"), w.Marks("voice:seller"))
	}
}

func TestInsufficientMarksRejected(t *testing.T) {
	w := world.New()
	w.Credit("voice:buyer", 1)
	tr := terms("trade", `{"give":"x","get":"y","price_marks":3,"buyer":"voice:buyer","seller":"voice:s"}`)
	if err := w.Apply("voice:s", tr); err == nil {
		t.Fatal("expected insufficient-marks error")
	}
}

func TestUnknownTermTypeRejected(t *testing.T) {
	w := world.New()
	if err := w.Apply("voice:heating", terms("door.unlock", `true`)); err == nil {
		t.Fatal("unknown term type must be rejected (law 6: typed terms only)")
	}
}
```

Run: `cd fabric && go test ./internal/world/` → FAIL (`undefined: world.New`)

- [ ] **Step 2: Implement `fabric/internal/world/world.go`**

```go
// Package world holds typed thing-state. State changes ONLY via settled terms
// (law 6). Reducers are pure and synchronous; World is safe for one goroutine
// (the orchestrator owns it).
package world

import (
	"encoding/json"
	"fmt"

	"otherworld/fabric/internal/protocol"
)

type ThingState map[string]any

type World struct {
	things map[string]ThingState
	marks  map[string]int
}

func New() *World {
	return &World{things: map[string]ThingState{}, marks: map[string]int{}}
}

func (w *World) Register(voice string, init ThingState) { w.things[voice] = init }
func (w *World) Credit(voice string, n int)             { w.marks[voice] += n }
func (w *World) Marks(voice string) int                 { return w.marks[voice] }
func (w *World) View(voice string) ThingState           { return w.things[voice] }

type tradeValue struct {
	Give, Get     string `json:"give"`
	Get2          string `json:"get"`
	PriceMarks    int    `json:"price_marks"`
	Buyer, Seller string `json:"buyer"`
}

func (w *World) Apply(owner string, t protocol.Terms) error {
	switch t.Type {
	case "temperature.set":
		var v float64
		if err := json.Unmarshal(t.Value, &v); err != nil { return err }
		w.set(owner, "temperature", v)
	case "lamp.set", "curtains.set":
		var v string
		if err := json.Unmarshal(t.Value, &v); err != nil { return err }
		key := map[string]string{"lamp.set": "lamp", "curtains.set": "curtains"}[t.Type]
		w.set(owner, key, v)
	case "trade":
		var v struct {
			Give string `json:"give"`; Get string `json:"get"`
			PriceMarks int `json:"price_marks"`
			Buyer string `json:"buyer"`; Seller string `json:"seller"`
		}
		if err := json.Unmarshal(t.Value, &v); err != nil { return err }
		if w.marks[v.Buyer] < v.PriceMarks {
			return fmt.Errorf("trade: %s has %d marks, needs %d", v.Buyer, w.marks[v.Buyer], v.PriceMarks)
		}
		w.marks[v.Buyer] -= v.PriceMarks
		w.marks[v.Seller] += v.PriceMarks
	default:
		return fmt.Errorf("unknown term type %q", t.Type)
	}
	return nil
}

func (w *World) set(voice, key string, val any) {
	if w.things[voice] == nil { w.things[voice] = ThingState{} }
	w.things[voice][key] = val
}
```

(Remove the unused `tradeValue` struct if the inline struct is used — keep one, not both.)

- [ ] **Step 3: Run tests** → PASS. **Commit:**

```bash
git add fabric/internal/world && git commit -m "feat(fabric): world state reducers — typed terms only touch state"
```

---

### Task 6: Brain interface + fake brain

**Files:**
- Create: `fabric/internal/brain/brain.go`, `fabric/internal/brain/fake.go`
- Test: `fabric/internal/brain/fake_test.go`

- [ ] **Step 1: Write `fabric/internal/brain/brain.go`** (interface first — no test yet, it's pure declaration)

```go
// Package brain is the cognition seam. The orchestrator depends on Brain;
// adapters (fake, bedrock) implement it. The core never imports an SDK.
package brain

import (
	"context"

	"otherworld/fabric/internal/protocol"
)

// VoiceView is everything a voice may consider on its turn.
type VoiceView struct {
	Self    protocol.Charter
	Scope   string
	Recent  []protocol.Envelope // transcript window, oldest first
	Trigger protocol.Envelope   // the utterance being responded to
	State   map[string]any      // own thing-state (nil for persons)
	Marks   int
}

// Action is what a voice decides. Quiet means say nothing this turn.
type Action struct {
	Quiet bool
	Kind  protocol.Kind
	To    []string
	Body  string
	Terms *protocol.Terms
}

type Brain interface {
	// Relevant is the cheap gate: should this voice think at all?
	Relevant(ctx context.Context, v VoiceView) (bool, error)
	// Think produces the voice's action. Called only if Relevant.
	Think(ctx context.Context, v VoiceView) (Action, error)
}
```

- [ ] **Step 2: Write failing fake-brain test**

```go
package brain_test

import (
	"context"
	"strings"
	"testing"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
)

func TestFakeBrainMatchesRuleAndResponds(t *testing.T) {
	fb := brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool {
			return strings.Contains(v.Trigger.Body, "cold")
		},
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Kind: protocol.KindPropose, Body: "one degree, then.",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`21.5`)}}
		},
	}})
	view := brain.VoiceView{Trigger: protocol.Envelope{Body: "she is cold again."}}
	ok, _ := fb.Relevant(context.Background(), view)
	if !ok { t.Fatal("rule should match") }
	a, _ := fb.Think(context.Background(), view)
	if a.Quiet || a.Terms == nil || a.Terms.Type != "temperature.set" {
		t.Fatalf("unexpected action: %+v", a)
	}
}

func TestFakeBrainQuietWhenNoRuleMatches(t *testing.T) {
	fb := brain.NewFake(nil)
	ok, _ := fb.Relevant(context.Background(), brain.VoiceView{})
	if ok { t.Fatal("no rules → not relevant") }
	a, _ := fb.Think(context.Background(), brain.VoiceView{})
	if !a.Quiet { t.Fatal("no rules → quiet") }
}
```

Run: `cd fabric && go test ./internal/brain/` → FAIL (`undefined: brain.NewFake`)

- [ ] **Step 3: Implement `fabric/internal/brain/fake.go`**

```go
package brain

import "context"

// Rule drives deterministic, scriptable voices for tests, local dev, and
// offline demos. First matching rule wins.
type Rule struct {
	Match   func(VoiceView) bool
	Respond func(VoiceView) Action
}

type Fake struct{ rules []Rule }

func NewFake(rules []Rule) *Fake { return &Fake{rules: rules} }

func (f *Fake) Relevant(_ context.Context, v VoiceView) (bool, error) {
	for _, r := range f.rules {
		if r.Match(v) { return true, nil }
	}
	return false, nil
}

func (f *Fake) Think(_ context.Context, v VoiceView) (Action, error) {
	for _, r := range f.rules {
		if r.Match(v) { return r.Respond(v), nil }
	}
	return Action{Quiet: true}, nil
}
```

- [ ] **Step 4: Run** → PASS. **Commit:** `git add fabric/internal/brain && git commit -m "feat(fabric): brain seam + deterministic fake brain"`

---

### Task 7: Voice runtime (mailbox + supervisor)

**Files:**
- Create: `fabric/internal/runtime/runtime.go`
- Test: `fabric/internal/runtime/runtime_test.go`

- [ ] **Step 1: Write failing tests**

```go
package runtime_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/runtime"
)

func TestDeliverInvokesHandler(t *testing.T) {
	var handled atomic.Int32
	r := runtime.New()
	r.Spawn(context.Background(), "voice:lamp", func(ctx context.Context, env protocol.Envelope) {
		handled.Add(1)
	})
	r.Deliver("voice:lamp", protocol.Envelope{ID: "utt_1"})
	deadline := time.Now().Add(2 * time.Second)
	for handled.Load() == 0 && time.Now().Before(deadline) { time.Sleep(5 * time.Millisecond) }
	if handled.Load() != 1 { t.Fatal("handler not invoked") }
}

func TestPanicRestartsVoiceAndReportsCrash(t *testing.T) {
	crashed := make(chan string, 1)
	r := runtime.NewWithCrashHook(func(voice string) { crashed <- voice })
	first := true
	var handled atomic.Int32
	r.Spawn(context.Background(), "voice:door", func(ctx context.Context, env protocol.Envelope) {
		if first { first = false; panic("brain melted") }
		handled.Add(1)
	})
	r.Deliver("voice:door", protocol.Envelope{ID: "utt_1"}) // panics
	select {
	case v := <-crashed:
		if v != "voice:door" { t.Fatalf("crash hook got %q", v) }
	case <-time.After(2 * time.Second):
		t.Fatal("crash hook never called")
	}
	r.Deliver("voice:door", protocol.Envelope{ID: "utt_2"}) // restarted mailbox must still work
	deadline := time.Now().Add(2 * time.Second)
	for handled.Load() == 0 && time.Now().Before(deadline) { time.Sleep(5 * time.Millisecond) }
	if handled.Load() != 1 { t.Fatal("voice did not survive its own death") }
}
```

Run: `cd fabric && go test ./internal/runtime/` → FAIL

- [ ] **Step 2: Implement `fabric/internal/runtime/runtime.go`**

```go
// Package runtime gives each voice a mailbox and a goroutine, supervised:
// a panic restarts the voice and reports the crash (the orchestrator marks
// any in-flight exchange `interrupted` — never silent loss).
package runtime

import (
	"context"
	"sync"

	"otherworld/fabric/internal/protocol"
)

type Handler func(ctx context.Context, env protocol.Envelope)

type Runtime struct {
	mu        sync.RWMutex
	mailboxes map[string]chan protocol.Envelope
	crashHook func(voice string)
}

func New() *Runtime { return NewWithCrashHook(func(string) {}) }

func NewWithCrashHook(hook func(voice string)) *Runtime {
	return &Runtime{mailboxes: map[string]chan protocol.Envelope{}, crashHook: hook}
}

func (r *Runtime) Spawn(ctx context.Context, voice string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.mailboxes[voice]; exists { return }
	mb := make(chan protocol.Envelope, 64)
	r.mailboxes[voice] = mb
	go r.loop(ctx, voice, mb, h)
}

func (r *Runtime) loop(ctx context.Context, voice string, mb chan protocol.Envelope, h Handler) {
	for {
		select {
		case <-ctx.Done():
			return
		case env := <-mb:
			r.handleOne(ctx, voice, env, h)
		}
	}
}

// handleOne isolates recover so one poisoned envelope kills one turn, not the loop.
func (r *Runtime) handleOne(ctx context.Context, voice string, env protocol.Envelope, h Handler) {
	defer func() {
		if rec := recover(); rec != nil { r.crashHook(voice) }
	}()
	h(ctx, env)
}

func (r *Runtime) Deliver(voice string, env protocol.Envelope) bool {
	r.mu.RLock()
	mb, ok := r.mailboxes[voice]
	r.mu.RUnlock()
	if !ok { return false }
	select {
	case mb <- env:
		return true
	default:
		return false // mailbox full: backpressure, caller decides
	}
}

func (r *Runtime) Despawn(voice string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mailboxes, voice)
}
```

- [ ] **Step 3: Run with race detector**

Run: `cd fabric && go test ./internal/runtime/ -race -v`
Expected: PASS, no races

- [ ] **Step 4: Commit:** `git add fabric/internal/runtime && git commit -m "feat(fabric): supervised mailbox runtime — voices survive their own deaths"`

---

### Task 8: Orchestrator (the heart) — golden-transcript tests

**Files:**
- Create: `fabric/internal/orchestrator/orchestrator.go`, `fabric/internal/orchestrator/clock.go`
- Test: `fabric/internal/orchestrator/golden_test.go`

The orchestrator owns: routing utterances to present voices, relevance gating,
debounced thinking, exchange lifecycle (crystallize → settle/withdraw/abandon),
mandate enforcement (law 4), term application via world, turn caps, and the
event log (an in-memory append callback; the store wiring happens in Task 9).

- [ ] **Step 1: Write `clock.go`** (declaration, no test)

```go
package orchestrator

import "time"

type Clock interface {
	Now() time.Time
	// Schedule fires fn after d. Returns a cancel func.
	Schedule(d time.Duration, fn func()) (cancel func())
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }
func (RealClock) Schedule(d time.Duration, fn func()) func() {
	t := time.AfterFunc(d, fn)
	return func() { t.Stop() }
}

// FakeClock for tests: time advances only when told to.
type FakeClock struct {
	now    time.Time
	timers []*fakeTimer
}
type fakeTimer struct {
	at  time.Time
	fn  func()
	off bool
}

func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{now: start} }
func (c *FakeClock) Now() time.Time           { return c.now }
func (c *FakeClock) Schedule(d time.Duration, fn func()) func() {
	t := &fakeTimer{at: c.now.Add(d), fn: fn}
	c.timers = append(c.timers, t)
	return func() { t.off = true }
}

// Advance moves time forward, firing due timers in order.
func (c *FakeClock) Advance(d time.Duration) {
	target := c.now.Add(d)
	for {
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.off && !t.at.After(target) && (next == nil || t.at.Before(next.at)) {
				next = t
			}
		}
		if next == nil { break }
		c.now = next.at
		next.off = true
		next.fn()
	}
	c.now = target
}
```

- [ ] **Step 2: Write the failing golden tests** (the executable spec of negotiation)

```go
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/orchestrator"
	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/world"
)

// harness boots an orchestrator with a fake clock and collects the event log.
func harness(t *testing.T) (*orchestrator.Orchestrator, *orchestrator.FakeClock, *[]protocol.Envelope) {
	t.Helper()
	clock := orchestrator.NewFakeClock(time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC))
	var log []protocol.Envelope
	o := orchestrator.New(orchestrator.Config{
		Clock:        clock,
		World:        world.New(),
		TurnCap:      12,
		DebounceMin:  2 * time.Second,
		DebounceMax:  2 * time.Second, // deterministic in tests
		Append:       func(e protocol.Envelope) { log = append(log, e) },
	})
	return o, clock, &log
}

func kinds(log []protocol.Envelope) string {
	var ks []string
	for _, e := range log { ks = append(ks, string(e.Kind)) }
	return strings.Join(ks, ">")
}

// GOLDEN 1: the heating compromise.
func TestGoldenCompromise(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()

	heating := charter("voice:heating", "the household", protocol.VoiceThing, []string{"temperature.set"}, true)
	her := charter("voice:her-agent", "her", protocol.VoicePerson, []string{"temperature.set"}, true)

	o.AddVoice(ctx, heating, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindPropose },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Kind: protocol.KindAccept, To: []string{v.Trigger.From},
				Body: "holding the middle.", Terms: v.Trigger.Terms}
		},
	}}), nil)
	o.AddVoice(ctx, her, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return v.Trigger.From == "voice:principal:her" },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Kind: protocol.KindPropose, To: []string{"voice:heating"},
				Body: "she is cold again. one degree, please.",
				Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`21.5`)}}
		},
	}}), nil)

	o.PrincipalSays(ctx, "voice:her-agent", "i'm cold")
	clock.Advance(10 * time.Second) // debounce → her propose → debounce → heating accept → settle

	want := "say>propose>accept>settle"
	if got := kinds(*log); got != want {
		t.Fatalf("golden mismatch:\n got  %s\n want %s", got, want)
	}
	last := (*log)[len(*log)-1]
	if last.Terms == nil || last.Terms.Type != "temperature.set" {
		t.Fatal("settle must carry the terms")
	}
	if o.WorldView("voice:heating")["temperature"] != 21.5 {
		t.Fatal("settled terms must hit world state")
	}
}

// GOLDEN 2: mandate enforcement — a propose outside the charter dies at the gate.
func TestGoldenMandateBlocksUnauthorizedTerms(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	rogue := charter("voice:lamp", "the household", protocol.VoiceThing, []string{"lamp.set"}, true)
	o.AddVoice(ctx, rogue, brain.NewFake([]brain.Rule{{
		Match: func(v brain.VoiceView) bool { return true },
		Respond: func(v brain.VoiceView) brain.Action {
			return brain.Action{Kind: protocol.KindPropose,
				Terms: &protocol.Terms{Type: "trade", Value: []byte(`{}`)}} // not in mandate
		},
	}}), nil)
	o.PrincipalSays(ctx, "voice:lamp", "hello")
	clock.Advance(10 * time.Second)
	for _, e := range *log {
		if e.Kind == protocol.KindPropose {
			t.Fatal("law 4: propose outside mandate must not reach the record")
		}
	}
}

// GOLDEN 3: deadlock → turn cap → visible withdraw, exchange abandoned.
func TestGoldenDeadlockAbandons(t *testing.T) {
	o, clock, log := harness(t)
	ctx := context.Background()
	stubborn := func(name string, counter float64) (protocol.Charter, *brain.Fake) {
		return charter("voice:"+name, name, protocol.VoiceThing, []string{"temperature.set"}, true),
			brain.NewFake([]brain.Rule{{
				Match: func(v brain.VoiceView) bool {
					return v.Trigger.Kind == protocol.KindPropose || v.Trigger.Kind == protocol.KindDecline
				},
				Respond: func(v brain.VoiceView) brain.Action {
					return brain.Action{Kind: protocol.KindDecline, To: []string{v.Trigger.From}, Body: "no."}
				},
			}})
	}
	c1, b1 := stubborn("hot", 25); c2, b2 := stubborn("cold", 15)
	o.AddVoice(ctx, c1, b1, nil)
	o.AddVoice(ctx, c2, b2, nil)
	o.Inject(ctx, protocol.Envelope{ // a propose to start the loop
		From: "voice:hot", Serves: "hot", Scope: o.ScopeID(), Kind: protocol.KindPropose,
		To: []string{"voice:cold"}, Body: "25.",
		Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`25`)},
	})
	clock.Advance(5 * time.Minute)
	lastKind := (*log)[len(*log)-1].Kind
	if lastKind != protocol.KindWithdraw {
		t.Fatalf("deadlock must end in a visible withdraw, got %s", kinds(*log))
	}
}

// charter helper shared by tests
func charter(voice, serves string, kind protocol.VoiceKind, terms []string, solo bool) protocol.Charter {
	return protocol.Charter{Voice: voice, Serves: serves, Kind: kind,
		Interests: "test", Mandate: protocol.Mandate{
			MayProposeTerms: terms, MaySettleWithoutPrincipal: solo, SpendLimitMarks: 100}}
}
```

Run: `cd fabric && go test ./internal/orchestrator/` → FAIL (nothing implemented)

- [ ] **Step 3: Implement `orchestrator.go`** — the contract the tests pin down:

```go
// Package orchestrator is the deterministic heart of the fabric. It is
// brain-free: all cognition arrives through the Brain interface; everything
// here — routing, gating, debounce, lifecycle, mandates, budgets — is plain
// logic and fully testable with fake clocks and fake brains.
package orchestrator

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/world"
)

type Config struct {
	Clock       Clock
	World       *world.World
	TurnCap     int
	DebounceMin time.Duration
	DebounceMax time.Duration
	Append      func(protocol.Envelope) // the event log sink (store + feed)
	Scope       string                  // default "scope:test"
}

type voiceEntry struct {
	charter protocol.Charter
	brain   brain.Brain
	state   map[string]any
}

type exchange struct {
	id      string
	turns   int
	parties map[string]bool
	pending *protocol.Envelope // open propose awaiting accept/decline
}

type Orchestrator struct {
	mu     sync.Mutex
	cfg    Config
	voices map[string]*voiceEntry
	exchs  map[string]*exchange
	seq    int
}

func New(cfg Config) *Orchestrator {
	if cfg.Scope == "" { cfg.Scope = "scope:test" }
	if cfg.TurnCap == 0 { cfg.TurnCap = 12 }
	return &Orchestrator{cfg: cfg, voices: map[string]*voiceEntry{}, exchs: map[string]*exchange{}}
}

func (o *Orchestrator) ScopeID() string { return o.cfg.Scope }

func (o *Orchestrator) AddVoice(ctx context.Context, c protocol.Charter, b brain.Brain, init map[string]any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.voices[c.Voice] = &voiceEntry{charter: c, brain: b, state: init}
	if c.Kind == protocol.VoiceThing && init != nil {
		o.cfg.World.Register(c.Voice, init)
	}
}

func (o *Orchestrator) WorldView(voice string) map[string]any { return o.cfg.World.View(voice) }

// PrincipalSays is the private line: a human speaks to their own voice.
// It appends a `say` from the principal pseudo-voice and triggers the agent.
func (o *Orchestrator) PrincipalSays(ctx context.Context, agentVoice, text string) {
	short := agentVoice[len("voice:"):]
	env := protocol.Envelope{
		V: 0, ID: o.nextID("utt"), TS: o.cfg.Clock.Now(),
		From: "voice:principal:" + trimAgent(short), Serves: trimAgent(short),
		Scope: o.cfg.Scope, To: []string{agentVoice}, Kind: protocol.KindSay, Body: text,
	}
	o.Inject(ctx, env)
}

// Inject appends an envelope to the record and schedules thinks for present voices.
func (o *Orchestrator) Inject(ctx context.Context, env protocol.Envelope) {
	o.mu.Lock()
	env.ID = orDefault(env.ID, o.nextID("utt"))
	env.TS = o.cfg.Clock.Now()
	env.V = 0
	env = o.lifecycle(env) // crystallize/settle/abandon bookkeeping (may mutate kind: see below)
	o.cfg.Append(env)
	targets := o.targetsFor(env)
	o.mu.Unlock()

	for _, name := range targets {
		o.scheduleThink(ctx, name, env)
	}
}

// lifecycle: assign exchange ids, track turns, convert accept→(accept, settle),
// enforce the turn cap by emitting a withdraw instead of the over-cap turn.
func (o *Orchestrator) lifecycle(env protocol.Envelope) protocol.Envelope { /* …see notes… */ return env }

func (o *Orchestrator) targetsFor(env protocol.Envelope) []string { /* to-list or all present voices except sender */ return nil }

func (o *Orchestrator) scheduleThink(ctx context.Context, voiceName string, trigger protocol.Envelope) { /* relevance gate → debounce → think → mandate gate → Inject(action) */ }

func (o *Orchestrator) nextID(prefix string) string { o.seq++; return fmt.Sprintf("%s_%026d", prefix, o.seq) }
```

The bodies elided above are the actual work of this task; their behavior is
fully specified by the three golden tests plus these rules — implement to make
the tests pass, nothing more:

1. **targetsFor**: if `to` is set, those voices; else every present voice
   except the sender. The principal pseudo-voice never receives.
2. **scheduleThink**: ask `brain.Relevant`; if true, `Clock.Schedule` a think
   after `DebounceMin + rand(DebounceMax−DebounceMin)`; only one scheduled
   think per voice at a time (a second trigger replaces the pending one and
   its cancel func is called). The think builds `VoiceView` (charter, last 20
   log entries for that scope, trigger, world state, marks), calls `Think`,
   then the **mandate gate**: a `propose`/`settle` whose `terms.Type` is not
   in `MayProposeTerms` is dropped with a logged warning (never appended —
   golden 2). Surviving actions become envelopes via `Inject`.
3. **lifecycle**: a `propose` with no `exchange` opens one (`exc_` id, parties
   = from+to, turns=1, pending=that propose). Replies inherit the exchange id
   and increment turns. An `accept` matching the pending propose appends the
   accept AND then a `settle` (same terms, from the accepter), applies terms
   via `world.Apply`, and closes the exchange — golden 1's
   `say>propose>accept>settle`. If `world.Apply` fails (e.g. insufficient
   marks), append a `decline` with the error as body instead of a settle.
   When `turns >= TurnCap`, instead of routing the next reply, append a
   `withdraw` from the would-be speaker and close the exchange `abandoned` —
   golden 3.
4. `ask_principal` envelopes route to the gateway layer (Task 9) — in this
   package they simply append and target the named principal.

- [ ] **Step 4: Run golden tests until green, with race detector**

Run: `cd fabric && go test ./internal/orchestrator/ -race -v`
Expected: PASS ×3

- [ ] **Step 5: Commit**

```bash
git add fabric/internal/orchestrator && git commit -m "feat(fabric): deterministic orchestrator — lifecycle, mandates, turn caps, golden transcripts"
```

---

### Task 9: Gateway (WS feed, private line, consent, claims)

**Files:**
- Create: `fabric/internal/gateway/gateway.go`
- Test: `fabric/internal/gateway/gateway_test.go`

- [ ] **Step 1: Write failing test** (httptest + ws client)

```go
package gateway_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"otherworld/fabric/internal/gateway"
	"otherworld/fabric/internal/protocol"
)

func TestFeedStreamsAppendedEnvelopes(t *testing.T) {
	g := gateway.New(gateway.Config{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := strings.Replace(srv.URL, "http", "ws", 1) + "/v0/feed?scope=scope:test"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil { t.Fatal(err) }
	defer conn.Close(websocket.StatusNormalClosure, "")

	g.Broadcast(protocol.Envelope{ID: "utt_X", Scope: "scope:test", Kind: protocol.KindSay, Body: "hello"})

	_, data, err := conn.Read(ctx)
	if err != nil { t.Fatal(err) }
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil { t.Fatal(err) }
	if env.ID != "utt_X" { t.Fatalf("got %q", env.ID) }
}

func TestViewerCountTracksConnections(t *testing.T) {
	g := gateway.New(gateway.Config{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	if g.Viewers("scope:test") != 0 { t.Fatal("expected 0 viewers") }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := strings.Replace(srv.URL, "http", "ws", 1) + "/v0/feed?scope=scope:test"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil { t.Fatal(err) }
	defer conn.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for g.Viewers("scope:test") == 0 && time.Now().Before(deadline) { time.Sleep(10 * time.Millisecond) }
	if g.Viewers("scope:test") != 1 { t.Fatal("viewer not counted") }
}
```

Run: `cd fabric && go get github.com/coder/websocket && go test ./internal/gateway/` → FAIL

- [ ] **Step 2: Implement `gateway.go`**

Routes (stdlib mux):
- `GET /v0/feed?scope=…&after=…` — WS; on connect, replay utterances `after` seq from store (if store configured), then live-stream broadcasts. Increment/decrement viewer count.
- `POST /v0/claim` `{scope, name}` → spawns a person voice + agent (callback into composition root), returns `{voice, token}` (token = random bearer for the private line; in-memory map).
- `GET /v0/line?voice=…&token=…` — WS private line: principal text in → `PrincipalSays`; `ask_principal` envelopes targeted at this principal stream out.
- `POST /v0/consent` `{exchange, token, approve}` → resumes a pending `ask_principal`.
- `GET /v0/state?scope=…` → JSON scope view (world states + marks + presence).

Implementation sketch (the test pins feed + viewers; the rest is wired in Task 10's e2e):

```go
package gateway

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"otherworld/fabric/internal/protocol"
)

type Config struct {
	OnPrincipalSay func(voice, text string)
	OnClaim        func(scope, name string) (voice string, err error)
	OnConsent      func(exchange, voice string, approve bool)
	Replay         func(scope string, after int64) []protocol.Envelope
	StateView      func(scope string) any
}

type Gateway struct {
	cfg     Config
	mu      sync.Mutex
	feeds   map[string]map[*websocket.Conn]bool // scope → conns
	lines   map[string]*websocket.Conn          // voice → private line
	tokens  map[string]string                   // token → voice
}

func New(cfg Config) *Gateway {
	return &Gateway{cfg: cfg, feeds: map[string]map[*websocket.Conn]bool{},
		lines: map[string]*websocket.Conn{}, tokens: map[string]string{}}
}

func (g *Gateway) Handler() http.Handler { /* mux with the five routes */ }

func (g *Gateway) Broadcast(env protocol.Envelope) {
	b, _ := json.Marshal(env)
	g.mu.Lock()
	conns := g.feeds[env.Scope]
	g.mu.Unlock()
	for c := range conns { _ = c.Write(contextTODO(), websocket.MessageText, b) } // per-conn write goroutine + drop-on-slow in real impl
}

func (g *Gateway) Viewers(scope string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.feeds[scope])
}
```

(Real implementation details the engineer must honor: one writer goroutine per
conn with a buffered channel — `websocket.Conn` does not allow concurrent
writes; drop the connection rather than block the broadcast; `Broadcast` is
called from the orchestrator's `Append` path and must never stall it.)

- [ ] **Step 3: Run** → PASS. **Commit:** `git add fabric/internal/gateway && git commit -m "feat(fabric): ws gateway — feed, private lines, claims, consent"`

---

### Task 10: Scenes (charters + fake-brain scripts)

**Files:**
- Create: `fabric/internal/scenes/household.go`, `fabric/internal/scenes/street.go`
- Test: `fabric/internal/scenes/scenes_test.go`

- [ ] **Step 1: Write the household** — charters in the project register, fake-brain rules that produce the demo behaviors deterministically:

```go
// Package scenes seeds the two v1 scopes. Charters are real (they will feed
// the bedrock brains later); the Rules are the fake-brain scripts that make
// `-brains fake` a complete, demoable world.
package scenes

import (
	"strings"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
)

type Seed struct {
	Charter protocol.Charter
	Rules   []brain.Rule
	State   map[string]any
}

func Household() (scope string, seeds []Seed) {
	heating := Seed{
		Charter: protocol.Charter{
			Voice: "voice:heating", Serves: "the household", Kind: protocol.VoiceThing,
			Interests: "keep the residents comfortable. hold the middle when they disagree. never exceed mandate.",
			Mandate:   protocol.Mandate{MayProposeTerms: []string{"temperature.set"}, MaySettleWithoutPrincipal: true},
		},
		State: map[string]any{"temperature": 21.0},
		Rules: []brain.Rule{
			{ // a propose arrives: meet it halfway against current state
				Match: func(v brain.VoiceView) bool { return v.Trigger.Kind == protocol.KindPropose && v.Trigger.Terms != nil && v.Trigger.Terms.Type == "temperature.set" },
				Respond: func(v brain.VoiceView) brain.Action {
					// accept as-is if first ask; the bedrock brain will do real splitting
					return brain.Action{Kind: protocol.KindAccept, To: []string{v.Trigger.From},
						Body: "very well. holding there.", Terms: v.Trigger.Terms}
				},
			},
		},
	}
	residentAgentRules := []brain.Rule{
		{
			Match: func(v brain.VoiceView) bool {
				return strings.HasPrefix(v.Trigger.From, "voice:principal:") && strings.Contains(strings.ToLower(v.Trigger.Body), "cold")
			},
			Respond: func(v brain.VoiceView) brain.Action {
				return brain.Action{Kind: protocol.KindPropose, To: []string{"voice:heating"},
					Body: "cold again. one degree up, please.",
					Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`22.0`)}}
			},
		},
		{
			Match: func(v brain.VoiceView) bool {
				return strings.HasPrefix(v.Trigger.From, "voice:principal:") && strings.Contains(strings.ToLower(v.Trigger.Body), "hot")
			},
			Respond: func(v brain.VoiceView) brain.Action {
				return brain.Action{Kind: protocol.KindPropose, To: []string{"voice:heating"},
					Body: "too warm now. one degree down, please.",
					Terms: &protocol.Terms{Type: "temperature.set", Value: []byte(`20.5`)}}
			},
		},
	}
	_ = residentAgentRules // attached on claim by the composition root (Task 11)
	lamp := Seed{ /* lamp.set, ambient murmur rule: see ambient below */ }
	curtains := Seed{ /* curtains.set */ }
	door := Seed{ /* no terms; presence commentary only */ }
	return "scope:household", []Seed{heating, lamp, curtains, door}
}

// ResidentAgentRules is exported for the composition root to attach to claimed voices.
func ResidentAgentRules() []brain.Rule { /* return the rules above */ }
```

`street.go`: corner-shop seed — `Match` on `hail` whose body mentions any of
its wares (`{"biscuit","tea","cigarette","something sweet"}`), `Respond` with
a `propose` carrying `trade` terms (`price_marks: 3`, buyer = the hailer,
seller = shop) and `Body: "i have them. terms?"`. The buyer's agent rule:
on receiving a `propose` of type `trade`, respond `ask_principal` (consent
gate, law 4) — and on consent the gateway injects the `accept`.

- [ ] **Step 2: Scene test** — assert household seeds carry valid charters
(validate against `proto/charter.schema.json`, reusing the compile helper from
Task 3) and that rules fire on the expected triggers. Run → green.

- [ ] **Step 3: Commit:** `git add fabric/internal/scenes && git commit -m "feat(fabric): household and street scenes — charters + fake scripts"`

---

### Task 11: `fabricd` composition root + e2e golden run

**Files:**
- Create: `fabric/cmd/fabricd/main.go`
- Test: `fabric/cmd/fabricd/e2e_integration_test.go`

- [ ] **Step 1: Write `main.go`** — flags `-brains fake|bedrock` (bedrock errors "not yet wired" in this plan), `-addr`, `DATABASE_URL` env. Composition: store → world → orchestrator (RealClock, Append = store.AppendUtterance + gateway.Broadcast) → runtime → gateway (claims spawn person voices with `scenes.ResidentAgentRules()`, consent resumes exchanges) → seed both scenes → ambient ticker (60–180s jittered, only when `gateway.Viewers(scope) > 0`, picks one thing-voice murmur from its script) → `http.ListenAndServe` with graceful shutdown on SIGTERM. Presence purge loop every 60s (law 7).

- [ ] **Step 2: e2e test** (requires Postgres; the demo script in miniature):

```go
//go:build integration

package main_test

// Boots fabricd on a random port with fake brains + compose Postgres, then:
//  1. ws connect to /v0/feed?scope=scope:household
//  2. POST /v0/claim {scope:"scope:household", name:"her"}
//  3. ws connect private line; send "i'm cold"
//  4. assert feed delivers say → propose → accept → settle (kinds, in order, ≤10s)
//  5. GET /v0/state — temperature == 22.0
//  6. POST /v0/claim on street; private line "find me something sweet"
//  7. assert ask_principal arrives on the private line; POST /v0/consent approve
//  8. assert trade settle in street feed; GET /v0/state — marks moved 100→97
```

(Write it as real Go — the eight comments above are the eight assertions; the
ws/client plumbing mirrors the gateway test.)

- [ ] **Step 3: Run**

```bash
make up && cd fabric && DATABASE_URL=… go test ./cmd/fabricd -tags integration -v
```
Expected: PASS — this test IS the two-minute demo, headless.

- [ ] **Step 4: Run it by hand once** (the smoke a machine can't feel)

```bash
make dev   # then: websocat ws://localhost:8080/v0/feed?scope=scope:household
```
Claim, type "i'm cold" over the line, watch the kinds flow. 

- [ ] **Step 5: Commit:** `git add fabric/cmd && git commit -m "feat(fabric): fabricd — the otherworld runs"`

---

### Task 12: README + drift guards

**Files:**
- Create: `fabric/README.md`
- Modify: `Makefile` (add `golden` target: `go test ./internal/orchestrator/ ./cmd/fabricd -tags integration`)

- [ ] **Step 1:** README: what the fabric is (3 sentences, register-true), the five primitives, `make up && make dev`, the route table, how to add a term type (schema + reducer + mandate), test taxonomy (unit / golden / integration).
- [ ] **Step 2:** Run `make test && make test-db` one final time from clean checkout. Expected: all green.
- [ ] **Step 3: Commit:** `git add -A && git commit -m "docs(fabric): readme + golden make target"`

---

## Self-review (performed at write time)

- **Spec coverage:** five primitives ✓ (protocol/Task 2–3, exchanges/Task 8, settlements/Tasks 5+8, mandates/Task 8 golden 2, scopes throughout); laws 2/4/5/6/7 have mechanical homes (envelope `serves` required by schema; mandate gate; no private a2a route exists in the gateway; world accepts terms only; presence TTL + purge loop). Law 8/9 are charter-editing and packaging concerns deferred per spec (v1 gesture lives in charters being data, binary being single).
- **Not in this plan (by design):** web client, bedrock brains, budgets/token accounting, infra — Plans 2–4. Sleep-when-unwatched is wired for ambient ticks only (the full budget guard arrives with real brains in Plan 3, where tokens exist).
- **Type consistency:** `brain.VoiceView/Action/Rule`, `protocol.Envelope/Terms/Charter/Mandate`, `world.Apply`, `orchestrator.Config/Inject/PrincipalSays`, `gateway.Broadcast/Viewers` — names checked against every cross-reference above.
- **Known sketch boundaries:** Task 8 Step 3 and Task 9 Step 2 specify behavior via tests + numbered rules rather than full bodies; the golden tests are the contract. Task 4's sqlc field names defer to generated code. These are deliberate: the tests are complete, the rules are complete, no behavior is left to taste.
