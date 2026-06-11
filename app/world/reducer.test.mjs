/**
 * reducer.test.mjs — unit tests for the pure /world page reducer.
 *
 * Run with: node --test app/world/reducer.test.mjs
 * Or via:   npm run test:web
 *
 * No React, no DOM, no network — the reducer is plain data logic.
 */

import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  FRAME_CAP,
  fullNotice,
  initialWorld,
  scopeTitle,
  settleLine,
  stateLineOf,
  voiceName,
  worldReducer,
} from "./reducer.mjs";

const SCOPE = "scope:household";

/** Build a frame action. */
function frame(seq, env) {
  return { type: "frame", frame: { seq, env } };
}

/** Reduce a list of actions over the initial state. */
function run(actions, s = initialWorld(SCOPE)) {
  return actions.reduce(worldReducer, s);
}

// ─── frame ordering / dedup ─────────────────────────────────────────────────

describe("frame ordering and dedup", () => {
  it("appends frames in order and tracks lastSeq", () => {
    const s = run([
      frame(1, { kind: "say", from: "voice:lamp", body: "mine never sleeps." }),
      frame(2, { kind: "say", from: "voice:door", body: "no one has called today." }),
    ]);
    assert.equal(s.frames.length, 2);
    assert.deepEqual(s.frames.map((f) => f.seq), [1, 2]);
    assert.equal(s.lastSeq, 2);
  });

  it("drops a duplicate seq without producing new state", () => {
    const s1 = run([frame(5, { kind: "say", from: "voice:lamp", body: "x" })]);
    const s2 = worldReducer(s1, frame(5, { kind: "say", from: "voice:lamp", body: "x" }));
    assert.equal(s2, s1); // same reference: React skips the render
  });

  it("drops frames at or below the cursor (reconnect replay overlap)", () => {
    const s1 = run([
      frame(3, { kind: "say", from: "voice:lamp", body: "a" }),
      frame(7, { kind: "say", from: "voice:lamp", body: "b" }),
    ]);
    const s2 = worldReducer(s1, frame(4, { kind: "say", from: "voice:lamp", body: "late" }));
    assert.equal(s2, s1);
    assert.equal(s2.frames.length, 2);
  });

  it("ignores malformed frames", () => {
    const s0 = initialWorld(SCOPE);
    assert.equal(worldReducer(s0, { type: "frame" }), s0);
    assert.equal(worldReducer(s0, { type: "frame", frame: { seq: "nan" } }), s0);
  });
});

// ─── 200-cap ────────────────────────────────────────────────────────────────

describe("frame cap", () => {
  it("keeps only the last 200 frames", () => {
    let s = initialWorld(SCOPE);
    for (let i = 1; i <= FRAME_CAP + 5; i++) {
      s = worldReducer(s, frame(i, { kind: "say", from: "voice:lamp", body: "m" + i }));
    }
    assert.equal(s.frames.length, FRAME_CAP);
    assert.equal(s.frames[0].seq, 6); // 1..5 evicted
    assert.equal(s.frames[s.frames.length - 1].seq, FRAME_CAP + 5);
    assert.equal(s.lastSeq, FRAME_CAP + 5);
  });
});

// ─── settle participants tracking ───────────────────────────────────────────

