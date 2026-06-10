"use client";

import { useLayoutEffect, useRef } from "react";

/**
 * Server-rendered HTML ships fully visible; this component hides itself only
 * after React is actually running (useLayoutEffect, before paint), so a failed
 * or blocked JS bundle can never strand the content invisible.
 */
export function Reveal({ children }: { children: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    el.classList.add("is-hidden");
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          el.classList.remove("is-hidden");
          observer.disconnect();
        }
      },
      { threshold: 0.2 },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  return (
    <div ref={ref} data-reveal>
      {children}
    </div>
  );
}
