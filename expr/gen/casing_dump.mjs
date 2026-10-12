// SPDX-License-Identifier: MIT
//
// Dumps the JavaScript toUpperCase/toLowerCase mapping of every code point
// whose mapping is not the identity, as JSON on stdout. It is the input of
// casing.go, which turns it into ../casing_tables.go:
//
//   go generate ./expr      (runs: node gen/casing_dump.mjs | go run gen/casing.go -o casing_tables.go)
//
// The mapping is that of the JavaScript engine running the script (Node/V8),
// which is what @actions/expressions uses for case-insensitive comparisons.
const upper = {};
const lower = {};
for (let cp = 0; cp <= 0x10ffff; cp++) {
  if (cp >= 0xd800 && cp <= 0xdfff) continue;
  const s = String.fromCodePoint(cp);
  const u = s.toUpperCase();
  const l = s.toLowerCase();
  if (u !== s) upper[cp] = u;
  if (l !== s) lower[cp] = l;
}
process.stdout.write(JSON.stringify({ upper, lower }));
