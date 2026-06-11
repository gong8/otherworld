# World Client (/world) Implementation Plan — Plan 2 of 4

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The séance — a live web client for the fabric at `/world`, in the landing page's exact register, at the literal minimum UX: one page, one input, nothing else.

**Architecture:** The existing Next.js app (repo root) gains one route. Everything renders from the public feed (`Frame{seq, env}` over WS); the private line WS exists only to *send* text and receive consent prompts; consent answers POST back. TS protocol types are generated from `proto/` (the schemas stay the single source of truth). No state libraries, no UI libraries — the landing's CSS system extended by one stylesheet's worth of rules.

**Tech Stack:** Next.js 16 (existing), TypeScript, `json-schema-to-typescript` (dev-only codegen), native WebSocket, the existing EB Garamond/bone-paper tokens.

**THE BINDING CONSTRAINT (user directive, overrides spec details):** literal minimal UX. Concretely:

- ONE page: `/world` (household). The street is the same page at `/world?scope=street`. The only navigation is one quiet micro-caps link to the other scope and the wordmark home.
- NO separate settlements ledger, NO sidebar, NO buttons except two inline consent words. Settles render as ledger-style lines inside the feed itself.
- ONE input at the foot. Before a claim it asks for a name; after, it speaks to your agent. Slots full → it becomes the sentence "the household is full. you are overhearing." Disconnected → "the line is quiet. reconnecting…"
- The spec's "settlements as small-caps ledger entries [panel]" is amended to inline-in-feed by this directive.

**Visual register (from the landing, binding):** bone paper `#ECE9E1`, ink tokens as in `app/globals.css`, EB Garamond throughout, speakers in manual small-caps, speech in italic, hairline rules, micro-caps furniture, opacity-only motion, `prefers-reduced-motion` = still. No spinners (text states instead), no toasts, no badges, no border-radius.

**Environment:** fabric at `NEXT_PUBLIC_FABRIC_URL` (default `http://localhost:8080`); WS URL derived by protocol swap. For dev: `make up && make dev` (fabric) + `npm run dev` (web).

---

### Task W1: TS protocol types generated from proto/

**Files:**
- Create: `scripts/gen-protocol.mjs`, `lib/protocol/types.ts` (generated, committed), `lib/protocol/index.ts`
- Modify: `package.json` (devDependency `json-schema-to-typescript`, script `gen:protocol`)
- Test: `lib/protocol/agreement.test.mjs` run via `node --test` (script `test:protocol`)

- [ ] **Step 1:** `npm i -D json-schema-to-typescript@^15`
- [ ] **Step 2:** Write `scripts/gen-protocol.mjs` — compile `proto/envelope.schema.json` and `proto/charter.schema.json` to interfaces, concatenated into `lib/protocol/types.ts` with a `// generated from proto/ — do not edit; npm run gen:protocol` header:

```js
import { compileFromFile } from "json-schema-to-typescript";
import { writeFile } from "node:fs/promises";

const opts = { bannerComment: "", additionalProperties: false, style: { singleQuote: false } };
const envelope = await compileFromFile("proto/envelope.schema.json", opts);
const charter = await compileFromFile("proto/charter.schema.json", opts);
const header = "// generated from proto/ — do not edit; npm run gen:protocol\n\n";
await writeFile("lib/protocol/types.ts", header + envelope + "\n" + charter);
console.log("lib/protocol/types.ts written");
```

- [ ] **Step 3:** `package.json` scripts: `"gen:protocol": "node scripts/gen-protocol.mjs"`, `"test:protocol": "node --test lib/protocol/agreement.test.mjs"`. Run `npm run gen:protocol`; commit the output. Inspect: the envelope type must carry `kind` as the eight-kind union and `terms` optional.
- [ ] **Step 4:** Write `lib/protocol/index.ts` — hand-written thin layer over the generated types:

```ts
export type { Envelope, Charter } from "./types";

// Frame is the gateway's wire shape: the seq is the reconnect cursor.
export type Frame = { seq: number; env: import("./types").Envelope };

export const principalOf = (agentVoice: string) =>
  "voice:principal:" + agentVoice.replace(/^voice:/, "").replace(/-agent$/, "");
```

- [ ] **Step 5:** Write `lib/protocol/agreement.test.mjs` — drift guard without a TS toolchain: assert the generated file contains the eight kind literals and the `serves` required marker, and that re-running the generator is idempotent (`git diff --exit-code lib/protocol/types.ts` after a regen):

```js
import test from "node:test";
import assert from "node:assert";
import { readFileSync } from "node:fs";
import { execSync } from "node:child_process";

test("generated types carry the protocol", () => {
  const src = readFileSync("lib/protocol/types.ts", "utf8");
  for (const k of ["say","hail","propose","accept","decline","withdraw","ask_principal","settle"])
    assert.ok(src.includes(`"${k}"`), k);
  assert.ok(src.includes("serves"));
});

test("generator is idempotent against proto/", () => {
  execSync("node scripts/gen-protocol.mjs");
  execSync("git diff --exit-code -- lib/protocol/types.ts");
});
```

