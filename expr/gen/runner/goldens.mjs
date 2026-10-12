// SPDX-License-Identifier: MIT
//
// For each testdata/tojson/NAME.json, runs toJSON(fromJSON(<file>)) through the
// C# runner's engine and writes testdata/tojson/NAME.runner when the answer is
// not the JavaScript one (NAME.golden). golden_test.go prefers NAME.runner.
//
//   dotnet build -c Release expr/gen/runner -p:RunnerSrc=<runner>/src/Sdk
//   node expr/gen/runner/goldens.mjs

import { readFileSync, readdirSync, writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { runDriver, here, checkEnvironment } from "./env.mjs";

const dir = join(here, "../../testdata/tojson");
const names = readdirSync(dir).filter(f => f.endsWith(".json")).map(f => f.slice(0, -5)).sort();
const cases = names.map(n => ({ e: `toJSON(fromJSON('${readFileSync(join(dir, n + ".json"), "utf8").replaceAll("'", "''")}'))` }));
const tmp = mkdtempSync(join(tmpdir(), "goldens-"));
writeFileSync(join(tmp, "in.json"), JSON.stringify({ contexts: {}, cases }));
const out = runDriver([join(tmp, "in.json")]).split("\n").filter(Boolean);
rmSync(tmp, { recursive: true });
names.forEach((n, i) => {
  const r = JSON.parse(out[i]);
  const golden = readFileSync(join(dir, n + ".golden"), "utf8");
  if (r.err) { console.error(`${n}: runner error: ${r.err.m}`); return; }
  if (r.s !== golden) { writeFileSync(join(dir, n + ".runner"), r.s); console.log(`${n}: differs`); }
});
