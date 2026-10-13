// SPDX-License-Identifier: MIT

package action

import (
	"sort"
	"strings"
	"testing"

	"bnema.dev/ermite-actions/template"
)

func mustParse(t *testing.T, src string) *Action {
	t.Helper()
	a, errs := Parse("action.yml", []byte(src))
	if len(errs) > 0 {
		t.Fatalf("errors: %q", errTexts(errs))
	}
	return a
}

func TestModel(t *testing.T) {
	a := mustParse(t, `
name: Say
description: Says things
author: me
inputs:
  Who:
    description: the one
    required: true
    default: world
  old:
    deprecationMessage: use who
  expr:
    default: ${{ github.ref }}
outputs:
  out:
    description: the out
    value: ${{ steps.a.outputs.r }}
branding:
  icon: award
  color: blue
runs:
  using: Node20
  main: dist/index.js
  post: dist/post.js
`)
	if a.Name != "Say" || a.Description != "Says things" || a.Author != "me" {
		t.Errorf("header: %+v", a)
	}
	if len(a.Inputs) != 3 || a.Inputs[0].Name != "Who" || !a.Inputs[0].Required || a.Inputs[0].Default.String() != "world" || a.Inputs[0].Description != "the one" {
		t.Errorf("inputs: %+v", a.Inputs)
	}
	if in := a.Input("who"); in == nil || in.Name != "Who" {
		t.Errorf("Input(who) = %v", in)
	}
	if in := a.Input("nope"); in != nil {
		t.Errorf("Input(nope) = %v", in)
	}
	if !a.Inputs[1].Deprecated() || a.Inputs[1].DeprecationMessage != "use who" || a.Inputs[1].HasDefault || a.Inputs[1].Default.String() != "" {
		t.Errorf("deprecated input: %+v", a.Inputs[1])
	}
	if got := a.Inputs[2].Default.String(); got != "${{ github.ref }}" || !a.Inputs[2].HasDefault {
		t.Errorf("expression default: %q", got)
	}
	if len(a.Outputs) != 1 || a.Outputs[0].Name != "out" || a.Outputs[0].Value.String() != "${{ steps.a.outputs.r }}" || a.Outputs[0].Description != "the out" {
		t.Errorf("outputs: %+v", a.Outputs)
	}
	r := a.Runs
	if r.Kind != RunsNode || r.Using != "node20" || r.Main != "dist/index.js" || r.Post != "dist/post.js" || r.Pre != "" {
		t.Errorf("runs: %+v", r)
	}
	// The runner defaults both conditions to always(), pre or not.
	if r.PreIf != "always()" || r.PostIf != "always()" || r.HasPre() || !r.HasPost() {
		t.Errorf("conditions: %+v", r)
	}
}

func TestRunsConditions(t *testing.T) {
	a := mustParse(t, "runs:\n  using: docker\n  image: Dockerfile\n  pre-entrypoint: p.sh\n  pre-if: runner.os == 'Linux'\n  post-entrypoint: q.sh\n  post-if: failure()\n  entrypoint: e.sh\n  args: [a, '${{ inputs.x }}']\n  env: {K: V}\n")
	r := a.Runs
	// Conditions are kept as written: the runner does not add success().
	if r.PreIf != "runner.os == 'Linux'" || r.PostIf != "failure()" || !r.HasPre() || !r.HasPost() {
		t.Errorf("%+v", r)
	}
	if r.Image != "Dockerfile" || r.Entrypoint != "e.sh" || r.PreEntrypoint != "p.sh" || r.PostEntrypoint != "q.sh" {
		t.Errorf("%+v", r)
	}
	if r.Args == nil || len(r.Args.Items) != 2 || r.Args.Items[1].String() != "${{ inputs.x }}" || r.Env == nil || r.Env.Pairs[0].Value.String() != "V" {
		t.Errorf("args/env: %+v %+v", r.Args, r.Env)
	}
	c := mustParse(t, "runs:\n  using: composite\n  steps: []\n")
	if c.Runs.PreIf != "always()" || c.Runs.PostIf != "always()" || c.Runs.HasPre() || c.Runs.HasPost() {
		t.Errorf("composite: %+v", c.Runs)
	}
}

