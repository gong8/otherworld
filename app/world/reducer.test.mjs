/**
 * reducer.test.mjs — unit tests for the pure /world chat reducer.
 *
 * Run with: node --test app/world/reducer.test.mjs
 * Or via:   npm run test:web
 *
 * No React, no DOM, no network — the reducer is plain data logic. The model
 * under test: frames are classified at reduce time into the agent-centric
 * thread (mine / agent / other / system / consent); everything outside the
 * claimed agent's dealings is excluded.
 */

import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  ITEM_CAP,
  classifyFrame,
  displayName,
  fullNotice,
  initialWorld,
  principalOf,
  scopeTitle,
  settleText,
  stateLineOf,
  worldReducer,
} from "./reducer.mjs";

const SCOPE = "scope:household";
const CLAIM = { voice: "voice:alex-agent", token: "t0k3n", name: "alex" };
const ME = "voice:principal:alex";
const AGENT = "voice:alex-agent";

/** Build a frame action. */
function frame(seq, env) {
  return { type: "frame", frame: { seq, env } };
}

/** A claimed initial state. */
function claimedState() {
  return worldReducer(initialWorld(SCOPE), { type: "claimed", claim: CLAIM });
}

/** Reduce a list of actions over a claimed state. */
function run(actions, s = claimedState()) {
  return actions.reduce(worldReducer, s);
}

// ─── classification: the thread filter ──────────────────────────────────────

describe("classification", () => {
  it("my text to my agent renders as mine", () => {
    const s = run([
      frame(1, { kind: "say", from: ME, to: [AGENT], body: "i'm cold" }),
    ]);
    assert.equal(s.items.length, 1);
    assert.deepEqual(s.items[0], { kind: "mine", key: "f1", body: "i'm cold" });
  });

  it("my agent speaking renders as agent, labelled plainly when not third-party", () => {
    const s = run([
      frame(1, { kind: "say", from: AGENT, to: [ME], body: "done." }),
      frame(2, { kind: "hail", from: AGENT, body: "anyone near holding something sweet?" }),
    ]);
    assert.deepEqual(s.items.map((i) => i.kind), ["agent", "agent"]);
    assert.equal(s.items[0].label, "your agent");
    assert.equal(s.items[1].label, "your agent"); // broadcast: no third party
  });

  it("my agent addressing a third party gets the arrow label", () => {
    const s = run([
      frame(1, {
        kind: "propose",
        from: AGENT,
        to: ["voice:heating"],
        body: "cold again. one degree up, please.",
        terms: { type: "temperature.set", value: 23 },
        exchange: "exch_1",
      }),
    ]);
    assert.equal(s.items[0].kind, "agent");
    assert.equal(s.items[0].label, "your agent → the heating");
  });

  it("others addressing my agent render as other with their display name", () => {
    const s = run([
      frame(1, {
        kind: "accept",
        from: "voice:heating",
        to: [AGENT],
        body: "holding there.",
        terms: { type: "temperature.set", value: 23 },
        exchange: "exch_1",
      }),
      frame(2, {
        kind: "propose",
        from: "voice:corner-shop",
        to: [AGENT],
        body: "i have them. terms?",
        terms: { type: "trade", value: { give: "one biscuit", price_marks: 3 } },
        exchange: "exch_2",
      }),
    ]);
    assert.deepEqual(s.items.map((i) => i.kind), ["other", "other"]);
    assert.equal(s.items[0].label, "the heating");
    assert.equal(s.items[1].label, "the corner shop");
  });

  it("a settle my agent is a party to becomes a system line", () => {
    const s = run([
      frame(1, {
        kind: "settle",
        from: "voice:heating",
        to: [AGENT],
        exchange: "exch_1",
        terms: { type: "temperature.set", value: 21 },
      }),
      frame(2, {
        kind: "settle",
        from: AGENT, // my agent was the accepter
        to: ["voice:corner-shop"],
        exchange: "exch_2",
        terms: { type: "trade", value: { give: "one biscuit", price_marks: 3 } },
      }),
    ]);
    assert.deepEqual(s.items.map((i) => i.kind), ["system", "system"]);
    assert.equal(s.items[0].text, "temperature settled at 21.0°");
    assert.equal(s.items[1].text, "one biscuit · 3 marks · settled");
  });

  it("excludes stranger traffic: another resident's whole exchange", () => {
    const SAM = "voice:principal:sam";
    const SAM_AGENT = "voice:sam-agent";
    const s = run([
      frame(1, { kind: "say", from: SAM, to: [SAM_AGENT], body: "too hot in here" }),
      frame(2, {
        kind: "propose",
        from: SAM_AGENT,
        to: ["voice:heating"],
        body: "too warm now. one degree down, please.",
        terms: { type: "temperature.set", value: 19 },
        exchange: "exch_s",
      }),
      frame(3, {
        kind: "propose",
        from: "voice:heating",
        to: [SAM_AGENT],
        body: "two of you disagree tonight. i can hold the middle at 21.0.",
        terms: { type: "temperature.set", value: 21 },
        exchange: "exch_s",
      }),
      frame(4, {
        kind: "settle",
        from: SAM_AGENT,
        to: ["voice:heating"],
        exchange: "exch_s",
        terms: { type: "temperature.set", value: 21 },
      }),
    ]);
    assert.deepEqual(s.items, []); // none of it is my agent's dealings
    assert.equal(s.lastSeq, 4); // but the cursor still advanced
    assert.equal(s.stateStale, true); // and the settle re-arms the status fetch
  });

  it("excludes ambient murmurs (broadcast says with no addressee)", () => {
    const s = run([
      frame(1, { kind: "say", from: "voice:lamp", body: "mine never sleeps." }),
      frame(2, { kind: "say", from: "voice:door", body: "no one has called today." }),
    ]);
    assert.deepEqual(s.items, []);
  });

  it("excludes everything before a claim exists", () => {
    const s = run(
      [
        frame(1, { kind: "say", from: ME, to: [AGENT], body: "hello" }),
        frame(2, { kind: "say", from: "voice:lamp", body: "mine never sleeps." }),
      ],
      initialWorld(SCOPE) // unclaimed
    );
    assert.deepEqual(s.items, []);
    assert.equal(s.lastSeq, 2);
  });

  it("never renders an empty bubble", () => {
    const s = run([frame(1, { kind: "accept", from: "voice:heating", to: [AGENT] })]);
    assert.deepEqual(s.items, []);
  });

  it("classifyFrame is null without a claim", () => {
    assert.equal(
      classifyFrame({ kind: "say", from: ME, to: [AGENT], body: "x" }, null, "k"),
      null
    );
  });
});

