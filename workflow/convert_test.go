// SPDX-License-Identifier: MIT

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// refResult is what testdata/convert.json records for one file: the output of
// @actions/workflow-parser 0.3.61 (see gen/convert.mjs).
type refResult struct {
	Errors []string `json:"errors"`
	Jobs   []struct {
		ID    string   `json:"id"`
		Name  string   `json:"name"`
		If    string   `json:"if"`
		Needs []string `json:"needs"`
		Steps []struct {
			ID string `json:"id"`
			If string `json:"if"`
		} `json:"steps"`
	} `json:"jobs"`
	Events map[string]any `json:"events"`
}

func loadRef(t *testing.T) map[string]refResult {
	t.Helper()
	data, err := os.ReadFile("testdata/convert.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]refResult
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func errorTexts(errs []Error) []string {
	out := make([]string, len(errs))
	for i := range errs {
		out[i] = errs[i].Error()
	}
	return out
}

// The reference (@actions/workflow-parser) words expression parse errors like
// the JavaScript expression library: no position suffix, and the function name
// in arity errors. package expr follows the C# runner, which is what runs the
// workflow: "<description>: '<token>'. Located at position N within
// expression: <expression>", with the "(" token in arity errors. Only the
// reference's side is rewritten (sameErrorAsReference); what this package reports
// is compared as it is, and must carry the runner's suffix.
var (
	runnerArity = regexp.MustCompile(`((?:Too few|Too many) parameters supplied|Even number of parameters supplied, requires an odd number of parameters): '[^']*'`)
	// exprError recognises the messages of package expr that the runner ends
	// with its position suffix.
	exprError    = regexp.MustCompile(`: (?:Unrecognized named-value|Unrecognized function|Unexpected symbol|Unexpected end of expression|Too few parameters supplied|Too many parameters supplied|Even number of parameters supplied, requires an odd number of parameters): '`)
	runnerSuffix = regexp.MustCompile(`^\. Located at position \d+ within expression: .+$`)
)

// sameErrorAsReference reports whether got, the message of this package, is the
// reference's want in the runner's wording: the reference's arity errors name
// the function where the runner names "(", and its expression errors lack the
// position suffix that the runner adds (when the reference has it already, as
// for lexer errors, the message must be the same).
func sameErrorAsReference(got, want string) bool {
	want = runnerArity.ReplaceAllString(want, "$1: '('")
	if !exprError.MatchString(want) {
		return got == want
	}
	if strings.Contains(want, ". Located at position ") {
		return got == want
	}
	rest, ok := strings.CutPrefix(got, want)
	return ok && runnerSuffix.MatchString(rest)
}

func sameErrorsAsReference(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !sameErrorAsReference(got[i], want[i]) {
			return false
		}
	}
	return true
}

func TestSameErrorAsReference(t *testing.T) {
	const loc = ". Located at position 3 within expression: a b"
	for _, tc := range []struct {
		name, got, want string
		ok              bool
	}{
		{"suffix added", "f.yml (Line: 1, Col: 1): Unrecognized named-value: 'a'" + loc, "f.yml (Line: 1, Col: 1): Unrecognized named-value: 'a'", true},
		{"suffix missing", "f.yml (Line: 1, Col: 1): Unrecognized named-value: 'a'", "f.yml (Line: 1, Col: 1): Unrecognized named-value: 'a'", false},
		{"suffix empty expression", "f.yml (Line: 1, Col: 1): Unexpected symbol: 'a'. Located at position 3 within expression: ", "f.yml (Line: 1, Col: 1): Unexpected symbol: 'a'", false},
		{"arity names the paren", "f.yml (Line: 1, Col: 1): Too many parameters supplied: '('" + loc, "f.yml (Line: 1, Col: 1): Too many parameters supplied: 'failure'", true},
		{"arity names the function", "f.yml (Line: 1, Col: 1): Too many parameters supplied: 'failure'" + loc, "f.yml (Line: 1, Col: 1): Too many parameters supplied: 'failure'", false},
		{"different token", "f.yml (Line: 1, Col: 1): Unrecognized named-value: 'b'" + loc, "f.yml (Line: 1, Col: 1): Unrecognized named-value: 'a'", false},
		{"reference has the suffix", "f.yml (Line: 1, Col: 1): Unexpected symbol: '+'. Located at position 3 within expression: 1 +", "f.yml (Line: 1, Col: 1): Unexpected symbol: '+'. Located at position 3 within expression: 1 +", true},
		{"reference suffix differs", "f.yml (Line: 1, Col: 1): Unexpected symbol: '+'. Located at position 4 within expression: 1 +", "f.yml (Line: 1, Col: 1): Unexpected symbol: '+'. Located at position 3 within expression: 1 +", false},
		{"not an expression error", "f.yml (Line: 1, Col: 1): Unexpected value '${{ x }}'", "f.yml (Line: 1, Col: 1): Unexpected value '${{ x }}'", true},
		{"not an expression error with a suffix", "f.yml (Line: 1, Col: 1): Unexpected value '${{ x }}'" + loc, "f.yml (Line: 1, Col: 1): Unexpected value '${{ x }}'", false},
	} {
		if got := sameErrorAsReference(tc.got, tc.want); got != tc.ok {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.ok)
		}
	}
}