func TestCompositeSteps(t *testing.T) {
	a := mustParse(t, `
runs:
  using: composite
  steps:
    - run: echo
      shell: bash
    - uses: actions/checkout@v4
      with:
        fetch-depth: 0
    - id: named
      if: always()
      run: echo
      shell: sh
      working-directory: sub
      env:
        A: ${{ inputs.a }}
      continue-on-error: true
      name: n
    - uses: docker://alpine:3
    - uses: ./x
    - uses: actions/checkout@v4
`)
	var ids []string
	for _, s := range a.Runs.Steps {
		ids = append(ids, s.ID)
	}
	if got, want := strings.Join(ids, ","), "__run,__actions_checkout,named,__alpine_3,__self,__actions_checkout_2"; got != want {
		t.Errorf("ids %s, want %s", got, want)
	}
	s := a.Runs.Steps[2]
	if !s.Explicit || s.If.Expression != "always()" || s.Shell.String() != "bash" && s.Shell.String() != "sh" || s.WorkingDirectory.String() != "sub" || s.Name.String() != "n" || s.ContinueOnError.String() != "true" {
		t.Errorf("step: %+v", s)
	}
	if got := a.Runs.Steps[0].If.Expression; got != "success()" {
		t.Errorf("default if %q", got)
	}
	if got := a.Runs.Steps[1].With.Pairs[0].Value.String(); got != "0" {
		t.Errorf("with %q", got)
	}
}

func TestStepIfIsWrappedLikeTheRunner(t *testing.T) {
	a := mustParse(t, "runs:\n  using: composite\n  steps:\n    - run: x\n      shell: sh\n      if: github.ref == 'main'\n    - run: y\n      shell: sh\n      if: ${{ failure() }}\n    - run: z\n      shell: sh\n      if: vars.A == needs.b.result\n")
	want := []string{"success() && (github.ref == 'main')", "failure()", "success() && (vars.A == needs.b.result)"}
	for i, s := range a.Runs.Steps {
		if s.If.Expression != want[i] {
			t.Errorf("step %d: %q, want %q", i, s.If.Expression, want[i])
		}
	}
}

