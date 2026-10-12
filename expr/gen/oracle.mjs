// SPDX-License-Identifier: MIT
//
// Generates testdata/oracle.json.gz: the results of the reference implementation
// (@actions/expressions 0.3.61, MIT, GitHub) on a corpus of expressions. The Go
// tests (oracle_test.go) replay the corpus and compare token by token what
// GitHub's JavaScript library does with what this package does.
//
//   node gen/oracle.mjs /path/to/@actions/expressions/dist [/path/to/actions-expressions-X.tgz] \\
//     | gzip -9 -n > testdata/oracle.json.gz
//
// The optional tarball is the npm package the dist directory came from: its
// sha512 (base64, the "integrity" field of the npm registry) is recorded next
// to the Node version in the output, so the provenance of the oracle is
// checkable.
//
// The corpus is hand-written edge cases plus a seeded pseudo-random walk over
// the grammar and over mutations of it, so the file is reproducible.

import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import path from "node:path";

const dist = path.resolve(process.argv[2] ?? "node_modules/@actions/expressions/dist");
const ex = await import(pathToFileURL(path.join(dist, "index.js")).href);
const { Lexer, Parser, Evaluator, data } = ex;

const pkgJson = JSON.parse(readFileSync(path.join(dist, "..", "package.json"), "utf8"));
const integrity = process.argv[3]
  ? "sha512-" + createHash("sha512").update(readFileSync(process.argv[3])).digest("base64")
  : "";
const reference = {
  package: `${pkgJson.name} ${pkgJson.version}`,
  integrity,
  node: process.version,
  v8: process.versions.v8,
  unicode: process.versions.unicode,
};

// ---- contexts -------------------------------------------------------------

const contexts = {
  github: {
    event_name: "push",
    ref: "refs/heads/main",
    sha: "0123abc",
    actor: "Octocat",
    repository: "octo/Hello-World",
    run_number: 7,
    run_id: "1658821493",
    event: {
      number: 12,
      commits: [
        { id: "1", message: "fix: bug", author: { name: "Ann", email: "a@x" }, added: ["a.txt"] },
        { id: "2", message: "feat: thing", author: { name: "Bob" }, added: [] },
        { id: "3", message: "Chore", added: ["b", "c"] },
      ],
      head_commit: { message: "hello", timestamp: null },
      labels: [{ name: "bug" }, { name: "Docs" }, { name: "help wanted" }],
      pull_request: { draft: false, title: "A title", labels: [{ name: "x" }] },
      nested: [[1, 2], [3], [], [4, 5, 6]],
    },
    "weird-key": 1,
    "a b": "space",
  },
  env: { FOO: "bar", EMPTY: "", NUM: "42", TRUE: "true", JSONTEXT: '{"a":[1,2,3]}', Mixed: "MiXeD", Float: "1.50", Hex: "0x1F" },
  matrix: {
    os: "ubuntu",
    node: [14, 16, 18],
    include: [{ os: "windows", node: 20 }, { os: "mac", experimental: true }],
    v: 1.5,
    flag: true,
    nothing: null,
    ver: "3.10",
    big: 12345678901234567890,
    neg: -0.25,
    tiny: 1e-7,
    zero: 0,
    obj: { k: "v" },
  },
  inputs: { name: "World", count: 3, deploy: false, list: ["a", "b"], empty: {} },
  steps: { "10": { outputs: { r: "ten" } }, "2": { outputs: { r: "two" } }, build: { outputs: { r: "b", Other: "o" }, outcome: "success" } },
  needs: {},
  vars: { A: "1", B: "two" },
};
const ctxNames = Object.keys(contexts);

const extFuncs = [
  { name: "success", description: "", minArgs: 0, maxArgs: 0, call: () => new data.BooleanData(true) },
  { name: "always", description: "", minArgs: 0, maxArgs: 0, call: () => new data.BooleanData(true) },
  {
    name: "hashFiles",
    description: "",
    minArgs: 1,
    maxArgs: 255,
    call: (...a) => new data.StringData("hash:" + a.map(x => x.coerceString()).join("|")),
  },
];
const extInfos = extFuncs.map(f => ({ name: f.name, description: f.description, minArgs: f.minArgs, maxArgs: f.maxArgs }));
const funcMap = new Map(extFuncs.map(f => [f.name.toLowerCase(), f]));

const rootDict = new data.Dictionary(
  ...Object.keys(contexts).map(k => ({ key: k, value: JSON.parse(JSON.stringify(contexts[k]), data.reviver) }))
);

// ---- running the reference -------------------------------------------------

function kindOf(v) {
  switch (v.kind) {
    case data.Kind.Null: return "null";
    case data.Kind.Boolean: return "boolean";
    case data.Kind.Number: return "number";
    case data.Kind.String: return "string";
    case data.Kind.Array: return "array";
    case data.Kind.Dictionary: return "object";
  }
  return "?";
}

