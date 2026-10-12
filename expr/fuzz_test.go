// SPDX-License-Identifier: MIT

package expr

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// Seeds for the fuzz targets: the corpus of the table tests and a few shapes
// that stress the lexer and the parser.
var fuzzSeeds = []string{
	"", " ", "true", "github.event_name == 'push'", "1 == '1'", "'it''s'", "'unterminated", "a-b", "NaN", "-Infinity", "0x1F", "1e3", ".5", "5.",
	"github.event.commits.*.id", "github['x'][0].*", "contains(github.ref, 'main') && !failure()", "format('{0} {{}} {1}', 1, null)",
	"fromJSON('[1,{\"a\":null}]')[1].a", "toJSON(fromJSON('{\"b\":1,\"1\":2}'))", "join(matrix.node.*, ', ')", "case(true, 1, 2)", "case(false, 1, true, 2, 3)",
	"((((((((((1))))))))))", "!!!!!!true", "1 || 2 && 3 == 4 < 5", "hashFiles('**/*.go', '!vendor/**')", "success() && always()", "\n\t \r", "\u00a0", "'😀' == '\U00010000'",
	"=", "&", "|", "!=", "<=", ">=", "()", "(1", "1)", "a.", ".a", "a..b", "a[", "a[]", "a[*]", "*", "a.*.*", "1.2.3", "--1", "1_000", "'\xed\xa0\x80'",
	"fromJSON('\"\\ud800\"') == fromJSON('\"\\udc00\"')", "format('{0}{1}', fromJSON('\"\\ud83d\"'), fromJSON('\"\\ude00\"'))", "contains(fromJSON('\"\\ud83d\\ude00\"'), fromJSON('\"\\ude00\"'))", "format('{0}{0}', format('{0}{0}', 'xxxxxxxx'))", "github.event.commits.*.*.*", "format('{0:x}{', 1)", "format('}}{{', 1)", "contains(1,2,3)", "ß == 'SS'", "İ", "'ı' == 'I'", "'\u03a3' < 'z'",
}

func addSeeds(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
}

// FuzzLex checks that lexing never panics, that it is deterministic, and
// that successful lexing yields ordered tokens ending with exactly one EOF.
func FuzzLex(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, s string) {
		toks, err := Lex(s)
		toks2, err2 := Lex(s)
		if (err == nil) != (err2 == nil) || len(toks) != len(toks2) {
			t.Fatal("lexing is not deterministic")
		}
		if err != nil {
			if _, ok := err.(*Error); !ok {
				t.Fatalf("error is %T, not *Error", err)
			}
			return
		}
		if len(toks) == 0 || toks[len(toks)-1].Type != TokenEOF {
			t.Fatal("token stream does not end with EOF")
		}
		for i, tk := range toks {
			if tk.Type == TokenEOF && i != len(toks)-1 {
				t.Fatal("EOF in the middle of the token stream")
			}
			if tk.Type != TokenEOF && tk.Lexeme == "" {
				t.Fatalf("empty lexeme for token %v", tk.Type)
			}
			if !utf8.ValidString(tk.Lexeme) && utf8.ValidString(s) {
				t.Fatalf("invalid UTF-8 lexeme %q from valid input", tk.Lexeme)
			}
			// The EOF token of the reference starts at a stale offset, so after
			// a trailing newline its start column is -1; every other token
			// has a column of at least 0.
			if tk.Range.Start.Line < 0 || (tk.Type != TokenEOF && tk.Range.Start.Column < 0) || tk.Range.End.Line < tk.Range.Start.Line {
				t.Fatalf("bad range %+v", tk.Range)
			}
		}
	})
}

// FuzzParse checks that parsing never panics, that errors are *Error and
// that the depth limit holds: no accepted expression nests deeper than the
// parser allows.
func FuzzParse(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, s string) {
		node, err := Parse(s, []string{"github", "matrix", "env"}, []FuncInfo{{Name: "hashFiles", MinArgs: 1, MaxArgs: -1}, {Name: "success"}})
		if err != nil {
			if _, ok := err.(*Error); !ok {
				t.Fatalf("error is %T, not *Error", err)
			}
			if node != nil {
				t.Fatal("node returned with an error")
			}
			return
		}
		if node == nil && strings.TrimFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }) != "" {
			t.Fatalf("empty tree for non-blank input %q", s)
		}
		if d := nodeDepth(node); d > 2*MaxParserDepth+2 {
			t.Fatalf("tree depth %d exceeds the parser limit", d)
		}
	})
}

