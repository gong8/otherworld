# the otherworld — fabric & demo v1

2026-06-11 · approved direction (gong: "this is cool. keep it simple, smooth,
agile, modular.") · startup context: this is the company's first real artifact;
it must demo flawlessly in two minutes and be future-headed without speculative
generality.

## goal

Build **the fabric** — the protocol server of the otherworld, in Go — and its
first client (the web séance), running two scenes on the same five primitives:

- **household**: thing-voices (heating, lamp, curtains, door) and visitor-claimed
  resident voices negotiate comfort.
- **street**: visitors' agents discover each other by hail and settle small
  trades in play money.

Success criteria:

1. The two-minute demo script (below) runs flawlessly for a stranger on their
   own laptop browser.
2. `fabricd` is one static binary; `make dev` runs the whole world locally with
   no AWS account (fake brains).
3. Token spend stays within a configured budget; the world visibly "rests"
   rather than silently dying when the budget trips.
4. v2 directions (federation, real devices, real money) are additions, not
   rewrites — the protocol is the seam.

## principles (the user's four words, as engineering rules)

- **simple** — exactly five primitives: voice, scope, hail/exchange, settlement,
  mandate. No framework where stdlib serves. Any feature that does not serve
  the demo script is cut. One binary, one schema, one database.
- **smooth** — utterances stream word-by-word into the feed; perceived first
  motion < 1s after a principal speaks; the feed is never dead while watched
  (ambient life); reconnects are invisible; nothing jumps (the landing page's
  no-layout-shift discipline carries over).
- **agile** — local-first: `docker compose up` (Postgres) + `fabricd -brains
  fake` + `next dev` is the whole loop. Fake brains make every part of the
  system testable and demoable offline. Deploy is one command. Protocol changes
  are schema edits + codegen, reviewed like API changes.
- **modular** — four top-level modules with one seam (the protocol). Inside the
  fabric, packages own one thing each. The brain is an interface; Bedrock is an
  adapter behind it, not a dependency of the core.

## monorepo

```
proto/      protocol source of truth: JSON Schema + settlement term registry
            codegen → fabric/internal/protocol (Go) + web/lib/protocol (TS)
fabric/     Go. cmd/fabricd + internal/{protocol, store, runtime,
            orchestrator, brain, world, gateway}
web/        Next.js (existing landing + new /world client)
infra/      CDK (TS): vpc, fargate service + alb, rds postgres, alarms
docs/       specs, MISSION.md stays at root
```

## the protocol (v0)

Envelope (every message in the otherworld, no exceptions):

```json
{
  "v": 0,
  "id": "utt_…",
  "ts": "…",
  "from": "voice:heating",
  "serves": "the household",          // law 2: attribution, mandatory
  "scope": "scope:household",
  "to": ["voice:her-agent"],          // empty = scope broadcast (hail/say)
  "kind": "say | hail | propose | accept | decline | withdraw | ask_principal | settle",
  "exchange": "exc_… | null",         // null until an exchange crystallizes
  "body": "his asked me down an hour ago. i am holding the middle.",
  "terms": { "type": "temperature.set", "value": 20.5 }   // propose/accept/settle only
}
```

- **Exchange lifecycle**: first reply to a `hail`/`say` crystallizes an
  exchange; it dissolves on `settle`, `withdraw` by all proposers, or
  orchestrator abandonment (visible in the record). Participants are explicit.
- **Settlement term registry v0**: `temperature.set`, `lamp.set`,
  `curtains.set`, `trade` (`{give, get, price_marks}`). Terms are the only
  objects that touch world state (law 6). Adding a term type = one schema file
  + one reducer.
- **Charter** (per voice): `serves`, `interests` (prose), `mandate` (hard
  caps: may_propose_terms[], may_settle_without_principal: bool,
  spend_limit_marks). Person-voices and thing-voices are the same type.
- **Versioning**: `v` field; additive-only evolution until v1 freeze.

## the fabric (Go)

- **runtime** — voice = mailbox (channel) + goroutine + `recover`; panics
  restart the voice and mark any in-flight exchange `interrupted` in the
  transcript (never silent loss). Thing-voices are permanent; person-voices
  spawn on claim, despawn on leave. No actor framework — ~200 lines of stdlib.
- **orchestrator** — the craft lives here, deterministic and brain-free:
  relevance gate → think → act. Debounce (1.5–3s, jittered, so the room never
  machine-guns); per-exchange turn cap (12); cooling-off after abandonment;
  global tokens/hour budget; **sleep-when-unwatched** (ambient ticks require
  ≥1 viewer; a hail always wakes the scope).
- **brain** — `type Brain interface { Think(ctx, VoiceView) (Action, error) }`.
  Two implementations: `bedrock` (Claude — Haiku for relevance gates and
  thing-voices, Sonnet for person-voices and any turn carrying terms; prompt
  caching for charter + transcript window) and `fake` (scripted, for tests,
  local dev, and offline demos). The core imports the interface, never the SDK.
- **world** — each thing-voice owns its typed state; reducers apply settled
  terms; a scope view aggregates states for the client (`the room · 20.5° ·
  lamp lit`). State changes only via settlements.
- **store** — Postgres via sqlc: `voices, utterances, exchanges, settlements,
  presence_events`. Presence events carry TTL and are purged — the door
  forgets by schema, not by policy document (law 7).
- **gateway** — WebSocket: the public feed (everything; law 5 says there is
  nothing else) + your private line (you ↔ your agent only). REST: claim a
  voice, read state, consent to `ask_principal`. Per-IP rate limits.

## the scenes

- **household** — 4 thing-voices with short charters in the project register;
  2 claimable resident slots (first-come, queue beyond; everyone else
  overhears). Residents type to their agent; the agent represents them in the
  scope. Humans never write into the fabric directly — the principal-agent
  structure is the prompt-injection firewall.
- **street** — every visitor's agent may `hail`; a permanent corner-shop voice
  seeds the market; visitors arrive with 100 marks (play money). Any `trade`
  settles only after `ask_principal` consent from both humans — a quiet
  in-feed prompt, recorded in the transcript (law 4).

## the web client (`/world`)

The OVERHEARD section come alive, in the landing's exact register: bone paper,
EB Garamond, the live feed as the centerpiece, one print-register state line,
settlements as small-caps ledger entries, your private line at the bottom,
consent prompts inline. Streaming text; silent reconnect; read-only mode when
slots are full. Later, the landing page's rotating exchanges become curated
real traffic from the fabric.

## laws → mechanics

| law | mechanism |
|---|---|
| 2 attribution | `serves` is a mandatory envelope field, rendered under every voice |
| 4 mandate | charter caps enforced in the orchestrator; `ask_principal` round-trips consent |
| 5 overhearable | the schema has no private agent-to-agent path at all |
| 6 settle in terms | effectors accept typed terms only, never prose |
| 7 forgetting | presence events TTL'd and purged at the store layer |
| 8 loyal by structure | charters resident-editable; maker defaults visible (v1 gesture) |
| 9 protocol not platform | `proto/` is published; `fabricd` is one self-hostable binary |

## testing

- **orchestrator** — golden-transcript tests with fake brains: scripted
  scenarios (compromise, trade, deadlock, panic-mid-exchange) assert exact
  event sequences. The scheduler is deterministic; this is the heart of CI.
- **protocol** — schema round-trip and codegen-drift tests in both languages.
- **prompts** — small manual rubric v1 (register, brevity, never breaks
  character, never invents mandate); automated evals are a later cycle.
- **load smoke** — 200 viewers, 20 voices, one box.

## failure modes

| failure | behavior |
|---|---|
| brain timeout | voice goes quiet this turn; exchange cools off; feed unaffected |
| Bedrock throttle | backpressure queue; utterances delayed, never dropped |
| negotiation deadlock | turn cap → visible `withdraw` (abandoned, on the record) |
| budget tripped | the world "rests" — honest banner, feed stays readable |
| voice panic | restart; exchange marked `interrupted` in the transcript |
| ws drop | client reconnects silently, replays from last event id |

## deploy & ops

Local: docker compose (Postgres) + `fabricd -brains fake`. Prod: Fargate + ALB
(WebSockets) + RDS Postgres via CDK; Bedrock for inference (AWS credits pay
compute *and* cognition); CloudWatch token-spend alarms; kill-switch = budget
trip → rest mode. Web stays on Vercel.

## the two-minute demo (acceptance script)

1. Open `/world`. The household is mid-life: lamp and curtains murmuring.
   (0:00)
2. Claim a resident voice. Type "i'm cold." Your agent speaks into the scope;
   the heating answers; a `temperature.set` settlement lands; the state line
   ticks up. (0:30)
3. Second person (the YC partner's phone) claims the other resident: "too hot
   in here." Their agent pushes back; the heating holds the middle; the
   compromise settles on the record. (1:10)
4. Switch to the street. Type "find me something sweet." Your agent hails;
   the corner shop bids; terms appear; **your phone asks for consent**; you
   approve; 3 marks move; the trade settles. (1:50)
5. Point at the feed: "everything you just watched is the audit log. there is
   no other channel." (2:00)

## cut from v1 (ruthlessly)

Auth, real money, real devices, federation, audio/voice, mobile apps,
languages beyond English, more than two scopes, agent memory beyond the
transcript window, automated prompt evals.

## v2 doors (left open, not built)

Scope federation (third-party `fabricd` instances), Home Assistant bridge
(real homes), Stripe (real money under law 4 consent), the fiduciary
architecture for law 8, publishing `proto/` as an RFC-style document.
