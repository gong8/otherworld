/**
 * reducer.mjs — pure state logic for /world, the chat with your agent.
 *
 * Plain JS with JSDoc types (the same pattern as lib/fabric-pure.mjs) so
 * `node --test app/world/reducer.test.mjs` can import it without compiling
 * TypeScript. World.tsx is the only React consumer; nothing here touches the
 * DOM, the network, or React.
 *
 * The model: the public feed supplies every frame in the scope; the reducer
 * filters them to the agent-centric slice at reduce time. With
 * me = voice:principal:<name> and agent = voice:<name>-agent, a frame becomes
 * a thread item only when it is mine, my agent's, addressed to my agent (or
 * to me), or a settlement my agent is a party to. Everything else in the
 * scope is excluded: the world is visible only through your agent's dealings.
 */

/**
 * @typedef {(
 *   | { kind: "mine",    key: string, body: string }
 *   | { kind: "agent",   key: string, body: string, label: string }
 *   | { kind: "other",   key: string, body: string, label: string }
 *   | { kind: "system",  key: string, text: string }
 *   | { kind: "consent", key: string, id: string, exchange: string, body: string, answered: boolean }
 * )} ThreadItem
 */
/** @typedef {{ voice: string, token: string, name: string }} Claim */

/** How many thread items the page remembers. */
export const ITEM_CAP = 200;

/**
 * "scope:household" → "the household"; "scope:street" → "the street".
 *
 * @param {string} scope
 * @returns {string}
 */
export function scopeTitle(scope) {
  return (
    "the " +
    String(scope || "")
      .replace(/^scope:/, "")
      .replace(/-/g, " ")
  );
}

/**
 * The notice shown in place of the input when a scope has no voices left.
 *
 * @param {string} scope
 * @returns {string}
 */
export function fullNotice(scope) {
  return scopeTitle(scope) + " is full";
}

/**
 * The agent voice's principal pseudo-voice, the same rule the gateway and
 * orchestrator apply: "voice:alex-agent" → "voice:principal:alex".
 *
 * @param {string} agentVoice
 * @returns {string}
 */
export function principalOf(agentVoice) {
  return (
    "voice:principal:" +
    String(agentVoice || "")
      .replace(/^voice:/, "")
      .replace(/-agent$/, "")
  );
}

/**
 * Display name for a voice, as a chat sender label:
 *
 *   voice:principal:sam → "sam"            (a person)
 *   voice:sam-agent     → "sam's agent"    (their agent)
 *   voice:heating       → "the heating"    (a thing)
 *   voice:corner-shop   → "the corner shop"
 *
 * @param {string} voice
 * @returns {string}
 */
export function displayName(voice) {
  const s = String(voice || "");
  if (!s) return "someone";
  if (s.startsWith("voice:principal:")) {
    return s.slice("voice:principal:".length).replace(/-/g, " ");
  }
  const bare = s.replace(/^voice:/, "");
  if (bare.endsWith("-agent")) {
    return bare.slice(0, -"-agent".length).replace(/-/g, " ") + "'s agent";
  }
  return "the " + bare.replace(/-/g, " ");
}

/**
 * The system line for a settlement my agent was a party to:
 *
 *   temperature.set 21   → "temperature settled at 21.0°"
 *   lamp.set "on"        → "lamp on · settled"
 *   trade {one biscuit}  → "one biscuit · 3 marks · settled"
 *
 * @param {Record<string, any>} env  the settle envelope
 * @returns {string}
 */
export function settleText(env) {
  const terms = env && env.terms ? env.terms : null;
  if (!terms) return "settled";
  const label = String(terms.type || "").replace(/\.set$/, "");
  const v = terms.value;
  if (label === "trade" && v && typeof v === "object") {
    const give = typeof v.give === "string" ? v.give : "";
    const marks = typeof v.price_marks === "number" ? v.price_marks : 0;
    return (give ? give + " · " : "") + marks + " marks · settled";
  }
  if (typeof v === "number") {
    const shown = label === "temperature" ? v.toFixed(1) + "°" : String(v);
    return (label ? label + " " : "") + "settled at " + shown;
  }
  if (typeof v === "string" && v) {
    return (label ? label + " " : "") + v + " · settled";
  }
  return label ? label + " · settled" : "settled";
}