// isYAMLError recognises the reference's YAML syntax errors, whose text is
// the JavaScript library's and cannot be reproduced word for word.
func isYAMLError(s string) bool { return strings.Contains(s, " at line ") }

// ermiteOnly lists corpus files on which this package reports errors the
// reference does not; the errors expected are given here instead.
var ermiteOnly = map[string][]string{
	"on_filters_both.yml": {
		"on_filters_both.yml (Line: 2, Col: 3): Event 'push' cannot use both 'branches' and 'branches-ignore'. Use only one of them.",
		"on_filters_both.yml (Line: 2, Col: 3): Event 'push' cannot use both 'tags' and 'tags-ignore'. Use only one of them.",
		"on_filters_both.yml (Line: 2, Col: 3): Event 'push' cannot use both 'paths' and 'paths-ignore'. Use only one of them.",
	},
}

func TestConvertAgainstReference(t *testing.T) {
	ref := loadRef(t)
	files, err := filepath.Glob("testdata/convert/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus: %v", err)
	}
	for _, f := range files {
		name := filepath.Base(f)
		want, ok := ref[name]
		if !ok {
			t.Errorf("%s: missing from convert.json", name)
			continue
		}
		t.Run(strings.TrimSuffix(name, ".yml"), func(t *testing.T) {
			content, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			w, errs := Parse(name, content)
			got := errorTexts(errs)
			if extra, ok := ermiteOnly[name]; ok {
				if !reflect.DeepEqual(got, extra) || w != nil {
					t.Errorf("errors:\n got %q\nwant %q", got, extra)
				}
				return
			}
			if len(want.Errors) == 1 && isYAMLError(want.Errors[0]) {
				// Syntax errors: the Go parser words them differently. Only
				// the fact that there is exactly one, on the file, is checked.
				if len(got) != 1 || !strings.HasPrefix(got[0], name+": ") {
					t.Errorf("syntax error: got %q, reference %q", got, want.Errors)
				}
				if w != nil {
					t.Error("workflow returned with errors")
				}
				return
			}
			if !sameErrorsAsReference(nonNil(got), nonNil(want.Errors)) {
				t.Errorf("errors differ\n got: %q\nwant: %q", got, want.Errors)
			}
			if len(want.Errors) > 0 {
				if w != nil {
					t.Error("workflow returned with errors")
				}
				return
			}
			if w == nil {
				t.Fatal("no workflow and no errors")
			}
			if len(w.Jobs) != len(want.Jobs) {
				t.Fatalf("jobs: got %d, reference %d", len(w.Jobs), len(want.Jobs))
			}
			for i, wj := range want.Jobs {
				j := w.Jobs[i]
				if j.ID != wj.ID || j.Name.String() != wj.Name || j.If.Expression != wj.If {
					t.Errorf("job %d: got id=%q name=%q if=%q, reference id=%q name=%q if=%q", i, j.ID, j.Name, j.If.Expression, wj.ID, wj.Name, wj.If)
				}
				if !reflect.DeepEqual(nonNil(j.Needs), nonNil(wj.Needs)) && (!j.IsReusable() || len(wj.Needs) != 0) {
					t.Errorf("job %s needs: got %v, reference %v", j.ID, j.Needs, wj.Needs)
				}
				if len(j.Steps) != len(wj.Steps) {
					t.Errorf("job %s: %d steps, reference %d", j.ID, len(j.Steps), len(wj.Steps))
					continue
				}
				for k, ws := range wj.Steps {
					if j.Steps[k].ID != ws.ID || j.Steps[k].If.Expression != ws.If {
						t.Errorf("job %s step %d: got id=%q if=%q, reference id=%q if=%q", j.ID, k, j.Steps[k].ID, j.Steps[k].If.Expression, ws.ID, ws.If)
					}
				}
			}
			checkEvents(t, w, want.Events)
		})
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// checkEvents compares the filters of the events with the reference.
func checkEvents(t *testing.T, w *Workflow, want map[string]any) {
	t.Helper()
	var names []string
	for _, e := range w.On.Events {
		names = append(names, e.Name)
	}
	var wnames []string
	for k := range want {
		wnames = append(wnames, k)
	}
	sort.Strings(names)
	sort.Strings(wnames)
	if !reflect.DeepEqual(names, wnames) {
		t.Errorf("events: got %v, reference %v", names, wnames)
		return
	}
	for _, e := range w.On.Events {
		wf, _ := want[e.Name].(map[string]any)
		for key, got := range map[string][]string{
			"branches": e.Branches, "branches-ignore": e.BranchesIgnore, "tags": e.Tags,
			"tags-ignore": e.TagsIgnore, "paths": e.Paths, "paths-ignore": e.PathsIgnore, "types": e.Types,
		} {
			raw, present := wf[key]
			if (got != nil) != present {
				t.Errorf("event %s filter %s: present=%v, reference present=%v", e.Name, key, got != nil, present)
				continue
			}
			if present {
				var ws []string
				for _, x := range raw.([]any) {
					ws = append(ws, x.(string))
				}
				if !reflect.DeepEqual(nonNil(got), nonNil(ws)) {
					t.Errorf("event %s filter %s: got %v, reference %v", e.Name, key, got, ws)
				}
			}
		}
	}
}
