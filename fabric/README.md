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
- **mandate** — per-voice hard caps: `may_propose_terms[]` is enforced at the orchestrator gate before anything reaches the record; trades require an explicit consent round-trip (`ask_principal`); `may_settle_without_principal` and `spend_limit_marks` are carried in every charter and become orchestrator-enforced when real brains land in plan 3

## quickstart

```sh
make up   # start compose postgres on :55432
make dev  # fabricd on :8080 with fake brains
```

real brains: `make dev-real` (see [real brains](#real-brains) — bedrock model
access required).

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

## real brains

`make dev-real` runs the same world on Claude models over Amazon Bedrock
(`-brains bedrock`), with the default debounce — real pacing, thinks take
seconds. fake brains remain the default everywhere else (`make dev`, all of CI).

**prerequisite: model access.** AWS credentials resolving via the default
chain are not enough — Anthropic model access must be enabled per account, per
region, in the console: AWS console → Amazon Bedrock → *Model access* →
*Modify model access* → request the Anthropic Claude models (Haiku 4.5 and
Sonnet 4.6) → submit. Access is usually granted within minutes. Until then the
boot preflight (one tiny Haiku call) refuses with the exact message naming this
page; nothing is wiped, nothing serves.

**region note.** the region resolves from `AWS_REGION` / `AWS_DEFAULT_REGION`,
defaulting to `us-east-1`. observed at the time of writing: the bare
`anthropic.claude-sonnet-4-6` model id returned **404 not_found in
us-east-1** — some models are served only through cross-region inference
profiles or in other regions. if the preflight passes (Haiku) but person
thinks drop as `think.error`, override the person model, e.g.
`OW_PERSON_MODEL=us.anthropic.claude-sonnet-4-6` (the `us.` inference-profile
prefix) or pick a region that serves it. `OW_GATE_MODEL` and `OW_THINK_MODEL`
override the same way; defaults live in `internal/brain/bedrock`.

**the cost story.** the relevance gate is heuristics — free, no model call —
and kills most triggers at the source (unaddressed chatter, post-settle
ping-pong). what survives thinks on Haiku for terms-free thing turns and on
Sonnet for person-voices and anything carrying terms, so spend is
Sonnet-dominant. `-budget-tokens-per-hour` (default 500k) is the tripwire:
priced at Sonnet rates that bounds the hour at ~$1.50 all-input to ~$7.50
all-output, and real traffic — transcript windows in, two sentences out —
sits near the input end, call it **≲$2/hour**. past the cap the world *rests*
(thinks become silence, `/v0/state` shows `"resting": true`, the chat says so)
rather than overspends, and wakes the next hour.

**credentials hygiene.** the adapter uses the default AWS chain, which happily
picks up root-account keys from `~/.aws/credentials`. fine for a local demo;
before any public deploy, mint an IAM user (or role) whose policy allows
`bedrock:InvokeModel` on the Anthropic model/inference-profile ARNs and
nothing else, and run fabricd with that. the world should not hold keys that
can do more than think.

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
| 4 mandate | `may_propose_terms` enforced at the orchestrator gate before Append; `ask_principal` round-trips consent for trades; `may_settle_without_principal` and `spend_limit_marks` carried in every charter, orchestrator-enforced when real brains land in plan 3 |
| 5 overhearable | the schema has no private agent-to-agent path; the feed carries everything |
| 6 settle in terms | world.Apply accepts typed terms only, never prose; settles are synthesised by the orchestrator, never spoken |
| 7 forgetting | presence events carry a 24h TTL and are purged at the store layer every minute |
| 8 loyal by structure | charters are resident-editable; maker defaults visible (v1 gesture) |
| 9 protocol not platform | `proto/` is the source of truth; `fabricd` is one self-hostable binary |

## the web client

`/world` is the séance — the live record, one page, one input; run it with `npm run dev` at the repo root and point `NEXT_PUBLIC_FABRIC_URL` at the fabric (defaults to `http://localhost:8080`). the fabric's CORS dev-wildcard pairs with the origins allowlist in `next.config.ts`; tighten both together when you deploy. the landing page is not wired to `/world` — linking "yours is listening." is the founder's call, deliberately deferred.

## what is not here yet

- **deploy** — Fargate + ALB + RDS via CDK; Plan 4; `make dev` / `make dev-real` cover local
- **runtime package** — `fabric/internal/runtime` is built and tested but deliberately NOT wired: it hosts async voice loops for slow LLM brains; not needed while fake brains run under the orchestrator lock
- **federation, real money, real devices** — v2 doors, all left open in the protocol