func TestErrorsAreTheRunners(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string
	}{
		{"unsupported using", "runs:\n  using: node99\n  main: x\n", []string{
			"System.ArgumentOutOfRangeException: Specified argument was out of the range of valid values. (Parameter ''using: node99' is not supported, use 'docker', 'node12', 'node16', 'node20' or 'node24' instead.')"}},
		{"missing using", "runs:\n  main: x\n", []string{
			"System.NotSupportedException: Missing 'using' value. 'using' requires 'composite', 'docker', 'node12', 'node16', 'node20' or 'node24'."}},
		{"no main", "runs:\n  using: node20\n", []string{
			"action.yml (Line: 2, Col: 3): There's not enough info to determine what you meant. Add one of these properties: args, entrypoint, env, image, main, post, post-entrypoint, pre, pre-entrypoint, steps",
			"System.ArgumentNullException: Value cannot be null. (Parameter 'You are using a JavaScript Action but there is not an entry JavaScript file provided in action.yml.')"}},
		{"no image", "runs:\n  using: docker\n", []string{
			"action.yml (Line: 2, Col: 3): There's not enough info to determine what you meant. Add one of these properties: args, entrypoint, env, image, main, post, post-entrypoint, pre, pre-entrypoint, steps",
			"System.ArgumentNullException: Value cannot be null. (Parameter 'You are using a Container Action but an image is not provided in action.yml.')"}},
		{"no steps", "runs:\n  using: composite\n", []string{
			"action.yml (Line: 2, Col: 3): There's not enough info to determine what you meant. Add one of these properties: args, entrypoint, env, image, main, post, post-entrypoint, pre, pre-entrypoint, steps",
			"System.ArgumentNullException: Value cannot be null. (Parameter 'You are using a composite action but there are no steps provided in action.yml.')"}},
		{"no runs", "name: a\n", []string{"Top level 'runs:' section is required for action.yml"}},
		{"shell required", "runs:\n  using: composite\n  steps:\n    - run: x\n", []string{
			"action.yml (Line: 4, Col: 7): Required property is missing: shell"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, errs := Parse("action.yml", []byte(tc.src))
			if a != nil {
				t.Fatal("action returned")
			}
			got := errTexts(errs)
			if tc.name == "shell required" {
				// The runner follows the schema error with the exception that
				// stops the load.
				if len(got) != 2 || got[0] != tc.want[0] || !strings.HasPrefix(got[1], "GitHub.DistributedTask.ObjectTemplating.TemplateValidationException: The template is not valid. action.yml (Line: 4, Col: 7): Required property is missing: shell") {
					t.Errorf("got %q", got)
				}
				return
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
	if got := LoadFailed("action.yml"); got != "Failed to load action.yml" {
		t.Errorf("LoadFailed: %q", got)
	}
}

func TestLimits(t *testing.T) {
	// Ten errors at most, as the runner keeps.
	var b strings.Builder
	b.WriteString("runs:\n  using: node20\n  main: m\n")
	for i := 0; i < 25; i++ {
		b.WriteString("  k" + string(rune('a'+i)) + ": 1\n")
	}
	_, errs := Parse("action.yml", []byte(b.String()))
	if len(errs) != 10 {
		t.Errorf("%d errors, want 10", len(errs))
	}
	// The runner's TemplateMemory allows 100 nested collections, the root
	// mapping being the first: 99 brackets pass, 100 do not (depth_seq_99 and
	// depth_seq_100 in the oracle).
	deep := func(n int) string {
		return "x: " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\nruns:\n  using: node20\n  main: m\n"
	}
	if _, errs := Parse("action.yml", []byte(deep(99))); len(errs) != 0 {
		t.Errorf("99 levels: %q", errTexts(errs))
	}
	_, errs = Parse("action.yml", []byte(deep(100)))
	if len(errs) != 2 || errs[0].Error() != "action.yml: Maximum object depth exceeded" {
		t.Errorf("100 levels: %q", errTexts(errs))
	}
}

func TestLintIsTheLanguageService(t *testing.T) {
	// The runner runs this file; the language service refuses it.
	src := "icon: x.svg\nruns:\n  using: node20\n  main: m.js\n"
	if _, errs := Parse("action.yml", []byte(src)); len(errs) != 0 {
		t.Errorf("Parse: %q", errTexts(errs))
	}
	got := errTexts(Lint("action.yml", []byte(src)))
	want := []string{
		"action.yml (Line: 1, Col: 1): Unexpected value 'icon'",
		"action.yml (Line: 1, Col: 1): Required property is missing: name",
		"action.yml (Line: 1, Col: 1): Required property is missing: description",
	}
	// Order: the reference reports the unexpected key first, the missing
	// properties after the mapping.
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Lint:\n got %q\nwant %q", got, want)
	}
	ok := "name: a\ndescription: d\nruns:\n  using: node20\n  main: m.js\n"
	if errs := Lint("action.yml", []byte(ok)); len(errs) != 0 {
		t.Errorf("Lint of a valid file: %q", errTexts(errs))
	}
}

func contexts(t *testing.T, tok interface {
	Definition() *template.DefinitionInfo
}) string {
	t.Helper()
	d := tok.Definition()
	if d == nil {
		t.Fatal("token without definition")
	}
	names := append([]string(nil), d.NamedContexts()...)
	for _, f := range d.Functions() {
		names = append(names, f.Name+"()")
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// The contexts an expression may use come from the schema, key by key; the
// tokens carry them so that a caller can evaluate with the right environment.
func TestContextsPerKey(t *testing.T) {
	a := mustParse(t, `
inputs:
  d:
    default: ${{ github.ref }}
outputs:
  o:
    value: ${{ steps.a.outputs.x }}
runs:
  using: composite
  steps:
    - run: echo ${{ inputs.d }}
      shell: sh
      env:
        A: ${{ matrix.a }}
    - uses: a/b@c
      with:
        k: ${{ steps.a.outputs.x }}
`)
	const stepCtx = "env,github,hashFiles(),inputs,job,matrix,runner,steps,strategy"
	if got, want := contexts(t, a.Inputs[0].Default), "github,hashFiles(),job,matrix,runner,strategy"; got != want {
		t.Errorf("input default: %s, want %s", got, want)
	}
	if got, want := contexts(t, a.Outputs[0].Value), "env,github,inputs,job,matrix,runner,steps,strategy"; got != want {
		t.Errorf("output value: %s, want %s", got, want)
	}
	if got := contexts(t, a.Runs.Steps[0].Run); got != stepCtx {
		t.Errorf("run: %s, want %s", got, stepCtx)
	}
	if got := contexts(t, a.Runs.Steps[0].Env.Pairs[0].Value); got != stepCtx {
		t.Errorf("env: %s", got)
	}
	if got := contexts(t, a.Runs.Steps[1].With.Pairs[0].Value); got != stepCtx {
		t.Errorf("with: %s", got)
	}
	// "if" takes the status functions, and the runner adds vars and needs.
	if got, want := contexts(t, a.Runs.Steps[0].If), "always(),cancelled(),env,failure(),github,hashFiles(),inputs,job,matrix,needs,runner,steps,strategy,success(),vars"; got != want {
		t.Errorf("if: %s, want %s", got, want)
	}
	b := mustParse(t, "runs:\n  using: docker\n  image: Dockerfile\n  args: ['${{ inputs.x }}']\n  env:\n    K: ${{ inputs.y }}\n")
	if got := contexts(t, b.Runs.Args.Items[0]); got != "inputs" {
		t.Errorf("args: %s", got)
	}
	if got := contexts(t, b.Runs.Env.Pairs[0].Value); got != "inputs" {
		t.Errorf("env: %s", got)
	}
}