function run(expr) {
  const out = { e: expr };
  let tokens;
  try {
    tokens = new Lexer(expr).lex().tokens;
  } catch (e) {
    if (!(e instanceof Error) || e instanceof TypeError || e instanceof RangeError) { out.skip = String(e); return out; }
    out.lexErr = e.message;
    return out;
  }
  out.tokens = tokens.map(t => [t.type, t.lexeme, t.range.start.line, t.range.start.column, t.range.end.line, t.range.end.column,
    t.type === 17 ? (Number.isNaN(t.value) ? "NaN" : String(t.value)) : t.type === 18 ? t.value : null]);
  let ast;
  try {
    ast = new Parser(tokens, ctxNames, extInfos).parse();
  } catch (e) {
    if (e instanceof TypeError || e instanceof RangeError) { out.skip = String(e); return out; }
    out.parseErr = e.message;
    if (e.pos) out.pos = [e.pos.line, e.pos.column];
    return out;
  }
  if (ast === undefined) { out.empty = true; return out; }
  let res;
  try {
    res = new Evaluator(ast, rootDict, funcMap).evaluate();
  } catch (e) {
    if (e instanceof TypeError || e instanceof RangeError) { out.skip = String(e); return out; }
    out.evalErr = e.message;
    return out;
  }
  if (res === undefined) { out.skip = "undefined result"; return out; }
  const str = res.coerceString();
  out.r = { k: kindOf(res), s: str, j: JSON.stringify(res, data.replacer, "  ") };
  if (!str.isWellFormed()) {
    // A string holding a lone surrogate cannot travel through JSON text and
    // UTF-8: give its UTF-16 code units instead.
    out.r.s = "";
    out.r.u = Array.from({ length: str.length }, (_, i) => str.charCodeAt(i));
  }
  return out;
}

// ---- corpus ----------------------------------------------------------------

const corpus = [];
const add = (...xs) => corpus.push(...xs);

// literals, numbers
add("true", "false", "null", "NaN", "Infinity", "-Infinity", "+Infinity", "1", "-1", "+1", "1.5", ".5", "-.5", "5.", "1e3", "1E-3", "1e21", "1e-7",
  "0x10", "0X1f", "0o17", "0b101", "-0x10", "0x", "0b2", "1_000", "1e", "1e+", "--1", "+-1", "-", "+", ".", "..", "1.2.3", "0.1", "0.30000000000000004",
  "100000000000000000000", "1000000000000000000000", "123456789012345678", "-0", "0", "00", "007", "1e308", "1e309", "-1e309", "4.9e-324",
  "0.000001", "0.0000001", "123456789.123456789", "0.1234567890123456789", "9.9999999999999995", "0.9999999999999999", "0.99999999999999999",
  "1.0000000000000002", "2.5", "0.5", "1.005", "1.45", "8.345", "0.000000000000005", "0.0000000000000005", "0.0000000000000004999",
  "1e15", "1e16", "123456789012345.6", "12345678901234567.5");
for (const n of ["0.1", "0.5", "1.5", "2.5", "1e21", "1e-7", "1e300", "-1e-7", "0.000001", "123456789012345678901", "5e-324", "1.7976931348623157e308",
  "0.30000000000000004", "1.005", "0.1234567890123456", "0.12345678901234567", "1234567.1234567890123456", "-0.0000000000000005", "0.0000000000000005",
  "NaN", "Infinity", "-Infinity", "100", "1e20", "123e-20", "99999999999999999999", "0.000000000000001", "0.0000000000000011", "4503599627370497.5",
  "0.45", "0.55", "-1.5", "1.0000000000000005", "1.0000000000000004", "10.000000000000005"]) {
  add(`format('{0}', ${n})`, `toJSON(${n})`, `${n} == '${n}'`, `'${n}' == ${n}`, `join(fromJSON('[${n}]'), ',')`);
}

// strings
add("'a'", "''", "''''", "'it''s'", "'a''b''c'", "'''a'", "'a'''", "'a''''b'", "'unterminated", "'", "'a'b", "'a' 'b'", "'multi\nline'",
  "'\u00e9'", "'\u{1F600}'", "'\u{1F600}' == '\u{1F600}'", "\"dq\"", "'tab\there'");

// whitespace & lexing oddities
add("  true  ", "\ttrue\r\n", "\n\ntrue", "\u00a0true", "true\u00a0", "tr\u00a0ue", "\vtrue", "true\u2028", "true\ufeff", "a", "github", "GITHUB", "GitHub.Event_Name",
  "github .event_name", "github. event_name", "github.\nevent_name", "github\n.event_name", "github.event_name.", "github..event_name", ".github", "github.",
  "github[", "github[]", "github['event_name'", "github['event_name']]", "github[0]", "github[-1]", "github[1.5]", "github['0']", "github[true]", "github[null]",
  "github[nan]", "github[NaN]", "github[Infinity]", "github[github]", "github[github.event]", "github['EVENT_NAME']", "github[ 'event_name' ]",
  "=", "==", "===", "= =", "&", "&&", "&&&", "|", "||", "|||", "!", "!!", "!!!true", "!=", "!==", "!==true", "<", "<=", "<=>", ">", ">=", ">==",
  "1 = 1", "1 & 1", "1 | 1", "1 &&", "&& 1", "1 && && 1", "1 ||", "|| 1", "@", "#", "$", "1 + 1", "1 - 1", "1 * 1", "1 / 1", "a-b", "a_b", "a1", "1a", "_a", "-a", "a-",
  "github-", "github-x", "github.event-name", "github.event_name-x", "github.0", "github.1a", "github.-x", "github.true", "github.null", "github.NaN", "github.Infinity",
  "github.false", "env.TRUE", "env.true", "env.null", "(", ")", "()", "(())", "(1", "1)", "((1)", "(1))", "(1)(2)", "1 2", "1,2", ",", "1,", "[", "]", "[]", "[1]", "{}",
  "*", "*.a", "1.*", "'a'.b", "'a'[0]", "(github).event_name", "(github)['event_name']", "(github.event).number", "(1).a", "(1)[0]", "1[0]", "true.a", "null.a", "null[0]",
  "true[0]", "'abc'[0]", "'abc'.length", "(('a'))[0]", "toJSON(1)[0]", "fromJSON('[1,2]')[1]", "fromJSON('{\"a\":1}').a", "fromJSON('{\"a\":1}')['A']", "fromJSON('[[1]]')[0][0]");

