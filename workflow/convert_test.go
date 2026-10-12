// SPDX-License-Identifier: MIT

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

// isYAMLError recognises the reference's YAML syntax errors, whose text is
// the JavaScript library's and cannot be reproduced word for word.
func isYAMLError(s string) bool { return strings.Contains(s, " at line ") }

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
			if !reflect.DeepEqual(nonNil(got), nonNil(want.Errors)) {
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
				if !reflect.DeepEqual(nonNil(j.Needs), nonNil(wj.Needs)) && !(j.IsReusable() && len(wj.Needs) == 0) {
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
