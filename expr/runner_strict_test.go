// SPDX-License-Identifier: MIT

package expr

import (
	"encoding/json"
	"sort"
	"testing"
)

// knownRunnerDifference classifies the answers of the corpus on which this
// package knowingly differs from the runner. It returns the reason, or "".
func knownRunnerDifference(g, r runnerCase) string {
	switch r.E {
	case "-0", "toJSON(-0)":
		// .NET Core formats negative zero as "-0". The runner's newer copy
		// (src/Sdk/Expressions/EvaluationResult.cs) maps it back to "0" and the
		// shared fixtures (coerce_string.json) say "0", so "0" is kept.
		return "negative zero is written 0"
	}
	return ""
}

// TestRunnerOracle holds the package to the answers of the runner's own code on
// the 34 765 expressions of the oracle corpus: same result kind, string form
// and toJSON text, same error phase and kind (the error texts of the runner's
// Json.NET are not reproduced).
func TestRunnerOracle(t *testing.T) {
	of, ctxValues, ctxNames := loadOracle(t)
	rf := loadRunner(t)
	if len(rf.Cases) != len(of.Cases) {
		t.Fatalf("runner answers %d cases, oracle has %d", len(rf.Cases), len(of.Cases))
	}
	t.Logf("runner: %s @ %s, %s", rf.Reference.Source, rf.Reference.Commit, rf.Reference.Runtime)
	ec := EvalContext{Contexts: ctxValues, Funcs: oracleFuncs, MaxResultBytes: -1}
	known := map[string]int{}
	failures := 0
	for i, rc := range rf.Cases {
		if rc.E != of.Cases[i].E {
			t.Fatalf("case %d: runner expression %q, oracle %q", i, rc.E, of.Cases[i].E)
		}
		g := goAnswer(rc.E, ctxNames, ec)
		if sameAnswer(g, rc) {
			continue
		}
		if reason := knownRunnerDifference(g, rc); reason != "" {
			known[reason]++
			continue
		}
		failures++
		if failures <= 30 {
			gj, _ := json.Marshal(g)
			rj, _ := json.Marshal(rc)
			t.Errorf("%q:\n  go     %s\n  runner %s", clip(rc.E), gj, rj)
		}
	}
	reasons := make([]string, 0, len(known))
	for k := range known {
		reasons = append(reasons, k)
	}
	sort.Strings(reasons)
	for _, k := range reasons {
		t.Logf("known difference from the runner (%d cases): %s", known[k], k)
	}
	t.Logf("%d cases, %d failures", len(rf.Cases), failures)
	if failures > 0 {
		t.Fatalf("%d mismatches with the runner", failures)
	}
}
