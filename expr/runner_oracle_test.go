// SPDX-License-Identifier: MIT

package expr

import (
	"compress/gzip"
	"context"
	"encoding/json"
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
	E     string `json:"e"`
	Empty bool   `json:"empty"`
	K     string `json:"k"`
	S     string `json:"s"`
	J     string `json:"j"`
	Err   *struct {
		T string `json:"t"`
		M string `json:"m"`
	} `json:"err"`
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
		rc.Err = &struct {
			T string `json:"t"`
			M string `json:"m"`
		}{"parse", err.Error()}
		return rc
	}
	if node == nil {
		rc.Empty = true
		return rc
	}
	v, err := Evaluate(context.Background(), node, ec)
	if err != nil {
		rc.Err = &struct {
			T string `json:"t"`
			M string `json:"m"`
		}{"eval", err.Error()}
		return rc
	}
	rc.K = v.Kind().String()
	rc.S = lossy(CoerceString(v))
	rc.J = lossy(ToJSON(v))
	return rc
}

// sameAnswer reports whether a Go answer matches the runner's. Errors match
// when they are of the same phase and, for parse errors, name the same kind of
// problem (the text before the colon); the runner appends the position and
// names the parenthesis rather than the function in arity errors, and its
// fromJSON errors are Json.NET messages. Other evaluation errors must be equal.
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
	if strings.Contains(strings.ToLower(r.E), "fromjson") {
		return true
	}
	return g.Err.M == r.Err.M
}

func clip(s string) string {
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}
