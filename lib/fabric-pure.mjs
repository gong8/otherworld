/**
 * fabric-pure.mjs — pure (no I/O, no WebSocket) helpers extracted from the
 * fabric client so they can be tested directly with `node --test` without
 * needing to compile TypeScript or mock browser globals.
 *
 * Design note: lib/fabric.ts imports these helpers rather than duplicating the
 * logic. The helpers are plain JS with JSDoc types; TypeScript treats the file
 * as `allowJs` ambient, so no separate .d.ts is needed.
 */

/**
 * Advance the reconnect cursor: only move forward, never back.
 *
 * @param {number} current  The current cursor value.
 * @param {number} frameSeq The seq from the received Frame.
 * @returns {number}
 */
export function nextCursor(current, frameSeq) {
  return frameSeq > current ? frameSeq : current;
}

/**
 * Capped exponential backoff: doubles prev, capped at 8000 ms.
 * First call should pass 0 to get the initial 1000 ms delay.
 *
 * @param {number} prev  Previous backoff in ms (0 → first call).
 * @returns {number}     Next backoff in ms, in [1000, 8000].
 */
export function backoff(prev) {
  if (prev <= 0) return 1000;
  return Math.min(prev * 2, 8000);
}