/**
 * The status line under the agent's name, from GET /v0/state's JSON.
 *
 *   household: "the household · 21.0° · lamp on · curtains open"
 *   street:    "the street · corner shop holds 97 marks"
 *
 * When the state reports `resting: true` (the world's token budget is
 * spent), the line ends with " · resting" — the same read-a-flag-off-state
 * shape as `degraded`, but baked into the line because rest belongs to the
 * world, not to this client's connection.
 *
 * @param {string} scope
 * @param {Record<string, any>} j
 * @returns {string}
 */
export function stateLineOf(scope, j) {
  const obj = j && typeof j === "object" ? j : {};
  const things = obj.things || {};
  const resting = obj.resting === true ? " · resting" : "";
  if (scope === "scope:street") {
    const marks = obj.marks || {};
    const held = marks["voice:corner-shop"];
    return (
      "the street · corner shop holds " +
      (typeof held === "number" ? held : 0) +
      " marks" +
      resting
    );
  }
  const parts = [scopeTitle(scope)];
  const t = things.heating && things.heating.temperature;
  if (typeof t === "number") parts.push(t.toFixed(1) + "°");
  const lamp = things.lamp && things.lamp.lamp;
  if (typeof lamp === "string") parts.push("lamp " + lamp);
  const curtains = things.curtains && things.curtains.curtains;
  if (typeof curtains === "string") parts.push("curtains " + curtains);
  return parts.join(" · ") + resting;
}

/**
 * Classify a feed envelope into a thread item, or null when it is outside
 * the agent-centric slice (or there is no claim yet — before claiming, the
 * thread shows nothing but the join hint).
 *
 * @param {Record<string, any>} env
 * @param {Claim | null} claim
 * @param {string} key  React key for the item (the frame's seq)
 * @returns {ThreadItem | null}
 */
export function classifyFrame(env, claim, key) {
  if (!claim || !claim.voice) return null;
  const agent = claim.voice;
  const me = principalOf(agent);
  const to = Array.isArray(env.to) ? env.to : [];
  const body = typeof env.body === "string" ? env.body : "";

  if (env.kind === "settle") {
    if (env.from === agent || to.includes(agent)) {
      return { kind: "system", key, text: settleText(env) };
    }
    return null; // someone else's settlement
  }

  if (env.kind === "ask_principal") {
    if (env.from === agent && to.includes(me)) {
      return {
        kind: "consent",
        key,
        id: String(env.id || ""),
        exchange: String(env.exchange || ""),
        body,
        answered: false,
      };
    }
    return null; // another principal's consent prompt
  }

  if (!body) return null; // never render an empty bubble

  if (env.from === me) return { kind: "mine", key, body };

  if (env.from === agent) {
    // my agent speaking — to me, to the world, or to a third party. When
    // addressed to a non-principal third party, say so in the label.
    const third = to.find(
      (t) => t !== me && !String(t).startsWith("voice:principal:")
    );
    const label = third ? "your agent → " + displayName(third) : "your agent";
    return { kind: "agent", key, body, label };
  }

  if (to.includes(agent) || to.includes(me)) {
    return { kind: "other", key, body, label: displayName(env.from) };
  }

  return null; // stranger traffic: not my agent's dealings
}

/**
 * Append an item, evicting from the front past ITEM_CAP.
 *
 * @param {ThreadItem[]} items
 * @param {ThreadItem} it
 * @returns {ThreadItem[]}
 */
function appendItem(items, it) {
  const out = [...items, it];
  return out.length > ITEM_CAP ? out.slice(out.length - ITEM_CAP) : out;
}

/**
 * Mark any unanswered consent on this exchange answered (its buttons go
 * away; the outcome arrives as subsequent thread items). Returns the same
 * array reference when nothing changed.
 *
 * @param {ThreadItem[]} items
 * @param {string} exchange
 * @returns {ThreadItem[]}
 */
function resolveConsents(items, exchange) {
  if (
    !items.some(
      (it) => it.kind === "consent" && it.exchange === exchange && !it.answered
    )
  ) {
    return items;
  }
  return items.map((it) =>
    it.kind === "consent" && it.exchange === exchange && !it.answered
      ? { ...it, answered: true }
      : it
  );
}

/**
 * Initial page state. `stateStale: true` makes the mount effect fetch
 * /v0/state once; every settle frame re-arms it.
 *
 * @param {string} scope
 */
