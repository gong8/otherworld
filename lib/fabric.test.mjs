/**
 * fabric.test.mjs — unit tests for the pure, I/O-free helpers in
 * fabric-pure.mjs.
 *
 * Run with: node --test lib/fabric.test.mjs
 * Or via:   npm run test:web
 *
 * No jsdom, no WebSocket, no browser globals needed — the helpers are plain
 * functions that only do arithmetic.
 */

import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { nextCursor, backoff } from "./fabric-pure.mjs";

// ─── nextCursor ─────────────────────────────────────────────────────────────

describe("nextCursor", () => {
  it("advances when frameSeq is greater", () => {
    assert.equal(nextCursor(5, 10), 10);
  });

  it("stays put when frameSeq equals current", () => {
    assert.equal(nextCursor(7, 7), 7);
  });

  it("stays put when frameSeq is less than current", () => {
    assert.equal(nextCursor(10, 3), 10);
  });

  it("works from zero (initial state)", () => {
    assert.equal(nextCursor(0, 1), 1);
  });

  it("works with large int64-range values", () => {
    const big = 9_007_199_254_740_991; // Number.MAX_SAFE_INTEGER
    assert.equal(nextCursor(big - 1, big), big);
  });
});

// ─── backoff ────────────────────────────────────────────────────────────────

describe("backoff", () => {
  it("returns 1000 on first call (prev = 0)", () => {
    assert.equal(backoff(0), 1000);
  });

  it("returns 1000 for any non-positive prev", () => {
    assert.equal(backoff(-100), 1000);
  });

  it("doubles on second call", () => {
    assert.equal(backoff(1000), 2000);
  });

  it("doubles again", () => {
    assert.equal(backoff(2000), 4000);
  });

  it("caps at 8000", () => {
    assert.equal(backoff(4000), 8000);
  });

  it("stays at 8000 once capped", () => {
    assert.equal(backoff(8000), 8000);
  });

  it("caps correctly from a very large value", () => {
    assert.equal(backoff(100_000), 8000);
  });

  it("full sequence: 0 → 1000 → 2000 → 4000 → 8000 → 8000", () => {
    let d = 0;
    const sequence = [];
    for (let i = 0; i < 5; i++) {
      d = backoff(d);
      sequence.push(d);
    }
    assert.deepEqual(sequence, [1000, 2000, 4000, 8000, 8000]);
  });
});