// ─── consent prompts ────────────────────────────────────────────────────────

describe("consent prompts", () => {
  const ask = {
    v: 0,
    id: "utt_42",
    kind: "ask_principal",
    from: AGENT,
    to: [ME],
    exchange: "exch_t",
    body: "the corner shop offers one biscuit for 3 marks. shall i?",
  };

  it("an ask_principal on the private line becomes an unanswered consent item", () => {
    const s = worldReducer(claimedState(), { type: "prompt", env: ask });
    assert.equal(s.items.length, 1);
    const it = s.items[0];
    assert.equal(it.kind, "consent");
    assert.equal(it.id, "utt_42");
    assert.equal(it.exchange, "exch_t");
    assert.equal(it.answered, false);
  });

  it("the feed copy of the same envelope id dedupes (line first)", () => {
    let s = worldReducer(claimedState(), { type: "prompt", env: ask });
    s = worldReducer(s, frame(9, ask));
    assert.equal(s.items.filter((i) => i.kind === "consent").length, 1);
    assert.equal(s.lastSeq, 9); // the frame still advanced the cursor
  });

  it("the line copy of the same envelope id dedupes (feed first)", () => {
    let s = worldReducer(claimedState(), frame(9, ask));
    assert.equal(s.items.length, 1);
    assert.equal(s.items[0].kind, "consent");
    const again = worldReducer(s, { type: "prompt", env: ask });
    assert.equal(again, s); // same reference: React skips the render
  });

  it("another principal's ask_principal is excluded from my thread", () => {
    const s = run([
      frame(1, {
        ...ask,
        from: "voice:sam-agent",
        to: ["voice:principal:sam"],
      }),
    ]);
    assert.deepEqual(s.items, []);
  });

  it("unprompt marks the consent answered but keeps the bubble", () => {
    let s = worldReducer(claimedState(), { type: "prompt", env: ask });
    s = worldReducer(s, { type: "unprompt", exchange: "exch_t" });
    assert.equal(s.items.length, 1);
    assert.equal(s.items[0].answered, true);
  });

  it("unprompt of an unknown exchange is a no-op (same reference)", () => {
    const s = worldReducer(claimedState(), { type: "prompt", env: ask });
    assert.equal(worldReducer(s, { type: "unprompt", exchange: "nope" }), s);
  });

  it("a closing frame on the exchange resolves the prompt (answered elsewhere)", () => {
    let s = worldReducer(claimedState(), { type: "prompt", env: ask });
    s = worldReducer(
      s,
      frame(10, {
        kind: "accept",
        from: AGENT,
        to: ["voice:corner-shop"],
        exchange: "exch_t",
        body: "my principal agrees.",
        terms: { type: "trade", value: { give: "one biscuit", price_marks: 3 } },
      })
    );
    assert.equal(s.items[0].answered, true);
    // the accept itself also lands as an agent bubble
    assert.equal(s.items[1].kind, "agent");
    assert.equal(s.items[1].label, "your agent → the corner shop");
  });

  it("a settle on the exchange resolves the prompt too", () => {
    let s = worldReducer(claimedState(), { type: "prompt", env: ask });
    s = worldReducer(
      s,
      frame(11, {
        kind: "settle",
        from: AGENT,
        to: ["voice:corner-shop"],
        exchange: "exch_t",
        terms: { type: "trade", value: { give: "one biscuit", price_marks: 3 } },
      })
    );
    assert.equal(s.items[0].answered, true);
    assert.equal(s.items[1].kind, "system");
  });

  it("ignores a prompt without an exchange", () => {
    const s0 = claimedState();
    assert.equal(worldReducer(s0, { type: "prompt", env: { body: "?" } }), s0);
  });
});

