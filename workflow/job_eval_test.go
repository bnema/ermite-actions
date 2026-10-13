// SPDX-License-Identifier: MIT

package workflow

import (
	"reflect"
	"strings"
	"testing"
)

func TestEvaluateRunsOn(t *testing.T) {
	w := mustParse(t, `on: push
jobs:
  s:
    runs-on: ubuntu-latest
    steps: [{run: a}]
  l:
    runs-on: [self-hosted, "${{ matrix.arch }}"]
    steps: [{run: a}]
  m:
    runs-on:
      group: org/big
      labels: ${{ fromJSON('["a","b"]') }}
    steps: [{run: a}]
  e:
    runs-on: ${{ matrix.os }}
    steps: [{run: a}]
  g:
    runs-on:
      group: ${{ matrix.g }}
    steps: [{run: a}]
  r:
    uses: ./x.yml
`)
	env := envWith(t, map[string]string{"matrix": `{"arch": "arm64", "os": "linux", "g": "bogus/x", "n": 1}`})
	tests := []struct {
		job, labels, group, err string
	}{
		{"s", "ubuntu-latest", "", ""},
		{"l", "self-hosted,arm64", "", ""},
		{"m", "a,b", "org/big", ""},
		{"e", "linux", "", ""},
		{"g", "", "", "Invalid runs-on group name 'bogus/x. Please use"},
		{"r", "", "", "has no runs-on"},
	}
	for _, tc := range tests {
		labels, group, err := w.Job(tc.job).EvaluateRunsOn(ctxbg, env)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: err = %v, want %q", tc.job, err, tc.err)
			}
			continue
		}
		if err != nil || strings.Join(labels, ",") != tc.labels || group != tc.group {
			t.Errorf("%s: %q, %q, %v", tc.job, labels, group, err)
		}
	}
	// Non-string results.
	bad := mustParse(t, "on: push\njobs:\n  a:\n    runs-on: ${{ matrix.n }}\n    steps: [{run: a}]\n  b:\n    runs-on: [\"${{ matrix.n }}\"]\n    steps: [{run: a}]\n")
	for _, id := range []string{"a", "b"} {
		_, _, err := bad.Job(id).EvaluateRunsOn(ctxbg, env)
		if err == nil || !strings.Contains(err.Error(), "Unexpected type") {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}

func TestEvaluateIf(t *testing.T) {
	w := mustParse(t, `on: push
jobs:
  a:
    runs-on: x
    steps: [{run: a}]
  b:
    needs: a
    if: needs.a.result == 'failure'
    runs-on: x
    steps:
      - run: a
        if: failure()
      - run: b
      - run: c
        if: always()
  c:
    needs: a
    if: ${{ always() && !cancelled() }}
    runs-on: x
    steps: [{run: a}]
`)
	env := envWith(t, map[string]string{"needs": `{"a": {"result": "failure"}}`})
	b := w.Job("b")
	tests := []struct {
		name string
		got  func() (bool, error)
		want bool
	}{
		{"job, success", func() (bool, error) { return b.EvaluateIf(ctxbg, env, StatusFuncs{Success: true}) }, true},
		{"job, after failure", func() (bool, error) { return b.EvaluateIf(ctxbg, env, StatusFuncs{Failure: true}) }, false},
		{"step failure()", func() (bool, error) { return b.Steps[0].EvaluateIf(ctxbg, env, StatusFuncs{Failure: true}) }, true},
		{"step failure() when ok", func() (bool, error) { return b.Steps[0].EvaluateIf(ctxbg, env, StatusFuncs{Success: true}) }, false},
		{"step default", func() (bool, error) { return b.Steps[1].EvaluateIf(ctxbg, env, StatusFuncs{Success: true}) }, true},
		{"step default after failure", func() (bool, error) { return b.Steps[1].EvaluateIf(ctxbg, env, StatusFuncs{Failure: true}) }, false},
		{"always", func() (bool, error) { return b.Steps[2].EvaluateIf(ctxbg, env, StatusFuncs{Cancelled: true}) }, true},
		{"always and not cancelled", func() (bool, error) { return w.Job("c").EvaluateIf(ctxbg, env, StatusFuncs{Cancelled: true}) }, false},
		{"always and not cancelled, ok", func() (bool, error) { return w.Job("c").EvaluateIf(ctxbg, env, StatusFuncs{Failure: true}) }, true},
	}
	for _, tc := range tests {
		got, err := tc.got()
		if err != nil || got != tc.want {
			t.Errorf("%s = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
	// The caller's functions (hashFiles) are kept, and env is not modified.
	env.Funcs = nil
	if _, err := b.EvaluateIf(ctxbg, env, StatusFuncs{}); err != nil {
		t.Fatal(err)
	}
	if env.Funcs != nil {
		t.Error("env.Funcs was modified")
	}
}

func TestJobLevels(t *testing.T) {
	w := mustParse(t, `on: push
jobs:
  deploy:
    needs: [test, lint]
    runs-on: x
    steps: [{run: a}]
  test:
    needs: build
    runs-on: x
    steps: [{run: a}]
  lint:
    runs-on: x
    steps: [{run: a}]
  build:
    runs-on: x
    steps: [{run: a}]
  docs:
    needs: [build]
    runs-on: x
    steps: [{run: a}]
  notify:
    needs: [deploy, docs]
    runs-on: x
    steps: [{run: a}]
`)
	var got []string
	for _, l := range w.JobLevels() {
		var ids []string
		for _, j := range l {
			ids = append(ids, j.ID)
		}
		got = append(got, strings.Join(ids, ","))
	}
	if want := "lint,build|test,docs|deploy|notify"; strings.Join(got, "|") != want {
		t.Errorf("levels = %v, want %s", got, want)
	}
	// Stable across calls; a hand-built cycle is left out, not looped on.
	cyc := &Workflow{Jobs: []*Job{{ID: "a", Needs: []string{"b"}}, {ID: "b", Needs: []string{"a"}}, {ID: "c"}, {ID: "d", Needs: []string{"zz"}}}}
	l := cyc.JobLevels()
	if len(l) != 1 || len(l[0]) != 1 || l[0][0].ID != "c" {
		t.Errorf("cycle levels = %v", l)
	}
	if len((&Workflow{}).JobLevels()) != 0 {
		t.Error("empty workflow has levels")
	}
}

func TestDisplayNameAstWalk(t *testing.T) {
	// The word "matrix." in a string literal or another context is not a use
	// of the matrix context.
	w := mustParse(t, `on: push
jobs:
  a:
    name: "${{ github.event.matrix }} and matrix. text"
    runs-on: x
    strategy:
      matrix:
        os: [l]
    steps: [{run: a}]
  b:
    name: ${{ format('{0}', matrix.os) }}
    runs-on: x
    strategy:
      matrix:
        os: [l]
    steps: [{run: a}]
`)
	env := envWith(t, map[string]string{"github": `{"event": {"matrix": "m"}}`})
	for id, want := range map[string]string{"a": "m and matrix. text (l)", "b": "l"} {
		j := w.Job(id)
		s, err := j.EvaluateStrategy(ctxbg, env)
		if err != nil {
			t.Fatal(err)
		}
		got, err := j.DisplayName(ctxbg, mustMatrixEnv(t, s, env, 0), s, 0)
		if err != nil || got != want {
			t.Errorf("%s: name = %q, %v; want %q", id, got, err, want)
		}
	}
}

// needsFailedDependency is the workflow of the recorded scenario of that name.
const needsFailedDependency = `on:
  push:
    branches: ['scn/**']
jobs:
  broken:
    runs-on: ubuntu-24.04
    steps:
      - run: exit 1
  plain:
    needs: broken
    runs-on: ubuntu-24.04
    steps:
      - run: echo SHOULD-NOT-RUN
  with-always:
    needs: broken
    if: always()
    runs-on: ubuntu-24.04
    steps:
      - run: echo
  chain-after-always:
    needs: with-always
    runs-on: ubuntu-24.04
    steps:
      - run: echo chain-after-always-runs-because-with-always-succeeded
  diamond:
    needs: [chain-after-always, plain, with-always]
    runs-on: ubuntu-24.04
    steps:
      - run: echo
`

// TestAncestors checks the transitive needs that GitHub's default success()
// looks at (recorded: needs-failed-dependency, chain-after-always was skipped).
func TestAncestors(t *testing.T) {
	w, errs := Parse("w.yml", []byte(needsFailedDependency))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	for id, want := range map[string][]string{
		"broken":             {},
		"plain":              {"broken"},
		"with-always":        {"broken"},
		"chain-after-always": {"broken", "with-always"},
		"diamond":            {"broken", "plain", "with-always", "chain-after-always"},
		"nowhere":            nil,
	} {
		if got := w.Ancestors(id); !reflect.DeepEqual(got, want) {
			t.Errorf("Ancestors(%q) = %q, want %q", id, got, want)
		}
	}
}
