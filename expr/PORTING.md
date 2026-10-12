# Porting notes for package expr

The package is a port of `@actions/expressions` 0.3.61 (MIT, GitHub,
actions/languageservices). The JavaScript is the specification wherever this
package could be checked against it: `testdata/oracle.json.gz` holds what the
reference answers for 33 575 expressions (tokens with positions, error
messages, result kinds, string coercions, `toJSON` output), and
`TestOracle` replays it. `gen/oracle.mjs` regenerates it, `gen/golden.mjs` the
`testdata/tojson` goldens, and `gen/casing_dump.mjs` + `gen/casing.go` the
casing tables (`go generate ./expr`; Node 25 / V8 14.1, Unicode 16). The
tables are relative to the Go toolchain's own Unicode tables
(`unicode.Version`); `TestCasingTablesMatchGoUnicodeVersion` fails with the
regeneration command when the toolchain changes. The oracle records the
npm integrity (sha512) of the reference tarball and the Node/V8/Unicode
versions in its `reference` field, and `TestOracle` logs them.

The C# runner (actions/runner, `DistributedTask/Expressions2`) is the
implementation that runs on github.com. It was not available while this was
written. Nothing below has been checked against it.

## Limits added by this port (not in the JavaScript)

The reference has no bound on what an evaluation may build. These are
additions, each reported as an `*EvalError` (or `*Error` for the parser) and
none changes the result of an evaluation that stays within them.

- **Output budget** (`MaxResultBytes`, 1 MiB by default, `EvalContext.
  MaxResultBytes` to override, negative to disable). One budget per
  `Evaluate`, charged by `format` (every piece appended), `join` (items and
  separators), `toJSON` (the encoder stops as soon as the output would exceed
  what is left) and by every element of the arrays built by `.*` / `[*]`
  (16 bytes each). `format('{0}{0}', format('{0}{0}', ...))` chains and
  `a.*.*.*` fan-outs over shared arrays are cut off instead of exhausting
  memory. Strings that come out of `fromJSON` are not charged: their size is
  bounded by the input the caller provided. The cost per filtered element is a
  guess; revisit it if real workflows hit the limit.
- **Cancellation**: `Evaluate(ctx, ...)` checks `ctx` every 64 nodes and in the
  loops of filters, `join` and `contains`, and passes `ctx` to extension
  functions.
- **Evaluation depth** of 1000 for hand-built trees (parsed trees stay far
  below it), **JSON depth** `MaxJSONDepth` = 1000 for `FromJSON` and for the
  encoder (cyclic values built with `Array.Append` yield an error from
  `EncodeJSON`, `"null"` from `ToJSON`, an `*EvalError` from `toJSON()`).
  The JS recursion limit is engine-dependent, around 5 000 to 10 000.
- **Built-in arity is re-checked at evaluation**, so a hand-built AST gets an
  `*EvalError`, not a panic.
- **Input length** is checked before the input is converted, without
  allocating.

Other limits that exist in the JS and are ported: expression length 21000
UTF-16 units and parser depth 50 (see below).

## Performance notes (deferred)

- GO-008: `Evaluate` rebuilds the case-folded context and function maps on
  every call, and `foldKey` lower-cases on each lookup. Fine for per-step
  evaluation; when a caller evaluates thousands of expressions against the
  same contexts, add a prepared `Environment` (fold once, reuse) and cache
  folded names in the AST.

## Where the JavaScript is believed to differ from the C# runner

Each item is implemented as the JavaScript does. Check the runner's tests
(`src/Test/L0/Expressions*`, `Sdk/Expressions2/...`) and decide for each one
whether to keep the JS behaviour or follow the runner.

- [x] **String escapes: every `''` is unescaped.** Resolved: follows the C#.
      `LexicalAnalyzer.ReadStringToken` (`Tokens/LexicalAnalyzer.cs`, the
      "Escaped single quote" branch) skips the second quote of each pair and
      appends the first, so `'a''b''c'` is `a'b'c`. The JavaScript lexer does
      `value.replace("''", "'")` (first pair only) and gives `a'b''c`; that is
      a bug of the editor library. Tests: `TestEvalLiteralsAndOperators`; the
      oracle excuses the JS mismatch because `testdata/runner.json.gz` agrees
      with this package.
- [ ] **`case()` availability.** The JS gates it behind the experimental flag
      `allowCaseFunction`, but only in completion; the parser and the
      evaluator always accept it. Here it is always on. Confirm whether the
      hosted service accepts `case` and with which arity rules (the JS: at
      least 3 arguments, an odd number, predicates must be booleans).
