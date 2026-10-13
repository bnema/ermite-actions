// SPDX-License-Identifier: MIT

package action

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bnema.dev/ermite-actions/template"
)

// testdata/runner.json is what the C# runner's own loader answers on the files
// of testdata/cases (see gen/runner): the errors it reports and a summary of
// what it loaded. TestAgainstRunner replays it.

type runnerAnswer struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Errors      []string `json:"errors"`
	Inputs      []struct {
		Name    string  `json:"name"`
		Default *string `json:"default"`
	} `json:"inputs"`
	Deprecated map[string]string `json:"deprecated"`
	Execution  *struct {
		Type       string      `json:"type"`
		Using      string      `json:"using"`
		Image      string      `json:"image"`
		Args       []string    `json:"args"`
		Entrypoint *string     `json:"entrypoint"`
		Env        [][2]string `json:"env"`
		Main       string      `json:"main"`
		Pre        *string     `json:"pre"`
		PreIf      string      `json:"preIf"`
		Post       *string     `json:"post"`
		PostIf     string      `json:"postIf"`
		Plugin     string      `json:"plugin"`
		Steps      []struct {
			ID              string      `json:"id"`
			If              string      `json:"if"`
			Kind            string      `json:"kind"`
			Name            *string     `json:"name"`
			ContinueOnError *string     `json:"continueOnError"`
			Timeout         *string     `json:"timeout"`
			Env             [][2]string `json:"env"`
			EnvExpr         *string     `json:"envExpr"`
			WithExpr        *string     `json:"withExpr"`
			Script          *string     `json:"script"`
			Shell           *string     `json:"shell"`
			WorkingDir      *string     `json:"workingDirectory"`
			With            [][2]string `json:"with"`
			Reference       *struct {
				Type  string  `json:"type"`
				Image string  `json:"image"`
				Name  *string `json:"name"`
				Ref   *string `json:"ref"`
				Path  *string `json:"path"`
			} `json:"reference"`
		} `json:"steps"`
		Outputs [][2]*string `json:"outputs"`
	} `json:"execution"`
}

func loadRunner(t *testing.T) map[string]runnerAnswer {
	t.Helper()
	data, err := os.ReadFile("testdata/runner.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]runnerAnswer
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func errTexts(errs []Error) []string {
	out := make([]string, len(errs))
	for i := range errs {
		out[i] = errs[i].Error()
	}
	return out
}

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func pairsOf(m *template.Mapping) [][2]string {
	if m == nil {
		return nil
	}
	var out [][2]string
	for _, p := range m.Pairs {
		out = append(out, [2]string{p.Key.String(), p.Value.String()})
	}
	return out
}

func TestAgainstRunner(t *testing.T) {
	want := loadRunner(t)
	files, err := filepath.Glob("testdata/cases/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no cases: %v", err)
	}
	if len(files) != len(want) {
		t.Errorf("%d case files, %d answers: run gen/runner/build.sh", len(files), len(want))
	}
	for _, f := range files {
		name := filepath.Base(f)
		w, ok := want[name]
		if !ok {
			t.Errorf("%s: no answer in runner.json", name)
			continue
		}
		t.Run(strings.TrimSuffix(name, ".yml"), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			a, errs := Parse(name, data)
			got := errTexts(errs)
			if mismatch := compareErrors(got, w.Errors); mismatch != "" {
				t.Fatalf("errors differ: %s\n got: %q\nwant: %q", mismatch, got, w.Errors)
			}
			if len(w.Errors) > 0 {
				if a != nil {
					t.Error("action returned with errors")
				}
				return
			}
			if a == nil {
				t.Fatal("no action and no errors")
			}
			compareAction(t, a, w)
		})
	}
}

// compareErrors compares messages with the runner's, which are recorded
// without their stack traces. A YAML syntax error has YamlDotNet's wording in
// the runner and yaml.v3's here: only the shape is compared.
// yamlDotNetError matches the runner's YAML syntax errors: "file.yml: (Line: 3,
// Col: 1, Idx: 16) - (Line: 3, Col: 1, Idx: 16): While parsing a node, ...".
var yamlDotNetError = regexp.MustCompile(`^[^ ]+: \(Line: \d+, Col: \d+, Idx: \d+\) - \(Line: \d+, Col: \d+, Idx: \d+\): `)

