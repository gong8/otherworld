import { compileFromFile } from "json-schema-to-typescript";
import { writeFile } from "node:fs/promises";

const opts = { bannerComment: "", additionalProperties: false, style: { singleQuote: false } };
const envelope = await compileFromFile("proto/envelope.schema.json", opts);
const charter = await compileFromFile("proto/charter.schema.json", opts);
const header = "// generated from proto/ — do not edit; npm run gen:protocol\n\n";
// Add clean aliases so consumers can import { Envelope, Charter } without the schema-derived prefix.
const aliases =
  "// Clean aliases\n" +
  "export type Envelope = OtherworldEnvelope;\n" +
  "export type Charter = OtherworldCharter;\n";
await writeFile("lib/protocol/types.ts", header + envelope + "\n" + charter + "\n" + aliases);
console.log("lib/protocol/types.ts written");
