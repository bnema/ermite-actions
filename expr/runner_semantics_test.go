// SPDX-License-Identifier: MIT

package expr

import (
	"context"
	"strings"
	"testing"
	"unicode/utf16"
)

// TestRunnerSemantics pins behaviours of the PORTING.md list that the C# runner
// and the JavaScript reference share or differ on. Every expectation was
// obtained by running the expression through the runner's own code
// (gen/runner); the bulk is also covered by TestRunnerOracle.
func TestRunnerSemantics(t *testing.T) {
	ctx := map[string]Value{"github": mustJSON(t, `{
		"commits": [{"id": "1"}, {"id": "2", "x": 1}, {"y": 3}],
		"obj": {"a": {"a": 1}, "K": "v", "b": {"c": 2}},
		"nested": [[1, 2], [3], []]}`)}
	tests := []struct{ src, want string }{
		// Index access: strings and numbers index arrays by their numeric value,
		// booleans and null become 1 and 0, negative, NaN and infinite give null.
		{"github.commits['1'].id", "String:2"},
		{"github.commits[' 1 '].id", "String:2"},
		{"github.commits['1.9'].id", "String:2"},
		{"github.commits[1.9].id", "String:2"},
		{"github.commits[true].id", "String:2"},
		{"github.commits[null].id", "String:1"},
		{"github.commits[-1]", "Null:"},
		{"github.commits[NaN]", "Null:"},
		{"github.commits[Infinity]", "Null:"},
		{"github.commits['x']", "Null:"},
		{"github.commits[5].id", "Null:"},
		// On a dictionary only the string form counts, case-insensitively.
		{"github.obj[true]", "Null:"},
		{"github.obj[null]", "Null:"},
		{"github.obj[1]", "Null:"},
		{"github.obj['K']", "String:v"},
		{"GITHUB.OBJ.K", "String:v"},
		{"github.OBJ.k", "String:v"},
		{"github.nope.deeper", "Null:"},
		// Filters.
		{"toJSON(github.obj.*.a)", "String:[\n  1\n]"},
		{"toJSON(github.commits.*.id)", "String:[\n  \"1\",\n  \"2\"\n]"},
		{"toJSON(github.commits.*.*)", "String:[\n  \"1\",\n  \"2\",\n  1,\n  3\n]"},
		{"toJSON(github.nested.*.*)", "String:[\n  1,\n  2,\n  3\n]"},
		{"toJSON(github.nested.*[0])", "String:[\n  1,\n  3\n]"},
		{"github.commits.* == github.commits.*", "Boolean:false"},
		{"github.obj.* == github.obj.*", "Boolean:false"},
		{"github.commits == github.commits", "Boolean:true"},
		{"github.obj == github.obj", "Boolean:true"},
		// Comparison.
		{"null < 1", "Boolean:true"},
		{"null > -1", "Boolean:true"},
		{"null == 0", "Boolean:true"},
		{"null < true", "Boolean:true"},
		{"null < ''", "Boolean:false"},
		{"null == ''", "Boolean:true"},
		{"NaN == NaN", "Boolean:false"},
		{"NaN != NaN", "Boolean:true"},
		{"NaN < 1", "Boolean:false"},
		{"true > false", "Boolean:true"},
		{"true > 0", "Boolean:true"},
		// && and || return an operand.
		{"'' || 'x'", "String:x"},
		{"'a' && 'b'", "String:b"},
		{"0 && 'x'", "Number:0"},
		{"null || false", "Boolean:false"},
		// White space may surround "." and "[" and sit between a function name
		// and its "(" (the lexer looks ahead past it).
		{"github . obj . K", "String:v"},
		{"github.obj.\nK", "String:v"},
		{"github.obj .K", "String:v"},
		{"github [ 'obj' ].K", "String:v"},
		{"toJSON (1)", "String:1"},
		// White space is Char.IsWhiteSpace: U+00A0, U+000B and U+0085 are skipped.
		{"1\u00a0", "Number:1"},
		{"\u00a01", "Number:1"},
		{"\v1", "Number:1"},
		{"'a'\u0085", "String:a"},
	}
	for _, tc := range tests {
		node, err := Parse(tc.src, []string{"github"}, nil)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.src, err)
			continue
		}
		v, err := Evaluate(context.Background(), node, EvalContext{Contexts: ctx})
		if err != nil {
			t.Errorf("Evaluate(%q): %v", tc.src, err)
			continue
		}
		if got := v.Kind().String() + ":" + CoerceString(v); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}

	// Parse errors.
	for _, tc := range []struct{ src, want string }{
		{"contains('a','b',)", "Unexpected symbol: ')'. Located at position 18 within expression: contains('a','b',)"},
		{"contains(,'a')", "Unexpected symbol: ','. Located at position 10 within expression: contains(,'a')"},
		{"[1] == [1]", "Unexpected symbol: '['. Located at position 1 within expression: [1] == [1]"},
		{"1 \u00a0 2", "Unexpected symbol: '2'. Located at position 5 within expression: 1 \u00a0 2"},
	} {
		if _, err := Parse(tc.src, []string{"github"}, nil); err == nil || err.Error() != tc.want {
			t.Errorf("Parse(%q) = %v, want %q", tc.src, err, tc.want)
		}
	}

	// An empty expression is not an error and has no tree.
	for _, src := range []string{"", "   ", "\n", "\u00a0"} {
		if n, err := Parse(src, nil, nil); n != nil || err != nil {
			t.Errorf("Parse(%q) = %v, %v; want nil, nil", src, n, err)
		}
	}
}

// BenchmarkParseLong parses an expression of the maximum length made of many
// short tokens spread over many lines: building the position of every token
// must not rescan the expression.
func BenchmarkParseLong(b *testing.B) {
	src := strings.Repeat("1 ||\n", MaxExpressionLength/5-1) + "1"
	if len(src) > MaxExpressionLength {
		b.Fatal(len(src))
	}
	b.SetBytes(int64(len(src)))
	for i := 0; i < b.N; i++ {
		if _, err := Parse(src, nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPositionLines(t *testing.T) {
	src := "a\n\nbc d\n  e"
	p := &csParser{units: utf16.Encode([]rune(src))}
	for _, tc := range []struct{ idx, line, col int }{{0, 0, 0}, {1, 0, 1}, {2, 1, 0}, {3, 2, 0}, {5, 2, 2}, {7, 2, 4}, {8, 3, 0}, {10, 3, 2}, {len(src), 3, 3}} {
		if got := p.position(tc.idx); got.Line != tc.line || got.Column != tc.col {
			t.Errorf("position(%d) = %v, want %d:%d", tc.idx, got, tc.line, tc.col)
		}
	}
}
