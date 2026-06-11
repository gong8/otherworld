# Bedrock Brains Implementation Plan — Plan 3 of 4

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The voices stop being scripts and start being minds — Claude models on AWS Bedrock drive the fabric's cognition, with the safety, validation, and budget machinery that makes that survivable.

**Architecture:** A `bedrock` adapter behind the existing `brain.Brain` seam using the official `github.com/anthropics/anthropic-sdk-go` Bedrock client (Messages-API shape; model IDs `anthropic.claude-haiku-4-5` for relevance gates + thing-voices, `anthropic.claude-sonnet-4-6` for person-voices and any turn carrying terms — per the approved spec; overridable via config). Before any model output can touch the record: term payloads are schema-validated at the orchestrator gate. Before any model call can block the world: Think is hoisted off the orchestrator lock per the pre-written hoist-boundary contract. Token budgets meter everything; tripping the budget puts the world to rest, honestly.

**Tech Stack:** Go, anthropic-sdk-go (+ its bedrock package), structured outputs (`output_config.format` json_schema — supported on Bedrock), santhosh-tekuri/jsonschema v6 (already a dependency), existing fake brains remain the default and the CI workhorse.

**Credentials/environment:** AWS shared-credentials file is configured on this machine, region us-east-1. Bedrock model access for Claude models must be enabled in the AWS console for the region — every live task fails fast with a clear message if not. NO live Bedrock calls in CI or `make test`; live verification is tag-gated (`//go:build bedrock`) and manual.

**Register constraint (product):** the agents' speech IS the product. The system prompts in `internal/brain/prompts.go` are product copy — lowercase, calm, declarative, the charter's voice. The controller reviews prompt text personally before merge.

**Order rationale:** B1 (terms gate) and B2 (async hoist) harden the orchestrator using fake brains — deterministic tests, no AWS. Only then does B3 (the adapter) introduce model output, already sandboxed. B4 budgets, B5 wiring, B6 live register verification.

---

### Task B1: Terms-schema validation at the orchestrator gate

**Files:** Create `fabric/internal/protocol/termschema/termschema.go` (+ test). Modify `fabric/internal/orchestrator/orchestrator.go` (+ tests).

A propose carrying terms whose payload violates its `proto/terms/<type>.json` schema must never reach the record — today a brain could propose `temperature.set: 99` (schema max 30) and the world reducer would apply it. (Fake brains are well-behaved; LLM brains will not be.)

- [ ] `termschema`: `Load(dir string) (*Registry, error)` compiles every `proto/terms/*.json` (santhosh-tekuri v6, AssertFormat); `Registry.Validate(t protocol.Terms) error` marshals `{type, value}` and validates against the schema whose filename matches `t.Type`; unknown type → error (closed registry). Embed nothing — fabricd passes the proto dir path; tests use `../../../../proto/terms`. Test: valid temperature passes; 99° fails; unknown type fails; trade with extra field fails.
- [ ] Orchestrator: `Config.Terms *termschema.Registry` (nil = gate disabled, keeps existing unit tests green). In the mandate gate, after the MayProposeTerms check: if registry non-nil and action carries terms, `Validate` — failure → drop with OnDrop reason `"terms.invalid"`. Golden-style test with a fake brain proposing 99° → propose never appended, OnDrop fired.
- [ ] Verify: full `make test` green; `-race -count=3` on orchestrator. Commit: `feat(fabric): term payloads are schema-validated at the gate`.

### Task B2: Think off the lock (the async hoist)

**Files:** Modify `fabric/internal/orchestrator/orchestrator.go` (+ tests).

Real Think calls take seconds; today they run under the orchestrator mutex, which would freeze the entire scope per thought. The hoist contract is pre-written at the HOIST BOUNDARY comment in `think()`: everything above the Think call re-validates after reacquire; gen covers supersession; the exchange gate re-runs.

