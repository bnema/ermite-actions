// SPDX-License-Identifier: MIT

package expr

import "testing"

// mustJSON parses JSON text into a Value or fails the test.
func mustJSON(tb testing.TB, s string) Value {
	tb.Helper()
	v, err := FromJSON([]byte(s))
	if err != nil {
		tb.Fatalf("FromJSON(%q): %v", s, err)
	}
	return v
}

// testContexts is the environment of the table tests.
func testContexts(tb testing.TB) map[string]Value {
	tb.Helper()
	return map[string]Value{
		"github": mustJSON(tb, `{
			"event_name": "push",
			"ref": "refs/heads/main",
			"actor": "Octocat",
			"run_number": 7,
			"event": {
				"number": 12,
				"commits": [
					{"id": "1", "message": "fix: bug", "author": {"name": "Ann"}, "added": ["a.txt"]},
					{"id": "2", "message": "feat: thing", "author": {"name": "Bob"}, "added": []},
					{"id": "3", "message": "Chore", "added": ["b", "c"]}
				],
				"issue": {"labels": [{"name": "bug"}, {"name": "Docs"}]},
				"nested": [[1, 2], [3], [], [4, 5, 6]]
			}
		}`),
		"env":    mustJSON(tb, `{"FOO": "bar", "EMPTY": "", "NUM": "42", "Mixed": "MiXeD"}`),
		"matrix": mustJSON(tb, `{"os": "ubuntu", "node": [14, 16, 18], "nothing": null, "ver": "3.10", "obj": {"k": "v"}}`),
		"inputs": mustJSON(tb, `{"name": "World", "count": 3, "deploy": false, "empty": {}}`),
		"steps":  mustJSON(tb, `{"10": {"outputs": {"r": "ten"}}, "2": {"outputs": {"r": "two"}}}`),
	}
}

var testContextNames = []string{"github", "env", "matrix", "inputs", "steps"}

// evalString parses and evaluates src against the test contexts and returns
// the result as "kind:string" or the error text prefixed by "ERR ".
func evalString(tb testing.TB, src string) string {
	tb.Helper()
	node, err := Parse(src, testContextNames, nil)
	if err != nil {
		return "PARSE " + err.Error()
	}
	v, err := Evaluate(node, EvalContext{Contexts: testContexts(tb)})
	if err != nil {
		return "ERR " + err.Error()
	}
	return v.Kind().String() + ":" + CoerceString(v)
}
