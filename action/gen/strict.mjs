// SPDX-License-Identifier: MIT
//
// Generates testdata/strict.json: for each file in testdata/cases/*.yml, the
// errors that @actions/workflow-parser 0.3.61 (MIT, GitHub,
// actions/languageservices) reports when it parses the file with parseAction
// (schema "action-root-strict") and converts it with convertActionTemplate
// (which validates the pre-if and post-if conditions and the step conditions).
// strict_test.go replays the answers against ParseStrict.
//
//   action/gen/regen.sh

import { pathToFileURL } from "node:url";
import path from "node:path";
import fs from "node:fs";

const dist = path.resolve(process.argv[2] ?? new URL("../../gen/node_modules/@actions/workflow-parser/dist", import.meta.url).pathname);
const imp = p => import(pathToFileURL(path.join(dist, p)).href);
const { parseAction } = await imp("actions/action-parser.js");
const { convertActionTemplate } = await imp("actions/action-template.js");
const { NoOperationTraceWriter } = await imp("templates/trace-writer.js");

const dir = new URL("../testdata/cases/", import.meta.url);
const out = {};
for (const f of fs.readdirSync(dir).sort()) {
  if (!f.endsWith(".yml")) continue;
  const content = fs.readFileSync(new URL(f, dir), "utf8");
  const r = parseAction({ name: f, content }, new NoOperationTraceWriter());
  const res = { errors: [] };
  if (r.value) {
    // Default error policy: the conversion only runs on a document without
    // schema errors, and adds its own (pre-if, post-if, step conditions).
    const t = convertActionTemplate(r.context, r.value);
    res.name = t.name; res.description = t.description; res.author = t.author;
    res.inputs = (t.inputs || []).map(i => ({ id: i.id, required: i.required, deprecationMessage: i.deprecationMessage, default: i.default === undefined ? undefined : String(i.default) }));
    res.outputs = (t.outputs || []).map(o => ({ id: o.id, value: o.value === undefined ? undefined : String(o.value) }));
    res.runs = { using: t.runs?.using, main: t.runs?.main, image: t.runs?.image, preIf: t.runs?.preIf, postIf: t.runs?.postIf, steps: (t.runs?.steps || []).length };
  }
  res.errors = r.context.errors.getErrors().map(e => e.message);
  out[f] = res;
}
console.log(JSON.stringify(out, null, 1));