// ─── frame ordering / dedup / cap ───────────────────────────────────────────

describe("frame ordering and dedup", () => {
  it("drops a duplicate seq without producing new state", () => {
    const env = { kind: "say", from: ME, to: [AGENT], body: "x" };
    const s1 = run([frame(5, env)]);
    const s2 = worldReducer(s1, frame(5, env));
    assert.equal(s2, s1);
  });

  it("drops frames at or below the cursor (reconnect replay overlap)", () => {
    const s1 = run([
      frame(3, { kind: "say", from: ME, to: [AGENT], body: "a" }),
      frame(7, { kind: "say", from: ME, to: [AGENT], body: "b" }),
    ]);
    const s2 = worldReducer(s1, frame(4, { kind: "say", from: ME, to: [AGENT], body: "late" }));
    assert.equal(s2, s1);
    assert.equal(s2.items.length, 2);
  });

  it("ignores malformed frames", () => {
    const s0 = claimedState();
    assert.equal(worldReducer(s0, { type: "frame" }), s0);
    assert.equal(worldReducer(s0, { type: "frame", frame: { seq: "nan" } }), s0);
  });

  it("keeps only the last ITEM_CAP items", () => {
    let s = claimedState();
    for (let i = 1; i <= ITEM_CAP + 5; i++) {
      s = worldReducer(s, frame(i, { kind: "say", from: ME, to: [AGENT], body: "m" + i }));
    }
    assert.equal(s.items.length, ITEM_CAP);
    assert.equal(s.items[0].body, "m6"); // 1..5 evicted
    assert.equal(s.lastSeq, ITEM_CAP + 5);
  });
});

// ─── claim transitions ──────────────────────────────────────────────────────

