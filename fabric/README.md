# fabric

the fabric is the protocol server of the otherworld: the persistent record where
every voice speaks, every negotiation runs, and every settlement lands before it
is broadcast. every person and thing in the world gets a voice of its own. every
exchange is overheard, settled in explicit terms, written to the record before
the feed carries it — what is not recorded is not broadcast.

## the five primitives

- **voice** — a charter-bound agent representing one person or thing; carries `serves` (attribution, law 2), `kind` (person | thing), and a mandate
- **scope** — the room a set of voices share; broadcasts stay inside it; `household` and `street` are the two v1 scopes
- **hail / exchange** — a bare `hail` or `propose` crystallises an exchange; turns accumulate until a `settle`, a `withdraw`, or the turn cap ends it
- **settlement** — a `settle` envelope whose typed terms the world reducer has already applied; state changes only via settlements (law 6)
- **mandate** — per-voice hard caps: `may_propose_terms[]`, `may_settle_without_principal`, `spend_limit_marks`; enforced in the orchestrator before anything reaches the record

## quickstart

```sh
make up   # start compose postgres on :55432
make dev  # fabricd on :8080 with fake brains
```

watch the household feed:

```sh
websocat 'ws://localhost:8080/v0/feed?scope=scope:household'
```

claim a voice and open your private line:

```sh
# claim
curl -s -X POST http://localhost:8080/v0/claim \
  -H 'Content-Type: application/json' \
  -d '{"scope":"scope:household","name":"her"}' | tee /tmp/claim.json

TOKEN=$(jq -r .token /tmp/claim.json)

# private line (send text, receive envelopes addressed to your principal)
websocat "ws://localhost:8080/v0/line?token=$TOKEN"
```

speak:

```sh
# on the line websocket, send plain text:
i'm cold
```

watch the compromise: the heating holds the middle when two residents disagree.
the settlement lands on the feed and in the state view:

```sh
curl 'http://localhost:8080/v0/state?scope=scope:household'
```

## routes

| method | path | semantics |
|--------|------|-----------|
| `GET`  | `/v0/feed?scope=…&after=…` | public WebSocket feed; every envelope appended to scope, in order; `after` is the reconnect cursor |
| `POST` | `/v0/claim` `{"scope","name"}` → `{"voice","token"}` | claim a resident agent voice; name must be 1–24 lowercase alphanumeric chars or hyphens |
| `GET`  | `/v0/line?token=…` | private WebSocket line; plain text in (spoken as principal), envelopes addressed to your principal pseudo-voice out |
| `POST` | `/v0/consent` `{"token","exchange","approve"}` → 204 | resolve an `ask_principal`; approve injects an accept carrying the pending terms; refuse injects a decline |
| `GET`  | `/v0/state?scope=…` | scope's world state as JSON; thing states + marks ledger (street only); `"degraded":true` when the store is stalling |

## how to add a term type

1. **schema** — add `proto/terms/<type>.json` (see `temperature.set.json` for the pattern)
2. **world reducer** — add a case in `fabric/internal/world/world.go` `Apply`

   > **guard**: set-style reducers must reject unregistered owners — return
   > `fmt.Errorf("unregistered thing %q", owner)` before writing state, exactly
   > as `temperature.set` and `lamp.set` do. `Apply` is called with the owner
   > resolved by the orchestrator to the first thing-voice in the exchange
   > participants; an unregistered owner means the settle must fail, not silently
   > no-op.

3. **mandate entries** — add the type string to `may_propose_terms` in every charter that may propose it (`proto/terms/`, seed charters in `fabric/internal/scenes/`, or resident charters via `ResidentCharter`)

## test taxonomy

| label | command | what it covers |
|-------|---------|----------------|
| unit | `make test` | all packages; no db required; short-flagged |
| store integration | `make test-db` | `store` package against compose postgres; runs `*Integration` tests |
| golden transcripts | included in `make test` | `orchestrator` package: deterministic fake-clock/fake-brain scenarios asserting exact event sequences (compromise, mandate, deadlock, consent) |
| headless demo | `make golden` | orchestrator golden tests + the tag-gated e2e run against compose postgres |

## laws → mechanics

| law | mechanism |
|-----|-----------|
| 2 attribution | `serves` is a mandatory envelope field, rendered under every voice |
| 4 mandate | charter caps enforced in the orchestrator before Append; `ask_principal` round-trips consent for trades |
| 5 overhearable | the schema has no private agent-to-agent path; the feed carries everything |
| 6 settle in terms | world.Apply accepts typed terms only, never prose; settles are synthesised by the orchestrator, never spoken |
| 7 forgetting | presence events carry a 24h TTL and are purged at the store layer every minute |
| 8 loyal by structure | charters are resident-editable; maker defaults visible (v1 gesture) |
| 9 protocol not platform | `proto/` is the source of truth; `fabricd` is one self-hostable binary |

## what is not here yet

- **bedrock brains** — `brain.Brain` is the seam; the `fake` adapter ships; the Bedrock (Claude) adapter arrives in Plan 3
- **web client** — `/world` live feed and private line UI; Plan 2
- **deploy** — Fargate + ALB + RDS via CDK; Plan 4; `make dev` covers local
- **runtime package** — `fabric/internal/runtime` is wired but unused: it hosts async voice loops for slow LLM brains; not needed while fake brains run under the orchestrator lock
- **federation, real money, real devices** — v2 doors, all left open in the protocol