describe("settle participants", () => {
  it("tracks the propose/accept From-pair and resolves it on settle", () => {
    const s = run([
      frame(1, {
        kind: "propose",
        from: "voice:her-agent",
        exchange: "exch_1",
        terms: { type: "temperature.set", value: 23 },
      }),
      frame(2, {
        kind: "accept",
        from: "voice:heating",
        exchange: "exch_1",
        terms: { type: "temperature.set", value: 23 },
      }),
      frame(3, {
        kind: "settle",
        from: "voice:heating",
        to: ["voice:her-agent"],
        exchange: "exch_1",
        terms: { type: "temperature.set", value: 23 },
      }),
    ]);
    const settle = s.frames[2];
    assert.deepEqual(settle.parties, ["voice:her-agent", "voice:heating"]);
    assert.equal(s.exchanges["exch_1"], undefined); // closed: the index forgets it
  });

  it("keeps the proposer first through a counter-propose round", () => {
    const s = run([
      frame(1, {
        kind: "propose",
        from: "voice:her-agent",
        exchange: "exch_2",
        terms: { type: "temperature.set", value: 23 },
      }),
      frame(2, {
        kind: "propose",
        from: "voice:heating",
        exchange: "exch_2",
        terms: { type: "temperature.set", value: 22 },
      }),
      frame(3, {
        kind: "accept",
        from: "voice:her-agent",
        exchange: "exch_2",
        terms: { type: "temperature.set", value: 22 },
      }),
      frame(4, {
        kind: "settle",
        from: "voice:her-agent",
        to: ["voice:heating"],
        exchange: "exch_2",
        terms: { type: "temperature.set", value: 22 },
      }),
    ]);
    assert.deepEqual(s.frames[3].parties, ["voice:her-agent", "voice:heating"]);
  });

  it("falls back to the settle's own from/to when the exchange opened off-window", () => {
    const s = run([
      frame(1, {
        kind: "settle",
        from: "voice:her-agent",
        to: ["voice:corner-shop"],
        exchange: "exch_3",
        terms: { type: "trade", value: { give: "one biscuit", price_marks: 3 } },
      }),
    ]);
    assert.deepEqual(s.frames[0].parties, ["voice:her-agent", "voice:corner-shop"]);
  });

  it("flags the state line stale on settle", () => {
    let s = initialWorld(SCOPE);
    s = worldReducer(s, { type: "stateLine", text: "the household · 21.0°" });
    assert.equal(s.stateStale, false);
    s = worldReducer(
      s,
      frame(1, {
        kind: "settle",
        from: "voice:heating",
        exchange: "exch_4",
        terms: { type: "temperature.set", value: 22 },
      })
    );
    assert.equal(s.stateStale, true);
  });

  it("forgets an exchange on withdraw and decline too", () => {
    const open = frame(1, {
      kind: "propose",
      from: "voice:her-agent",
      exchange: "exch_w",
      terms: { type: "temperature.set", value: 23 },
    });

    let s = run([
      open,
      frame(2, { kind: "withdraw", from: "voice:her-agent", exchange: "exch_w", body: "turn cap reached" }),
    ]);
    assert.equal(s.exchanges["exch_w"], undefined);

    s = run([
      { ...open, frame: { ...open.frame, env: { ...open.frame.env, exchange: "exch_d" } } },
      frame(2, { kind: "decline", from: "voice:heating", exchange: "exch_d", body: "my principal declines." }),
    ]);
    assert.equal(s.exchanges["exch_d"], undefined);
  });
});

// ─── claim transitions ──────────────────────────────────────────────────────

describe("claim transitions", () => {
  const granted = { voice: "voice:her-agent", token: "t0k3n", name: "her" };

  it("claimed stores the claim and clears any prior error", () => {
    let s = initialWorld(SCOPE);
    s = worldReducer(s, { type: "claimError", text: 'name "her" is taken\n' });
    s = worldReducer(s, { type: "claimed", claim: granted });
    assert.deepEqual(s.claim, granted);
    assert.equal(s.claimError, null);
    assert.equal(s.full, false);
  });

  it("claimError becomes the lowercase, trimmed placeholder text", () => {
    const s = worldReducer(initialWorld(SCOPE), {
      type: "claimError",
      text: "Name must be 1-24 chars: lowercase letters, digits, hyphens\n",
    });
    assert.equal(
      s.claimError,
      "name must be 1-24 chars: lowercase letters, digits, hyphens"
    );
    assert.equal(s.full, false);
  });

  it('detects "is full" and locks into the overhearing state', () => {
    const s = worldReducer(initialWorld(SCOPE), {
      type: "claimError",
      text: "scope:household is full\n",
    });
    assert.equal(s.full, true);
    assert.equal(s.claimError, null);
  });

  it("full notice is in register", () => {
    assert.equal(fullNotice(SCOPE), "the household is full. you are overhearing.");
    assert.equal(fullNotice("scope:street"), "the street is full. you are overhearing.");
  });

  it("lapsed reverts to state A: claim gone, prompts gone, foot says why", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "claimed", claim: granted });
    s = worldReducer(s, { type: "status", channel: "line", status: "open" });
    s = worldReducer(s, {
      type: "prompt",
      env: { kind: "ask_principal", from: "voice:her-agent", exchange: "exch_p", body: "shall i?" },
    });
    s = worldReducer(s, { type: "lapsed" });
    assert.equal(s.claim, null);
    assert.deepEqual(s.prompts, []);
    assert.equal(s.lineOpen, null);
    assert.equal(s.claimError, "your voice has lapsed. claim again to speak.");
    assert.equal(s.full, false); // back to state A, not state C
  });
});