// operators, coercions
const vals = ["null", "true", "false", "0", "1", "-1", "0.5", "NaN", "Infinity", "''", "'0'", "'1'", "' 1 '", "'abc'", "'ABC'", "'a'", "'b'", "'true'", "'null'", "'NaN'", "'Infinity'", "'0x10'", "'1e2'", "'-1'", "'1.0'", "'\u00df'", "'SS'", "'ss'", "'\u0131'", "'I'", "'i'", "'\u0130'", "'\u03c3'", "'\u03c2'", "'\u03a3'", "'\ufb03'", "'\u212a'", "'k'", "'K'", "'\u{1F600}'", "'\ue000'", "'\u{10000}'", "github", "github.event.commits", "github.event.commits[0]", "env", "matrix.node", "matrix.obj", "inputs.empty", "fromJSON('[]')", "fromJSON('{}')", "github.event.commits.*", "github.event.nope.*", "matrix.nothing", "env.NUM", "env.EMPTY"];
for (const a of vals) {
  add(`!${a}`, `!!${a}`, `${a} || 'dflt'`, `${a} && 'then'`, `format('{0}', ${a})`, `toJSON(${a})`, `join(${a})`, `join(${a}, '-')`, `fromJSON(${a})`, `case(${a}, 'x', 'y', 'z')`, `case(${a} == ${a}, 1, 2, 3)`);
  for (const b of vals) {
    for (const op of ["==", "!=", "<", "<=", ">", ">="]) add(`${a} ${op} ${b}`);
    add(`contains(${a}, ${b})`, `startsWith(${a}, ${b})`, `endsWith(${a}, ${b})`);
  }
}

// precedence and chains
add("true || false && false", "(true || false) && false", "false && true || true", "1 == 1 == true", "1 < 2 == true", "true == 1 < 2", "!1 == 2", "!(1 == 2)", "!1 < 2", "1 < !2", "!true == false",
  "1 || 2 || 3", "0 || '' || null", "0 || '' || 'x'", "1 && 2 && 3", "1 && 0 && 3", "null && 1", "null || null", "'a' && 'b' || 'c'", "'' && 'b' || 'c'", "a && b", "true && (false || true)",
  "1 == 1 && 2 == 2 || 3 == 4", "1 != 1 || 2 != 2", "github.event_name == 'push' && contains(github.ref, 'main')", "github.event_name != 'push' || !contains(github.ref, 'dev')",
  "contains(github.event.commits.*.message, 'fix: bug')", "contains(github.event.commits.*.message, 'FIX: BUG')", "contains(toJSON(github.event.commits), 'Bob')",
  "github.event.commits[0].author.name", "github.event.commits[1].author.email", "github.event.commits[2].author.name", "github.event.commits[3]", "github.event.commits[-1]", "github.event.commits['1'].id",
  "github.event.commits[1.9].id", "github.event.commits['1.9'].id", "github.event.commits[' 1 '].id", "github.event.commits[true]", "github.event.commits[false].id", "github.event.commits[null].id",
  "github.event.commits[1e0].id", "github.event.commits[0x1].id", "github.event.commits[Infinity]", "github.event.commits[-0].id", "github.event.commits[1e400]", "github.event.commits[1e20]",
  "github.event.commits.*", "github.event.commits.*.id", "github.event.commits.*.author", "github.event.commits.*.author.name", "github.event.commits.*.author.email", "github.event.commits.*.added",
  "github.event.commits.*.added.*", "github.event.commits.*.added[0]", "github.event.commits.*.added[1]", "github.event.commits.*.*", "github.event.commits[*].id", "github.event.commits[*]",
  "github.event.commits[*][*]", "github.event.commits.*[0]", "github.event.commits.*['id']", "github.event.commits.*.author.*", "github.*", "github.event.*", "github.*.number", "github.*.*", "github.event.*.*",
  "github.event.nested.*", "github.event.nested.*.*", "github.event.nested[*][0]", "github.event.nested.*[1]", "github.event.nested.*.*.*", "github.event.nested[3][1]", "github.event.nested[2][0]",
  "github.event.labels.*.name", "join(github.event.labels.*.name, ', ')", "join(github.event.labels.*.name)", "toJSON(github.event.labels.*.name)", "toJSON(github.event.labels.*)",
  "github.event.head_commit.*", "github.event.head_commit.message", "github.event.head_commit.timestamp", "github.event.head_commit.timestamp.x", "github.event.head_commit.timestamp.*",
  "github.event.pull_request.draft", "github.event.pull_request.draft == false", "!github.event.pull_request.draft", "github.event.pull_request.labels.*.name",
  "github['weird-key']", "github.weird-key", "github['a b']", "github.a b", "github.EVENT.NUMBER", "github.Event['Number']", "github.event.number == 12", "github.event.number == '12'", "github.event.number > 5",
  "github.event.number > '5'", "github.event.number > 'x'", "github.run_number", "github.run_id", "github.run_id == 1658821493", "github.run_id > 1658821492",
  "env.FOO", "env.foo", "env.Mixed == 'mixed'", "env.NUM == 42", "env.NUM > 41", "env.NUM + 1", "env.Float == 1.5", "env.Hex == 31", "env.TRUE == true", "env.EMPTY == null", "env.EMPTY == false", "env.EMPTY",
  "fromJSON(env.JSONTEXT).a[1]", "fromJSON(env.JSONTEXT).a.*", "toJSON(fromJSON(env.JSONTEXT))",
  "matrix.os", "matrix.node", "matrix.node[0]", "matrix.node.*", "matrix.include.*.os", "matrix.include[0].node", "matrix.include[1].experimental", "matrix.v", "matrix.flag", "matrix.nothing", "matrix.neg", "matrix.tiny",
  "matrix.big", "matrix.big == 12345678901234567890", "format('{0}', matrix.big)", "format('{0} {1}', matrix.tiny, matrix.neg)", "matrix.ver", "matrix.ver == 3.1", "matrix.ver == '3.10'", "matrix.zero || 'z'",
  "matrix.obj", "matrix.obj == matrix.obj", "matrix.obj != matrix.obj", "matrix.node == matrix.node", "github.event.commits == github.event.commits", "github.event.commits.* == github.event.commits.*",
  "matrix.obj.k", "matrix.obj['k']", "matrix.obj[0]", "matrix.obj.*", "matrix.node['0']", "matrix.node[0] == 14", "matrix.node[0] < matrix.node[1]", "contains(matrix.node, 16)", "contains(matrix.node, '16')", "contains(matrix.node, 17)",
  "contains(matrix.include.*.os, 'MAC')", "inputs.name", "inputs.count", "inputs.count == 3", "inputs.deploy", "inputs.deploy == false", "!inputs.deploy", "inputs.list", "inputs.list.*", "inputs.empty", "inputs.empty.*", "inputs.empty.a",
  "inputs.missing", "inputs.missing.deeper", "inputs.missing[0]", "inputs.missing.*", "inputs.missing.*.x", "inputs['missing']['x']", "steps.build.outputs.r", "steps.BUILD.OUTPUTS.OTHER", "steps.build.outcome == 'success'", "steps.build.outputs.*",
  "steps.10.outputs.r", "steps['10'].outputs.r", "steps[10].outputs.r", "steps[2].outputs.r", "steps.*", "toJSON(steps)", "toJSON(steps.*.outputs)", "steps.*.outputs.r", "needs", "needs.x", "needs.x.result", "needs.*", "toJSON(needs)", "toJSON(needs.*)",
  "vars.A", "vars.B", "vars.A == 1", "vars.C || 'default'", "toJSON(vars)", "toJSON(github.event.commits[0])", "toJSON(github.event)", "toJSON(contains)", "toJSON(github)", "toJSON(matrix)", "toJSON(inputs)", "toJSON(env)",
  "toJSON(github.event.commits.*.added)", "toJSON(github.event.nested)", "toJSON(github.event.nested.*)", "toJSON(github.event.nested.*.*)", "toJSON(matrix.big)", "toJSON(matrix.tiny)", "toJSON(matrix.neg)", "toJSON(matrix.nothing)", "toJSON(inputs.empty)", "toJSON(inputs.list)");

