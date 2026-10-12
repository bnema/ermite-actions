// SPDX-License-Identifier: MIT

package expr

import (
	"context"
	"strings"
	"testing"
)

// Ports of the expression-semantic L0 tests of actions/runner
// (commit 5f0b3ddcc6ee9fe9441e00d55e85b94ea4aad798, MIT, GitHub, Inc.):
//
//	src/Test/L0/Sdk/ExpressionParserL0.cs
//	src/Test/L0/Worker/Expressions/ConditionFunctionsL0.cs

// TestRunnerExpressionParserL0 ports ExpressionParserL0: the parser must
// reject names that were not declared (the regression was a positional
// argument that turned on "allow unknown keywords" for every parse).
func TestRunnerExpressionParserL0(t *testing.T) {
	tests := []struct {
		name     string // the C# test method
		src      string
		contexts []string
		wantErr  string // substring of the error; empty means success
	}{
		{"CreateTree_RejectsUnrecognizedNamedValue", "github.event.repository.private", []string{"inputs"}, "Unrecognized named-value"},
		{"CreateTree_AcceptsRecognizedNamedValue", "inputs.foo", []string{"inputs"}, ""},
		{"CreateTree_CaseFunctionWorks", "case(github.event_name, 'push', 'Push Event')", []string{"github"}, ""},
		{"CreateTree_CaseFunctionDoesNotAffectUnknownKeywords", "github.ref", []string{"inputs"}, "Unrecognized named-value"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			node, err := Parse(tc.src, tc.contexts, nil)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Parse(%q) error = %v, want one containing %q", tc.src, err, tc.wantErr)
				}
				return
			}
			if err != nil || node == nil {
				t.Fatalf("Parse(%q) = %v, %v; want a tree", tc.src, node, err)
			}
		})
	}
}

// Job and action results, as the runner's ActionResult enum names them.
const (
	resNone      = ""
	resSuccess   = "Success"
	resFailure   = "Failure"
	resCancelled = "Cancelled"
)

// statusFuncs mirrors src/Runner.Worker/Expressions/{Always,Cancelled,Failure,
// Success}Function.cs. A missing job status counts as Success. In a composite
// action's main step (isEmbedded and main stage) failure() and success() read
// the action status instead of the job status; cancelled() and always() never do.
func statusFuncs(jobStatus, actionStatus string, compositeMain bool) (map[string]Func, []FuncInfo) {
	orSuccess := func(s string) string {
		if s == resNone {
			return resSuccess
		}
		return s
	}
	status := func() string {
		if compositeMain {
			return orSuccess(actionStatus)
		}
		return orSuccess(jobStatus)
	}
	fn := func(f func() bool) Func {
		return func(context.Context, []Value) (Value, error) { return Boolean(f()), nil }
	}
	funcs := map[string]Func{
		"always":    fn(func() bool { return true }),
		"cancelled": fn(func() bool { return orSuccess(jobStatus) == resCancelled }),
		"failure":   fn(func() bool { return status() == resFailure }),
		"success":   fn(func() bool { return status() == resSuccess }),
	}
	var infos []FuncInfo
	for _, n := range []string{"always", "cancelled", "failure", "success"} {
		infos = append(infos, FuncInfo{Name: n, MinArgs: 0, MaxArgs: 0})
	}
	return funcs, infos
}

func evalCondition(t *testing.T, src, jobStatus, actionStatus string, compositeMain bool) bool {
	t.Helper()
	funcs, infos := statusFuncs(jobStatus, actionStatus, compositeMain)
	node, err := Parse(src, nil, infos)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	v, err := Evaluate(context.Background(), node, EvalContext{Funcs: funcs})
	if err != nil {
		t.Fatalf("Evaluate(%q): %v", src, err)
	}
	return Truthy(v)
}

// TestRunnerConditionFunctionsL0 ports AlwaysFunction, CancelledFunction,
// FailureFunction and SuccessFunction of ConditionFunctionsL0, and the two
// composite-action theories. The functions themselves live in the runner; what
// is checked here is that package expr carries zero-argument extension
// functions the way the runner declares them (FunctionInfo(name, 0, 0)).
func TestRunnerConditionFunctionsL0(t *testing.T) {
	jobStatuses := []string{resNone, resCancelled, resFailure, resSuccess}
	want := map[string]map[string]bool{
		"always":    {resNone: true, resCancelled: true, resFailure: true, resSuccess: true},
		"cancelled": {resNone: false, resCancelled: true, resFailure: false, resSuccess: false},
		"failure":   {resNone: false, resCancelled: false, resFailure: true, resSuccess: false},
		"success":   {resNone: true, resCancelled: false, resFailure: false, resSuccess: true},
	}
	for name, byStatus := range want {
		for _, js := range jobStatuses {
			if got := evalCondition(t, name+"()", js, "", false); got != byStatus[js] {
				t.Errorf("%s() with job status %q = %v, want %v", name, js, got, byStatus[js])
			}
			// Names are case-insensitive, as in the runner's dictionaries.
			if got := evalCondition(t, strings.ToUpper(name)+"()", js, "", false); got != byStatus[js] {
				t.Errorf("%s() with job status %q = %v, want %v", strings.ToUpper(name), js, got, byStatus[js])
			}
		}
	}

	composite := []struct {
		fn          string
		job, action string
		want        bool
	}{
		{"failure", resFailure, resFailure, true},
		{"failure", resFailure, resSuccess, false},
		{"failure", resSuccess, resFailure, true},
		{"failure", resSuccess, resSuccess, false},
		{"failure", resSuccess, resNone, false},
		{"success", resFailure, resFailure, false},
		{"success", resFailure, resSuccess, true},
		{"success", resSuccess, resFailure, false},
		{"success", resSuccess, resSuccess, true},
		{"success", resSuccess, resNone, true},
	}
	for _, tc := range composite {
		if got := evalCondition(t, tc.fn+"()", tc.job, tc.action, true); got != tc.want {
			t.Errorf("%s() in a composite main step, job %q action %q = %v, want %v", tc.fn, tc.job, tc.action, got, tc.want)
		}
	}

	// Arity is declared 0..0: the parser rejects arguments.
	_, infos := statusFuncs("", "", false)
	if _, err := Parse("success(1)", nil, infos); err == nil || !strings.Contains(err.Error(), "Too many parameters supplied") {
		t.Errorf("success(1) = %v, want Too many parameters supplied", err)
	}
	// A condition combining them: runner conditions are ordinary expressions.
	if !evalCondition(t, "failure() || cancelled()", resCancelled, "", false) || evalCondition(t, "!always()", resFailure, "", false) {
		t.Error("combined status conditions")
	}
}