// ─── prompts ────────────────────────────────────────────────────────────────

describe("prompt add/remove", () => {
  const ask = {
    kind: "ask_principal",
    from: "voice:her-agent",
    exchange: "exch_5",
    body: "the corner shop offers one biscuit for 3 marks. shall i?",
  };

  it("adds a prompt and dedupes by exchange", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "prompt", env: ask });
    assert.equal(s.prompts.length, 1);
    assert.equal(s.prompts[0].exchange, "exch_5");
    const again = worldReducer(s, { type: "prompt", env: ask });
    assert.equal(again, s);
  });

  it("stacks prompts from different exchanges", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "prompt", env: ask });
    s = worldReducer(s, { type: "prompt", env: { ...ask, exchange: "exch_6" } });
    assert.deepEqual(s.prompts.map((p) => p.exchange), ["exch_5", "exch_6"]);
  });

  it("unprompt removes only the answered exchange", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "prompt", env: ask });
    s = worldReducer(s, { type: "prompt", env: { ...ask, exchange: "exch_6" } });
    s = worldReducer(s, { type: "unprompt", exchange: "exch_5" });
    assert.deepEqual(s.prompts.map((p) => p.exchange), ["exch_6"]);
  });

  it("unprompt of an unknown exchange is a no-op (same reference)", () => {
    const s = worldReducer(initialWorld(SCOPE), { type: "prompt", env: ask });
    assert.equal(worldReducer(s, { type: "unprompt", exchange: "nope" }), s);
  });

  it("ignores a prompt without an exchange", () => {
    const s0 = initialWorld(SCOPE);
    assert.equal(worldReducer(s0, { type: "prompt", env: { body: "?" } }), s0);
  });
});

// ─── degraded flag ──────────────────────────────────────────────────────────

describe("degraded flag", () => {
  it("sets and clears", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "degraded", value: true });
    assert.equal(s.degraded, true);
    s = worldReducer(s, { type: "degraded", value: false });
    assert.equal(s.degraded, false);
  });

  it("is a no-op when unchanged", () => {
    const s = worldReducer(initialWorld(SCOPE), { type: "degraded", value: true });
    assert.equal(worldReducer(s, { type: "degraded", value: true }), s);
  });
});

// ─── status / state line ────────────────────────────────────────────────────

describe("feed status and state line", () => {
  it("feedOpen starts unknown, then follows the socket", () => {
    let s = initialWorld(SCOPE);
    assert.equal(s.feedOpen, null);
    s = worldReducer(s, { type: "status", status: "open" });
    assert.equal(s.feedOpen, true);
    s = worldReducer(s, { type: "status", status: "closed" });
    assert.equal(s.feedOpen, false);
  });

  it("routes status by channel: feed and line are independent", () => {
    let s = initialWorld(SCOPE);
    assert.equal(s.lineOpen, null);
    s = worldReducer(s, { type: "status", channel: "feed", status: "open" });
    s = worldReducer(s, { type: "status", channel: "line", status: "open" });
    assert.equal(s.feedOpen, true);
    assert.equal(s.lineOpen, true);
    s = worldReducer(s, { type: "status", channel: "line", status: "closed" });
    assert.equal(s.lineOpen, false);
    assert.equal(s.feedOpen, true); // the feed banner is untouched
  });

  it("an unchanged status is a no-op (same reference)", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "status", channel: "line", status: "open" });
    assert.equal(worldReducer(s, { type: "status", channel: "line", status: "open" }), s);
    s = worldReducer(s, { type: "status", channel: "feed", status: "open" });
    assert.equal(worldReducer(s, { type: "status", channel: "feed", status: "open" }), s);
  });

  it("a feed reopening after a drop re-arms the state fetch", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "status", channel: "feed", status: "open" });
    s = worldReducer(s, { type: "stateLine", text: "the household · 21.0°" });
    assert.equal(s.stateStale, false);
    s = worldReducer(s, { type: "status", channel: "feed", status: "closed" });
    assert.equal(s.stateStale, false); // a drop alone changes nothing
    s = worldReducer(s, { type: "status", channel: "feed", status: "open" });
    assert.equal(s.stateStale, true); // the present may have moved: refetch
  });

  it("the first feed open does not re-arm the state fetch", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "stateLine", text: "x" });
    s = worldReducer(s, { type: "status", channel: "feed", status: "open" });
    assert.equal(s.stateStale, false); // null → open is a fresh page, not a gap
  });

  it("stateLine sets the text and disarms the refresh", () => {
    let s = initialWorld(SCOPE);
    assert.equal(s.stateStale, true); // arms the mount fetch
    s = worldReducer(s, { type: "stateLine", text: "the household · 21.0°" });
    assert.equal(s.stateLine, "the household · 21.0°");
    assert.equal(s.stateStale, false);
  });

  it("a null text keeps the prior line but disarms the refresh (failed fetch)", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "stateLine", text: "kept" });
    s = worldReducer(s, frame(1, { kind: "settle", from: "voice:heating", exchange: "e", terms: { type: "lamp.set", value: "on" } }));
    s = worldReducer(s, { type: "stateLine", text: null });
    assert.equal(s.stateLine, "kept");
    assert.equal(s.stateStale, false);
  });
});