// functions: arity and behaviours
add("contains()", "contains(1)", "contains(1,2)", "contains(1,2,3)", "contains('a','b',)", "contains(,)", "contains('abc','B')", "contains('abc','')", "contains('', '')", "contains('', 'a')", "contains(null, '')", "contains(null, null)", "contains(1234, 23)", "contains(true, 'RU')", "contains(github, 'a')",
  "contains('a', github)", "contains(github.event.commits, 'x')", "contains('\u0130', 'i')", "contains('I', '\u0131')", "contains('\u00df', 'ss')", "contains('SS', '\u00df')", "contains('\u03a3', '\u03c3')", "contains('\u03c2', '\u03c3')", "contains('A\u03a3', 'a\u03c2')", "contains('\u212a', 'k')",
  "contains(fromJSON('[1,\"2\",null]'), null)", "contains(fromJSON('[1,\"2\",null]'), '')", "contains(fromJSON('[1,\"2\",null]'), 2)", "contains(fromJSON('[]'), null)", "contains(fromJSON('[[1]]'), fromJSON('[1]'))", "contains(github.event.commits.*.id, 2)", "contains(github.event.commits.*.id, '2')",
  "startsWith()", "startsWith('a')", "startsWith('abc','AB')", "startsWith('abc','')", "startsWith('', '')", "startsWith(null, '')", "startsWith(123, 12)", "startsWith(github, 'a')", "startsWith('\u0131', 'I')", "startsWith('\u0131', '\u0131')", "startsWith('i', '\u0131')", "startsWith('\u00df', 'ss')", "startsWith('SS', '\u00df')",
  "endsWith('abc','BC')", "endsWith('abc','')", "endsWith(null, 'a')", "endsWith(1.50, '5')", "endsWith(1.50, 5)", "endsWith('\u00df', 'S')", "endsWith('abc', 'abcd')",
  "format()", "format('a')", "format('')", "format(null)", "format(1)", "format(true)", "format('{0}')", "format('{0}', 1)", "format('{0}', 1, 2)", "format('{1}', 1)", "format('{1}', 1, 2)", "format('{0}{0}', 'x')", "format('{0}{1}{0}', 'a', 'b')", "format('{{')", "format('}}')", "format('{{}}')", "format('{{0}}', 1)",
  "format('{{{0}}}', 1)", "format('{', 1)", "format('}', 1)", "format('{}', 1)", "format('{ 0}', 1)", "format('{0 }', 1)", "format('{0:x}', 1)", "format('{0,5}', 1)", "format('{0 anything}', 1)", "format('{00}', 1)", "format('{01}', 1, 2)", "format('{-1}', 1)", "format('{a}', 1)", "format('{0', 1)", "format('0}', 1)", "format('{{0}', 1)", "format('{0}}', 1)",
  "format('{0}}}', 1)", "format('a{0}b{1}c', 1, 2)", "format('{0}', null)", "format('{0}', true)", "format('{0}', github)", "format('{0}', github.event.commits)", "format('{0}', matrix.node.*)", "format('{12345678901234567890123}', 1)", "format('{99999999999999999999999999999999999}', 1)", "format('{0}\n{0}', 'a')", "format('x}', 1)", "format('}{', 1)", "format('{{{{', 1)", "format('{}}', 1)", "format('{0}{{', 1)", "format('{{{0}', 1)",
  "format('{0}', 'a', 'b', 'c')", "format('{3}', 0, 1, 2, 3)", "format('{2}', 0, 1, 2, 3)", "format('{255}', 1)", "format('\u{1F600}{0}\u{1F600}', 'x')", "format('{0}', '\u{1F600}')", "format('{ {0}', 1)", "format('  {0}  ', 1)",
  "fromJSON()", "fromJSON('1','2')", "fromJSON('')", "fromJSON('  ')", "fromJSON('\n')", "fromJSON(null)", "fromJSON('null')", "fromJSON('true')", "fromJSON('false')", "fromJSON('1')", "fromJSON('-1.5e2')", "fromJSON('\"s\"')", "fromJSON('[1,2,3]')", "fromJSON('{\"a\":1}')", "fromJSON('{\"a\":1,\"A\":2}')", "fromJSON('{\"a\":1,\"a\":2}')", "toJSON(fromJSON('{\"a\":1,\"A\":2}'))", "toJSON(fromJSON('{\"a\":1,\"a\":2,\"b\":3}'))",
  "toJSON(fromJSON('{\"b\":1,\"2\":2,\"1\":3,\"a\":4,\"01\":5,\"4294967294\":6,\"4294967295\":7,\"-1\":8}'))", "toJSON(fromJSON('{\"\":1}'))", "toJSON(fromJSON('{\"a\":{\"b\":{\"c\":[]}}}'))", "fromJSON('{')", "fromJSON('{\"a\"')", "fromJSON('[1,]')", "fromJSON('[1 2]')", "fromJSON('{a:1}')", "fromJSON(\"{'a':1}\")", "fromJSON('01')", "fromJSON('1.')", "fromJSON('.5')", "fromJSON('+1')", "fromJSON('NaN')", "fromJSON('Infinity')", "fromJSON('undefined')", "fromJSON('nul')", "fromJSON('truee')",
  "fromJSON('1 2')", "fromJSON(' 1 ')", "fromJSON('\"\\u0041\"')", "fromJSON('\"\\ud83d\\ude00\"')", "fromJSON('\"\\x\"')", "fromJSON('\"a\nb\"')", "fromJSON('\"\\/\"')", "fromJSON('1e999')", "fromJSON('-1e999')", "fromJSON('-0')", "toJSON(fromJSON('-0'))", "toJSON(fromJSON('1e999'))", "toJSON(fromJSON('1E2'))", "toJSON(fromJSON('0.1e1'))", "toJSON(fromJSON('123456789012345678901234567890'))",
  "toJSON(fromJSON('[1e21,1e-7,0.000001,100,1.5,-2.5e-10]'))", "toJSON(fromJSON('\"\\u0000\\u001f\\u007f\\u2028\\\"\\\\\"'))", "toJSON('\u0000')", "toJSON('\u001f\u007f\u0080\u2028\u2029\ufeff')", "toJSON('\"\\')", "toJSON('a\nb\tc\rd\be\ff')", "toJSON('\u00e9\u00df\u{1F600}')", "toJSON(fromJSON('[]'))", "toJSON(fromJSON('{}'))", "toJSON(fromJSON('[[],{}]'))", "toJSON(fromJSON('{\"a\":[],\"b\":{}}'))", "toJSON(fromJSON('[null]'))",
  "toJSON()", "toJSON(1,2)", "toJSON(null)", "toJSON(true)", "toJSON(1)", "toJSON(NaN)", "toJSON(Infinity)", "toJSON(-Infinity)", "toJSON(-0)", "toJSON('x')", "toJSON('')", "toJSON(1e21)", "toJSON(1e-7)", "toJSON(0.1)", "toJSON(123456789012345680000)",
  "join()", "join('a')", "join('a','b')", "join(null)", "join(1.50)", "join(true, 'x')", "join(github)", "join(github, 'x')", "join(fromJSON('[]'))", "join(fromJSON('[1,2,3]'))", "join(fromJSON('[1,2,3]'), '')", "join(fromJSON('[1,2,3]'), ', ')", "join(fromJSON('[1,2,3]'), 1)", "join(fromJSON('[1,2,3]'), null)", "join(fromJSON('[1,2,3]'), true)", "join(fromJSON('[1,2,3]'), github)", "join(fromJSON('[1,2,3]'), fromJSON('[]'))", "join(fromJSON('[1,2,3]'), NaN)",
  "join(fromJSON('[null,true,\"a\",1.5,[1],{}]'))", "join(matrix.node, '.')", "join(matrix.node.*, '.')", "join(github.event.commits.*.id, '/')", "join(1,2,3)", "join(fromJSON('[\"a\",\"b\"]'), '\u{1F600}')",
  "case()", "case(true)", "case(true, 1)", "case(true, 1, 2)", "case(false, 1, 2)", "case(true, 1, false, 2, 3)", "case(false, 1, true, 2, 3)", "case(false, 1, false, 2, 3)", "case(false, 1, false, 2, 3, 4)", "case(false, 1, false, 2, 3, 4, 5)", "case(1, 2, 3)", "case('true', 2, 3)", "case(null, 2, 3)", "case(false, 2, 3, 4)", "case(true, 2, 1, 3)", "case(true, 2, null, 3)", "case(false, 1, 'x', 3)",
  "case(true, github.event_name, 'other')", "case(github.event_name == 'push', 'a', github.event_name == 'pr', 'b', 'c')", "CASE(true, 1, 2)", "case(true, matrix.node, 2)", "toJSON(case(true, matrix.node.*, 2))", "case(1 == 1, 'a', 'b')", "case(!0, 'a', 'b')", "case(fromJSON('true'), 'a', 'b')",
  "CONTAINS('a','A')", "Contains('a','A')", "STARTSWITH('a','A')", "endswith('a','A')", "FORMAT('{0}',1)", "fromjson('1')", "FROMJSON('1')", "tojson(1)", "TOJSON(1)", "JOIN(fromJSON('[1]'))", "unknown()", "unknown(1)", "Constantine()", "notafunction", "success", "success()", "success(1)", "always()", "ALWAYS()", "hashFiles()", "hashFiles('a')", "hashFiles('a','b')", "hashFiles('**/*.go', '!x')", "HASHFILES('a')", "failure()", "cancelled()",
  "success() && hashFiles('a') != ''", "contains(github.ref, 'main') && success()", "always() || false", "hashFiles(github.sha, null, 1.5)", "hashFiles(1,2,3,4,5,6,7,8,9,10)", "format()", "format(1,2,3,4)");
