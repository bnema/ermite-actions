// SPDX-License-Identifier: MIT

package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"bnema.dev/ermite-actions/expr"
	"bnema.dev/ermite-actions/template"
)

var ctxbg = context.Background()

func jsonValue(t testing.TB, s string) expr.Value {
	t.Helper()
	v, err := expr.FromJSON([]byte(s))
	if err != nil {
		t.Fatalf("FromJSON(%s): %v", s, err)
	}
	return v
}

func compact(s string) string {
	return strings.NewReplacer("\n", "", "  ", "", `": `, `":`).Replace(s)
}

// summarize renders combos as "k=v,k=v" lines with the display segments.
func summarize(combos []MatrixCombo) []string {
	var out []string
	for _, c := range combos {
		var kv []string
		for _, p := range c.Values.Pairs() {
			kv = append(kv, p.Key+"="+compact(expr.ToJSON(p.Value)))
		}
		out = append(out, strings.Join(kv, ",")+" | "+c.DisplayName("job"))
	}
	return out
}

func TestExpandMatrix(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		want     []string
	}{
		{"no strategy", `null`, []string{" | job"}},
		{"strategy without matrix", `{"fail-fast": false}`, []string{" | job"}},
		{"empty matrix", `{"matrix": {}}`, []string{" | job"}},
		{
			"one vector",
			`{"matrix": {"os": ["ubuntu", "macos"]}}`,
			[]string{`os="ubuntu" | job (ubuntu)`, `os="macos" | job (macos)`},
		},
		{
			"cross product, first key slowest",
			`{"matrix": {"os": ["a", "b"], "node": [14, 16, 18]}}`,
			[]string{
				`os="a",node=14 | job (a, 14)`, `os="a",node=16 | job (a, 16)`, `os="a",node=18 | job (a, 18)`,
				`os="b",node=14 | job (b, 14)`, `os="b",node=16 | job (b, 16)`, `os="b",node=18 | job (b, 18)`,
			},
		},
		{
			// GitHub docs, "Expanding or adding matrix configurations".
			"docs include example",
			`{"matrix": {"fruit": ["apple", "pear"], "animal": ["cat", "dog"], "include": [
				{"color": "green"},
				{"color": "pink", "animal": "cat"},
				{"fruit": "apple", "shape": "circle"},
				{"fruit": "banana"},
				{"fruit": "banana", "animal": "cat"}
			]}}`,
			[]string{
				`fruit="apple",animal="cat",color="pink",shape="circle" | job (apple, cat, pink, circle)`,
				`fruit="apple",animal="dog",color="green",shape="circle" | job (apple, dog, green, circle)`,
				`fruit="pear",animal="cat",color="pink" | job (pear, cat, pink)`,
				`fruit="pear",animal="dog",color="green" | job (pear, dog, green)`,
				`fruit="banana" | job (banana)`,
				`fruit="banana",animal="cat" | job (banana, cat)`,
			},
		},
		{
			// GitHub docs, "Excluding matrix configurations".
			"docs exclude example",
			`{"matrix": {"os": ["macos-latest", "windows-latest"], "version": [12, 14, 16], "environment": ["staging", "production"],
				"exclude": [{"os": "macos-latest", "version": 12, "environment": "production"}, {"os": "windows-latest", "version": 16}]}}`,
			[]string{
				`os="macos-latest",version=12,environment="staging" | job (macos-latest, 12, staging)`,
				`os="macos-latest",version=14,environment="staging" | job (macos-latest, 14, staging)`,
				`os="macos-latest",version=14,environment="production" | job (macos-latest, 14, production)`,
				`os="macos-latest",version=16,environment="staging" | job (macos-latest, 16, staging)`,
				`os="macos-latest",version=16,environment="production" | job (macos-latest, 16, production)`,
				`os="windows-latest",version=12,environment="staging" | job (windows-latest, 12, staging)`,
				`os="windows-latest",version=12,environment="production" | job (windows-latest, 12, production)`,
				`os="windows-latest",version=14,environment="staging" | job (windows-latest, 14, staging)`,
				`os="windows-latest",version=14,environment="production" | job (windows-latest, 14, production)`,
			},
		},
		{
			// GitHub docs, "Using a matrix with only include": the include
			// entries do not overwrite a vector value.
			"include does not overwrite original values",
			`{"matrix": {"os": ["a", "b"], "include": [{"os": "c"}, {"os": "a", "node": 1}]}}`,
			[]string{
				`os="a",node=1 | job (a, 1)`,
				`os="b" | job (b)`,
				`os="c" | job (c)`,
			},
		},
		{
			"include only",
			`{"matrix": {"include": [{"os": "a", "n": 1}, {"os": "b", "n": 2}]}}`,
			[]string{`os="a",n=1 | job (a, 1)`, `os="b",n=2 | job (b, 2)`},
		},
		{
			"added value overwritten by a later include",
			`{"matrix": {"os": ["a"], "include": [{"color": "red"}, {"color": "blue", "os": "a"}]}}`,
			[]string{`os="a",color="blue" | job (a, blue)`},
		},
		{
			"exclude then include re-adds",
			`{"matrix": {"os": ["a", "b"], "exclude": [{"os": "a"}], "include": [{"os": "a", "x": 1}]}}`,
			[]string{`os="b" | job (b)`, `os="a",x=1 | job (a, 1)`},
		},
		{
			"exclude partial match",
			`{"matrix": {"a": [1, 2], "b": [1, 2], "exclude": [{"a": 1}]}}`,
			[]string{`a=2,b=1 | job (2, 1)`, `a=2,b=2 | job (2, 2)`},
		},
		{
			"exclude no match",
			`{"matrix": {"a": [1], "exclude": [{"a": 2}]}}`,
			[]string{`a=1 | job (1)`},
		},
		{
			"booleans, null and numbers in names",
			`{"matrix": {"a": [true, null, 1.5]}}`,
			[]string{`a=true | job (true)`, `a=null | job`, `a=1.5 | job (1.5)`},
		},
		{
			"objects and arrays are not in names",
			`{"matrix": {"v": [{"k": 1}, [1]], "w": ["x"]}}`,
			[]string{`v={"k":1},w="x" | job (x)`, `v=[1],w="x" | job (x)`},
		},
		{
			"exclude by object containment",
			`{"matrix": {"v": [{"k": 1, "m": 2}, {"k": 2}], "exclude": [{"v": {"k": 1}}]}}`,
			[]string{`v={"k":2} | job`},
		},
		{
			"keys are case-insensitive for include matching",
			`{"matrix": {"OS": ["a"], "include": [{"os": "a", "x": 1}]}}`,
			[]string{`OS="a",x=1 | job (a, 1)`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			combos, err := ExpandMatrix(ctxbg, jsonValue(t, tc.strategy))
			if err != nil {
				t.Fatal(err)
			}
			if got := summarize(combos); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}
}

func TestExpandMatrixErrors(t *testing.T) {
	tests := []struct{ name, strategy, want string }{
		{"strategy not a mapping", `[1]`, "the strategy must be a mapping"},
		{"matrix not a mapping", `{"matrix": [1]}`, "the matrix must be a mapping"},
		{"vector empty", `{"matrix": {"a": []}}`, "matrix vector 'a' does not contain any values"},
		{"vector scalar", `{"matrix": {"a": 1}}`, "matrix vector 'a' does not contain any values"},
		{"include not a list", `{"matrix": {"include": 1}}`, "matrix include must be a sequence of mappings"},
		{"include item", `{"matrix": {"include": [1]}}`, "matrix include must be a sequence of mappings"},
		{"exclude item", `{"matrix": {"a": [1], "exclude": ["x"]}}`, "matrix exclude must be a sequence of mappings"},
		{"exclude unknown key", `{"matrix": {"a": [1], "exclude": [{"zzz": 1}]}}`, "Matrix exclude key 'zzz' does not match any key within the matrix"},
		{"exclude include-only key", `{"matrix": {"a": [1], "include": [{"b": 1}], "exclude": [{"b": 1}]}}`, "Matrix exclude key 'b' does not match any key within the matrix"},
		{"exclude without vectors", `{"matrix": {"exclude": [{"a": 1}]}}`, "Matrix exclude key 'a' does not match any key within the matrix"},
		{"too many includes", `{"matrix": {"include": ` + entries(257) + `}}`, "more than 256 entries"},
		{"too many excludes", `{"matrix": {"a": [1], "exclude": ` + entries(257) + `}}`, "more than 256 entries"},
		{"all excluded", `{"matrix": {"a": [1], "exclude": [{"a": 1}]}}`, "all of them are excluded"},
		{"fail-fast", `{"fail-fast": "maybe"}`, "fail-fast"},
		{"max-parallel", `{"max-parallel": 0}`, "max-parallel"},
		{"max-parallel fraction", `{"max-parallel": 1.5}`, "max-parallel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExpandMatrix(ctxbg, jsonValue(t, tc.strategy))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestMatrixLimit(t *testing.T) {
	vec := func(n int) string {
		var items []string
		for i := 0; i < n; i++ {
			items = append(items, fmt.Sprint(i))
		}
		return "[" + strings.Join(items, ",") + "]"
	}
	if c, err := ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"a": `+vec(16)+`, "b": `+vec(16)+`}}`)); err != nil || len(c) != 256 {
		t.Errorf("256 jobs: %d, %v", len(c), err)
	}
	_, err := ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"a": `+vec(16)+`, "b": `+vec(16)+`, "include": [{"a": 99}]}}`))
	if err == nil || !strings.Contains(err.Error(), "Strategy produced 257 configurations which exceeds the maximum of 256 configurations") {
		t.Errorf("257 jobs: %v", err)
	}
	_, err = ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"a": `+vec(17)+`, "b": `+vec(16)+`}}`))
	if err == nil {
		t.Error("272 jobs: no error")
	}
	// Excluding brings it back under the limit.
	if c, err := ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"a": `+vec(17)+`, "b": `+vec(16)+`, "exclude": [{"a": 16}]}}`)); err != nil || len(c) != 256 {
		t.Errorf("272 minus 16: %d, %v", len(c), err)
	}
	// A huge product fails fast.
	_, err = ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"a": `+vec(200)+`, "b": `+vec(200)+`, "c": `+vec(200)+`, "d": `+vec(200)+`}}`))
	if err == nil {
		t.Error("huge product: no error")
	}
}

func TestStrategyOptions(t *testing.T) {
	s, err := ParseStrategy(ctxbg, jsonValue(t, `{"fail-fast": "false", "max-parallel": 2, "matrix": {"a": [1, 2, 3]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.FailFast || s.MaxParallel != 2 || !s.HasMatrix || len(s.Combos) != 3 {
		t.Errorf("%+v", s)
	}
	d := s.Context(1)
	if got := expr.ToJSON(d); got != "{\n  \"fail-fast\": false,\n  \"job-index\": 1,\n  \"job-total\": 3,\n  \"max-parallel\": 2\n}" {
		t.Errorf("context = %s", got)
	}
	s, err = ParseStrategy(ctxbg, nil)
	if err != nil || !s.FailFast || s.MaxParallel != 0 || s.HasMatrix || len(s.Combos) != 1 {
		t.Errorf("default: %+v, %v", s, err)
	}
	if got := expr.ToJSON(s.Context(0)); !strings.Contains(got, `"max-parallel": 1`) {
		t.Errorf("default context = %s", got)
	}
}

func jsonValue2(s string) expr.Value {
	v, err := expr.FromJSON([]byte(s))
	if err != nil {
		return nil
	}
	return v
}

func entries(n int) string {
	var items []string
	for i := 0; i < n; i++ {
		items = append(items, `{"a": 1}`)
	}
	return "[" + strings.Join(items, ",") + "]"
}

func TestMatrixEntryLimitsAtTheEdge(t *testing.T) {
	if _, err := ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"a": [1], "exclude": `+strings.ReplaceAll(entries(256), `"a": 1`, `"a": 9`)+`}}`)); err != nil {
		t.Errorf("256 excludes: %v", err)
	}
	if c, err := ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"a": [1], "include": `+entries(256)+`}}`)); err != nil || len(c) != 1 {
		t.Errorf("256 includes: %d, %v", len(c), err)
	}
}

