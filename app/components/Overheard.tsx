"use client";

import { useEffect, useRef, useState } from "react";
import { exchanges } from "@/lib/exchanges";

const ROTATE_MS = 14_000;

export function Overheard() {
  const [active, setActive] = useState(0);
  const hovered = useRef(false);

  useEffect(() => {
    const mql = window.matchMedia("(prefers-reduced-motion: reduce)");
    let id: number | undefined;

    const start = () => {
      if (id !== undefined) return;
      id = window.setInterval(() => {
        // hover pauses rotation so slow readers can finish (WCAG 2.2.2)
        if (!hovered.current) setActive((a) => (a + 1) % exchanges.length);
      }, ROTATE_MS);
    };
    const stop = () => {
      if (id !== undefined) {
        window.clearInterval(id);
        id = undefined;
      }
    };
    const sync = () => (mql.matches ? stop() : start());

    sync();
    mql.addEventListener("change", sync);
    return () => {
      stop();
      mql.removeEventListener("change", sync);
    };
  }, []);

  return (
    <>
      <div
        className="exchange-stack"
        aria-hidden="true"
        onPointerEnter={() => {
          hovered.current = true;
        }}
        onPointerLeave={() => {
          hovered.current = false;
        }}
      >
        {exchanges.map((exchange, i) => (
          <div key={i} className={i === active ? "exchange is-active" : "exchange"}>
            {exchange.lines.map((line, j) => (
              <p key={j} className="line">
                <span className="who">{line.who}</span> — {line.said}
              </p>
            ))}
            <p className="settled micro">[ settled · {exchange.settled} ]</p>
          </div>
        ))}
      </div>
      <div className="sr-only">
        {exchanges.map((exchange, i) => (
          <p key={i}>
            {exchange.lines.map((line) => `${line.who} — ${line.said}`).join(" ")} settled:{" "}
            {exchange.settled}.
          </p>
        ))}
      </div>
    </>
  );
}
