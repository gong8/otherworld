import { readFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const TYPES_PATH = "lib/protocol/types.ts";

// Snapshot the committed file before any test re-runs the generator.
const committed = await readFile(TYPES_PATH, "utf8");

const regenerate = async () => {
  execFileSync("node", ["scripts/gen-protocol.mjs"], { stdio: "pipe" });
  return readFile(TYPES_PATH, "utf8");
};

// (a) Generated file contains all eight kind literals and serves: string
describe("types.ts content", () => {
  const kinds = ["say", "hail", "propose", "accept", "decline", "withdraw", "ask_principal", "settle"];

  for (const kind of kinds) {
    it(`contains kind literal "${kind}"`, () => {
      assert.ok(committed.includes(`"${kind}"`), `Missing kind literal: "${kind}"`);
    });
  }

  it("declares serves as a required string", () => {
    assert.match(committed, /serves:\s*string/);
  });
});

// (b) Generator behavior — pure content comparison, no git involved.
describe("generator", () => {
  it("is idempotent: two consecutive runs produce identical output", async () => {
    const first = await regenerate();
    const second = await regenerate();
    assert.equal(second, first);
  });

  it("committed types.ts matches fresh generator output", async () => {
    const fresh = await regenerate();
    assert.equal(committed, fresh);
  });
});
