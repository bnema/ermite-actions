// SPDX-License-Identifier: MIT
//
// Generates testdata/runner.json.gz: what the C# runner's expression engine
// (actions/runner, src/Sdk/DTExpressions2) answers on the expressions of
// testdata/oracle.json.gz. Needs the .NET SDK and a clone of actions/runner:
//
//   dotnet build -c Release expr/gen/runner -p:RunnerSrc=<runner>/src/Sdk
//   node expr/gen/runner/answers.mjs <runner clone> | gzip -9 -n > expr/testdata/runner.json.gz

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { gunzipSync } from "node:zlib";

const here = dirname(fileURLToPath(import.meta.url));
const runnerClone = process.argv[2];
if (!runnerClone) throw new Error("usage: answers.mjs <actions/runner clone>");

const oracle = JSON.parse(gunzipSync(readFileSync(join(here, "../../testdata/oracle.json.gz"))));
const dir = mkdtempSync(join(tmpdir(), "runner-answers-"));
const input = join(dir, "in.json");
writeFileSync(input, JSON.stringify({ contexts: oracle.contexts, cases: oracle.cases.map(c => ({ e: c.e })) }));
const out = execFileSync("dotnet", [join(here, "bin/Release/net9.0/runner.dll"), input], { maxBuffer: 1 << 30 }).toString("utf8").split("\n").filter(Boolean);
if (out.length !== oracle.cases.length) throw new Error(`got ${out.length} answers for ${oracle.cases.length} cases`);
const git = (...a) => execFileSync("git", ["-C", runnerClone, ...a]).toString().trim();
const dotnet = execFileSync("dotnet", ["--list-runtimes"]).toString().split("\n").find(l => l.includes("Microsoft.NETCore.App 9")) ?? "";
const cases = out.map((l, i) => {
  const r = JSON.parse(l);
  if (r.err) delete r.err.x; // .NET exception type name
  return { e: oracle.cases[i].e, ...r };
});
process.stdout.write(JSON.stringify({
  reference: { source: "actions/runner src/Sdk/DTExpressions2 (Expressions2)", commit: git("rev-parse", "HEAD"), runtime: dotnet.trim() },
  cases,
}));
