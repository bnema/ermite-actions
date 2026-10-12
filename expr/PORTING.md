# Porting notes for package expr

The package is a port of `@actions/expressions` 0.3.61 (MIT, GitHub,
actions/languageservices). The JavaScript is the specification wherever this
package could be checked against it: `testdata/oracle.json.gz` holds what the
reference answers for 33 575 expressions (tokens with positions, error
messages, result kinds, string coercions, `toJSON` output), and
`TestOracle` replays it. `gen/oracle.mjs` regenerates it, `gen/golden.mjs` the
`testdata/tojson` goldens, and `gen/casing_dump.mjs` + `gen/casing.go` the
casing tables (Node 25 / V8 14.1, Unicode 16; Go 1.27 ships Unicode 15).

The C# runner (actions/runner, `DistributedTask/Expressions2`) is the
implementation that runs on github.com. It was not available while this was
written. Nothing below has been checked against it.

## Where the JavaScript is believed to differ from the C# runner

Each item is implemented as the JavaScript does. Check the runner's tests
(`src/Test/L0/Expressions*`, `Sdk/Expressions2/...`) and decide for each one
whether to keep the JS behaviour or follow the runner.

- [ ] **String escapes: only the first `''` is unescaped.** The lexer does
      `value.replace("''", "'")` with a string pattern, which replaces one
      occurrence: `'a''b''c'` is `a'b''c`. The C# lexer should unescape every
      pair (`StringBuilder`/`Replace`). Probable JS bug. (`lexer.go`,
      `consumeString`; tests: `TestEvalLiteralsAndOperators`.)
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
- [ ] **`contains()` lower-cases (`toLowerCase`), `startsWith`/`endsWith`
      upper-case (`toUpperSpecial`).** The C# uses `OrdinalIgnoreCase`
      comparisons, which differ for `ß`, `İ`, `ı`, the Kelvin sign and final
      sigma. `contains('ß', 'ss')` is false in JS; `startsWith('ß', 'ss')` is
      true. Check what the runner does.
- [ ] **`==` on strings uses `toUpperSpecial`** (upper-case with the dotless
      `ı` left alone, full Unicode mapping so `ß == 'SS'`), and `<`/`>` compare
      the upper-cased UTF-16 code units. C# `OrdinalIgnoreCase` upper-cases
      per UTF-16 code unit with simple mappings only: `ß == 'SS'` should be
      false there.
- [ ] **Number to string (`CoerceString`)**: `(+x.toFixed(15)).toString()`.
      C# formats a `double` with `"G15"`-like rules. They agree for ordinary
      values; check very small and very large numbers (`1e-7` here is `1e-7`,
      `1e21` is `1e+21`), `-0`, and 16-17 significant digit values.
- [ ] **String to number**: `Number()` semantics (hex, octal, binary,
      `Infinity`, whitespace trimmed, empty is 0). C# `double.TryParse` with
      `NumberStyles` differs (no octal/binary; check `0x` and `Infinity`).
      The lexer applies the same conversion to number literals, so `0x10`,
      `0b11`, `1e3`, `.5`, `5.` and `+1` are all literals here.
- [ ] **Number literals after `-` / `+`**: `-1` is one literal token; `a -1`
      is therefore a parse error, as in the JS.
- [ ] **Object key order from `fromJSON`**: array-index keys first, ascending
      (JavaScript `Object.keys`), then insertion order. `Json.NET` keeps
      document order. The same ordering is applied by `toJSON` (the JS
      replacer builds a plain object). Check against the runner for
      `toJSON(fromJSON('{"b":1,"1":2}'))`.
- [ ] **Duplicate JSON keys**: last value wins at the first position (JS);
      keys that differ only by case collapse to the first.
- [ ] **`fromJSON` errors**: the JS message is
      `Error parsing JSON when evaluating fromJson` with the V8 `SyntaxError`
      as cause; here the cause is a Go error with similar but not identical
      wording (only the outer message is stable). `empty input` for blank
      input. C# messages differ; check.
- [ ] **`toJSON`**: `JSON.stringify(v, replacer, "  ")`: two-space indent,
      `[]` and `{}` for empties, `NaN`/`Infinity` as `null`, `-0` as `0`,
      no escaping of `<`, `>`, `&`, U+2028/U+2029; lone surrogates as
      `\udXXX`. Json.NET differs on some of these (`Infinity`, `-0`, escapes).
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
  dependent (about 5 000 to 10 000 levels). `FromJSON` accepts 10 000 levels.

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
- [ ] Case-insensitive comparison and `contains`: if C# `OrdinalIgnoreCase`
      is the truth, replace `toUpperSpecial`/`jsToLower` by per-UTF-16-unit
      simple upper-casing and regenerate the oracle tests accordingly.
- [ ] Secret masking and the `${{ }}` splitting of a larger string are not
      part of this package (they belong to the template layer): `Parse`
      takes the text between the braces only.