add("hashFiles(" + Array.from({ length: 255 }, (_, i) => `'${i}'`).join(",") + ")", "hashFiles(" + Array.from({ length: 256 }, (_, i) => `'${i}'`).join(",") + ")");
add("format('{0}'" + ", 1".repeat(254) + ")", "format('{0}'" + ", 1".repeat(255) + ")", "case(" + Array.from({ length: 101 }, (_, i) => i < 100 ? (i % 2 == 0 ? "false" : "1") : "'end'").join(",") + ")");
add("contains('a', 'b',)", "contains(1,)", "contains(,1)", "contains('a' 'b')", "contains('a';'b')", "contains(('a'), ('b'))", "contains(contains('a','a'), true)", "format(format('{{0}}'), 5)", "toJSON(toJSON(1))", "fromJSON(toJSON(github.event.commits[0])).author.name", "fromJSON(fromJSON('\"[1]\"'))[0]", "fromJSON('[1,2]')[fromJSON('1')]",
  "join(fromJSON(toJSON(github.event.labels.*.name)), '+')", "toJSON(fromJSON(toJSON(github.event.commits)))", "format('{0}', toJSON(null))", "fromJSON(format('[{0},{1}]', 1, 2))[1]", "fromJSON('[{\"a\":1},{\"a\":2},{\"b\":3}]').*.a", "toJSON(fromJSON('[{\"a\":1},{\"a\":2},{\"b\":3}]').*.a)", "toJSON(fromJSON('[{\"a\":[1,2]},{\"a\":[3]}]').*.a.*)", "toJSON(fromJSON('[{\"a\":[1,2]},{\"a\":[3]}]').*.a[0])", "toJSON(fromJSON('[[1,2],[3]]').*[0])", "toJSON(fromJSON('[[1,2],[3]]')[*][1])",
  "toJSON(fromJSON('{\"a\":{\"x\":1},\"b\":{\"x\":2}}').*.x)", "toJSON(fromJSON('{\"a\":{\"x\":1},\"b\":{\"x\":2}}')[*].x)", "toJSON(fromJSON('{\"a\":{\"x\":1},\"b\":2}').*.x)", "toJSON(fromJSON('[1,2]').*.x)", "toJSON(fromJSON('1').*)", "toJSON(null.*)", "toJSON('abc'.*)", "toJSON(fromJSON('[]').*.a)", "toJSON(fromJSON('[]').*)", "toJSON(fromJSON('{}').*)");

