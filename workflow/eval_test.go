// SPDX-License-Identifier: MIT

package workflow

import (
	"context"
	"strings"
	"testing"

	"bnema.dev/ermite-actions/expr"
	"bnema.dev/ermite-actions/template"
)

func envWith(t testing.TB, contexts map[string]string) template.Env {
	t.Helper()
	env := template.Env{Contexts: map[string]expr.Value{}}
	for k, v := range contexts {
		env.Contexts[k] = jsonValue(t, v)
	}
	return env
}

func mustParse(t testing.TB, src string) *Workflow {
	t.Helper()
	w, errs := Parse("w.yml", []byte(src))
	if len(errs) > 0 {
		t.Fatalf("Parse: %v", errs)
	}
	return w
}

func TestEvaluateStrategyEndToEnd(t *testing.T) {
	w := mustParse(t, `on: push
jobs:
  build:
    name: Build
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      max-parallel: 2
      matrix:
        os: [ubuntu-latest, macos-latest]
        node: ${{ fromJSON(needs.setup.outputs.versions) }}
        include:
          - os: ubuntu-latest
            extra: ${{ github.ref_name }}
    steps:
      - run: echo
  setup:
    runs-on: x
    steps: [{run: a}]
`)
	env := envWith(t, map[string]string{
		"github": `{"ref_name": "main"}`,
		"needs":  `{"setup": {"outputs": {"versions": "[18, 20]"}}}`,
	})
	job := w.Job("build")
	s, err := job.EvaluateStrategy(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if s.FailFast || s.MaxParallel != 2 || len(s.Combos) != 4 {
		t.Fatalf("strategy = %+v", s)
	}
	names := make([]string, len(s.Combos))
	runsOn := make([]string, len(s.Combos))
	for i := range s.Combos {
		e := s.MatrixEnv(env, i)
		names[i], err = job.DisplayName(context.Background(), e, s, i)
		if err != nil {
			t.Fatal(err)
		}
		runsOn[i], err = template.EvaluateString(context.Background(), job.RunsOn, e)
		if err != nil {
			t.Fatal(err)
		}
	}
	wantNames := []string{
		"Build (ubuntu-latest, 18, main)", "Build (ubuntu-latest, 20, main)",
		"Build (macos-latest, 18)", "Build (macos-latest, 20)",
	}
	if strings.Join(names, "|") != strings.Join(wantNames, "|") {
		t.Errorf("names = %q, want %q", names, wantNames)
	}
	if runsOn[0] != "ubuntu-latest" || runsOn[3] != "macos-latest" {
		t.Errorf("runs-on = %q", runsOn)
	}
	// The strategy context.
	schema, _ := template.WorkflowSchema()
	e := template.NewExpression(template.Origin{}, "format('{0}/{1}', strategy.job-index, strategy.job-total)", schema.Info("runs-on"))
	if got, err := template.EvaluateString(context.Background(), e, s.MatrixEnv(env, 2)); err != nil || got != "2/4" {
		t.Errorf("strategy context = %q, %v", got, err)
	}
}

func TestDisplayNameUsingMatrix(t *testing.T) {
	w := mustParse(t, `on: push
jobs:
  a:
    name: build-${{ matrix.os }}
    runs-on: x
    strategy:
      matrix:
        os: [l, m]
    steps: [{run: a}]
  b:
    runs-on: x
    steps: [{run: a}]
`)
	env := envWith(t, nil)
	j := w.Job("a")
	s, err := j.EvaluateStrategy(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := j.DisplayName(context.Background(), s.MatrixEnv(env, 1), s, 1); got != "build-m" {
		t.Errorf("name = %q", got)
	}
	b := w.Job("b")
	sb, _ := b.EvaluateStrategy(context.Background(), env)
	if got, _ := b.DisplayName(context.Background(), env, sb, 0); got != "b" {
		t.Errorf("name = %q", got)
	}
}

func TestEvaluateStrategyErrors(t *testing.T) {
	w := mustParse(t, `on: push
jobs:
  a:
    runs-on: x
    strategy:
      matrix: ${{ fromJSON(inputs.m) }}
    steps: [{run: a}]
`)
	env := envWith(t, map[string]string{"inputs": `{"m": "not json"}`})
	_, err := w.Job("a").EvaluateStrategy(context.Background(), env)
	if err == nil || !strings.Contains(err.Error(), "w.yml (Line: 6, Col: 15)") {
		t.Errorf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.Job("a").EvaluateStrategy(ctx, envWith(t, map[string]string{"inputs": `{"m": "{}"}`})); err == nil {
		t.Error("cancelled context: no error")
	}
}

func TestJobIfAndStatusFunctions(t *testing.T) {
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
        if: always()
      - run: b
`)
	b := w.Job("b")
	env := envWith(t, map[string]string{"needs": `{"a": {"result": "failure"}}`})
	funcs := map[string]expr.Func{
		"success":   func(context.Context, []expr.Value) (expr.Value, error) { return expr.Boolean(true), nil },
		"always":    func(context.Context, []expr.Value) (expr.Value, error) { return expr.Boolean(true), nil },
		"failure":   func(context.Context, []expr.Value) (expr.Value, error) { return expr.Boolean(false), nil },
		"cancelled": func(context.Context, []expr.Value) (expr.Value, error) { return expr.Boolean(false), nil },
	}
	env.Funcs = funcs
	if b.If.Expression != "success() && (needs.a.result == 'failure')" {
		t.Fatalf("if = %s", b.If.Expression)
	}
	ok, err := template.EvaluateBool(context.Background(), b.If, env)
	if err != nil || !ok {
		t.Errorf("job if = %v, %v", ok, err)
	}
	if b.Steps[0].If.Expression != "always()" || b.Steps[1].If.Expression != "success()" {
		t.Errorf("step ifs = %s, %s", b.Steps[0].If.Expression, b.Steps[1].If.Expression)
	}
}
