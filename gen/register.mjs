// SPDX-License-Identifier: MIT
//
// Node 22+ refuses to import a .json file without `with { type: "json" }`,
// which @actions/workflow-parser 0.3.61 does for workflow-v1.0.min.json. This
// loader hook adds the attribute so that the package can run unmodified:
//
//   node --import ./gen/register.mjs workflow/gen/convert.mjs
import { register } from "node:module";

register("./json-hook.mjs", import.meta.url);