// lone surrogates (kept as WTF-8 by the Go port)
{
  const H = "fromJSON('\"\\ud800\"')", L = "fromJSON('\"\\udc00\"')", H2 = "fromJSON('\"\\ud83d\"')", L2 = "fromJSON('\"\\ude00\"')";
  const lone = [H, L, H2, L2, "'\u{1F600}'", "'\ue000'", "'\uffff'", "'a'", "''", "'\ufffd'", "'\u{10000}'"];
  for (const a of lone) {
    add(`toJSON(${a})`, `format('{0}', ${a})`, `join(${a})`, `${a} || 'd'`, `startsWith(${a}, ${a})`, `case(true, ${a}, 1)`);
    for (const b of lone) {
      for (const op of ["==", "!=", "<", "<=", ">", ">="]) add(`${a} ${op} ${b}`);
      add(`contains(${a}, ${b})`, `startsWith(${a}, ${b})`, `endsWith(${a}, ${b})`, `format('{0}{1}', ${a}, ${b})`, `join(fromJSON('[]'), ${a}) == ''`,
        `toJSON(fromJSON(toJSON(${a})))`, `format('{0}', ${a}) == format('{0}', ${b})`);
    }
  }
  add(`${H} == ${L}`, `${H} != ${L}`, `${H} < ${L}`, `${H} > ${L}`, `contains('x${"\u{1F600}"}', ${H2})`, `contains('x\u{1F600}', ${L2})`, `startsWith('\u{1F600}', ${H2})`, `endsWith('\u{1F600}', ${L2})`,
    `format('{0}{1}', ${H2}, ${L2}) == '\u{1F600}'`, `format('{0}{1}', ${L2}, ${H2}) == '\u{1F600}'`, `join(fromJSON('["\\ud83d","\\ude00"]'), '') == '\u{1F600}'`,
    `join(fromJSON('["\\ud83d","\\ude00"]'), '')`, `format('{0}{1}', ${H2}, ${L2})`, `format('{0}{1}{0}', ${H2}, ${L2})`, `toJSON(format('{0}{1}', ${H2}, ${L2}))`,
    `toJSON(fromJSON('{"\\ud800":1,"\\ud800x":2,"a\\udc00":3}'))`, `fromJSON('{"\\ud800":1}')[${H}]`, `fromJSON('{"\\ud800":1}')[${L}]`, `toJSON(fromJSON('["\\ud800\\ud800","\\udc00\\ud800"]'))`,
    `${H} == '\ufffd'`, `'\ufffd' == ${H}`, `'\ufffd' < ${H}`, `${H} < '\ufffd'`, `${H} < '\u{1F600}'`, `'\u{1F600}' < ${H}`, `${L} < '\ue000'`, `'\ue000' < ${L}`,
    `fromJSON('"\\ud800\\u0041"') == 'A'`, `contains(fromJSON('"A\\ud800"'), 'a')`, `startsWith(fromJSON('"a\\ud800"'), 'A')`, `endsWith(fromJSON('"\\ud800a"'), 'A')`,
    `contains(fromJSON('"\\ud800\\udc00"'), ${H})`, `contains(fromJSON('"\\ud800\\udc00"'), ${L})`, `startsWith(fromJSON('"\\ud800\\udc00"'), ${H})`, `endsWith(fromJSON('"\\ud800\\udc00"'), ${L})`,
    `contains(fromJSON('["\\ud800"]'), ${H})`, `contains(fromJSON('["\\ud800"]'), ${L})`, `fromJSON('"\\ud800"') == fromJSON('"\\ud800"')`);
}