- [ ] **`format()`**: the reference uses `parseInt` on the digits, so
      `{99999999999999999999}` is an Infinity-like index and reports "more
      arguments than were supplied". The `{0:x}` form silently ignores the
      specifier; C# may throw. The error texts are the JS ones. Check the C#
      `Format` node for the exact messages and for `{N}` with `N` > 255 or
      leading zeros.
- [x] **`contains()`, `startsWith`/`endsWith`, `==`, `<`, `>` and key lookup use
      `OrdinalIgnoreCase`.** Resolved: follows the C#. Every string comparison
      in the runner is `StringComparison.OrdinalIgnoreCase`
      (`EvaluationResult.cs` `AbstractEqual`/`AbstractGreaterThan`/
      `AbstractLessThan`; `Sdk/Functions/Contains.cs`, `StartsWith.cs`,
      `EndsWith.cs`; `StringComparer.OrdinalIgnoreCase` for context names,
      function names (`ExpressionParser.cs` `ParseContext`,
      `ExpressionConstants.WellKnownFunctions`) and dictionary keys
      (`DictionaryContextData.IndexLookup`)). That folds one code point at a
      time with a simple mapping: `ß` is not `SS`, `ı` and `İ` and `ſ` and the
      Kelvin sign are not `i`/`I`/`s`/`k`, `ς` and `σ` are equal, `ǅ` is `ǆ`.
      `contains()` no longer lower-cases (JS) and `startsWith`/`endsWith` no
      longer upper-case with full mappings (JS). Ordering (`<`, `>`) is by
      code point after folding, so a supplementary character sorts after
      U+E000..U+FFFF (JS orders UTF-16 units). `ordinal.go` implements it;
      `fold_tables.go` is generated from the runner's behaviour
      (`go generate`, `gen/runner --fold`), and
      `TestOrdinalFoldMatchesRunner` replays 1 564 pairs recorded from .NET.
      The table is the one of the .NET runtime that produced it (9.0); the
      runner ships its own runtime, so a different Unicode version could differ
      on newly assigned letters.
- [x] **`==` on strings, `<`, `>`**: see the previous item (same folding).
- [x] **Number to string (`CoerceString`)**: resolved, follows the C#. The
      runner converts every number with `Double.ToString("G15",
      InvariantCulture)` (`ExpressionConstants.NumberFormat`,
      `EvaluationResult.ConvertToString`, `src/Sdk/DTExpressions2/Expressions2/EvaluationResult.cs`),
      not `(+x.toFixed(15)).toString()`: 15 significant digits, positional
      for decimal exponents -5 < X < 15, otherwise `1E+21`, `1E-07`,
      `1.23456789012346E+17` (uppercase `E`, at least two exponent digits).
      So `1e15` is `1E+15`, `123456789012345678` is `1.23456789012346E+17`,
      `0.000001` is `1E-06`, `5e-324` is `4.94065645841247E-324`, `1e309` is
      `Infinity`. Negative zero is `0`: .NET Core prints `-0`, and
      `src/Sdk/Expressions/EvaluationResult.cs` (the copy used by the newer
      evaluator) maps it back to `0` ("keep our behavior consistent"); the
      older `DTExpressions2` copy still prints `-0`, but the shared fixtures
      (`coerce_string.json`, `format('{0}', -0)`) say `0`. Checked against the
      runner's code on 200 000 random doubles. The number *literal* in an
      expression is parsed independently of this (see below) and the JSON
      writer of `toJSON` uses the same conversion (next item).
