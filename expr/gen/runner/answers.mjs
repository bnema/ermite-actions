// SPDX-License-Identifier: MIT
//
// Generates testdata/runner.json.gz: what the C# runner's expression engine
// (actions/runner, src/Sdk/DTExpressions2) answers on the expressions of
// testdata/oracle.json.gz. Needs the .NET SDK and a clone of actions/runner:
//
//   dotnet build -c Release expr/gen/runner -p:RunnerSrc=<runner>/src/Sdk
//   RUNNER_CLONE=<runner clone> node expr/gen/runner/answers.mjs | gzip -9 -n > expr/testdata/runner.json.gz

import { readFileSync, writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { runDriver, here, checkEnvironment } from "./env.mjs";
import { gunzipSync } from "node:zlib";

const env = checkEnvironment(process.argv[2]);

const oracle = JSON.parse(gunzipSync(readFileSync(join(here, "../../testdata/oracle.json.gz"))));
const dir = mkdtempSync(join(tmpdir(), "runner-answers-"));
const input = join(dir, "in.json");
writeFileSync(input, JSON.stringify({ contexts: oracle.contexts, cases: oracle.cases.map(c => ({ e: c.e })) }));
const out = runDriver([input]).split("\n").filter(Boolean);
if (out.length !== oracle.cases.length) throw new Error(`got ${out.length} answers for ${oracle.cases.length} cases`);
const cases = out.map((l, i) => {
  const r = JSON.parse(l);
  return { e: oracle.cases[i].e, ...r };
});
process.stdout.write(JSON.stringify({
  reference: { source: "actions/runner src/Sdk/DTExpressions2 (Expressions2)", commit: env.commit, runtime: env.runtime },
  cases,
}));
