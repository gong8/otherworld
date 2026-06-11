/**
 * reducer.mjs — pure state logic for /world, the séance.
 *
 * Plain JS with JSDoc types (the same pattern as lib/fabric-pure.mjs) so
 * `node --test app/world/reducer.test.mjs` can import it without compiling
 * TypeScript. World.tsx is the only React consumer; nothing here touches the
 * DOM, the network, or React.
 */

/** @typedef {{ seq: number, env: Record<string, any>, parties?: string[] }} FrameRec */
/** @typedef {{ voice: string, token: string, name: string }} Claim */
/** @typedef {{ exchange: string, from: string, body: string }} Prompt */

/** How many frames the page remembers. */
export const FRAME_CAP = 200;

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
 * The permanent notice shown when a scope has no voices left to claim.
 *
 * @param {string} scope
 * @returns {string}
 */
export function fullNotice(scope) {
  return scopeTitle(scope) + " is full. you are overhearing.";
}

/**
 * Display name for a voice, in the landing's register:
 *
 *   voice:principal:her → "her"           (the person themselves)
 *   voice:her-agent     → "her agent"     (the voice lent to them)
 *   voice:heating       → "the heating"   (a thing)
 *   voice:corner-shop   → "the corner shop"
 *
 * @param {string} from
 * @returns {string}
 */
export function voiceName(from) {
  const s = String(from || "");
  if (s.startsWith("voice:principal:")) {
    return s.slice("voice:principal:".length).replace(/-/g, " ");
  }
  const bare = s.replace(/^voice:/, "").replace(/-/g, " ");
  if (bare === "agent" || bare.endsWith(" agent")) return bare;
  return "the " + bare;
}

/**
 * The ledger line for a settle frame:
 *
 *   · settled · temperature · 21.0° · her agent × the heating ·
 *   · settled · trade · one biscuit · 3 marks · the corner shop × her agent ·
 *
 * @param {Record<string, any>} env       the settle envelope
 * @param {string[] | undefined} parties  the exchange's propose/accept From-pair
 * @returns {string}
 */
export function settleLine(env, parties) {
  const terms = env && env.terms ? env.terms : null;
  const label = terms ? String(terms.type || "").replace(/\.set$/, "") : "";
  let value = "";
  if (terms) {
    const v = terms.value;
    if (label === "trade" && v && typeof v === "object") {
      const give = typeof v.give === "string" ? v.give : "";
      const marks = typeof v.price_marks === "number" ? v.price_marks : 0;
      value = give + " · " + marks + " marks";
    } else if (typeof v === "number") {
      value = label === "temperature" ? v.toFixed(1) + "°" : String(v);
    } else if (typeof v === "string") {
      value = v;
    }
  }
  const pair = (parties || []).slice(0, 2).map(voiceName).join(" × ");
  const segs = ["settled"];
  if (label) segs.push(label);
  if (value) segs.push(value);
  if (pair) segs.push(pair);
  return "· " + segs.join(" · ") + " ·";
}

/**
 * The state line under the furniture row, from GET /v0/state's JSON.
 * The degraded suffix is appended at render time from state.degraded.
 *
 *   household: "the household · 21.0° · lamp on · curtains open"
 *   street:    "the street · corner shop holds 97 marks"
 *
 * @param {string} scope
 * @param {Record<string, any>} j
 * @returns {string}
 */
export function stateLineOf(scope, j) {
  const things = (j && typeof j === "object" && j.things) || {};
  if (scope === "scope:street") {
    const marks = (j && typeof j === "object" && j.marks) || {};
    const held = marks["voice:corner-shop"];
    return (
      "the street · corner shop holds " +
      (typeof held === "number" ? held : 0) +
      " marks"
    );
  }
  const parts = [scopeTitle(scope)];
  const t = things.heating && things.heating.temperature;
  if (typeof t === "number") parts.push(t.toFixed(1) + "°");
  const lamp = things.lamp && things.lamp.lamp;
  if (typeof lamp === "string") parts.push("lamp " + lamp);
  const curtains = things.curtains && things.curtains.curtains;
  if (typeof curtains === "string") parts.push("curtains " + curtains);
  return parts.join(" · ");
}

/**
 * Resolve a settle's parties: the Froms tracked from the exchange's
 * propose/accept envelopes, topped up from the settle's own from/to if the
 * tracker saw less than a pair (e.g. a replay window that opened mid-exchange).
 *
 * @param {string[] | undefined} tracked
 * @param {Record<string, any>} env
 * @returns {string[]}
 */
function partiesOf(tracked, env) {
  /** @type {string[]} */
  const out = [];
  for (const v of tracked || []) {
    if (v && !out.includes(v)) out.push(v);
  }
  if (env.from && !out.includes(env.from)) out.push(env.from);
  for (const t of env.to || []) {
    if (out.length >= 2) break;
    if (t && !out.includes(t)) out.push(t);
  }
  return out.slice(0, 2);
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
    /** @type {FrameRec[]} */
    frames: [],
    lastSeq: 0,
    /** @type {Record<string, string[]>} exchange id → Froms seen on it */
    exchanges: {},
    /** @type {Prompt[]} */
    prompts: [],
    /** @type {boolean | null} null until the feed reports */
    feedOpen: null,
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
      let exchanges = s.exchanges;
      /** @type {FrameRec} */
      const rec = { seq: f.seq, env };

      if (
        env.exchange &&
        (env.kind === "propose" || env.kind === "accept") &&
        env.from
      ) {
        const prior = exchanges[env.exchange] || [];
        if (!prior.includes(env.from)) {
          exchanges = { ...exchanges, [env.exchange]: [...prior, env.from] };
        }
      }

      let stateStale = s.stateStale;
      if (env.kind === "settle") {
        stateStale = true;
        rec.parties = partiesOf(exchanges[env.exchange], env);
        if (env.exchange && exchanges[env.exchange]) {
          exchanges = { ...exchanges };
          delete exchanges[env.exchange];
        }
      }

      let frames = [...s.frames, rec];
      if (frames.length > FRAME_CAP) {
        frames = frames.slice(frames.length - FRAME_CAP);
      }
      return { ...s, frames, lastSeq: f.seq, exchanges, stateStale };
    }

    case "status":
      return { ...s, feedOpen: a.status === "open" };

    case "claimed":
      return { ...s, claim: a.claim, claimError: null, full: false };

    case "claimError": {
      const text = String(a.text || "")
        .trim()
        .toLowerCase();
      if (text.includes("is full")) {
        return { ...s, full: true, claimError: null };
      }
      return { ...s, claimError: text || null };
    }

    case "prompt": {
      const env = a.env || {};
      const exchange = typeof env.exchange === "string" ? env.exchange : "";
      if (!exchange) return s;
      if (s.prompts.some((p) => p.exchange === exchange)) return s;
      const prompt = {
        exchange,
        from: String(env.from || ""),
        body: String(env.body || ""),
      };
      return { ...s, prompts: [...s.prompts, prompt] };
    }

    case "unprompt": {
      if (!s.prompts.some((p) => p.exchange === a.exchange)) return s;
      return {
        ...s,
        prompts: s.prompts.filter((p) => p.exchange !== a.exchange),
      };
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
