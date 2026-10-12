// SPDX-License-Identifier: MIT

package expr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The cross-language fixtures of actions/languageservices
// (testdata/languageservices, see SOURCE there) are shared by the TypeScript
// and the C# implementations. This runner reproduces
// expressions/src/xlang.test.ts of that repository:
//
//   - contexts are revived into expression data, and their names are the
//     context names given to the parser; no extension functions are declared;
//   - a lexing error is compared with the error message of the lexer;
//   - a parsing error with "<message>. Located at position <column+1> within
//     expression: <expr>";
//   - an evaluation error with "<message>. Located within expression: <expr>";
//   - error expectations match by substring, result kinds by name, result
//     values by their JSON.stringify form (NaN and Infinity are null).
//
// The only deliberate difference from the TypeScript runner is that cases
// marked options.skip = ["typescript"] are run too: they are skipped there
// because the TypeScript port lacks a feature, not because the fixture is
// wrong. Fixtures that this package cannot satisfy are listed in
// xlangKnownDifferences with the reason.

type xlangResult struct {
	Kind  string          `json:"kind"`
	Value json.RawMessage `json:"value"`
}

type xlangErr struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type xlangCase struct {
	Expr     string          `json:"expr"`
	Result   *xlangResult    `json:"result"`
	Err      *xlangErr       `json:"err"`
	Contexts json.RawMessage `json:"contexts"`
	Options  struct {
		Skip []string `json:"skip"`
	} `json:"options"`
}

// xlangKnownDifferences maps "file: group (expr prefix)" keys to the reason
// the fixture is not satisfied. Keep it empty if possible.
var xlangKnownDifferences = map[string]string{
	"case: case#27":                  "the runner reports an even number of case() arguments on the '(' token (position 5), not on the name: ExpressionParser.FlushTopEndParameters passes the popped StartParameters token; checked by running the runner's code",
	"syntax-errors: memory-errors#0": "memory accounting differs from the runner's EvaluationMemory",
}

// xlangKind is the kind name the fixtures use.
func xlangKind(v Value) string {
	switch v.Kind() {
	case KindArray:
		return "Array"
	case KindBoolean:
		return "Boolean"
	case KindNull:
		return "Null"
	case KindNumber:
		return "Number"
	case KindString:
		return "String"
	}
	return "Object"
}

// jsonStringify mimics JSON.stringify(v) (no indentation) for a value.
func jsonStringify(b *strings.Builder, v Value) {
	switch x := v.(type) {
	case nil, Null:
		b.WriteString("null")
	case Boolean:
		b.WriteString(strconv.FormatBool(bool(x)))
	case Number:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			b.WriteString("null")
		} else {
			b.WriteString(jsNumberToString(f))
		}
	case String:
		s, _ := json.Marshal(string(x))
		b.Write(s)
	case *Array:
		b.WriteByte('[')
		for i, it := range x.Items() {
			if i > 0 {
				b.WriteByte(',')
			}
			jsonStringify(b, it)
		}
		b.WriteByte(']')
	case *Dictionary:
		b.WriteByte('{')
		for i, p := range x.Pairs() {
			if i > 0 {
				b.WriteByte(',')
			}
			k, _ := json.Marshal(p.Key)
			b.Write(k)
			b.WriteByte(':')
			jsonStringify(b, p.Value)
		}
		b.WriteByte('}')
	}
}

func stringifyValue(v Value) string {
	var b strings.Builder
	jsonStringify(&b, v)
	return b.String()
}

// xlangContexts returns the revived contexts and their names, sorted.
func xlangContexts(tb testing.TB, raw json.RawMessage) (map[string]Value, []string) {
	tb.Helper()
	ctx := map[string]Value{}
	if len(raw) == 0 {
		return ctx, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		tb.Fatalf("contexts: %v", err)
	}
	var names []string
	for k, r := range m {
		ctx[k] = mustJSON(tb, string(r))
		names = append(names, k)
	}
	sort.Strings(names)
	return ctx, names
}

