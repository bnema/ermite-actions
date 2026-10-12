// SPDX-License-Identifier: MIT

package expr

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// testdata/runner.json.gz holds what the C# runner's expression engine
// (actions/runner, src/Sdk/DTExpressions2) answers on the corpus of
// testdata/oracle.json.gz. It is produced by gen/runner. Where the runner and
// the JavaScript reference disagree, the runner is what users see at run time
// and this package follows it (see PORTING.md). TestOracle excuses a mismatch
// with the JavaScript only if this package gives the runner's answer there.

type runnerFile struct {
	Reference struct {
		Source  string `json:"source"`
		Commit  string `json:"commit"`
		Runtime string `json:"runtime"`
	} `json:"reference"`
	Cases []runnerCase `json:"cases"`
}

type runnerCase struct {
	E     string     `json:"e"`
	Empty bool       `json:"empty"`
	K     string     `json:"k"`
	S     string     `json:"s"`
	J     string     `json:"j"`
	Err   *runnerErr `json:"err"`
}

// runnerErr is an error of the runner: the phase, the message, and the name of
// the .NET exception class ("JsonReaderException" for a fromJSON failure).
type runnerErr struct {
	T string `json:"t"`
	M string `json:"m"`
	X string `json:"x"`
}

func loadRunner(tb testing.TB) *runnerFile {
	tb.Helper()
	f, err := os.Open("testdata/runner.json.gz")
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		tb.Fatal(err)
	}
	var rf runnerFile
	if err := json.NewDecoder(zr).Decode(&rf); err != nil {
		tb.Fatal(err)
	}
	return &rf
}

// lossy replaces every lone surrogate (WTF-8) by U+FFFD, which is what a JSON
// decoder makes of the \ud800 escapes in the recorded answers.
func lossy(s string) string {
	if !hasWTF8(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, w := decodeRune(s[i:])
		if r >= 0xD800 && r < 0xE000 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(s[i : i+w])
		}
		i += w
	}
	return b.String()
}

// goAnswer evaluates e like the runner harness does and returns its answer in
// the runner's format.
func goAnswer(e string, ctxNames []string, ec EvalContext) runnerCase {
	rc := runnerCase{E: e}
	node, err := Parse(e, ctxNames, oracleFuncInfos)
	if err != nil {
		rc.Err = &runnerErr{T: "parse", M: err.Error()}
		return rc
	}
	if node == nil {
		rc.Empty = true
		return rc
	}
	v, err := Evaluate(context.Background(), node, ec)
	if err != nil {
		rc.Err = &runnerErr{T: "eval", M: err.Error(), X: goErrorClass(err)}
		return rc
	}
	rc.K = v.Kind().String()
	rc.S = lossy(CoerceString(v))
	rc.J = lossy(ToJSON(v))
	return rc
}

// goErrorClass names the kind of an evaluation error of this package in the
// terms of runnerErr.X: "JsonReaderException" for the failures of fromJSON.
func goErrorClass(err error) string {
	var ee *EvalError
	if errors.As(err, &ee) && (ee.Message == "empty input" || ee.Message == "Error parsing JSON when evaluating fromJson") {
		return "JsonReaderException"
	}
	return ""
}

// sameAnswer reports whether a Go answer matches the runner's. Errors match
// when they are of the same phase and, for parse errors, name the same kind of
// problem (the text before the colon: the runner appends the position, and its
// arity errors name the parenthesis). Evaluation errors must have the same
// message, except a fromJSON failure, whose runner message is Json.NET's: it
// matches only a fromJSON failure of this package (both are JSON reader
// failures, never just "the expression mentions fromJSON").
func sameAnswer(g, r runnerCase) bool {
	if (g.Err == nil) != (r.Err == nil) {
		return false
	}
	if g.Err == nil {
		return g.Empty == r.Empty && g.K == r.K && g.S == r.S && g.J == r.J
	}
	if g.Err.T != r.Err.T {
		return false
	}
	if g.Err.T == "parse" {
		desc := func(m string) string { d, _, _ := strings.Cut(m, ":"); return d }
		return desc(g.Err.M) == desc(r.Err.M)
	}
	if r.Err.X == "JsonReaderException" || g.Err.X == "JsonReaderException" {
		return r.Err.X == g.Err.X
	}
	return g.Err.M == r.Err.M
}

func clip(s string) string {
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}

// TestSameAnswerFromJSON: an error is excused as "a fromJSON failure" only
// when both sides are one; an expression that merely mentions fromJSON does not
// get a free pass.
func TestSameAnswerFromJSON(t *testing.T) {
	ev := func(m, x string) runnerCase {
		return runnerCase{E: "fromJSON('x')", Err: &runnerErr{T: "eval", M: m, X: x}}
	}
	reader := ev("Error parsing JSON when evaluating fromJson", "JsonReaderException")
	runnerReader := ev("Unexpected character encountered while parsing value: x. Path '', line 0, position 0.", "JsonReaderException")
	format := ev("The following format string is invalid: {", "FormatException")
	value := runnerCase{E: "fromJSON('1')", K: "Number", S: "1", J: "1"}
	for _, tc := range []struct {
		name string
		g, r runnerCase
		want bool
	}{
		{"both reader failures", reader, runnerReader, true},
		{"go fails, runner succeeds", reader, value, false},
		{"runner fails, go succeeds", value, runnerReader, false},
		{"go reader failure, runner format error", reader, format, false},
		{"runner reader failure, go format error", format, runnerReader, false},
		{"same non-reader error", format, format, true},
	} {
		if got := sameAnswer(tc.g, tc.r); got != tc.want {
			t.Errorf("%s: sameAnswer = %v, want %v", tc.name, got, tc.want)
		}
	}
}
