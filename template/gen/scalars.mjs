// SPDX-License-Identifier: MIT
//
// Generates testdata/scalars.json: how the JavaScript `yaml` library (2.x, the
// one @actions/workflow-parser uses) types plain scalars.
//
//   node gen/scalars.mjs > testdata/scalars.json

import {parseDocument} from "yaml";
const vals = ["yes","no","on","off","y","n","~","null","Null","NULL","","true","True","TRUE","tRUE","false","0","-0","+1","017","0o17","0O17","0x1F","0X1f","0b11","1_000","1:30","1e3","1E3","1.","1.5","-.5",".5","+.5",".inf","-.inf",".Inf",".INF","+.inf",".nan",".NaN",".NAN","-.nan","1e","e1","0x","0xg","12abc","2001-01-01","1.5e+3","0.","00","08","-017","+0x1F","-0x1F","1,000","Infinity","NaN","inf","=","<<","!!str 1","!!int 5"];
const out = {};
for (const v of vals) {
  const d = parseDocument("k: " + v + "\n");
  const x = d.get("k");
  out[v] = d.errors.length ? "ERR" : (x === null || x === undefined ? "null" : typeof x === "number" ? "num:" + (Number.isNaN(x) ? "NaN" : x) : typeof x === "boolean" ? "bool:" + x : typeof x === "string" ? "str:" + x : typeof x);
}
console.log(JSON.stringify(out));
