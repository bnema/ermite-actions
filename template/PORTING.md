# Porting notes for package template

The package is a port of the template layer of `@actions/workflow-parser`
0.3.61 (MIT, GitHub, actions/languageservices): `templates/` (tokens, reader,
schema) and `workflows/yaml-object-reader`. The schema
`schema/workflow-v1.0.min.json` is copied unmodified. The reference's own
output is the specification: `../workflow/testdata/convert.json` holds what it
answers for 153 workflow files and `workflow.TestConvertAgainstReference`
replays them (messages, line and column included).

The C# runner (`ObjectTemplating`) is what runs on github.com. It was not
available. Nothing here has been checked against it.

## Reference versions and regeneration

The answers in `../workflow/testdata/convert.json` and `testdata/scalars.json`
come from the versions pinned in `../gen/package.json` and
`../gen/package-lock.json`: `@actions/workflow-parser` 0.3.61,
`@actions/expressions` 0.3.61, `yaml` 2.8.3, `cronstrue` 2.61.0 (only imported
by the parser's cron description helper, which is not used here). Run
`../gen/regen.sh`: it does an `npm ci` and rewrites both files; `git diff`
must show nothing. The loader hook `../gen/register.mjs` adds the
`with { type: "json" }` import attribute that Node 22+ requires and the
parser omits, so the packages run unmodified. The last check, on a clean
`npm ci`, gave identical files.

## Differences from the reference

- **YAML parser.** The reference uses the JavaScript `yaml` library (YAML 1.2,
  core schema); this port uses `go.yaml.in/yaml/v3` (a YAML 1.1/1.2 hybrid) and
  re-resolves plain scalars itself (`yaml.go`, `resolveScalar`) with the 1.2
  core schema: `yes`/`no`/`on`/`off` are strings, `0o17` and `0x1F` are
  numbers, `.inf` and `.nan` are numbers, `0b11`, `1_000` and sexagesimal
  numbers are strings. Quoted, block and `!!str` scalars are strings.
  `TestPlainScalarsAgainstReference` checks 64 plain scalars (octal `017`,
  `00`, `1.`, `.inf`, `-.nan`, `0b11`, `1_000`, dates...) against the
  JavaScript library's answers (`testdata/scalars.json`). Not checked: tags
  other than `!!str`, `!!binary`, merge keys `<<` (kept as an ordinary key).
- **YAML syntax errors** have yaml.v3's wording and only a line number, and are
  reported as `file.yml: message at line N`. The reference's are
  `file.yml: <message> at line N, column M:\n\n<excerpt>`. Only the shape
  (one error, prefixed by the file name) is tested.
- **Multiple documents** are an error, with the reference's message text.
- **Anchors and aliases** are expanded like the reference does. Added: a limit
  of `MaxNodes` (200 000) nodes after expansion, because the reference lets a
  few kilobytes of nested aliases allocate gigabytes; a circular alias is an
  error (the reference silently drops it).
- **Positions of expressions in multi-line scalars.** The reference computes a
  precise range for the expression inside a multi-line string; this port
  reports the position of the scalar, which is what the reference's error
  messages use too (checked on `multiline_*`, `expr_multi`).
  `BasicExpression.Source` is the value of the scalar, not the raw source text
  (so a block scalar's indentation is not in it).
- **File size.** A document over `MaxFileBytes` (1 MiB) is refused before any
  parsing, with `file.yml: The file is too large: N bytes, the limit is M
  bytes` (our wording; the reference has no limit, GitHub has its own).
  TODO(verify with recording).
- **Error limit.** `Context` keeps at most `MaxErrors` (1 000) errors.
- **Limits on evaluation.** `MaxEvalDepth` (100) nested tokens; the result
  budget of package expr applies per expression.
- **No `featureFlags`.** Background steps (`background`, `wait`, `wait-all`,
  `cancel`, `parallel`) are always disabled, which is the reference's default.
- **Duplicate keys** are compared case-insensitively with ASCII/Unicode upper
  casing (`strings.ToUpper`), the reference uses `toUpperCase`.
- Mapping keys that are collections (`? [a] : b`) are reported as
  `Unexpected value ''`; the reference reads `undefined` there.

## Runner mode (package action)

`Context` has four options that only package action sets, to read an
`action.yml` as the C# runner's loader does (`action/PORTING.md` lists what is
checked against the runner's own code): `MaxDepth` (`TemplateMemory`'s
`maxDepth`, counting skipped values too), `ErrorLimit` (the runner keeps ten),
`RunnerYAML` (anchors and aliases, tags other than `!!bool/!!float/!!int/!!null`,
32-bit limits of `0x`/`0o` integers, a second document, an empty file and a
collection used as a key all end the read with the runner's message; a number
read as a string is shown as .NET's `G15`, `DotNetNumberString`). Without them
nothing changes. `Error.Exception` marks an error that is a .NET exception the
runner reports without a position.

## To check against the C# runner / recordings

- [ ] A string with `${{` inside a non-expression place (`name:` of a
      reusable workflow...) and the `Exactly 0 parameter(s)...` wording.
- [ ] Which contexts and functions each key may use: taken from the schema,
      which is the language service's, not necessarily the server's.
- [ ] `EvaluateBool`/`EvaluateNumber` accept the strings `"true"`/`"12"`. The
      C# `TemplateEvaluator` converts scalars the same way (assumed).
- [ ] A mapping key expression that produces a non-scalar.