// TestHostileMatrixFailsFast is the probe of the review: many vectors whose
// product is huge and whose exclusions hide the 256-job limit used to run for
// close to a minute.
func TestHostileMatrixFailsFast(t *testing.T) {
	vec := func(n int) string {
		var items []string
		for i := 0; i < n; i++ {
			items = append(items, fmt.Sprint(i))
		}
		return "[" + strings.Join(items, ",") + "]"
	}
	src := `{"matrix": {"a": ` + vec(100) + `, "b": ` + vec(100) + `, "c": ` + vec(100) + `, "exclude": ` + strings.ReplaceAll(entries(256), `"a": 1`, `"c": 5`) + `}}`
	start := time.Now()
	_, err := ExpandMatrix(ctxbg, jsonValue(t, src))
	if err == nil || !strings.Contains(err.Error(), "matrix too large") {
		t.Errorf("err = %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
	// Just under the product cap, with nothing to stop the loop early, ends.
	var parts []string
	for _, k := range []string{"a", "b", "c", "d"} {
		parts = append(parts, `"`+k+`": `+vec(16))
	}
	start = time.Now()
	_, err = ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {`+strings.Join(parts, ",")+`, "exclude": [{"a": 0}, {"a": 1}, {"a": 2}, {"a": 3}, {"a": 4}, {"a": 5}, {"a": 6}, {"a": 7}, {"a": 8}, {"a": 9}, {"a": 10}, {"a": 11}, {"a": 12}, {"a": 13}, {"a": 14}]}}`))
	if err == nil {
		t.Error("65536 combinations of which 4096 remain: no error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestMatrixProductCap(t *testing.T) {
	vec := func(n int) string {
		var items []string
		for i := 0; i < n; i++ {
			items = append(items, fmt.Sprint(i))
		}
		return "[" + strings.Join(items, ",") + "]"
	}
	// 2^16 is allowed by the cap (and fails on the job limit), 2^16+1 is refused outright.
	_, err := ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"a": `+vec(MaxMatrixProduct/2)+`, "b": `+vec(2)+`, "c": `+vec(1)+`}}`))
	var limit *configurationLimitError
	if !errors.As(err, &limit) || limit.Count != MaxMatrixProduct {
		t.Errorf("err = %v", err)
	}
	if !errors.Is(err, ErrTooManyConfigurations) {
		t.Errorf("errors.Is(%v, ErrTooManyConfigurations) = false", err)
	}
	_, err = ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"a": `+vec(MaxMatrixProduct+1)+`}}`))
	if err == nil || !strings.Contains(err.Error(), "matrix too large") {
		t.Errorf("one huge vector: %v", err)
	}
}

func TestMatrixContextIsHonoured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// 3000 combinations, nearly all excluded, so the job limit does not stop the loop.
	var items []string
	for i := 0; i < 3000; i++ {
		items = append(items, fmt.Sprint(i))
	}
	_, err := ExpandMatrix(ctx, jsonValue(t, `{"matrix": {"a": [`+strings.Join(items, ",")+`], "exclude": [{"a": 1}]}}`))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	_, err = ParseStrategy(ctx, jsonValue(t, `{"matrix": {"a": [`+strings.Join(items, ",")+`]}}`))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("ParseStrategy err = %v", err)
	}
}

// TestRecordedMatrixNames follows the job names GitHub gave in the scenarios
// matrix-edge-values (b-null-values.yml) and matrix-job-name.
func TestRecordedMatrixNames(t *testing.T) {
	names := func(matrix string) []string {
		t.Helper()
		combos, err := ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": `+matrix+`}`))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range combos {
			out = append(out, c.DisplayName("default-name"))
		}
		return out
	}
	// v: [1, null, '', true, 1.50, 'x y'] gave default-name, default-name,
	// default-name (1), default-name (1.5), default-name (true) and
	// default-name (x y): null and '' add nothing, 1.50 is 1.5.
	want := []string{"default-name (1)", "default-name", "default-name", "default-name (true)", "default-name (1.5)", "default-name (x y)"}
	if got := names(`{"v": [1, null, "", true, 1.50, "x y"]}`); !reflect.DeepEqual(got, want) {
		t.Errorf("one axis: got %q, want %q", got, want)
	}
	// a: [x, null], b: ['', y] gave "two-axes (x)", "two-axes (x, y)",
	// "two-axes (y)" and "two-axes".
	want = []string{"default-name (x)", "default-name (x, y)", "default-name", "default-name (y)"}
	if got := names(`{"a": ["x", null], "b": ["", "y"]}`); !reflect.DeepEqual(got, want) {
		t.Errorf("two axes: got %q, want %q", got, want)
	}
	// matrix-job-name: os [a, b] x n [1, 2] gave "default-name (a, 1)" ...
	want = []string{"default-name (a, 1)", "default-name (a, 2)", "default-name (b, 1)", "default-name (b, 2)"}
	if got := names(`{"os": ["a", "b"], "n": [1, 2]}`); !reflect.DeepEqual(got, want) {
		t.Errorf("matrix-job-name: got %q, want %q", got, want)
	}
	// Objects are not listed. Assumed, not recorded: matrix-value-types has
	// custom names only.
	if got := names(`{"n": [1], "obj": [{"k": "v"}]}`); !reflect.DeepEqual(got, []string{"default-name (1)"}) {
		t.Errorf("object value: got %q", got)
	}
}