func nodeDepth(n Node) int {
	switch n := n.(type) {
	case *Unary:
		return 1 + nodeDepth(n.Expr)
	case *Binary:
		return 1 + max(nodeDepth(n.Left), nodeDepth(n.Right))
	case *Logical:
		d := 0
		for _, a := range n.Args {
			d = max(d, nodeDepth(a))
		}
		return 1 + d
	case *Grouping:
		return 1 + nodeDepth(n.Group)
	case *IndexAccess:
		return 1 + max(nodeDepth(n.Expr), nodeDepth(n.Index))
	case *FunctionCall:
		d := 0
		for _, a := range n.Args {
			d = max(d, nodeDepth(a))
		}
		return 1 + d
	}
	return 1
}

// FuzzEvaluate checks that evaluating an arbitrary parsed expression never
// panics, always returns a usable value or an *EvalError, and leaves the
// context untouched.
func FuzzEvaluate(f *testing.F) {
	addSeeds(f)
	ctx := EvalContext{
		Contexts: map[string]Value{
			"github": mustJSON(f, `{"event_name":"push","ref":"refs/heads/main","event":{"commits":[{"id":"1","added":["a"]},{"id":"2"},{"x":[[1],[2,3]]}],"n":12,"nested":[[1,2],[3]]}}`),
			"matrix": mustJSON(f, `{"node":[14,16],"os":"linux","o":{"k":null}}`),
			"env":    mustJSON(f, `{"A":"","B":"1","C":"x","J":"{\"a\":[1,2]}"}`),
		},
		Funcs: map[string]Func{
			"hashfiles": func(_ context.Context, args []Value) (Value, error) { return String("h"), nil },
			"success":   func(context.Context, []Value) (Value, error) { return Boolean(true), nil },
		},
	}
	before := map[string]string{}
	for k, v := range ctx.Contexts {
		before[k] = ToJSON(v)
	}
	f.Fuzz(func(t *testing.T, s string) {
		node, err := Parse(s, []string{"github", "matrix", "env"}, []FuncInfo{{Name: "hashFiles", MinArgs: 1, MaxArgs: -1}, {Name: "success"}})
		if err != nil || node == nil {
			return
		}
		v, err := Evaluate(context.Background(), node, ctx)
		if err != nil {
			if _, ok := err.(*EvalError); !ok {
				t.Fatalf("error is %T, not *EvalError", err)
			}
			return
		}
		if v == nil {
			t.Fatal("nil value without error")
		}
		_ = CoerceString(v)
		_ = Truthy(v)
		j := ToJSON(v)
		if !utf8.ValidString(j) {
			t.Fatalf("toJSON produced invalid UTF-8: %q", j)
		}
		if _, err := FromJSON([]byte(j)); err != nil {
			t.Fatalf("toJSON output does not parse: %v\n%s", err, j)
		}
		for k, c := range ctx.Contexts {
			if ToJSON(c) != before[k] {
				t.Fatalf("context %s was modified", k)
			}
		}
	})
}

// FuzzFromJSON checks that FromJSON never panics and that whatever it
// accepts prints as JSON that parses back to the same text.
func FuzzFromJSON(f *testing.F) {
	for _, s := range []string{`null`, `{"a":[1,2,{"b":null}]}`, `"\ud83d\ude00"`, `{"1":1,"a":2,"01":3}`, `[1e999]`, `-0`, `"\ud800"`, `{"a":1,"A":2}`, `[`, `{"a"`, "\"\x01\""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := FromJSON([]byte(s))
		if err != nil {
			return
		}
		j := ToJSON(v)
		v2, err := FromJSON([]byte(j))
		if err != nil {
			t.Fatalf("round trip failed: %v\n%s", err, j)
		}
		if j2 := ToJSON(v2); j2 != j {
			t.Fatalf("round trip changed output:\n%s\n%s", j, j2)
		}
	})
}