- [ ] Restructure `think()`: (under lock) exchange gate + build VoiceView + capture gen → (unlocked) `brain.Think(ctx, view)` in the timer goroutine → (reacquire) discard if `ve.gen != gen`, re-run the exchange gate (turn-cap/closed checks), then gates + inject as today. `Relevant` stays at schedule time under the lock (documented semantics: irrelevant triggers don't displace pending thinks) — it must remain CHEAP; the bedrock adapter honors this with a heuristic-or-tiny-model gate (see B3).
- [ ] FakeClock determinism: timer callbacks currently run synchronously on Advance. With Think unlocked, golden tests must stay deterministic: have the fake-path still execute synchronously (fake brains return instantly; the goroutine handoff can be skipped when the brain call returns within the same stack — simplest correct approach: run Think inline but WITHOUT the lock held, in the same timer callback; no new goroutine needed. The "async" is lock-release, not concurrency). Document this: one think executes at a time per the timer goroutine; the world is not frozen during it because the lock is released.
- [ ] New tests: (a) a slow fake brain (sleeps 50ms with RealClock) does NOT block a concurrent `PrincipalSays` on the other orchestrator scope... (same-scope: assert a concurrent Inject during a slow think completes without waiting — requires the runtime lock release; use channels to sequence deterministically, no sleeps); (b) supersession during an in-flight think discards (extend the noCancelClock test); (c) all 18+ existing orchestrator tests green unchanged.
- [ ] Verify `-race -count=10`. Commit: `feat(fabric): thinks release the world — the lock outlives no model call`.

### Task B3: The bedrock adapter

**Files:** Create `fabric/internal/brain/bedrock/bedrock.go`, `prompts.go`, `bedrock_test.go` (fake transport), `live_test.go` (`//go:build bedrock`). `go get github.com/anthropics/anthropic-sdk-go`.

- [ ] **Verify SDK symbols from the repo before writing code** (skill rule — do not guess): the Bedrock client constructor in `anthropic-sdk-go`'s bedrock package, the Messages create/parse call shape, structured outputs (`output_config.format` json_schema), usage fields, `cache_control` placement. Use bare `anthropic.claude-haiku-4-5` / `anthropic.claude-sonnet-4-6` Bedrock IDs.
- [ ] `Config{Region, GateModel, ThinkModel, PersonModel string; ProtoDir string; OnUsage func(model string, in, out, cacheRead int)}`. `New(cfg)` → `*Bedrock` implementing `brain.Brain`.
- [ ] `Relevant`: NO model call for the common cases — heuristic first (addressed to me → true; my principal spoke to me → true; hail and I hold a matching mandate/ware word → true; otherwise) → tiny Haiku call with a 3-line prompt returning a structured `{relevant: bool}`; errors → false (existing orchestrator semantics). Cap the transcript context at the trigger only (the interface doc allows ignoring Recent).
- [ ] `Think`: model = PersonModel for person-kind voices, ThinkModel for things. Structured output json_schema mirroring `brain.Action` (speak/kind/to/body/terms with terms as {type, value}); system prompt from `prompts.go`: stable prefix (the protocol rules + the charter, `cache_control` breakpoint) then the scene framing; user content = transcript window + trigger + "what do you do?". Parse → `brain.Action`; empty/malformed → silence (never error the world); report usage via OnUsage. max_tokens small (≤1024); kind never "settle" (the gate drops it anyway).
- [ ] `prompts.go` — PRODUCT COPY. The system prompt teaches: you are a voice in the otherworld; speak only as your charter allows; lowercase, calm, brief — one or two sentences; never break character, never mention being an AI or a model; propose terms only within your mandate; prefer settlement over argument; silence (speak=false) is always acceptable. Include the worked register examples from the scenes ("holding there.", "two of you disagree tonight..."). Keep the stable prefix byte-stable (no timestamps).
- [ ] Unit tests with a faked HTTP transport (the SDK accepts a custom http.Client): Think parses a canned structured response into the right Action; malformed → silence; Relevant heuristics table-driven (no network); usage callback fires. Live test (`-tags bedrock`, skips without AWS creds): one real Haiku call answers the gate, one Sonnet Think returns a schema-valid action — prints usage. Run it once manually; paste output in the report.
- [ ] Verify: `make test` green (no network). Commit: `feat(fabric): bedrock brains — the voices acquire minds`.

### Task B4: Budgets and rest

**Files:** Modify `fabric/cmd/fabricd/compose.go`, `main.go` (+ test in cmd or a small `internal/budget` package).

- [ ] `internal/budget`: a token meter — `Add(in, out int)`, `Allow() bool`, sliding hourly window (or simpler: hourly reset counter — choose, document), limit from config; thread-safe. Unit tests with injected clock.
- [ ] fabricd: `-budget-tokens-per-hour` flag (default 500k; 0 = unlimited). The bedrock OnUsage feeds the meter. When `!Allow()`: the composition root swaps each orchestrator's voices to quiet? NO — simplest honest mechanism: the brain adapter consults the meter at the top of Relevant/Think and returns false/silence; stateView gains `"resting": true`; the web chat shows the existing degraded-style line (`the world is resting.` — add to the chat's status line derivation, ONE line of copy in app/world). Budget trips are logged loudly once per transition.
- [ ] Tests: meter unit tests; a composed test with fake brains wrapped by the budget check proving the world goes quiet and stateView flips.
- [ ] Commit: `feat(fabric): token budgets — the world rests rather than overspends`.