// runXlangCase returns "" when the case passes, else the reason it fails.
func runXlangCase(tb testing.TB, c xlangCase) string {
	ctxValues, names := xlangContexts(tb, c.Contexts)
	expectErr := func(kind string) bool { return c.Err != nil && c.Err.Kind == kind }

	// Lex.
	_, err := Lex(c.Expr)
	if err != nil {
		if expectErr("lexing") {
			if strings.Contains(err.Error(), c.Err.Value) {
				return ""
			}
			return fmt.Sprintf("lexing error %q does not contain %q", err.Error(), c.Err.Value)
		}
		return "unexpected error lexing expression: " + err.Error()
	}
	if expectErr("lexing") {
		return "expected error lexing expression, but got none"
	}

	// Parse.
	node, err := Parse(c.Expr, names, nil)
	if err != nil {
		if expectErr("parsing") {
			var pe *Error
			if !errors.As(err, &pe) {
				return fmt.Sprintf("parse error has type %T", err)
			}
			// The TypeScript runner builds the suffix from a column; this
			// package, like the C# runner, already has it in the message.
			full := pe.Message
			if !strings.Contains(full, ". Located at position ") {
				full = fmt.Sprintf("%s. Located at position %d within expression: %s", pe.Message, pe.Pos.Column+1, c.Expr)
			}
			if strings.Contains(full, c.Err.Value) {
				return ""
			}
			return fmt.Sprintf("parsing error %q does not contain %q", full, c.Err.Value)
		}
		return "unexpected error parsing expression: " + err.Error()
	}
	if expectErr("parsing") {
		return "expected error parsing expression, but got none"
	}

	// Evaluate. An empty expression is null.
	var result Value = Null{}
	if node != nil {
		result, err = Evaluate(context.Background(), node, EvalContext{Contexts: ctxValues})
	}
	if err != nil {
		if expectErr("evaluation") {
			full := err.Error() + ". Located within expression: " + c.Expr
			if strings.Contains(full, c.Err.Value) {
				return ""
			}
			return fmt.Sprintf("evaluation error %q does not contain %q", full, c.Err.Value)
		}
		return "unexpected error evaluating expression: " + err.Error()
	}
	if expectErr("evaluation") {
		return "expected error evaluating expression, but got none"
	}
	if c.Result == nil {
		return "fixture has neither result nor err"
	}
	if k := xlangKind(result); k != c.Result.Kind {
		return fmt.Sprintf("kind: got %s, want %s", k, c.Result.Kind)
	}
	want := stringifyValue(mustJSON(tb, string(c.Result.Value)))
	if got := stringifyValue(result); got != want {
		return fmt.Sprintf("value: got %s, want %s", got, want)
	}
	return ""
}

func TestLanguageServicesFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/languageservices/*.json")
	if err != nil || len(files) != 28 {
		t.Fatalf("fixture files: %d, %v", len(files), err)
	}
	var total, skippedByTS, known, failed int
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var groups map[string][]xlangCase
		if err := json.Unmarshal(data, &groups); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		groupNames := make([]string, 0, len(groups))
		for g := range groups {
			groupNames = append(groupNames, g)
		}
		sort.Strings(groupNames)
		for _, g := range groupNames {
			for i, c := range groups[g] {
				total++
				for _, s := range c.Options.Skip {
					if s == "typescript" {
						skippedByTS++
					}
				}
				key := name + ": " + g
				label := fmt.Sprintf("%s/%s/%d", name, g, i)
				reason, isKnown := xlangKnownDifferences[fmt.Sprintf("%s#%d", key, i)]
				t.Run(label, func(t *testing.T) {
					msg := runXlangCase(t, c)
					switch {
					case msg == "" && isKnown:
						t.Errorf("listed as a known difference (%s) but passes", reason)
					case msg != "" && isKnown:
						known++
						t.Logf("known difference (%s): %s [%s]", reason, msg, c.Expr)
					case msg != "":
						failed++
						t.Errorf("%s [%s]", msg, c.Expr)
					}
				})
			}
		}
	}
	t.Logf("%d fixtures in %d files (%d skipped by the TypeScript runner and run here), %d known differences, %d failures", total, len(files), skippedByTS, known, failed)
	if total != 1020 {
		t.Errorf("expected 1020 fixtures, found %d", total)
	}
}
