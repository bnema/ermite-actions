// SPDX-License-Identifier: MIT

package template

import (
	"context"
	"errors"
	"strings"
	"testing"

	"bnema.dev/ermite-actions/expr"
)

func testEnv(t testing.TB) Env {
	t.Helper()
	gh, err := expr.FromJSON([]byte(`{"ref": "refs/heads/main", "n": 3, "flag": true}`))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := expr.FromJSON([]byte(`{"os": "linux", "extra": {"a": "1", "b": "2"}}`))
	return Env{Contexts: map[string]expr.Value{"github": gh, "matrix": m}}
}

func stepOf(t testing.TB, src string) *Mapping {
	t.Helper()
	tok, errs := readWorkflow(t, head+src)
	if len(errs) > 0 {
		t.Fatalf("read: %q", errs)
	}
	return tok.(*Mapping).Find("jobs").(*Mapping).Find("a").(*Mapping).Find("steps").(*Sequence).Items[0].(*Mapping)
}

func TestEvaluateTokens(t *testing.T) {
	step := stepOf(t, "      - name: n-${{ github.ref }}-${{ matrix.os }}\n        run: echo\n        env:\n          A: ${{ github.n }}\n          B: ${{ github.flag }}\n          C: x\n")
	env := testEnv(t)
	ctx := context.Background()
	name, err := EvaluateString(ctx, step.Find("name"), env)
	if err != nil || name != "n-refs/heads/main-linux" {
		t.Errorf("name = %q, %v", name, err)
	}
	v, err := Evaluate(ctx, step.Find("env"), env)
	if err != nil {
		t.Fatal(err)
	}
	if got := expr.ToJSON(v); got != "{\n  \"A\": 3,\n  \"B\": true,\n  \"C\": \"x\"\n}" {
		t.Errorf("env = %s", got)
	}
	s, err := EvaluateString(ctx, step.Find("env").(*Mapping).Find("A"), env)
	if err != nil || s != "3" {
		t.Errorf("A = %q, %v", s, err)
	}
}

func TestInsertAndExpressionKeys(t *testing.T) {
	tok, errs := readWorkflow(t, head+"      - run: x\n        env:\n          ${{ insert }}: ${{ matrix.extra }}\n          ${{ github.ref }}: v\n          K: w\n")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	env := tok.(*Mapping).Find("jobs").(*Mapping).Find("a").(*Mapping).Find("steps").(*Sequence).Items[0].(*Mapping).Find("env")
	v, err := Evaluate(context.Background(), env, testEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(strings.ReplaceAll(expr.ToJSON(v), "\n", ""), "  ", ""); got != `{"a": "1","b": "2","refs/heads/main": "v","K": "w"}` {
		t.Errorf("env = %s", got)
	}
	// insert needs a mapping.
	bad := NewMapping(Origin{})
	bad.Add(&InsertExpression{}, NewString(Origin{}, "x"))
	if _, err := Evaluate(context.Background(), bad, testEnv(t)); err == nil || !strings.Contains(err.Error(), "insert") {
		t.Errorf("err = %v", err)
	}
}

func TestEvaluateTypedHelpers(t *testing.T) {
	ctx := context.Background()
	env := testEnv(t)
	ex := func(s string) *BasicExpression {
		return NewExpression(Origin{File: "f.yml", Pos: Pos{4, 2}}, s, mustInfo(t, "step-env"))
	}
	if b, err := EvaluateBool(ctx, ex("github.flag"), env); err != nil || !b {
		t.Errorf("bool = %v, %v", b, err)
	}
	if b, err := EvaluateBool(ctx, ex("'FALSE'"), env); err != nil || b {
		t.Errorf("bool string = %v, %v", b, err)
	}
	if _, err := EvaluateBool(ctx, ex("github.n"), env); err == nil || !strings.Contains(err.Error(), "f.yml (Line: 4, Col: 2): Unexpected type 'NumberToken'") {
		t.Errorf("bool number: %v", err)
	}
	if n, err := EvaluateNumber(ctx, ex("github.n"), env); err != nil || n != 3 {
		t.Errorf("number = %v, %v", n, err)
	}
	if n, err := EvaluateNumber(ctx, ex("'12'"), env); err != nil || n != 12 {
		t.Errorf("number string = %v, %v", n, err)
	}
	if _, err := EvaluateNumber(ctx, ex("'x'"), env); err == nil {
		t.Error("number x: no error")
	}
	if _, err := EvaluateString(ctx, ex("matrix.extra"), env); err == nil || !strings.Contains(err.Error(), "MappingToken") {
		t.Errorf("string of mapping: %v", err)
	}
	if s, err := EvaluateString(ctx, ex("null"), env); err != nil || s != "" {
		t.Errorf("string of null = %q, %v", s, err)
	}
}

func mustInfo(t testing.TB, name string) *DefinitionInfo {
	t.Helper()
	s, err := WorkflowSchema()
	if err != nil {
		t.Fatal(err)
	}
	return s.Info(name)
}

func TestExpressionErrors(t *testing.T) {
	ctx := context.Background()
	bad := NewExpression(Origin{File: "f.yml", Pos: Pos{1, 1}}, "github.", mustInfo(t, "step-env"))
	if bad.Err() == nil {
		t.Fatal("parse error expected")
	}
	_, err := Evaluate(ctx, bad, testEnv(t))
	var te *Error
	if !errors.As(err, &te) || !strings.HasPrefix(err.Error(), "f.yml (Line: 1, Col: 1): ") {
		t.Errorf("err = %v", err)
	}
	// Not allowed context: secrets in job-if.
	e := NewExpression(Origin{}, "secrets.x", mustInfo(t, "job-if"))
	if e.Err() == nil || !strings.Contains(e.Err().Error(), "Unrecognized named-value: 'secrets'") {
		t.Errorf("err = %v", e.Err())
	}
	// Evaluation errors wrap the expr error.
	ev := NewExpression(Origin{}, "fromJSON('x')", mustInfo(t, "step-env"))
	_, err = Evaluate(ctx, ev, testEnv(t))
	var ee *expr.EvalError
	if !errors.As(err, &ee) {
		t.Errorf("err = %v (%T)", err, err)
	}
	// Cancellation.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Evaluate(cctx, NewExpression(Origin{}, "github.ref", mustInfo(t, "step-env")), testEnv(t)); err == nil {
		t.Error("cancelled: no error")
	}
	// Stray insert.
	if _, err := Evaluate(ctx, &InsertExpression{}, testEnv(t)); err == nil {
		t.Error("insert alone: no error")
	}
}

