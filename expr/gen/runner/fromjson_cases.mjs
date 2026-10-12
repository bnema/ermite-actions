// SPDX-License-Identifier: MIT
//
// Writes testdata/fromjson_cases.json: for a corpus of JSON-ish inputs (hand
// picked and seeded random mutations of them), whether the runner's
// fromJSON accepts the text and what toJSON of the result is. The Go tests
// replay it (TestFromJSONMatchesRunner) and use the inputs as fuzz seeds.
//
//   node expr/gen/runner/fromjson_cases.mjs > expr/testdata/fromjson_cases.json
//
// Needs the built driver, see README.md.

import { execFileSync } from "node:child_process";
import { writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const seeds = [
  "null", "true", "false", "NaN", "Infinity", "-Infinity", "undefined", "0", "-0", "010", "08", "0x1F", "1.5e3", ".5", "5.", "12345678901234567890",
  '"a"', "'a'", '"\\u00e9\\ud83d\\ude00"', '"\\ud800"', "[]", "{}", "[1,2,3]", "[1,]", "[,1]", "[1,,2]", '{"a":1}', '{"a":1,"A":2}', "{a:1}", "{'a':'b'}",
  '{"a":[1,{"b":null}]}', "[[[1]]]", "[null,true,false]", "1 2", "[] x", ",", ",1", "/*c*/1", "1/*c*/", "[1/*c*/,2]", "null/x", "new Date(1)",
  "[010,08,-010]", '{"a":010}', "1e5", "1E+5", "-1.5e-3", "0.1e1", "0e0", "00", "-00", "0.0", "00.5",
];
const alphabet = [...'"\'{}[],:/*-+.0123456789xXeE tfnuNaIy\\\n\tabc_$'];
let state = 20260101;
const rnd = n => { state = (state * 1103515245 + 12345) & 0x7fffffff; return state % n; };
const set = new Set(seeds);
for (let i = 0; i < 4000; i++) {
  const base = seeds[rnd(seeds.length)];
  let s = base;
  for (let k = 0, n = 1 + rnd(3); k < n; k++) {
    const pos = rnd(s.length + 1);
    switch (rnd(3)) {
      case 0: s = s.slice(0, pos) + alphabet[rnd(alphabet.length)] + s.slice(pos); break;
      case 1: s = s.slice(0, pos) + s.slice(pos + 1); break;
      default: s = s.slice(0, pos) + alphabet[rnd(alphabet.length)] + s.slice(pos + 1);
    }
  }
  set.add(s);
}
const inputs = [...set];
const lit = s => "'" + s.replaceAll("'", "''") + "'";
const cases = inputs.map(s => ({ e: `toJSON(fromJSON(${lit(s)}))` }));
const tmp = mkdtempSync(join(tmpdir(), "fj-"));
writeFileSync(join(tmp, "in.json"), JSON.stringify({ contexts: {}, cases }));
const out = execFileSync("dotnet", [join(here, "bin/Release/net9.0/runner.dll"), join(tmp, "in.json")], { maxBuffer: 1 << 28 }).toString("utf8").split("\n").filter(Boolean);
rmSync(tmp, { recursive: true });
if (out.length !== inputs.length) throw new Error("answer count");
const res = inputs.map((s, i) => {
  const r = JSON.parse(out[i]);
  if (r.err) {
    // An error while parsing the expression (e.g. the text broke the literal) is not about fromJSON.
    return r.err.t === "eval" ? { in: s, ok: false } : null;
  }
  return { in: s, ok: true, out: r.s };
}).filter(Boolean);
process.stdout.write(JSON.stringify(res));