describe("claim transitions", () => {
  it("claimed stores the claim and clears any prior error", () => {
    let s = initialWorld(SCOPE);
    s = worldReducer(s, { type: "claimError", text: 'name "alex" is taken\n' });
    s = worldReducer(s, { type: "claimed", claim: CLAIM });
    assert.deepEqual(s.claim, CLAIM);
    assert.equal(s.claimError, null);
    assert.equal(s.full, false);
  });

  it("re-dispatch of the same claim is a no-op: the thread is never wiped", () => {
    let s = claimedState();
    s = worldReducer(s, frame(1, { kind: "say", from: ME, to: [AGENT], body: "hi" }));
    const again = worldReducer(s, { type: "claimed", claim: { ...CLAIM } });
    assert.equal(again, s);
    assert.equal(again.items.length, 1);
  });

  it("a different claim starts a fresh thread", () => {
    let s = claimedState();
    s = worldReducer(s, frame(1, { kind: "say", from: ME, to: [AGENT], body: "hi" }));
    s = worldReducer(s, {
      type: "claimed",
      claim: { voice: "voice:bee-agent", token: "other", name: "bee" },
    });
    assert.deepEqual(s.items, []);
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

  it('detects "is full" and locks into the full state', () => {
    const s = worldReducer(initialWorld(SCOPE), {
      type: "claimError",
      text: "scope:household is full\n",
    });
    assert.equal(s.full, true);
    assert.equal(s.claimError, null);
  });

  it("full notice is plain", () => {
    assert.equal(fullNotice(SCOPE), "the household is full");
    assert.equal(fullNotice("scope:street"), "the street is full");
  });

  it("lapsed reverts to the join state: claim, thread and prompts all gone", () => {
    let s = claimedState();
    s = worldReducer(s, { type: "status", channel: "line", status: "open" });
    s = worldReducer(s, frame(1, { kind: "say", from: ME, to: [AGENT], body: "hi" }));
    s = worldReducer(s, {
      type: "prompt",
      env: { id: "utt_9", kind: "ask_principal", from: AGENT, to: [ME], exchange: "exch_p", body: "shall i?" },
    });
    s = worldReducer(s, { type: "lapsed" });
    assert.equal(s.claim, null);
    assert.deepEqual(s.items, []);
    assert.equal(s.lineOpen, null);
    assert.equal(s.claimError, "session lapsed · your name to rejoin");
    assert.equal(s.full, false);
  });
});

// ─── status / state line / degraded ─────────────────────────────────────────

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
    assert.equal(s.feedOpen, true);
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
    assert.equal(s.stateStale, false);
    s = worldReducer(s, { type: "status", channel: "feed", status: "open" });
    assert.equal(s.stateStale, true);
  });

  it("the first feed open does not re-arm the state fetch", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "stateLine", text: "x" });
    s = worldReducer(s, { type: "status", channel: "feed", status: "open" });
    assert.equal(s.stateStale, false);
  });

  it("a settle in my thread re-arms the state fetch", () => {
    let s = worldReducer(claimedState(), { type: "stateLine", text: "x" });
    s = worldReducer(
      s,
      frame(1, {
        kind: "settle",
        from: "voice:heating",
        to: [AGENT],
        exchange: "e",
        terms: { type: "temperature.set", value: 22 },
      })
    );
    assert.equal(s.stateStale, true);
  });

  it("stateLine sets the text and disarms the refresh", () => {
    let s = initialWorld(SCOPE);
    assert.equal(s.stateStale, true);
    s = worldReducer(s, { type: "stateLine", text: "the household · 21.0°" });
    assert.equal(s.stateLine, "the household · 21.0°");
    assert.equal(s.stateStale, false);
  });

  it("a null text keeps the prior line but disarms the refresh (failed fetch)", () => {
    let s = worldReducer(claimedState(), { type: "stateLine", text: "kept" });
    s = worldReducer(s, frame(1, { kind: "settle", from: AGENT, to: ["voice:lamp"], exchange: "e", terms: { type: "lamp.set", value: "on" } }));
    s = worldReducer(s, { type: "stateLine", text: null });
    assert.equal(s.stateLine, "kept");
    assert.equal(s.stateStale, false);
  });

  it("degraded sets, clears, and no-ops when unchanged", () => {
    let s = worldReducer(initialWorld(SCOPE), { type: "degraded", value: true });
    assert.equal(s.degraded, true);
    assert.equal(worldReducer(s, { type: "degraded", value: true }), s);
    s = worldReducer(s, { type: "degraded", value: false });
    assert.equal(s.degraded, false);
  });
});

// ─── formatting helpers ─────────────────────────────────────────────────────

describe("displayName", () => {
  it("renders principals, agents, and things as chat senders", () => {
    assert.equal(displayName("voice:principal:sam"), "sam");
    assert.equal(displayName("voice:sam-agent"), "sam's agent");
    assert.equal(displayName("voice:heating"), "the heating");
    assert.equal(displayName("voice:corner-shop"), "the corner shop");
  });

  it("degrades a nameless envelope to someone", () => {
    assert.equal(displayName(""), "someone");
    assert.equal(displayName(null), "someone");
    assert.equal(displayName(undefined), "someone");
  });
});

describe("principalOf", () => {
  it("derives the principal pseudo-voice from the agent voice", () => {
    assert.equal(principalOf("voice:alex-agent"), "voice:principal:alex");
    assert.equal(principalOf("voice:mary-jane-agent"), "voice:principal:mary-jane");
  });
});

describe("scopeTitle", () => {
  it("names the room", () => {
    assert.equal(scopeTitle("scope:household"), "the household");
    assert.equal(scopeTitle("scope:street"), "the street");
  });
});

describe("settleText", () => {
  it("formats a temperature settlement", () => {
    assert.equal(
      settleText({ kind: "settle", terms: { type: "temperature.set", value: 21 } }),
      "temperature settled at 21.0°"
    );
  });

  it("formats a lamp settlement with its string value", () => {
    assert.equal(
      settleText({ kind: "settle", terms: { type: "lamp.set", value: "on" } }),
      "lamp on · settled"
    );
  });

  it("formats a trade settlement as give · marks", () => {
    assert.equal(
      settleText({
        kind: "settle",
        terms: { type: "trade", value: { give: "one biscuit", price_marks: 3 } },
      }),
      "one biscuit · 3 marks · settled"
    );
  });

  it("omits a missing give instead of printing an empty segment", () => {
    assert.equal(
      settleText({ kind: "settle", terms: { type: "trade", value: { price_marks: 3 } } }),
      "3 marks · settled"
    );
  });

  it("degrades quietly when terms are missing", () => {
    assert.equal(settleText({ kind: "settle" }), "settled");
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
