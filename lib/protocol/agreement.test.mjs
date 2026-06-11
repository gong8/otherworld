import { readFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { describe, it, before } from "node:test";
import assert from "node:assert/strict";

// (a) Generated file contains all eight kind literals and `serves`
describe("types.ts content", async () => {
  let source;

  before(async () => {
    source = await readFile("lib/protocol/types.ts", "utf8");
  });

  const kinds = ["say", "hail", "propose", "accept", "decline", "withdraw", "ask_principal", "settle"];

  for (const kind of kinds) {
    it(`contains kind literal "${kind}"`, () => {
      assert.ok(source.includes(`"${kind}"`), `Missing kind literal: "${kind}"`);
    });
  }

  it("contains required field 'serves'", () => {
    assert.ok(source.includes("serves"), "Missing 'serves' field");
  });
});

// (b) Generator is idempotent: re-run then check git diff is clean
describe("generator idempotency", () => {
  it("re-running gen-protocol.mjs produces no diff in lib/protocol/types.ts", () => {
    execFileSync("node", ["scripts/gen-protocol.mjs"], { stdio: "inherit" });
    // This will throw (non-zero exit) if there are differences
    execFileSync("git", ["diff", "--exit-code", "--", "lib/protocol/types.ts"], { stdio: "inherit" });
  });
});
