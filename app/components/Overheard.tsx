"use client";

import { useEffect, useState } from "react";
import { exchanges } from "@/lib/exchanges";

const ROTATE_MS = 14_000;

export function Overheard() {
  const [active, setActive] = useState(0);

  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const id = window.setInterval(() => {
      setActive((a) => (a + 1) % exchanges.length);
    }, ROTATE_MS);
    return () => window.clearInterval(id);
  }, []);

  return (
    <>
      <div className="exchange-stack" aria-hidden="true">
        {exchanges.map((exchange, i) => (
          <div key={i} className={i === active ? "exchange is-active" : "exchange"}>
            {exchange.map((line, j) => (
              <p key={j} className="line">
                <span className="who">{line.who}</span> — {line.said}
              </p>
            ))}
          </div>
        ))}
      </div>
      <div className="sr-only">
        {exchanges.map((exchange, i) => (
          <p key={i}>{exchange.map((line) => `${line.who} — ${line.said}`).join(" ")}</p>
        ))}
      </div>
    </>
  );
}
