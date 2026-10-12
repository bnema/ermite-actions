// SPDX-License-Identifier: MIT
//
// Writes the golden files of TestToJSONGolden: for each input testdata/tojson/NAME.json
// it writes NAME.golden, which is what the reference implementation
// (@actions/expressions) prints for toJSON(fromJSON(input)), that is
// JSON.stringify(JSON.parse(input, reviver), replacer, "  ").
//
//   node gen/golden.mjs /path/to/@actions/expressions/dist testdata/tojson
//
// New inputs are added by hand to that directory (and listed in INPUTS below
// when they are generated rather than written out).

import { pathToFileURL } from "node:url";
import path from "node:path";
import fs from "node:fs";

const dist = path.resolve(process.argv[2]);
const dir = process.argv[3];
const { data } = await import(pathToFileURL(path.join(dist, "index.js")).href);

const INPUTS = {
  null: "null",
  true: "true",
  false: "false",
  zero: "0",
  negzero: "-0",
  int: "42",
  negative: "-17",
  float: "1.5",
  exp_small: "1e-7",
  exp_large: "1e21",
  exp_20: "1e20",
  exp_pos: "1E2",
  precision: "0.30000000000000004",
  big_int: "12345678901234567890",
  max_double: "1.7976931348623157e308",
  min_double: "5e-324",
  overflow: "1e999",
  string_empty: '""',
  string_plain: '"hello"',
  string_escapes: '"quote \\" backslash \\\\ slash \\/ nl \\n cr \\r tab \\t bs \\b ff \\f"',
  string_control: '"\\u0000\\u0001\\u001f\\u007f\\u0080"',
  string_unicode: '"h\\u00e9llo \\u4e16\\u754c \\ud83d\\ude00 \\u2028\\u2029"',
  string_lone_surrogate: '"\\ud800"',
  array_empty: "[]",
  array_flat: "[1,2,3]",
  array_mixed: '[null,true,"a",1.5,[],{}]',
  array_nested: "[[1,[2,[3,[]]]],[]]",
  object_empty: "{}",
  object_flat: '{"a":1,"b":"two","c":null}',
  object_nested: '{"a":{"b":{"c":{"d":[1,{"e":2}]}}},"f":[]}',
  object_order: '{"b":1,"a":2,"c":3}',
  object_numeric_keys: '{"b":1,"10":2,"2":3,"a":4,"01":5,"1":6,"4294967294":7,"4294967295":8,"-1":9}',
  object_case_dup: '{"a":1,"A":2,"b":3,"B":4}',
  object_dup_key: '{"a":1,"b":2,"a":3}',
  object_empty_key: '{"":1,"a":2}',
  object_key_escapes: '{"quote\\"key":1,"new\\nline":2,"\\u00e9":3}',
  github_event: '{"action":"opened","number":1,"pull_request":{"id":1,"draft":false,"labels":[{"name":"bug","color":"d73a4a"},{"name":"docs","color":null}],"head":{"ref":"feat/x","sha":"abc123"}},"repository":{"full_name":"octo/repo","private":true,"topics":[]}}',
};

fs.mkdirSync(dir, { recursive: true });
for (const [name, text] of Object.entries(INPUTS)) {
  fs.writeFileSync(path.join(dir, name + ".json"), text + "\n");
}
for (const f of fs.readdirSync(dir).filter(f => f.endsWith(".json")).sort()) {
  const text = fs.readFileSync(path.join(dir, f), "utf8");
  const value = JSON.parse(text, data.reviver);
  fs.writeFileSync(path.join(dir, f.replace(/\.json$/, ".golden")), JSON.stringify(value, data.replacer, "  "));
}
