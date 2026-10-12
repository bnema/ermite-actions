// SPDX-License-Identifier: MIT
//
// Writes testdata/ordinal_cases.txt: for pairs of strings chosen around the
// differences between .NET's OrdinalIgnoreCase and JavaScript's case mapping,
// what the runner's `--cmp` mode answers (equality, ordering, IndexOf,
// StartsWith, EndsWith). ordinal_test.go replays it.
//
//   dotnet build -c Release expr/gen/runner -p:RunnerSrc=<runner>/src/Sdk
//   node expr/gen/runner/ordinal_cases.mjs > expr/testdata/ordinal_cases.txt

import { writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { runDriver, here, checkEnvironment } from "./env.mjs";

const words = [
  "", "a", "A", "ab", "AB", "abc", "ABC", "Hello", "hELLO", "hello world", "true", "TRUE", "null",
  "ß", "SS", "ss", "ẞ", "ŉ", "ʼN", "ﬃ", "FFI", "ffi", "ı", "I", "i", "İ", "ſ", "s", "S",
  "\u212a", "k", "K", "\u212b", "å", "Å", "σ", "ς", "Σ", "ΑΣ", "ας", "ασ", "Ǆ", "ǅ", "ǆ", "ǈ", "ǉ",
  "é", "É", "e\u0301", "ÿ", "Ÿ", "µ", "μ", "Μ", "ǰ", "J\u030c", "ΐ", "ῼ", "ᾳ", "ᾼ",
  "\u00e9", "\u00c9", "ǅa", "Ǆa", "Ж", "ж", "ა", "Ა", "ꭰ", "Ꭰ", "ᏸ", "Ᏸ", "ⓐ", "Ⓐ", "ａ", "Ａ",
  "\ue000", "\uffff", "\ud83d\ude00", "\ud801\udc00", "\ud801\udc28", "\u{10428}", "\ud800\udc00", "ab\ud83d\ude00", "AB\ud83d\ude00",
  "x\u2028y", "x y", "x\u00a0y", "0", "00", "1e3", "-", "_", "a-b", "a_b", "[]", "'",
];
const hex = s => s.length ? [...s].flatMap(c => { const n = c.codePointAt(0); return n > 0xffff ? [0xd800 + ((n - 0x10000) >> 10), 0xdc00 + ((n - 0x10000) & 0x3ff)] : [n]; }).map(u => u.toString(16).toUpperCase()).join(".") : "-";
const lines = [];
for (const a of words) for (const b of words) lines.push(`${hex(a)} ${hex(b)}`);
const tmp = mkdtempSync(join(tmpdir(), "ordinal-"));
writeFileSync(join(tmp, "in.txt"), lines.join("\n") + "\n");
const out = runDriver(["--cmp", join(tmp, "in.txt")]).split("\n").filter(Boolean);
rmSync(tmp, { recursive: true });
if (out.length !== lines.length) throw new Error("answer count");
// Keep every pair that is interesting (some predicate true) and a sample of the rest, to bound the file.
const res = [];
lines.forEach((l, i) => {
  const interesting = out[i] !== "0 0 0 0 0" && out[i] !== "0 -1 0 0 0" && out[i] !== "0 1 0 0 0";
  if (interesting || i % 7 === 0) res.push(`${l} ${out[i]}`);
});
process.stdout.write(res.join("\n") + "\n");