// TestRecordedExcludeUnknownKey is matrix-edge-values a-exclude-unknown-key.yml:
// exclude: [{nokey: x}, {os: one, nokey: y}] made the workflow invalid. The
// recording holds only the failed run; the message, once per offending entry,
// was observed on the github.com run page.
func TestRecordedExcludeUnknownKey(t *testing.T) {
	_, err := ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"os": ["one", "two"], "exclude": [{"nokey": "x"}, {"os": "one", "nokey": "y"}]}}`))
	const one = "Matrix exclude key 'nokey' does not match any key within the matrix"
	if err == nil || err.Error() != one+"\n"+one {
		t.Fatalf("err = %v, want the message once per offending entry", err)
	}
	// An entry that is fine does not add one.
	_, err = ExpandMatrix(ctxbg, jsonValue(t, `{"matrix": {"os": ["one", "two"], "exclude": [{"os": "one"}, {"nokey": "x"}]}}`))
	if err == nil || err.Error() != one {
		t.Errorf("err = %v, want %q", err, one)
	}
}

// TestRecordedMatrixOver256 is the scenario matrix-over-256: 17 x 16 = 272
// configurations in job too-many made the workflow invalid. The recording
// holds only the failed run; the message was observed on the github.com run
// page.
func TestRecordedMatrixOver256(t *testing.T) {
	w, errs := Parse("ci.yml", []byte(`on: push