func TestEvaluateDepthLimit(t *testing.T) {
	var tok Token = NewString(Origin{}, "x")
	for i := 0; i < MaxEvalDepth+5; i++ {
		tok = NewSequence(Origin{}, tok)
	}
	if _, err := Evaluate(context.Background(), tok, Env{}); err == nil {
		t.Error("deep token: no error")
	}
}

func TestResultBudget(t *testing.T) {
	text := "'xxxxxxxxxxxxxxxx'"
	for i := 0; i < 7; i++ {
		text = "format('{0}{0}{0}{0}', " + text + ")"
	}
	e := NewExpression(Origin{}, text, mustInfo(t, "step-env"))
	if e.Err() != nil {
		t.Fatal(e.Err())
	}
	env := testEnv(t)
	env.MaxResultBytes = 1000
	_, err := Evaluate(context.Background(), e, env)
	var ee *expr.EvalError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v", err)
	}
	env.MaxResultBytes = -1
	if _, err := Evaluate(context.Background(), e, env); err != nil {
		t.Errorf("no limit: %v", err)
	}
}

func TestMatchDirective(t *testing.T) {
	tests := []struct {
		in      string
		match   bool
		wantErr string
	}{
		{"insert", true, ""},
		{"insert ", true, ""},
		{"inserted", false, ""},
		{"insert x", false, "Exactly 0 parameter(s) were expected following the directive 'insert'. Actual parameter count: 1"},
		{"insert x y", false, "Actual parameter count: 1"},
		{"github.ref", false, ""},
	}
	for _, tc := range tests {
		got, err := matchDirective(tc.in, "insert", 0)
		if got != tc.match || (tc.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("matchDirective(%q) = %v, %v", tc.in, got, err)
		}
	}
}

func TestJSNumberString(t *testing.T) {
	for in, want := range map[float64]string{
		0: "0", 1: "1", -1: "-1", 1.5: "1.5", 100: "100", 1e21: "1e+21", 1e-7: "1e-7", 123456789012: "123456789012", 0.000001: "0.000001",
	} {
		if got := JSNumberString(in); got != want {
			t.Errorf("JSNumberString(%v) = %s, want %s", in, got, want)
		}
	}
}
