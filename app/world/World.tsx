"use client";

import { useEffect, useReducer, useRef } from "react";
import {
  claim,
  consent,
  openFeed,
  openLine,
  state as worldState,
} from "@/lib/fabric";
import {
  fullNotice,
  initialWorld,
  settleLine,
  stateLineOf,
  voiceName,
  worldReducer,
} from "./reducer.mjs";

type Line = ReturnType<typeof openLine>;

/**
 * The séance: one feed, one input. All state lives in the pure reducer
 * (reducer.mjs); this component only wires sockets, fetches, and the DOM.
 */
export function World({ scope }: { scope: string }) {
  const [w, dispatch] = useReducer(worldReducer, scope, initialWorld);
  const lineRef = useRef<Line | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const atBottom = useRef(true);

  // a claim survives refresh; a new tab is a new visitor (sessionStorage).
  useEffect(() => {
    try {
      const raw = sessionStorage.getItem("ow:" + scope);
      if (!raw) return;
      const c = JSON.parse(raw);
      if (c && typeof c.voice === "string" && typeof c.token === "string") {
        dispatch({ type: "claimed", claim: c });
      }
    } catch {
      // an unreadable claim is no claim
    }
  }, [scope]);

  // the public feed, replayed from the beginning.
  useEffect(
    () =>
      openFeed(scope, 0, {
        onFrame: (f) => dispatch({ type: "frame", frame: f }),
        onStatus: (s) =>
          dispatch({ type: "status", channel: "feed", status: s }),
      }),
    [scope]
  );

  // the private line, once claimed. A dead token (three closes without ever
  // opening) means the world no longer knows this voice: the claim lapses.
  const token = w.claim ? w.claim.token : null;
  useEffect(() => {
    if (!token) return;
    const line = openLine(
      token,
      (env) => dispatch({ type: "prompt", env }),
      (s) => {
        if (s === "dead") {
          try {
            sessionStorage.removeItem("ow:" + scope);
          } catch {
            // nothing to forget is fine
          }
          dispatch({ type: "lapsed" });
          return;
        }
        dispatch({ type: "status", channel: "line", status: s });
      }
    );
    lineRef.current = line;
    return () => {
      lineRef.current = null;
      line.close();
    };
  }, [token, scope]);

  // the state line: fetched on mount, refreshed after every settle frame.
  useEffect(() => {
    if (!w.stateStale) return;
    const ctrl = new AbortController();
    worldState(scope, ctrl.signal)
      .then((j) => {
        if (ctrl.signal.aborted) return;
        dispatch({ type: "stateLine", text: stateLineOf(scope, j) });
        dispatch({ type: "degraded", value: Boolean(j.degraded) });
      })
      .catch(() => {
        // keep the prior line; disarm so the next settle retries
        if (!ctrl.signal.aborted) dispatch({ type: "stateLine", text: null });
      });
    return () => ctrl.abort();
  }, [w.stateStale, scope]);

  // autoscroll only when already at the foot — never fight a reading user.
  useEffect(() => {
    const measure = () => {
      atBottom.current =
        window.innerHeight + window.scrollY >=
        document.documentElement.scrollHeight - 120;
    };
    measure();
    window.addEventListener("scroll", measure, { passive: true });
    window.addEventListener("resize", measure, { passive: true });
    return () => {
      window.removeEventListener("scroll", measure);
      window.removeEventListener("resize", measure);
    };
  }, []);

  useEffect(() => {
    if (atBottom.current) {
      window.scrollTo({ top: document.documentElement.scrollHeight });
    }
  }, [w.frames]);

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const input = inputRef.current;
    if (!input) return;
    const text = input.value.trim();
    if (!text) return;
    input.value = "";

    if (w.claim) {
      // no optimistic echo: the say arrives via the feed — the record is the product
      lineRef.current?.send(text);
      return;
    }

    try {
      const granted = await claim(scope, text);
      const c = { voice: granted.voice, token: granted.token, name: text };
      try {
        sessionStorage.setItem("ow:" + scope, JSON.stringify(c));
      } catch {
        // a claim that cannot persist is still a claim for this visit
      }
      dispatch({ type: "claimed", claim: c });
    } catch (err) {
      dispatch({
        type: "claimError",
        text:
          err instanceof Error && err.message
            ? err.message
            : "the line is not answering.",
      });
    }
  }

  function answer(exchange: string, approve: boolean) {
    dispatch({ type: "unprompt", exchange });
    if (w.claim) {
      consent(w.claim.token, exchange, approve).catch(() => {
        // the record will show what actually settled
      });
    }
  }

  const stateText = w.degraded
    ? w.stateLine + " · the record is stalling"
    : w.stateLine;
  const placeholder = w.claim
    ? "speak to yours"
    : (w.claimError ?? "your name, to claim a voice");

  return (
    <div className="world-live">
      <p className="world-state micro">{stateText}</p>

      <div className="ruled-label">
        <div className="rule" />
        <span className="micro" id="overheard-label">
          overheard
        </span>
        <div className="rule" />
      </div>

      <section className="world-feed world-col" aria-labelledby="overheard-label">
        {w.frames.map((rec) =>
          rec.env.kind === "settle" ? (
            <div key={rec.seq} className="settle micro">
              {settleLine(rec.env, rec.parties)}
            </div>
          ) : (
            <p key={rec.seq} className="line">
              <span className="who">{voiceName(rec.env.from)}</span> —{" "}
              {rec.env.body}
            </p>
          )
        )}
      </section>

      {w.prompts.length > 0 && (
        <div className="world-prompts world-col">
          {w.prompts.map((p) => (
            <div key={p.exchange} className="prompt">
              <p className="line">
                <span className="who">{voiceName(p.from)}</span> — {p.body}
              </p>
              <p className="consent micro">
                <button type="button" onClick={() => answer(p.exchange, true)}>
                  yes
                </button>
                {" · "}
                <button type="button" onClick={() => answer(p.exchange, false)}>
                  no
                </button>
              </p>
            </div>
          ))}
        </div>
      )}

      <footer className="world-foot world-col">
        {w.feedOpen === false && (
          <p className="world-quiet micro">the line is quiet. reconnecting…</p>
        )}
        {w.claim != null && w.lineOpen === false && (
          <p className="world-quiet micro">your line is quiet. reconnecting…</p>
        )}
        {w.full ? (
          <p className="micro world-full">{fullNotice(scope)}</p>
        ) : (
          <form onSubmit={onSubmit}>
            <input
              ref={inputRef}
              className="speak-line"
              type="text"
              autoComplete="off"
              spellCheck={false}
              placeholder={placeholder}
              aria-label={
                w.claim ? "speak to yours" : "your name, to claim a voice"
              }
            />
          </form>
        )}
      </footer>
    </div>
  );
}
