// SPDX-License-Identifier: MIT

package workflow

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"bnema.dev/ermite-actions/expr"
)

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
			`{"matrix": {"a": [1], "exclude": [{"a": 2}, {"zzz": 1}]}}`,
			[]string{`a=1 | job (1)`},
		},
		{
			"booleans, null and numbers in names",
			`{"matrix": {"a": [true, null, 1.5]}}`,
			[]string{`a=true | job (true)`, `a=null | job ()`, `a=1.5 | job (1.5)`},
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
			combos, err := ExpandMatrix(jsonValue(t, tc.strategy))
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
		{"strategy not a mapping", `[1]`, "The strategy must be a mapping"},
		{"matrix not a mapping", `{"matrix": [1]}`, "The matrix must be a mapping"},
		{"vector empty", `{"matrix": {"a": []}}`, "Matrix vector 'a' does not contain any values"},
		{"vector scalar", `{"matrix": {"a": 1}}`, "Matrix vector 'a' does not contain any values"},
		{"include not a list", `{"matrix": {"include": 1}}`, "Matrix include must be a sequence of mappings"},
		{"include item", `{"matrix": {"include": [1]}}`, "Matrix include must be a sequence of mappings"},
		{"exclude item", `{"matrix": {"a": [1], "exclude": ["x"]}}`, "Matrix exclude must be a sequence of mappings"},
		{"all excluded", `{"matrix": {"a": [1], "exclude": [{"a": 1}]}}`, "all of them are excluded"},
		{"fail-fast", `{"fail-fast": "maybe"}`, "fail-fast"},
		{"max-parallel", `{"max-parallel": 0}`, "max-parallel"},
		{"max-parallel fraction", `{"max-parallel": 1.5}`, "max-parallel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExpandMatrix(jsonValue(t, tc.strategy))
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
	if c, err := ExpandMatrix(jsonValue(t, `{"matrix": {"a": `+vec(16)+`, "b": `+vec(16)+`}}`)); err != nil || len(c) != 256 {
		t.Errorf("256 jobs: %d, %v", len(c), err)
	}
	_, err := ExpandMatrix(jsonValue(t, `{"matrix": {"a": `+vec(16)+`, "b": `+vec(16)+`, "include": [{"a": 99}]}}`))
	if err == nil || !strings.Contains(err.Error(), "Matrix size limit of 256 exceeded") {
		t.Errorf("257 jobs: %v", err)
	}
	_, err = ExpandMatrix(jsonValue(t, `{"matrix": {"a": `+vec(17)+`, "b": `+vec(16)+`}}`))
	if err == nil {
		t.Error("272 jobs: no error")
	}
	// Excluding brings it back under the limit.
	if c, err := ExpandMatrix(jsonValue(t, `{"matrix": {"a": `+vec(17)+`, "b": `+vec(16)+`, "exclude": [{"a": 16}]}}`)); err != nil || len(c) != 256 {
		t.Errorf("272 minus 16: %d, %v", len(c), err)
	}
	// A huge product fails fast.
	_, err = ExpandMatrix(jsonValue(t, `{"matrix": {"a": `+vec(200)+`, "b": `+vec(200)+`, "c": `+vec(200)+`, "d": `+vec(200)+`}}`))
	if err == nil {
		t.Error("huge product: no error")
	}
}

func TestStrategyOptions(t *testing.T) {
	s, err := ParseStrategy(jsonValue(t, `{"fail-fast": "false", "max-parallel": 2, "matrix": {"a": [1, 2, 3]}}`))
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
	s, err = ParseStrategy(nil)
	if err != nil || !s.FailFast || s.MaxParallel != 0 || s.HasMatrix || len(s.Combos) != 1 {
		t.Errorf("default: %+v, %v", s, err)
	}
	if got := expr.ToJSON(s.Context(0)); !strings.Contains(got, `"max-parallel": 1`) {
		t.Errorf("default context = %s", got)
	}
}
