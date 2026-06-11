/**
 * fabric.ts — typed, framework-free client for the fabric gateway.
 *
 * All functions are browser-only. openFeed and openLine guard explicitly;
 * claim/consent/state rely on fetch which is universally available in modern
 * environments but also only used from client components.
 *
 * Pure helpers (nextCursor, backoff) live in fabric-pure.mjs so they can be
 * unit-tested with `node --test` without compiling TypeScript or mocking
 * browser globals. This file imports and re-exports them; no logic is
 * duplicated.
 */

import type { Envelope, Frame } from "./protocol";
import { nextCursor as _nextCursor, backoff as _backoff } from "./fabric-pure.mjs";

export { nextCursor, backoff } from "./fabric-pure.mjs";

const base = process.env.NEXT_PUBLIC_FABRIC_URL ?? "http://localhost:8080";
const wsBase = base.replace(/^http/, "ws");

// ─── Feed ──────────────────────────────────────────────────────────────────

export type FeedHandler = {
  onFrame: (f: Frame) => void;
  onStatus: (s: "open" | "closed") => void;
};

/**
 * openFeed — auto-reconnecting public feed socket.
 *
 * Replays from `after` on first connect; on reconnect the cursor advances to
 * the last seen seq so the gateway replays only the gap. Backoff starts at 1 s
 * and doubles up to 8 s. Returns a close() function; calling it prevents all
 * future reconnects.
 */
export function openFeed(
  scope: string,
  after: number,
  h: FeedHandler
): () => void {
  if (typeof window === "undefined") throw new Error("browser only");

  let closed = false;
  let cursor = after;
  let delay = 0; // _backoff(0) → 1000 on first failure

  function connect() {
    if (closed) return;

    const url = `${wsBase}/v0/feed?scope=${encodeURIComponent(scope)}&after=${cursor}`;
    const ws = new WebSocket(url);

    ws.onopen = () => {
      delay = 0; // reset backoff on successful open
      h.onStatus("open");
    };

    ws.onmessage = (ev) => {
      try {
        const f: Frame = JSON.parse(ev.data as string);
        cursor = _nextCursor(cursor, f.seq) as number;
        h.onFrame(f);
      } catch {
        // drop malformed frames silently — the feed must never throw into React
      }
    };

    ws.onclose = () => {
      h.onStatus("closed");
      if (!closed) {
        delay = _backoff(delay) as number;
        setTimeout(connect, delay);
      }
    };

    ws.onerror = () => {
      // onclose fires after onerror; let it handle reconnect
    };
  }

  connect();

  return () => {
    closed = true;
  };
}

// ─── Claim ─────────────────────────────────────────────────────────────────

/**
 * claim — POST /v0/claim. On failure throws an Error whose message is the
 * plain-text response body (the page shows this verbatim to the user).
 */
export async function claim(
  scope: string,
  name: string
): Promise<{ voice: string; token: string }> {
  const r = await fetch(`${base}/v0/claim`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ scope, name }),
  });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}

// ─── Line ──────────────────────────────────────────────────────────────────

/**
 * openLine — token-gated private line.
 *
 * RECEIVE: Frame JSON; only `ask_principal` envelopes reach onPrompt (other
 * principal-addressed envelopes are already visible on the public feed).
 * SEND: plain text spoken as the principal; no-ops unless the socket is OPEN.
 */
export function openLine(
  token: string,
  onPrompt: (env: Envelope) => void
): { send: (text: string) => void; close: () => void } {
  if (typeof window === "undefined") throw new Error("browser only");

  const url = `${wsBase}/v0/line?token=${encodeURIComponent(token)}`;
  const ws = new WebSocket(url);

  ws.onmessage = (ev) => {
    try {
      const f: Frame = JSON.parse(ev.data as string);
      if (f.env.kind === "ask_principal") {
        onPrompt(f.env);
      }
    } catch {
      // drop malformed frames silently
    }
  };

  return {
    send(text: string) {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(text);
      }
    },
    close() {
      ws.close();
    },
  };
}

// ─── Consent ───────────────────────────────────────────────────────────────

/**
 * consent — POST /v0/consent. Throws on !ok so the UI can re-surface the
 * prompt when the server rejects (e.g. unknown token, already resolved).
 */
export async function consent(
  token: string,
  exchange: string,
  approve: boolean
): Promise<void> {
  const r = await fetch(`${base}/v0/consent`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token, exchange, approve }),
  });
  if (!r.ok) throw new Error(await r.text());
}

// ─── State ─────────────────────────────────────────────────────────────────

/**
 * state — GET /v0/state. Returns the scope's world-state object; throws on
 * !ok.
 */
export async function state(scope: string): Promise<Record<string, unknown>> {
  const r = await fetch(
    `${base}/v0/state?scope=${encodeURIComponent(scope)}`
  );
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}