export function initialWorld(scope) {
  return {
    scope,
    /** @type {ThreadItem[]} */
    items: [],
    lastSeq: 0,
    /** @type {boolean | null} null until the feed reports */
    feedOpen: null,
    /** @type {boolean | null} null until the private line reports */
    lineOpen: null,
    /** @type {Claim | null} */
    claim: null,
    /** @type {string | null} placeholder override after a refused claim */
    claimError: null,
    full: false,
    stateLine: scopeTitle(scope),
    degraded: false,
    stateStale: true,
  };
}

/**
 * The reducer. Pure: same state in, same state out; unchanged references on
 * no-op actions so React can skip renders.
 *
 * @param {ReturnType<typeof initialWorld>} s
 * @param {Record<string, any>} a
 * @returns {ReturnType<typeof initialWorld>}
 */
export function worldReducer(s, a) {
  switch (a.type) {
    case "frame": {
      const f = a.frame;
      if (!f || typeof f.seq !== "number" || !(f.seq > s.lastSeq)) return s;
      const env = f.env || {};

      // any settlement moves the world, mine or not: refresh the status line
      let stateStale = s.stateStale;
      if (env.kind === "settle") stateStale = true;

      let items = s.items;

      // a closing frame on a consent's exchange resolves the prompt — the
      // answer may have come from this tab, another tab, or a timeout.
      if (
        env.exchange &&
        (env.kind === "settle" ||
          env.kind === "decline" ||
          env.kind === "withdraw" ||
          env.kind === "accept")
      ) {
        items = resolveConsents(items, env.exchange);
      }

      const it = classifyFrame(env, s.claim, "f" + f.seq);
      if (it) {
        // the same ask_principal arrives on the private line and the public
        // feed with one envelope id: render it once.
        const dup =
          it.kind === "consent" &&
          it.id &&
          items.some((p) => p.kind === "consent" && p.id === it.id);
        if (!dup) items = appendItem(items, it);
      }

      if (items === s.items && stateStale === s.stateStale) {
        return { ...s, lastSeq: f.seq };
      }
      return { ...s, items, lastSeq: f.seq, stateStale };
    }

    case "status": {
      const open = a.status === "open";
      if (a.channel === "line") {
        return s.lineOpen === open ? s : { ...s, lineOpen: open };
      }
      if (s.feedOpen === open) return s;
      // a feed reopening after a drop may have missed the present: the
      // status line is the page's only claim about NOW, so refetch it.
      const reopened = s.feedOpen === false && open;
      return { ...s, feedOpen: open, stateStale: s.stateStale || reopened };
    }

    case "claimed": {
      // re-dispatch of the same claim (e.g. an effect re-run) is a no-op so
      // an already-built thread is never wiped.
      if (s.claim && s.claim.token === a.claim.token) return s;
      return { ...s, claim: a.claim, items: [], claimError: null, full: false };
    }

    // the private line refused the token outright: the claim has lapsed.
    // Back to the join state; the thread and its prompts die with the line.
    case "lapsed":
      return {
        ...s,
        claim: null,
        items: [],
        lineOpen: null,
        claimError: "session lapsed · your name to rejoin",
      };

    case "claimError": {
      const text = String(a.text || "")
        .trim()
        .toLowerCase();
      if (text.includes("is full")) {
        return { ...s, full: true, claimError: null };
      }
      return { ...s, claimError: text || null };
    }

    // an ask_principal from the private line. The feed copy of the same
    // envelope id dedupes against this (and vice versa).
    case "prompt": {
      const env = a.env || {};
      const id = String(env.id || "");
      const exchange = typeof env.exchange === "string" ? env.exchange : "";
      if (!exchange) return s;
      if (
        s.items.some(
          (it) =>
            it.kind === "consent" &&
            ((id && it.id === id) || (!id && it.exchange === exchange))
        )
      ) {
        return s;
      }
      /** @type {ThreadItem} */
      const item = {
        kind: "consent",
        key: id || "x" + exchange,
        id,
        exchange,
        body: String(env.body || ""),
        answered: false,
      };
      return { ...s, items: appendItem(s.items, item) };
    }

    // the principal answered: the buttons go away immediately; the outcome
    // arrives as subsequent thread items.
    case "unprompt": {
      const items = resolveConsents(s.items, a.exchange);
      return items === s.items ? s : { ...s, items };
    }

    case "stateLine":
      return {
        ...s,
        stateLine: a.text == null ? s.stateLine : String(a.text),
        stateStale: false,
      };

    case "degraded": {
      const value = Boolean(a.value);
      return s.degraded === value ? s : { ...s, degraded: value };
    }

    default:
      return s;
  }
}