func compareErrors(got, want []string) string {
	if len(want) >= 1 && yamlDotNetError.MatchString(want[0]) {
		if len(got) == 0 || !strings.HasPrefix(got[0], strings.SplitN(want[0], ":", 2)[0]+": ") {
			return "syntax error not reported on the file"
		}
		// The runner follows a syntax error with the exception from the null
		// root; compare the rest.
		if len(got) != len(want) {
			return fmt.Sprintf("%d errors, want %d", len(got), len(want))
		}
		for i := 1; i < len(got); i++ {
			if got[i] != want[i] {
				return fmt.Sprintf("error %d", i)
			}
		}
		return ""
	}
	if len(got) != len(want) {
		return fmt.Sprintf("%d errors, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			return fmt.Sprintf("error %d", i)
		}
	}
	return ""
}

func compareAction(t *testing.T, a *Action, w runnerAnswer) {
	t.Helper()
	if a.Name != w.Name || a.Description != w.Description {
		t.Errorf("name/description: got %q/%q, want %q/%q", a.Name, a.Description, w.Name, w.Description)
	}
	if len(a.Inputs) != len(w.Inputs) {
		t.Fatalf("%d inputs, want %d", len(a.Inputs), len(w.Inputs))
	}
	for i, in := range w.Inputs {
		g := a.Inputs[i]
		if g.Name != in.Name || g.Default.String() != orEmpty(in.Default) {
			t.Errorf("input %d: got %q default %q, want %q default %q", i, g.Name, g.Default.String(), in.Name, orEmpty(in.Default))
		}
		if dep := w.Deprecated[in.Name]; g.DeprecationMessage != dep {
			t.Errorf("input %q: deprecation %q, want %q", in.Name, g.DeprecationMessage, dep)
		}
	}
	e := w.Execution
	r := a.Runs
	if e == nil || r == nil {
		t.Fatalf("execution: %v / %v", e, r)
	}
	kinds := map[string]RunsKind{"node": RunsNode, "docker": RunsDocker, "composite": RunsComposite, "plugin": RunsPlugin}
	if r.Kind != kinds[e.Type] {
		t.Fatalf("kind %v, want %s", r.Kind, e.Type)
	}
	switch r.Kind {
	case RunsNode:
		if r.Main != e.Main || r.Pre != orEmpty(e.Pre) || r.Post != orEmpty(e.Post) || r.PreIf != e.PreIf || r.PostIf != e.PostIf || !strings.EqualFold(r.Using, e.Using) {
			t.Errorf("node: got %+v, want %+v", r, e)
		}
	case RunsDocker:
		var args []string
		if r.Args != nil {
			for _, it := range r.Args.Items {
				args = append(args, it.String())
			}
		}
		if r.Image != e.Image || r.Entrypoint != orEmpty(e.Entrypoint) || r.PreEntrypoint != orEmpty(e.Pre) || r.PostEntrypoint != orEmpty(e.Post) || r.PreIf != e.PreIf || r.PostIf != e.PostIf {
			t.Errorf("docker: got %+v, want %+v", r, e)
		}
		if fmt.Sprint(args) != fmt.Sprint(e.Args) {
			t.Errorf("args: got %q, want %q", args, e.Args)
		}
		if fmt.Sprint(pairsOf(r.Env)) != fmt.Sprint(e.Env) {
			t.Errorf("env: got %q, want %q", pairsOf(r.Env), e.Env)
		}
	case RunsPlugin:
		if r.Plugin != e.Plugin {
			t.Errorf("plugin: %q, want %q", r.Plugin, e.Plugin)
		}
	case RunsComposite:
		if len(r.Steps) != len(e.Steps) {
			t.Fatalf("%d steps, want %d", len(r.Steps), len(e.Steps))
		}
		for i, ws := range e.Steps {
			s := r.Steps[i]
			if s.ID != ws.ID || s.If.String() != "${{ "+ws.If+" }}" && s.If.Expression != ws.If {
				t.Errorf("step %d: id %q if %q, want id %q if %q", i, s.ID, s.If.Expression, ws.ID, ws.If)
			}
			if tokStr(s.Name) != orEmpty(ws.Name) || tokStr(s.ContinueOnError) != orEmpty(ws.ContinueOnError) {
				t.Errorf("step %d: name/continue-on-error %q/%q, want %q/%q", i, tokStr(s.Name), tokStr(s.ContinueOnError), orEmpty(ws.Name), orEmpty(ws.ContinueOnError))
			}
			if tokStr(s.EnvExpression) != orEmpty(ws.EnvExpr) || tokStr(s.WithExpression) != orEmpty(ws.WithExpr) {
				t.Errorf("step %d env/with expression: got %q/%q, want %q/%q", i, tokStr(s.EnvExpression), tokStr(s.WithExpression), orEmpty(ws.EnvExpr), orEmpty(ws.WithExpr))
			}
			if fmt.Sprint(pairsOf(s.Env)) != fmt.Sprint(nilIfEmpty(ws.Env)) {
				t.Errorf("step %d env: got %q, want %q", i, pairsOf(s.Env), ws.Env)
			}
			switch ws.Kind {
			case "run":
				if s.Run == nil || tokStr(s.Run) != orEmpty(ws.Script) || tokStr(s.Shell) != orEmpty(ws.Shell) || tokStr(s.WorkingDirectory) != orEmpty(ws.WorkingDir) {
					t.Errorf("step %d run: got %q/%q/%q, want %+v", i, tokStr(s.Run), tokStr(s.Shell), tokStr(s.WorkingDirectory), ws)
				}
			case "uses":
				if s.Uses == nil || fmt.Sprint(pairsOf(s.With)) != fmt.Sprint(nilIfEmpty(ws.With)) {
					t.Errorf("step %d uses: got %v with %q, want %+v", i, s.Uses, pairsOf(s.With), ws)
				}
				u, err := ParseUses(s.Uses.Value)
				if err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				checkReference(t, i, u, ws.Reference)
			}
		}
		if len(a.Outputs) != len(e.Outputs) {
			t.Fatalf("%d outputs, want %d", len(a.Outputs), len(e.Outputs))
		}
		for i, o := range e.Outputs {
			if a.Outputs[i].Name != *o[0] || tokStr(a.Outputs[i].Value) != orEmpty(o[1]) {
				t.Errorf("output %d: got %q=%q, want %v", i, a.Outputs[i].Name, tokStr(a.Outputs[i].Value), o)
			}
		}
	}
}

