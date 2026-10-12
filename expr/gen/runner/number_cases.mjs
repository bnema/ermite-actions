// SPDX-License-Identifier: MIT
//
// Writes testdata/number_cases.txt: ExpressionUtility.ParseNumber of the
// runner (string to number, used for number literals and for comparisons of
// strings with numbers) on a corpus of strings, as `--num` prints it.
// number_test.go replays it.
//
//   dotnet build -c Release expr/gen/runner -p:RunnerSrc=<runner>/src/Sdk
//   node expr/gen/runner/number_cases.mjs > expr/testdata/number_cases.txt

import { execFileSync } from "node:child_process";
import { writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const set = new Set();
const add = s => set.add(s);
const base = ["", " ", "0", "00", "-0", "+0", "1", "-1", "+1", "12", "007", "1.5", "-1.5", "1.", ".5", "-.5", "+.5", ".", "-.", "+.", "..", "1..2", "1.2.3",
  "1e3", "1E3", "1e+3", "1e-3", "1e", "1e+", "1e-", "e3", "E3", ".e3", "1.e3", ".5e3", "1e3.5", "1e03", "1e0003", "1e308", "1e309", "-1e309", "1e-324", "1e-400", "4.9e-324", "2.5e-324", "1.7976931348623157e308", "1.7976931348623159e308",
  "0.1", "0.30000000000000004", "123456789012345678901234567890", "9007199254740993", "-9007199254740993", "0.000001", "1_000", "1,000", "1 000", "١٢٣", "１２３",
  "Infinity", "-Infinity", "+Infinity", "infinity", "INFINITY", "Infinit", "Infinityx", "inf", "Inf", "-inf", "+inf", "∞", "-∞", "\u2212Infinity", "\u2212" + "1",
  "NaN", "nan", "NAN", "-NaN", "+NaN", "NaNx", "N", "null", "true", "false", "abc", "x", "-", "+", "+-1", "-+1", "--1", "++1", "- 1", "+ 1", "1 -", "1-", "1+", "1 2",
  "0x", "0x0", "0x1", "0x10", "0xff", "0xFF", "0XFF", "0xfF", "0x7fffffff", "0x80000000", "0xffffffff", "0x100000000", "0xFFFFFFFFF", "0x00000001", "0x0000000000000001", "0x000000000000000000001", "-0x1", "+0x1", "0x-1", "0x+1", "0x1.0", "0x1p3", "0xg", "0x 1", "0 x1", "0x1 ",
  "0o", "0o0", "0o7", "0o8", "0o17", "0O17", "0o777", "0o17777777777", "0o20000000000", "0o37777777777", "0o40000000000", "0o77777777777", "0o000000000001", "0o0000000000000000000001", "-0o7", "0o-7", "0o1.5", "0o 7",
  "0b", "0b0", "0b1", "0b101", "0B101", "0b2", "0b1e3",
  "\t1", "1\t", "\n1\n", "\r1", "\u000b1", "\u000c1", "\u00a01", "1\u00a0", "\u20281", "\u20291", "\u00851", "1\u0085", "\u30001", "\u20031\u2003", "\ufeff1", "1\ufeff", "\u200b1", "\u180e1", "\u1680" + "1",
  "1\u0000", "\u00001", "1 ", " 1", "  1  ", "+ ", " -1.5e2 "];
base.forEach(add);
// systematically: signs x forms
for (const sg of ["", "+", "-"]) for (const f of ["0", "1", "10", "1.", ".1", "1.1", "1e1", "1e+1", "1e-1", ".1e1", "1.e1", "Infinity", "infinity", "NaN", "nan", "0x1", "0xA", "0o1", "0b1"]) add(sg + f);
for (let i = 0; i < 40; i++) { add("0x" + "f".repeat(i)); add("0o" + "7".repeat(i)); add("0x" + "0".repeat(i) + "1"); add("0o" + "0".repeat(i) + "1"); add("1" + "0".repeat(i)); add("0." + "0".repeat(i) + "1"); }
for (const n of [255, 256, 2147483647, 2147483648, 4294967295, 4294967296]) { add("0x" + n.toString(16)); add("0o" + n.toString(8)); add(String(n)); }
const hex = s => s.length ? [...s].flatMap(c => { const n = c.codePointAt(0); return n > 0xffff ? [0xd800 + ((n - 0x10000) >> 10), 0xdc00 + ((n - 0x10000) & 0x3ff)] : [n]; }).map(u => u.toString(16).toUpperCase()).join(".") : "-";
const all = [...set];
const tmp = mkdtempSync(join(tmpdir(), "num-"));
writeFileSync(join(tmp, "in.txt"), all.map(hex).join("\n") + "\n");
const out = execFileSync("dotnet", [join(here, "bin/Release/net9.0/runner.dll"), "--num", join(tmp, "in.txt")], { maxBuffer: 1 << 28 }).toString().split("\n").filter(Boolean);
rmSync(tmp, { recursive: true });
if (out.length !== all.length) throw new Error("answer count");
process.stdout.write(all.map((s, i) => `${hex(s)} ${out[i]}`).join("\n") + "\n");