// depth and length limits
const nest = (n, open, body, close) => open.repeat(n) + body + close.repeat(n);
for (const n of [1, 10, 24, 25, 26, 48, 49, 50, 51, 52, 100, 200]) {
  add(nest(n, "(", "1", ")"), "!".repeat(n) + "true", nest(n, "format(", "'x'", ")"), "github" + ".event".repeat(n), "github" + "[0]".repeat(n), "github" + "['event']".repeat(n),
    "1" + " || 1".repeat(n), "1" + " && 1".repeat(n), "1" + " == 1".repeat(n), nest(n, "(1 || ", "1", ")"), nest(n, "!(", "1", ")"), nest(n, "(1 && ", "1", ")"), nest(n, "github[", "0", "]"), nest(n, "contains(1, ", "1", ")"));
}
for (const n of [20990, 21000, 21001, 22000]) {
  add("'" + "a".repeat(n - 2) + "'", " ".repeat(n) + "1", "1" + " ".repeat(n - 1), "'" + "\u{1F600}".repeat(Math.floor((n - 2) / 2)) + "'", "'" + "\u{1F600}".repeat(Math.floor((n - 3) / 2)) + "'");
}

// positions and error messages
for (const e of ["1 +", "a b", "github github", "github.event_name ==", "== 1", "(github.event_name", "github.event_name)", "contains('a',", "contains('a'", "contains('a' 'b')", "contains(", "github[0", "github[0)",
  "1 ==\n 2 ===\n 3", "true &&\n  false ||\n  nope", "\n\n  bogus", "'a'\n 'b'\n  zzz", "true\n&\nfalse", "foo(", "foo", "github.event_name\n  .\n  ", "  )  ", "1 !", "1 !!", "1 ! 1", "!", "! ", "!(", "!)", "(!)", "[1]", "github.[0]", "github.*.", "github.*.*.", "github[*", "github[*]]", "github[* ]", "github[1 2]", "github[,]",
  "1 && ( 2 ||", "1\t2", "x\ty", "github\r\n.event", "a\u00a0b", "'x' =", "'x' = 'y'", "!= 1", ">", "<<", ">>", "1 <> 2", "1 >< 2", "1 =< 2", "1 => 2", "contains(github.event_name, 'push'", "contains(github.event_name 'push')", "$ {{ 1 }}", "${{ 1 }}", "${{", "}}", "{{ 1 }}", "{ 1 }", "1 }", "a.b.c", "github.a.b.c.d", "git hub", "github github.ref"]) add(e);

// ---- seeded random ----------------------------------------------------------

