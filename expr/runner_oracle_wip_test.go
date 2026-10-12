//go:build wip

package expr

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// runnerResidual lists, by reason, the corpus expressions on which this package
// knowingly differs from the runner. It is filled by classifyResidual.
func classifyResidual(g, r runnerCase) string {
	return ""
}

func TestRunnerOracle(t *testing.T) {
	of, ctxValues, ctxNames := loadOracle(t)
	rf := loadRunner(t)
	if len(rf.Cases) != len(of.Cases) {
		t.Fatalf("runner answers %d cases, oracle has %d", len(rf.Cases), len(of.Cases))
	}
	t.Logf("runner: %s @ %s, %s", rf.Reference.Source, rf.Reference.Commit, rf.Reference.Runtime)
	ec := EvalContext{Contexts: ctxValues, Funcs: oracleFuncs, MaxResultBytes: -1}
	residual := map[string]int{}
	failures := 0
	for i, rc := range rf.Cases {
		if rc.E != of.Cases[i].E {
			t.Fatalf("case %d: runner expression %q, oracle %q", i, rc.E, of.Cases[i].E)
		}
		g := goAnswer(rc.E, ctxNames, ec)
		if sameAnswer(g, rc) {
			continue
		}
		if reason := classifyResidual(g, rc); reason != "" {
			residual[reason]++
			continue
		}
		failures++
		if p := os.Getenv("WIP_OUT"); p != "" {
			f, _ := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			b1, _ := json.Marshal(g)
			b2, _ := json.Marshal(rc)
			_, _ = f.WriteString(string(b1) + "\n" + string(b2) + "\n")
			_ = f.Close()
		}
		if failures <= 30 {
			gj, _ := json.Marshal(g)
			rj, _ := json.Marshal(rc)
			t.Errorf("%q:\n  go     %s\n  runner %s", clip(rc.E), gj, rj)
		}
	}
	reasons := make([]string, 0, len(residual))
	for k := range residual {
		reasons = append(reasons, k)
	}
	sort.Strings(reasons)
	for _, k := range reasons {
		t.Logf("known difference from the runner (%d cases): %s", residual[k], k)
	}
	t.Logf("%d cases, %d differences from the runner", len(rf.Cases), failures)
	if failures > 0 {
		t.Fatalf("%d mismatches with the runner", failures)
	}
}