func nilIfEmpty(p [][2]string) [][2]string {
	if len(p) == 0 {
		return nil
	}
	return p
}

func tokStr(t template.Token) string {
	if t == nil {
		return ""
	}
	return t.String()
}

func checkReference(t *testing.T, i int, u Uses, ref *struct {
	Type  string  `json:"type"`
	Image string  `json:"image"`
	Name  *string `json:"name"`
	Ref   *string `json:"ref"`
	Path  *string `json:"path"`
}) {
	t.Helper()
	if ref == nil {
		t.Errorf("step %d: no reference in the answer", i)
		return
	}
	switch u.Kind {
	case UsesDocker:
		if ref.Type != "docker" || ref.Image != u.Image {
			t.Errorf("step %d: %+v, want %+v", i, u, ref)
		}
	case UsesLocal:
		if ref.Type != "self" || orEmpty(ref.Path) != u.Path {
			t.Errorf("step %d: %+v, want %+v", i, u, ref)
		}
	case UsesSelfRepository:
		if ref.Type != "selfRepository" || orEmpty(ref.Path) != u.Path {
			t.Errorf("step %d: %+v, want %+v", i, u, ref)
		}
	case UsesRepository:
		if ref.Type != "GitHub" || orEmpty(ref.Name) != u.Name || orEmpty(ref.Ref) != u.Ref || orEmpty(ref.Path) != u.Path {
			t.Errorf("step %d: %+v, want %+v", i, u, ref)
		}
	}
}
