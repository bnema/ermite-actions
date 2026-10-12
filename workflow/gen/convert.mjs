// SPDX-License-Identifier: MIT
//
// Generates testdata/convert.json: for each workflow in testdata/convert/*.yml,
// the errors and the model summary that @actions/workflow-parser 0.3.61 (MIT,
// GitHub) produces. convert_test.go replays the files and compares.
//
//   node gen/convert.mjs /path/to/@actions/workflow-parser/dist > testdata/convert.json
//
// The package imports "yaml" and "@actions/expressions"; the JSON import of the
// schema needs `with { type: "json" }` in dist/workflows/workflow-schema.js on
// recent Node versions.

import { pathToFileURL } from "node:url";
import path from "node:path";
import fs from "node:fs";

const dist = path.resolve(process.argv[2]);
const imp = p => import(pathToFileURL(path.join(dist, p)).href);
const { parseWorkflow } = await imp("workflows/workflow-parser.js");
const { NoOperationTraceWriter } = await imp("templates/trace-writer.js");
const { convertWorkflowTemplate } = await imp("model/convert.js");

const dir = new URL("../testdata/convert/", import.meta.url);
const out = {};
for (const f of fs.readdirSync(dir).sort()) {
  if (!f.endsWith(".yml")) continue;
  const content = fs.readFileSync(new URL(f, dir), "utf8");
  const r = parseWorkflow({ name: f, content }, new NoOperationTraceWriter());
  const res = { errors: [] };
  if (!r.value) {
    res.errors = r.context.errors.getErrors().map(e => e.message);
  } else {
    // Default options: ReturnErrorsOnly, i.e. the converter does not run when the
    // schema reported errors.
    const t = await convertWorkflowTemplate(r.context, r.value);
    res.errors = (t.errors || []).map(e => e.Message);
    res.jobs = (t.jobs || []).map(j => ({
      id: j.id.value,
      name: String(j.name),
      if: j.if?.expression,
      needs: (j.needs || []).map(n => n.value),
      steps: (j.steps || []).map(s => ({ id: s.id, if: s.if?.expression })),
    }));
    res.events = t.events;
  }
  out[f] = res;
}
console.log(JSON.stringify(out, null, 1));
