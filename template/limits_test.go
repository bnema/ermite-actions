// SPDX-License-Identifier: MIT

package template

import (
	"math"
	"strings"
	"testing"
)

// The options of Context that the C# runner's loader needs (package action):
// nesting limit, error limit and the runner's YAML restrictions.

func readWith(t *testing.T, set func(*Context), src string) []string {
	t.Helper()
	schema, err := WorkflowSchema()
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewContext(schema)
	set(ctx)
	ReadYAML(ctx, "workflow-root-strict", "f.yml", []byte(src))
	return ctx.Messages()
}

func TestMaxDepth(t *testing.T) {
	nest := func(n int) string { return "x: " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n" }
	set := func(c *Context) { c.MaxDepth = 10 }
	// Like TemplateMemory.IncrementDepth: the root mapping is level one, so
	// nine brackets fit and ten do not. The unexpected key "x" is skipped, and
	// skipping counts levels too.
	got := readWith(t, set, nest(9))
	for _, m := range got {
		if strings.Contains(m, "depth") {
			t.Errorf("9 brackets: %q", got)
		}
	}
	got = readWith(t, set, nest(10))
	if len(got) == 0 || got[len(got)-1] != "f.yml: Maximum object depth exceeded" {
		t.Errorf("10 brackets: %q", got)
	}
	// No limit by default.
	for _, m := range readWith(t, func(*Context) {}, nest(500)) {
		if strings.Contains(m, "depth") {
			t.Errorf("default limit: %q", m)
		}
	}
}

func TestErrorLimit(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString("k" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ": 1\n")
	}
	if got := readWith(t, func(c *Context) { c.ErrorLimit = 10 }, b.String()); len(got) != 10 {
		t.Errorf("%d errors with a limit of 10", len(got))
	}
	if got := readWith(t, func(*Context) {}, b.String()); len(got) < 30 {
		t.Errorf("%d errors without a limit", len(got))
	}
}

func TestRunnerYAML(t *testing.T) {
	runner := func(c *Context) { c.RunnerYAML = true }
	tests := []struct {
		name, src, want string
	}{
		{"anchor", "on: &a push\n", "f.yml: Anchors are not currently supported. Remove the anchor 'a'"},
		{"alias", "a: &a [1]\nb: *a\n", "f.yml: Anchors are not currently supported. Remove the anchor 'a'"},
		{"!!str is unknown to the runner", "on: !!str push\n", "f.yml: Unexpected tag 'tag:yaml.org,2002:str'"},
		{"!!int on a string", "on: !!int push\n", "f.yml: The value 'push' on line 1 and column 5 is invalid for the type 'tag:yaml.org,2002:int'"},
		{"tag on a quoted scalar", "on: !!int '1'\n", "f.yml: The scalar style 'SingleQuoted' on line 1 and column 5 is not valid with the tag 'tag:yaml.org,2002:int'"},
		{"second document", "on: push\n---\non: pull_request\n", "f.yml: Expected stream end parse event"},
		{"empty", "", "f.yml: Expected document start parse event"},
		{"collection key", "? [a]\n: b\n", "f.yml: Expected mapping end"},
	}
	for _, tc := range tests {
		got := readWith(t, runner, tc.src)
		found := false
		for _, m := range got {
			found = found || m == tc.want
		}
		if !found {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
		// None of this is an error without the option (except an empty file
		// and a collection key, which have other messages).
	}
	for _, src := range []string{"on: &a push\n", "on: !!str push\n", "on: !!int 1\n"} {
		for _, m := range readWith(t, func(*Context) {}, src) {
			if strings.Contains(m, "Anchors") || strings.Contains(m, "Unexpected tag") {
				t.Errorf("%q without RunnerYAML: %q", src, m)
			}
		}
	}
	// A valid tag passes.
	for _, src := range []string{"on: !!null\n", "on: !!int 12\n", "on: !!float 1.5\n", "on: !!bool true\n", "on: !!int 0x1F\n"} {
		for _, m := range readWith(t, runner, src) {
			if strings.Contains(m, "tag") || strings.Contains(m, "invalid for the type") {
				t.Errorf("%q: %q", src, m)
			}
		}
	}
}

func TestExceptionError(t *testing.T) {
	schema, _ := WorkflowSchema()
	ctx := NewContext(schema)
	ctx.ErrorException("System.ArgumentNullException", "msg")
	ctx.ErrorException("System.ArgumentOutOfRangeException", "msg")
	ctx.ErrorException("System.NotSupportedException", "msg")
	want := []string{
		"System.ArgumentNullException: Value cannot be null. (Parameter 'msg')",
		"System.ArgumentOutOfRangeException: Specified argument was out of the range of valid values. (Parameter 'msg')",
		"System.NotSupportedException: msg",
	}
	got := ctx.Messages()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q", got)
	}
}

func TestDotNetNumberString(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{0, "0"}, {1, "1"}, {1.5, "1.5"}, {-5, "-5"}, {0.1, "0.1"}, {0.30000000000000004, "0.3"},
		{100000, "100000"}, {1e14, "100000000000000"}, {1e15, "1E+15"}, {123456789012345678, "1.23456789012346E+17"},
		{1e20, "1E+20"}, {1e21, "1E+21"}, {0.0001, "0.0001"}, {0.00001, "1E-05"}, {1e-6, "1E-06"}, {1.5e-7, "1.5E-07"},
		{12345.6789, "12345.6789"}, {999999999999999, "999999999999999"}, {9999999999999999, "1E+16"},
	} {
		if got := DotNetNumberString(tc.in); got != tc.want {
			t.Errorf("DotNetNumberString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := DotNetNumberString(math.Copysign(0, -1)); got != "-0" {
		t.Errorf("negative zero: %q", got)
	}
	if DotNetNumberString(math.Inf(1)) != "Infinity" || DotNetNumberString(math.Inf(-1)) != "-Infinity" || DotNetNumberString(math.NaN()) != "NaN" {
		t.Error("non-finite numbers")
	}
}