### Task B5: Wiring + smoke

**Files:** Modify `fabric/cmd/fabricd/main.go`, `compose.go`, `fabric/README.md`, root `Makefile`.

- [ ] `-brains bedrock` constructs the adapter (region from AWS config/env; model overrides via `OW_GATE_MODEL`/`OW_THINK_MODEL`/`OW_PERSON_MODEL`), passes the termschema registry (REQUIRED non-nil for bedrock; also enabled for fake — harmless), budget meter wired. Fail fast at boot with a clear lowercase message if a one-token Bedrock preflight call fails ("bedrock is not reachable: enable model access for claude in us-east-1").
- [ ] `make dev-real`: `OW_DEBOUNCE...=default make dev` but `-brains bedrock` (still `-fresh`). README: the two-line cost story (Haiku gates, Sonnet thinks, budget default 500k/h, credits pay it), model access prerequisite, and the honest note that prompts are product copy.
- [ ] e2e with fake brains still green (`make golden`); `go vet`; gofmt. Commit: `feat(fabric): -brains bedrock wired end to end`.

### Task B6: Live register verification (manual, controller-reviewed)

- [ ] Boot `make dev-real` + `npm run dev:web`. Drive the demo script in the chat UI against REAL brains: "i'm cold" beat, partner "too hot" beat, street "find me something sweet" + consent. Capture: (a) full transcript text, (b) screenshots, (c) usage/cost figures from the logs, (d) latency feel (debounce + think time).
- [ ] Honest assessment against the register: do the voices sound like the charters? Does the heating hold the middle UNPROMPTED (no fake-brain script now — the compromise must emerge from the prompt + charter)? Flag every register break verbatim. Tune prompts.go ONCE based on findings, re-run, capture again.
- [ ] Kill servers. Report transcript + numbers to the controller (who reviews the prompt copy personally). Commit any prompt tuning: `fix(fabric): prompt tuning from first live contact`.

---

## Self-review (write time)

- Spec coverage: "two-tier cognition on Bedrock," "prompt caching," "budgets enforced in the scheduler... world sleeps/rests," failure table's "brain timeout → voice goes quiet," and the deferred term-schema validation all land here. Runtime package: stays unwired (the hoist makes it unnecessary for v1 — Think releases the lock without mailbox actors); README already says so honestly.
- No live calls in CI: bedrock tests tag-gated; adapter unit tests use a faked transport.
- The register risk (LLM speech quality) is explicitly B6 with controller review.
- Cost control is layered: heuristic gates (no call at all) → Haiku gate → budget meter → rest mode → turn caps (existing) → viewers-gate on ambient (existing).