jobs:
  too-many:
    runs-on: x
    strategy:
      matrix:
        a: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17]
        b: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]
    steps: [{run: a}]
`))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	_, err := w.Job("too-many").EvaluateStrategy(ctxbg, template.Env{})
	const want = "Strategy for job 'too-many' produced 272 configurations which exceeds the maximum of 256 configurations"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	var limit *configurationLimitError
	if !errors.As(err, &limit) || limit.Job != "too-many" || limit.Count != 272 {
		t.Errorf("err = %#v", err)
	}
	if !errors.Is(err, ErrTooManyConfigurations) {
		t.Errorf("errors.Is(%v, ErrTooManyConfigurations) = false", err)
	}
}

func TestMatrixEnvBounds(t *testing.T) {
	s, _ := ParseStrategy(ctxbg, jsonValue(t, `{"matrix": {"a": [1, 2]}}`))
	for _, i := range []int{-1, 2, 99} {
		if _, err := s.MatrixEnv(template.Env{}, i); err == nil {
			t.Errorf("MatrixEnv(%d): no error", i)
		}
	}
	j := &Job{Name: template.NewString(template.Origin{}, "n")}
	if _, err := j.DisplayName(ctxbg, template.Env{}, s, 5); err == nil {
		t.Error("DisplayName out of range: no error")
	}
}
