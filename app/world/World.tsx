"use client";

import Link from "next/link";
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
  scopeTitle,
  stateLineOf,
  worldReducer,
} from "./reducer.mjs";

type Line = ReturnType<typeof openLine>;

/**
 * The chat: a conventional messaging screen where the other side of the
 * thread is your agent at work. All state lives in the pure reducer
 * (reducer.mjs); this component only wires sockets, fetches, and the DOM.
 */
export function World({ scope }: { scope: string }) {
  const [w, dispatch] = useReducer(worldReducer, scope, initialWorld);
  const lineRef = useRef<Line | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const threadRef = useRef<HTMLDivElement>(null);
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

  // the public feed, replayed from the beginning; the reducer filters it
  // down to this agent's dealings.
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

  // the status line: fetched on mount, refreshed after every settle frame.
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
    const el = threadRef.current;
    if (!el) return;
    const measure = () => {
      atBottom.current =
        el.scrollTop + el.clientHeight >= el.scrollHeight - 80;
    };
    measure();
    el.addEventListener("scroll", measure, { passive: true });
    return () => el.removeEventListener("scroll", measure);
  }, []);

  useEffect(() => {
    const el = threadRef.current;
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [w.items]);

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
            : "the line is not answering",
      });
    }
  }

  function answer(exchange: string, approve: boolean) {
    dispatch({ type: "unprompt", exchange });
    if (w.claim) {
      consent(w.claim.token, exchange, approve).catch(() => {
        // the thread will show what actually settled
      });
    }
  }

  const title = w.claim ? w.claim.name + "'s agent" : "your agent";
  const statusText = w.degraded
    ? w.stateLine + " · running behind"
    : w.stateLine;
  const placeholder = w.claim
    ? "message your agent"
    : (w.claimError ?? "your name to join");
  const reconnecting =
    w.feedOpen === false || (w.claim != null && w.lineOpen === false);

  // sender labels show above the first bubble in a run
  const labels = w.items.map((it) =>
    it.kind === "agent" || it.kind === "consent"
      ? "label" in it
        ? it.label
        : "your agent"
      : it.kind === "other"
        ? it.label
        : null
  );

  return (
    <main className="chat">
      <header className="chat-top">
        <div className="chat-top-inner">
          <div className="chat-title">
            <h1>{title}</h1>
            <p>{statusText}</p>
          </div>
          {scope === "scope:household" ? (
            <Link className="chat-scope" href="/world?scope=street">
              street
            </Link>
          ) : (
            <Link className="chat-scope" href="/world">
              household
            </Link>
          )}
        </div>
      </header>

      <div className="chat-thread" ref={threadRef}>
        <div className="chat-thread-inner">
          {!w.claim && w.items.length === 0 && (
            <p className="chat-system chat-hint">
              enter a name to join {scopeTitle(scope)}
            </p>
          )}

          {w.items.map((it, i) => {
            const label = labels[i];
            const showLabel = label != null && label !== labels[i - 1];

            if (it.kind === "system") {
              return (
                <p key={it.key} className="chat-system">
                  {it.text}
                </p>
              );
            }
            if (it.kind === "mine") {
              return (
                <div key={it.key} className="chat-row mine">
                  <div className="chat-bubble mine">{it.body}</div>
                </div>
              );
            }
            if (it.kind === "consent") {
              return (
                <div key={it.key} className="chat-row">
                  {showLabel && <div className="chat-sender">{label}</div>}
                  <div className="chat-bubble agent">{it.body}</div>
                  {!it.answered && (
                    <div className="chat-consent">
                      <button
                        type="button"
                        onClick={() => answer(it.exchange, true)}
                      >
                        allow
                      </button>
                      <button
                        type="button"
                        onClick={() => answer(it.exchange, false)}
                      >
                        decline
                      </button>
                    </div>
                  )}
                </div>
              );
            }
            return (
              <div key={it.key} className="chat-row">
                {showLabel && <div className="chat-sender">{label}</div>}
                <div
                  className={
                    it.kind === "agent"
                      ? "chat-bubble agent"
                      : "chat-bubble other"
                  }
                >
                  {it.body}
                </div>
              </div>
            );
          })}

          {reconnecting && <p className="chat-system">reconnecting…</p>}
        </div>
      </div>

      <footer className="chat-compose">
        <div className="chat-compose-inner">
          {w.full ? (
            <p className="chat-full">{fullNotice(scope)}</p>
          ) : (
            <form onSubmit={onSubmit}>
              <input
                ref={inputRef}
                type="text"
                autoComplete="off"
                spellCheck={false}
                enterKeyHint="send"
                placeholder={placeholder}
                aria-label={w.claim ? "message your agent" : "your name to join"}
              />
            </form>
          )}
        </div>
      </footer>
    </main>
  );
}
