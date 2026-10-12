// SPDX-License-Identifier: MIT
//
// Runs the oracle corpus, the fromJSON corpus, the number corpus and the string
// comparison corpus through both expression engines of actions/runner and lists
// where they differ:
//   legacy  src/Sdk/DTExpressions2 (driver ../runner)
//   newer   src/Sdk/Expressions    (driver ./runner-new)
//
//   dotnet build -c Release expr/gen/runner     -p:RunnerSrc=$RUNNER_CLONE/src/Sdk
//   dotnet build -c Release expr/gen/runner-new -p:RunnerSrc=$RUNNER_CLONE/src/Sdk
//   RUNNER_CLONE=<clone> node expr/gen/runner-new/engines.mjs
//
// The exception message of a fromJSON failure is not compared (the engines word
// it differently); everything else must be identical. Exit code 1 if anything
// differs beyond the known difference: negative zero (the newer engine prints 0).

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { gunzipSync } from "node:zlib";
import { checkEnvironment } from "../runner/env.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const testdata = join(here, "../../testdata");
checkEnvironment(process.argv[2]);

const dec = h => (h === "-" ? "" : h.split(".").map(u => String.fromCharCode(parseInt(u, 16))).join(""));
const lit = s => "'" + s.replaceAll("'", "''") + "'";
const hasLone = s => /[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/.test(s);

const oracle = JSON.parse(gunzipSync(readFileSync(join(testdata, "oracle.json.gz"))));
const corpora = { oracle: { contexts: oracle.contexts, cases: oracle.cases.map(c => c.e) } };
const exprs = [];
for (const c of JSON.parse(readFileSync(join(testdata, "fromjson_cases.json"), "utf8"))) exprs.push(`toJSON(fromJSON(${lit(c.in)}))`);
for (const l of readFileSync(join(testdata, "number_cases.txt"), "utf8").trim().split("\n")) {
  const s = dec(l.split(" ")[0]);
  if (hasLone(s)) continue;
  for (const op of ["== 0", "== 1", "> 255", "< 0"]) exprs.push(`${lit(s)} ${op}`);
}
for (const l of readFileSync(join(testdata, "ordinal_cases.txt"), "utf8").trim().split("\n")) {
  const [a, b] = l.split(" ");
  const A = dec(a), B = dec(b);
  if (hasLone(A + B)) continue;
  exprs.push(`${lit(A)} == ${lit(B)}`, `${lit(A)} < ${lit(B)}`, `contains(${lit(A)}, ${lit(B)})`, `startsWith(${lit(A)}, ${lit(B)})`, `endsWith(${lit(A)}, ${lit(B)})`);
}
corpora.strings = { contexts: {}, cases: exprs };

const run = (dll, input) => execFileSync("dotnet", [dll, input], { maxBuffer: 1 << 30 }).toString("utf8").split("\n").filter(Boolean).map(l => JSON.parse(l));
const sig = r => (r.err ? JSON.stringify(["err", r.err.t, r.err.x === "JsonReaderException" ? r.err.x : r.err.m]) : JSON.stringify(r));

const tmp = mkdtempSync(join(tmpdir(), "engines-"));
let unexpected = 0, total = 0;
for (const [name, c] of Object.entries(corpora)) {
  const input = join(tmp, name + ".json");
  writeFileSync(input, JSON.stringify({ contexts: c.contexts, cases: c.cases.map(e => ({ e })) }));
  const a = run(join(here, "../runner/bin/Release/net9.0/runner.dll"), input);
  const b = run(join(here, "bin/Release/net9.0/runner-new.dll"), input);
  let diffs = 0;
  a.forEach((r, i) => {
    total++;
    if (sig(r) === sig(b[i])) return;
    diffs++;
    const known = /^(toJSON\(-0\)|-0|toJSON\(fromJSON\('-\.0'\)\))$/.test(c.cases[i]);
    if (!known) unexpected++;
    console.log(`${known ? "known " : "DIFF  "} ${name}: ${JSON.stringify(c.cases[i].slice(0, 80))}\n   legacy ${sig(r).slice(0, 100)}\n   newer  ${sig(b[i]).slice(0, 100)}`);
  });
  console.log(`${name}: ${a.length} expressions, ${diffs} differences`);
}
rmSync(tmp, { recursive: true });
console.log(`${total} expressions, ${unexpected} unexpected differences`);
process.exit(unexpected ? 1 : 0);