- [ ] **Step 6:** Run `npm run test:protocol` (PASS), `npx tsc --noEmit` clean, commit: `feat(web): protocol types generated from proto`

---

### Task W2: fabric client (`lib/fabric.ts`)

**Files:**
- Create: `lib/fabric.ts`
- Test: type-checks + used by W3's hook; logic kept trivially thin (the testable logic lives in W3's reducer)

A framework-free client, complete:

```ts
import type { Envelope, Frame } from "./protocol";

const base = process.env.NEXT_PUBLIC_FABRIC_URL ?? "http://localhost:8080";
const wsBase = base.replace(/^http/, "ws");

export type FeedHandler = {
  onFrame: (f: Frame) => void;
  onStatus: (s: "open" | "closed") => void;
};

// openFeed: auto-reconnecting feed socket. Returns a close function.
// Reconnect carries the last seen seq as the ?after cursor (the gateway
// replays the gap), with 1s→8s capped backoff.
export function openFeed(scope: string, after: number, h: FeedHandler): () => void {
  let ws: WebSocket | null = null;
  let cursor = after;
  let closed = false;
  let delay = 1000;

  const connect = () => {
    if (closed) return;
    ws = new WebSocket(`${wsBase}/v0/feed?scope=${encodeURIComponent(scope)}&after=${cursor}`);
    ws.onopen = () => { delay = 1000; h.onStatus("open"); };
    ws.onmessage = (e) => {
      const f = JSON.parse(e.data) as Frame;
      if (f.seq > cursor) cursor = f.seq;
      h.onFrame(f);
    };
    ws.onclose = () => {
      h.onStatus("closed");
      if (!closed) { setTimeout(connect, delay); delay = Math.min(delay * 2, 8000); }
    };
  };
  connect();
  return () => { closed = true; ws?.close(); };
}

export async function claim(scope: string, name: string): Promise<{ voice: string; token: string }> {
  const r = await fetch(`${base}/v0/claim`, {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ scope, name }),
  });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}

// openLine: the private line. Send text with the returned send(); ask_principal
// envelopes addressed to your principal arrive via onPrompt.
export function openLine(token: string, onPrompt: (env: Envelope) => void): { send: (text: string) => void; close: () => void } {
  const ws = new WebSocket(`${wsBase}/v0/line?token=${encodeURIComponent(token)}`);
  ws.onmessage = (e) => {
    const f = JSON.parse(e.data) as Frame;
    if (f.env.kind === "ask_principal") onPrompt(f.env);
  };
  return {
    send: (text) => { if (ws.readyState === WebSocket.OPEN) ws.send(text); },
    close: () => ws.close(),
  };
}

export async function consent(token: string, exchange: string, approve: boolean): Promise<void> {
  await fetch(`${base}/v0/consent`, {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ token, exchange, approve }),
  });
}

export async function state(scope: string): Promise<Record<string, unknown>> {
  const r = await fetch(`${base}/v0/state?scope=${encodeURIComponent(scope)}`);
  return r.json();
}
```

- [ ] Write it, `npx tsc --noEmit` clean, commit: `feat(web): fabric client — feed, line, claim, consent, state`

---

### Task W3: the /world page

**Files:**
- Create: `app/world/page.tsx` (server shell), `app/world/World.tsx` (the one client component), `app/world/world.css`
- Modify: `app/globals.css` only if a token is missing (prefer not to)

**Structure (binding):**

```
<main class="world">                       ← bone paper, same fade-in as landing
  furniture row: ◇ · [ THE HOUSEHOLD ] · № live      ← reuse .furniture/.micro
  state line:    the household · 21.0° · lamp on · curtains open · 2 residents
                                                     ← one .micro line from /v0/state,
                                                       refreshed on every settle frame
  ── OVERHEARD ──                                    ← reuse .overheard-label
  the feed                                           ← the page's whole body
     HER AGENT — cold again. one degree up, please.       (.who/.line, landing classes)
     THE HEATING — two of you disagree tonight. …
     · settled · temperature · 21.0 · her agent × the heating ·
                                                     ← settle = micro-caps ledger line,
                                                       hairline above+below
     [your consent prompt renders inline, highlighted by a left hairline:]
     YOUR AGENT — the corner shop offers one biscuit for 3 marks. shall i?
        yes · no                                     ← two micro-caps text links
  the foot: one input                                ← .speak-line
     state A (no claim):   placeholder "your name, to claim a voice"
     state B (claimed):    placeholder "speak to yours"
     state C (full):       input replaced by <p class="micro">the household is full. you are overhearing.</p>
     state D (reconnect):  …the line is quiet. reconnecting…
  scope link: a single micro-caps link "the street →" (or "← the household")
</main>
```