- [x] **String to number**: resolved, follows the C#. `ExpressionUtility.
      ParseNumber` (`src/Sdk/DTExpressions2/Expressions2/Sdk/ExpressionUtility.cs`)
      trims with `String.Trim()` (so U+0085 is white space and U+FEFF is not),
      accepts what `Double.TryParse(AllowLeadingSign | AllowDecimalPoint |
      AllowExponent)` accepts (ASCII digits only, trailing NULs ignored,
      `Infinity` in any letter case with an optional sign), then `0x` hex and
      `0o` octal (lower-case prefix only, no sign) read as a **32-bit signed
      integer** (`0xffffffff` is `-1`, anything over 32 bits is NaN). There is
      no `0b` form and no upper-case `0X`/`0O`. The lexer uses the same
      function for number literals, so `0b101`, `0X10` and `0xFFFFFFFFF` are
      "Unexpected symbol" errors and `0xffffffff` is `-1`. The same function is
      implemented as `parseNumber` in `number.go`;
      `TestParseNumberMatchesRunner` replays 443 strings recorded from the
      runner's code (`gen/runner/number_cases.mjs`).
- [x] **Number literals after `-` / `+`**: `-1` is one literal token; `a -1`
      is therefore a parse error. Same in the C# lexer (`LexicalAnalyzer.
      TryGetNextToken`: `c == '-' || c == '+' || digit` starts `ReadNumberToken`),
      checked by the differential run.
- [x] **Object key order from `fromJSON`**: resolved, follows the C#. Keys keep
      the document order (`JTokenExtensions.ToPipelineContextData` iterates
      `JObject.Properties()` into `DictionaryContextData`, a list); the JS
      `Object.keys` order (array-index keys first) is gone, from `FromJSON` and
      from `toJSON`. `toJSON(fromJSON('{"b":1,"1":2}'))` is `b` then `1`.
- [x] **Duplicate JSON keys**: resolved, follows the C#. `subContext[property.Name] =
      ...` on a `DictionaryContextData` whose index is `OrdinalIgnoreCase`
      (`DictionaryContextData.this[]` setter): the last value wins, at the
      position and with the spelling of the first. `{"a":1,"A":2}` is
      `{"a": 2}` (the JS keeps `1`). Json.NET itself also merges exact
      duplicates (last wins).
- [x] **`fromJSON` errors and grammar**: resolved, follows the C#. The reader is
      Json.NET's (`Sdk/Functions/FromJson.cs`: `JToken.ReadFrom` on a
      `JsonTextReader` with `DateParseHandling.None`,
      `FloatParseHandling.Double`), which is lenient: single-quoted strings,
      unquoted names, trailing commas, `[,1]` (an empty element is `""`),
      `NaN`/`Infinity`/`-Infinity`/`undefined`, `.5`, `5.`, `01`, `0x1F`, raw
      control characters in strings, anything after the first value, and
      `MaxDepth` 64. An unpaired surrogate escape becomes U+FFFD. Blank input
      is a `JsonReaderException` (here: "empty input", kept as the outer
      message; the Json.NET message texts are not reproduced, only that the
      call fails). Not ported: `/* */` and `//` comments (they are read as a
      value by the runner, i.e. `/*x*/1` is the string `x`) and
      `new Date(1)` constructors; both are errors here.
- [x] **`toJSON`**: resolved, follows the C#. `Sdk/Functions/ToJson.cs` builds
      the text itself: two-space indentation, `[]`/`{}` for empties, numbers
      through `ConvertToString` (so `NaN`, `Infinity` and `-Infinity` appear
      bare, `1E+21`, 15 significant digits, `-0` is `0`) and strings through
      `JsonUtility.ToString` (Json.NET defaults: `\b \f \n \r \t`, `\u00XX`
      for other controls, `\u0085 \u2028 \u2029`, no escaping of `< > & '`, and
      non-ASCII as is). Checked against the runner's code
      (`testdata/tojson/*.runner`). The JS `null` for non-finite numbers, the
      shortest round-trip form and the raw U+2028 are not followed.
      Caveat: `JsonUtility` is stubbed in `gen/runner/Stubs.cs` with
      `JsonConvert.SerializeObject`; the real one passes the runner's
      `VssJsonMediaTypeFormatter` settings, which do not change string
      escaping.
- [ ] **Index access**: `a[b]` where `b` is a number uses `floor(b)` and
      rejects NaN, negatives and infinities; where `b` is a string it is a key
      or a decimal index (`'1'`, `' 1 '`, `'1.9'` all index 1 of an array).
      Booleans and null index as `1`/`0` on arrays and as `"true"`/`""` keys on
      dictionaries.
- [ ] **Filtered arrays** (`a.*`): a dictionary filtered by `*` yields its
      values; `.*.b` skips elements without `b`; `.*.*` flattens one level;
      arrays produced by `*` are fresh, so `a.* == a.*` is false.
- [ ] **Comparison**: `null` is only ordered against numbers/booleans (it
      becomes 0); arrays and dictionaries are never ordered and `==` is
      identity; `NaN` is unordered and unequal to itself; `true > false`.