// ─── formatting helpers ─────────────────────────────────────────────────────

describe("voiceName", () => {
  it("renders principals, agents, and things in the landing's register", () => {
    assert.equal(voiceName("voice:principal:her"), "her");
    assert.equal(voiceName("voice:her-agent"), "her agent");
    assert.equal(voiceName("voice:heating"), "the heating");
    assert.equal(voiceName("voice:corner-shop"), "the corner shop");
  });

  it("degrades a nameless envelope to someone", () => {
    assert.equal(voiceName(""), "someone");
    assert.equal(voiceName(null), "someone");
    assert.equal(voiceName(undefined), "someone");
  });

  it("scopeTitle names the room", () => {
    assert.equal(scopeTitle("scope:household"), "the household");
    assert.equal(scopeTitle("scope:street"), "the street");
  });
});

describe("settleLine", () => {
  it("formats a temperature settlement", () => {
    const line = settleLine(
      { kind: "settle", terms: { type: "temperature.set", value: 21 } },
      ["voice:her-agent", "voice:heating"]
    );
    assert.equal(line, "· settled · temperature · 21.0° · her agent × the heating ·");
  });

  it("formats a lamp settlement with its string value", () => {
    const line = settleLine(
      { kind: "settle", terms: { type: "lamp.set", value: "on" } },
      ["voice:her-agent", "voice:lamp"]
    );
    assert.equal(line, "· settled · lamp · on · her agent × the lamp ·");
  });

  it("formats a trade settlement as give · marks", () => {
    const line = settleLine(
      {
        kind: "settle",
        terms: { type: "trade", value: { give: "one biscuit", price_marks: 3 } },
      },
      ["voice:corner-shop", "voice:her-agent"]
    );
    assert.equal(line, "· settled · trade · one biscuit · 3 marks · the corner shop × her agent ·");
  });

  it("omits a missing give instead of printing an empty segment", () => {
    const line = settleLine(
      { kind: "settle", terms: { type: "trade", value: { price_marks: 3 } } },
      ["voice:corner-shop", "voice:her-agent"]
    );
    assert.equal(line, "· settled · trade · 3 marks · the corner shop × her agent ·");
  });

  it("degrades quietly when terms are missing", () => {
    assert.equal(settleLine({ kind: "settle" }, []), "· settled ·");
  });
});

describe("stateLineOf", () => {
  it("composes the household line", () => {
    const line = stateLineOf(SCOPE, {
      scope: SCOPE,
      things: {
        heating: { temperature: 21 },
        lamp: { lamp: "on" },
        curtains: { curtains: "open" },
        door: {},
      },
    });
    assert.equal(line, "the household · 21.0° · lamp on · curtains open");
  });

  it("composes the street line from the corner shop's marks", () => {
    const line = stateLineOf("scope:street", {
      scope: "scope:street",
      things: { "corner-shop": {} },
      marks: { "voice:corner-shop": 97 },
    });
    assert.equal(line, "the street · corner shop holds 97 marks");
  });

  it("omits what the state does not report", () => {
    assert.equal(stateLineOf(SCOPE, { scope: SCOPE, things: {} }), "the household");
    assert.equal(
      stateLineOf("scope:street", { scope: "scope:street" }),
      "the street · corner shop holds 0 marks"
    );
  });
});