function mulberry32(a) {
  return function () {
    a |= 0; a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const rnd = mulberry32(20260512);
const pick = a => a[Math.floor(rnd() * a.length)];
const lits = ["true", "false", "null", "0", "1", "2", "42", "-1", "1.5", "0.1", "NaN", "Infinity", "''", "'a'", "'A'", "'abc'", "'1'", "'0'", "'true'", "'x y'", "'it''s'", "'{0}'", "'[1,2]'", "'{\"a\":1}'", "'\u00df'", "'\u0131'", "'\u03a3'", "'\u{1F600}'"];
const paths = ["github", "github.event_name", "github.event.commits", "github.event.commits.*", "github.event.commits.*.id", "github.event.commits[0]", "github.event.commits[1].author", "github.event.commits[1].author.name", "github.event.labels.*.name", "github.event.nested.*.*", "github.event.nested[3]", "github.ref", "github.nope", "github.event.nope.deep",
  "env", "env.FOO", "env.NUM", "env.EMPTY", "matrix", "matrix.node", "matrix.node.*", "matrix.node[1]", "matrix.v", "matrix.flag", "matrix.nothing", "matrix.include.*.os", "matrix.obj", "inputs", "inputs.count", "inputs.deploy", "inputs.list.*", "inputs.empty", "steps.build.outputs.r", "steps.*.outputs", "steps['10']", "needs", "needs.*", "vars.A", "vars.B"];
function gen(d) {
  const r = rnd();
  if (d <= 0 || r < 0.3) return rnd() < 0.5 ? pick(lits) : pick(paths);
  if (r < 0.4) return pick(paths) + (rnd() < 0.5 ? `[${gen(d - 1)}]` : rnd() < 0.5 ? ".*" : "[*]");
  if (r < 0.5) return `!${gen(d - 1)}`;
  if (r < 0.58) return `(${gen(d - 1)})`;
  if (r < 0.7) return `${gen(d - 1)} ${pick(["==", "!=", "<", "<=", ">", ">="])} ${gen(d - 1)}`;
  if (r < 0.78) return `${gen(d - 1)} ${pick(["&&", "||"])} ${gen(d - 1)}`;
  const fn = pick([["contains", 2], ["startsWith", 2], ["endsWith", 2], ["format", 3], ["fromJSON", 1], ["toJSON", 1], ["join", 2], ["case", 3], ["hashFiles", 2], ["success", 0]]);
  const n = fn[0] === "format" ? 1 + Math.floor(rnd() * 3) : fn[0] === "case" ? pick([1, 3, 5]) : fn[0] === "join" ? 1 + Math.floor(rnd() * 2) : fn[1];
  const args = Array.from({ length: n }, () => gen(d - 1));
  if (fn[0] === "format" && rnd() < 0.7) args[0] = pick(["'{0}'", "'{0}{1}'", "'a{1}b{0}c'", "'{{{0}}}'", "'{0:x}'", "'{2}'"]);
  if (fn[0] === "case" && rnd() < 0.7) for (let i = 0; i + 1 < args.length; i += 2) args[i] = pick(["true", "false", "1 == 1", "!0", "github.event_name == 'push'"]);
  if (fn[0] === "fromJSON" && rnd() < 0.5) args[0] = pick(["'[1,2]'", "'{\"a\":{\"b\":1}}'", "'null'", "'\"x\"'", "'1e3'", "'{'", "''", "'[{\"a\":1},{\"a\":2}]'", "toJSON(" + gen(d - 1) + ")"]);
  return `${fn[0]}(${args.join(", ")})`;
}
const alphabet = ["(", ")", "[", "]", ".", ",", "!", "=", "&", "|", "<", ">", "*", "'", " ", "\n", "-", "+", "0", "1", "a", "github", "true", "null", "\u00e9", "\u{1F600}", "''", "&&", "||", "==", "!=", ">=", "<="];
function mutate(s) {
  const chars = Array.from(s);
  const k = 1 + Math.floor(rnd() * 3);
  for (let i = 0; i < k; i++) {
    const at = Math.floor(rnd() * (chars.length + 1));
    switch (Math.floor(rnd() * 3)) {
      case 0: chars.splice(at, 0, pick(alphabet)); break;
      case 1: chars.splice(at, 1); break;
      default: chars.splice(at, 1, pick(alphabet));
    }
  }
  return chars.join("");
}
for (let i = 0; i < 2500; i++) add(gen(1 + Math.floor(rnd() * 4)));
for (let i = 0; i < 1500; i++) add(mutate(gen(1 + Math.floor(rnd() * 3))));
// random doubles as literals
for (let i = 0; i < 400; i++) {
  const digits = Array.from({ length: 1 + Math.floor(rnd() * 20) }, () => Math.floor(rnd() * 10)).join("");
  const dot = Math.floor(rnd() * (digits.length + 1));
  let lit = digits.slice(0, dot) + (dot < digits.length || rnd() < 0.3 ? "." : "") + digits.slice(dot);
  if (lit === "." || lit === "") lit = "0.5";
  if (rnd() < 0.3) lit += "e" + (rnd() < 0.5 ? "-" : "") + Math.floor(rnd() * 30);
  add(`format('{0}', ${lit})`, `toJSON(${lit})`);
}

const seen = new Set();
const cases = [];
for (const e of corpus) {
  if (seen.has(e)) continue;
  seen.add(e);
  cases.push(run(e));
}
const skipped = cases.filter(c => c.skip).length;
console.error(`cases=${cases.length} skipped(JS crash)=${skipped}`);
process.stdout.write(JSON.stringify({ reference, contexts, cases: cases.filter(c => !c.skip) }) + "\n");