- [ ] **`&&`/`||` return an operand**, not a boolean (`'' || 'x'` is `'x'`).
- [ ] **Limits**: max expression length 21000 (UTF-16 units; the JS throws a
      bare `Error("ErrorExceededMaxLength")`), max parse depth 50. The depth
      counting (including `index()` and the flat logical chains) is ported
      literally; C# `MaxDepth` is 50 with its own counting. Cross-check with
      deep-nesting tests. There is no node-count or evaluation-memory limit
      here; the C# runner has `MaxMemory` (1 MB) and a result-size limit.
- [ ] **Trailing comma in calls**: `contains('a', 'b',)` parses (the JS loop
      only requires a comma when the next token is not `)`). `f(,)` does not.
- [ ] **Whitespace**: only space, `\t`, `\r`, `\n` separate tokens. U+00A0 and
      other Unicode spaces end a word but are not skipped, so `1 \u00a0 2`
      reports an unexpected symbol. `\v`, `\f` likewise.
- [ ] **Empty expression**: the JS parser returns `undefined` for no tokens.
      `Parse` returns a nil Node and no error; `Evaluate(nil)` errors.
- [ ] **Unknown contexts at evaluation time** are null (the JS `Dictionary.get`
      returns `undefined` and `visitContextAccess` then crashes later); here
      declared-but-absent contexts are null, never a crash.
- [ ] **Error positions**: the JS EOF token's start column is negative after a
      trailing newline (stale `start`); ported as is
      (`TestEOFTokenPosition`).
- [ ] **Function names and context names are case-insensitive**; the JS
      lower-cases with `toLowerCase`, as does this port (`foldKey`). Extension
      functions override built-ins at evaluation (`functions?.get(...) ||
      wellKnownFunctions[...]`) but built-ins win at validation time (arity).

## JavaScript behaviours deliberately NOT ported

These are accidents of JavaScript objects used as maps; porting them would be
a bug.

- `Dictionary.add` tests `key.toLowerCase() in this.indexMap` on a plain
  object, so keys that collide with `Object.prototype` properties
  (`constructor`, `__proto__`, `valueOf`, ...) are silently dropped or hidden.
  Here every key is stored.
- `wellKnownFunctions[name]` is a plain-object lookup, so `constructor(1)`,
  `toString(1)` and `hasOwnProperty(1)` pass validation or crash with a
  `TypeError` instead of "Unrecognized function". Here they are "Unrecognized
  function".
- A `TypeError` in the JS (`Cannot read properties of undefined`) when
  evaluating e.g. `github.true` with an empty context: the reference does not
  handle a missing root context. Here it is null.
- Recursion limits: JS recursion depth for `fromJSON` input is engine
  dependent (about 5 000 to 10 000 levels). `FromJSON` accepts `MaxJSONDepth`
  (1000) levels.
- Lone surrogates: JS strings are UTF-16 and can hold them; Go strings are
  UTF-8. They are kept as WTF-8 (`0xED 0xA0-0xBF xx`) so that `fromJSON` /
  `toJSON` round-trip, `==`/`<`/`contains`/`startsWith`/`endsWith` compare
  them like code units, case mapping copies them, and `format`/`join` rejoin a
  high surrogate followed by a low one into the character they form. This is
  covered by oracle cases. Two surrogate strings concatenated by other means
  (an extension function) are not rejoined.

## Other things to check once the runner is available

- [ ] Run the runner's expression test vectors (`ExpressionParserL0`,
      `ExpressionTests`, `ExpressionValuesL0`, `ExpressionTestUtil`) through a
      table adapter; list every disagreement here before deciding.
- [ ] Decide whether `EvalContext` needs a hook for the functions the runner
      implements natively with special semantics: `success()`, `failure()`,
      `always()`, `cancelled()` (take no arguments; read job status) and
      `hashFiles()` (variadic glob patterns, string result). The API supports
      them through `FuncInfo` + `Func` without changes; arity is declared by
      the caller.
- [ ] Context names the runner allows per location (`github`, `env`, `vars`,
      `job`, `jobs`, `steps`, `runner`, `secrets`, `strategy`, `matrix`,
      `needs`, `inputs`) and which functions each location may call
      (`success`... only in `if`/`jobs.*.if`...). This belongs to the template
      layer, not to package expr.
- [ ] `format()` argument limit is 255 in the JS (`MAX_ARGUMENTS`); the C#
      limit is `ExpressionConstants.MaxArguments` (check).
- [x] Case-insensitive comparison and `contains`: `OrdinalIgnoreCase` is the
      truth, see above.
- [ ] Secret masking and the `${{ }}` splitting of a larger string are not
      part of this package (they belong to the template layer): `Parse`
      takes the text between the braces only.
