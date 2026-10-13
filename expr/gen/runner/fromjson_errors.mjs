// SPDX-License-Identifier: MIT
//
// Writes testdata/fromjson_errors.json: for the inputs of testdata/fromjson_cases.json
// that the runner's fromJSON rejects, plus a few hand-picked ones (blank input, a
// bad character on a later line or deep in a document), the message of the
// JsonReaderException. TestFromJSONErrorsMatchRunner replays it.
//
//   node expr/gen/runner/fromjson_errors.mjs > expr/testdata/fromjson_errors.json
//
// Needs the built driver, see README.md.

import { readFileSync, writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { runDriver, here, checkEnvironment } from "./env.mjs";

checkEnvironment();
const cases = JSON.parse(readFileSync(join(here, "../../testdata/fromjson_cases.json"), "utf8"));
const extra = [
  "", "   ", "\n", "not json", "  not json", "\nnot json", "[x]", "[1,x]", '{"a":x}', "[\n  x]", '{"a b":x}', '{"a.b":[1,x]}',
  "{\"it's\":x}", '{"a":{"b":[1,2,x]}}', "[[x]]", "{", "[", "[1,2", '{"a":1', '"abc', "{'a'}", "[1 2]", '{"a":1 "b":2}',
  "tru", '{"k":tru}', '"\\q"', "[1,\n2,\n x]", " \n x", '{"a":\n\n  [1,\n  x]}',
];
const inputs = [...new Set([...cases.filter(c => !c.ok).map(c => c.in), ...extra])];
const lit = s => "'" + s.replaceAll("'", "''") + "'";
const tmp = mkdtempSync(join(tmpdir(), "fje-"));
writeFileSync(join(tmp, "in.json"), JSON.stringify({ contexts: {}, cases: inputs.map(s => ({ e: `fromJSON(${lit(s)})` })) }));
const out = runDriver([join(tmp, "in.json")]).split("\n").filter(Boolean);
rmSync(tmp, { recursive: true });
if (out.length !== inputs.length) throw new Error("answer count");
const res = [];
inputs.forEach((s, i) => {
  const r = JSON.parse(out[i]);
  if (r.err && r.err.t === "eval") res.push({ in: s, msg: r.err.m });
});
process.stdout.write(JSON.stringify(res));