**Behavioral contract for `World.tsx`:**

1. Props: `scope` (from `searchParams`, default `scope:household`; only the two known scopes allowed — anything else 404s via `notFound()`).
2. State is one `useReducer`: `{frames: Frame[], status, claimed?: {voice, token, name}, prompts: Envelope[], full: boolean}`. The reducer is a pure exported function `worldReducer` — unit-testable.
3. On mount: `openFeed(scope, 0, …)`. Frames append in seq order (drop duplicates `f.seq <= last`). Keep the last 200 in memory.
4. Feed rendering by kind: `say/hail/propose/accept/decline/withdraw` → speaker line (small-caps `serves`-attributed voice name, em-dash, italic body — derive the display name from `env.from` by stripping `voice:` and `-agent`, replacing hyphens with spaces; principal pseudo-voices render as just the name). `settle` → the ledger line: `· settled · <terms.type stripped of ".set"> · <value summary> · <parties> ·`. `ask_principal` on the public feed renders as a normal speaker line; the actionable version comes via the private line into `prompts`.
5. Claim flow: submit in state A → `claim(scope, name)`; 400/409 → the input's placeholder becomes the error text in micro-caps (lowercase), no other UI. Success → `openLine(token, …)`, store `{voice, token, name}` in `sessionStorage` keyed by scope (refresh keeps the claim; new tab = new visitor — fine).
6. Speak flow: submit in state B → `line.send(text)`; clear input. No optimistic echo — the say arrives on the feed (the record is the product).
7. Consent: prompt's `yes`/`no` → `consent(token, env.exchange, bool)`, remove prompt. Multiple prompts stack chronologically.
8. Autoscroll: stick to bottom ONLY when already at bottom (don't fight the reader). New-frame reveal = opacity-only, 0.6s, none under reduced motion.
9. The `full` state derives from a failed claim containing "is full"; degraded state from `/v0/state` `degraded: true` → one micro-caps line above the foot: `the record is stalling. what you see may outrun what is written.`
10. NO other features. No sounds, no avatars, no typing indicators, no timestamps (the record has them; the page is a séance, not a log viewer), no unread markers, no emoji anywhere.

**Tests:** `worldReducer` unit tests (node --test via tsx, or vitest if already trivial to add — prefer `node --test` + plain assertions on the compiled reducer; if toolchain friction exceeds the value, the reducer test may run via a small `tsx` devDependency): frame ordering/dedup, claim transitions, prompt add/remove, full/degraded flags. Visual verification happens in W4.

- [ ] Implement (the implementer should invoke the frontend-design skill's discipline: restraint, the landing's system, zero new aesthetics), tsc clean, `npm run build` clean, commit: `feat(web): /world — the séance`

---

### Task W4: live verification against the real fabric

- [ ] **Step 1:** `make up && make dev` (fabric, background) + `npm run dev` (web, background).
- [ ] **Step 2:** Playwright script (pattern from /tmp/ow-shot.js era — channel chrome): open `/world`, screenshot empty state; claim `verifier`, send "i'm cold", wait for settle frame, screenshot; open `/world?scope=street` in a second page, claim, "find me something sweet", consent yes via the inline link, screenshot the settle + the marks in the state line. Full-page captures at 1440 and 390 widths.
- [ ] **Step 3:** READ the screenshots (multimodal). Checklist: register match with the landing (typeface, tracking, hairlines), no layout shift on frame arrival, the consent links look like print not buttons, mobile holds.
- [ ] **Step 4:** Fix what the screenshots reveal; re-shoot until clean. Commit: `fix(web): séance polish from live verification`
- [ ] **Step 5:** Kill the dev servers.

---

### Task W5: drift guards + docs

- [ ] `package.json`: `"build"` unchanged; add `"verify:world"` documenting the W4 playwright invocation. `fabric/README.md` gains a two-line "the web client" section pointing at `/world` + `NEXT_PUBLIC_FABRIC_URL`. The landing page is NOT modified — it stays a pure manifesto; linking "yours is listening." to /world is gong's call, explicitly deferred.
- [ ] Full pass: `npm run test:protocol && npx tsc --noEmit && npm run build` + `make test` (fabric untouched but confirm). Commit: `docs(web): wire-up notes; landing link deliberately deferred`

---

## Self-review (at write time)

- Spec coverage: feed/state-line/private-line/consent/reconnect/read-only all present; "settlements ledger panel" consciously amended to inline (user's minimality directive — recorded at top).
- The only new dependencies: json-schema-to-typescript (dev), possibly tsx (dev, test-only). Zero runtime deps.
- Types: Frame/Envelope shapes match the gateway's wire format (seq/env; envelope fields v/id/ts/from/serves/scope/to/kind/exchange/body/terms).
- No placeholders; W3's component internals are specified by structure+behavior contract with explicit latitude under the frontend-design discipline (visual fine-tuning), consistent with how scenes/orchestrator latitude was handled in Plan 1.
