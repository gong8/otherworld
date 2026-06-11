import { compile } from "json-schema-to-typescript";
import { readFile, writeFile } from "node:fs/promises";

const opts = { bannerComment: "", additionalProperties: false, style: { singleQuote: false } };

// Load a schema and drop the top-level allOf: the if/then conditionals there
// (e.g. "settle/propose require terms") collapse to an open
// `{[k:string]: unknown} &` intersection in the generated TS, which kills
// excess-property checking while adding nothing to the static type.
async function loadSchema(path) {
  const schema = JSON.parse(await readFile(path, "utf8"));
  delete schema.allOf;
  return schema;
}

const envelopeSchema = await loadSchema("proto/envelope.schema.json");
const charterSchema = await loadSchema("proto/charter.schema.json");

const envelope = await compile(envelopeSchema, envelopeSchema.title, opts);
const charter = await compile(charterSchema, charterSchema.title, opts);

const header = "// generated from proto/ — do not edit; npm run gen:protocol\n\n";
await writeFile("lib/protocol/types.ts", header + envelope + "\n" + charter);
console.log("lib/protocol/types.ts written");
