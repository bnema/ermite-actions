// SPDX-License-Identifier: MIT

// Regenerate the case-mapping tables (needs node; see gen/casing_dump.mjs).
//go:generate sh -c "node gen/casing_dump.mjs | go run gen/casing.go -o casing_tables.go"

// Package expr implements GitHub Actions expressions: the language found
// between the braces of ${{ }} in workflow files.
//
// The behaviour is that of the C# runner (actions/runner, MIT), the
// implementation that evaluates expressions on github.com: its tokenizer and
// parser, error messages, coercions, case-insensitive comparison, number
// formatting, JSON reader and writer, and built-in functions. It was first
// ported from GitHub's editor library, the npm package @actions/expressions
// (MIT, actions/languageservices), then checked against the runner on 34 765
// expressions and against the shared cross-language fixtures; where the two
// differ, the runner wins. PORTING.md lists every difference with the C#
// source that decides it.
//
// The entry points are [Parse], which turns the text of one expression into a
// [Node], and [Evaluate], which computes its [Value] against the named
// contexts and functions of an [EvalContext]:
//
//	node, err := expr.Parse("github.ref == 'refs/heads/main'", []string{"github"}, nil)
//	if err != nil { /* *expr.Error */ }
//	v, err := expr.Evaluate(ctx, node, expr.EvalContext{Contexts: map[string]expr.Value{"github": github}})
//	if err != nil { /* *expr.EvalError */ }
//	if expr.Truthy(v) { /* ... */ }
//
// Values are the JSON-like [Null], [Boolean], [Number], [String], [*Array]
// and [*Dictionary]. Dictionaries keep insertion order and look keys up
// case-insensitively. [FromJSON] and [ToJSON] convert to and from JSON text
// exactly as the fromJSON and toJSON functions do.
//
// Evaluation is bounded: [Evaluate] honours its context.Context and stops
// when the strings and filtered arrays it builds exceed
// [EvalContext.MaxResultBytes] (default [MaxResultBytes]); the parser limits
// length and nesting ([MaxExpressionLength], [MaxParserDepth]).
package expr
