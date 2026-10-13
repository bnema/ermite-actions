// SPDX-License-Identifier: MIT

package action

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testdata/strict.json is what @actions/workflow-parser 0.3.61 answers on the
// files of testdata/cases with parseAction and convertActionTemplate (see
// gen/strict.mjs). TestLintAgainstReference replays it against Lint.

type strictAnswer struct {
	Errors []string `json:"errors"`
	Name   string   `json:"name"`
	Descr  string   `json:"description"`
	Author string   `json:"author"`
	Inputs []struct {
		ID                 string `json:"id"`
		Required           bool   `json:"required"`
		DeprecationMessage string `json:"deprecationMessage"`
		Default            string `json:"default"`
	} `json:"inputs"`
	Outputs []struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	} `json:"outputs"`
	Runs struct {
		Using  string `json:"using"`
		Main   string `json:"main"`
		Image  string `json:"image"`
		PreIf  string `json:"preIf"`
		PostIf string `json:"postIf"`
		Steps  int    `json:"steps"`
	} `json:"runs"`
}

var (
	// The reference words expression parse errors like the JavaScript
	// expression library: no position suffix. package expr follows the C#
	// runner, which adds ". Located at position N within expression: ...".
	exprSuffix = regexp.MustCompile(`\. Located at position \d+ within expression: .*$`)
	// yaml.v3 and the JavaScript yaml library word syntax errors differently.
	jsYAMLError = regexp.MustCompile(` at line \d+, column \d+:\n`)
)

// knownLintDifferences are the files on which the reference's answer is not
// reproduced, with the reason.
var knownLintDifferences = map[string]string{
	"yaml_key_collection.yml": "a mapping key that is a collection: the reference reads it as undefined (\"Unexpected value 'undefined'\"), package template reports \"Unexpected value ''\" (twice); see template/PORTING.md",
}

func TestLintAgainstReference(t *testing.T) {
	data, err := os.ReadFile("testdata/strict.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]strictAnswer
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("testdata/cases/*.yml")
	for _, f := range files {
		name := filepath.Base(f)
		w, ok := want[name]
		if !ok {
			t.Errorf("%s: missing from strict.json", name)
			continue
		}
		t.Run(strings.TrimSuffix(name, ".yml"), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			errs := Lint(name, src)
			if len(w.Errors) == 1 && jsYAMLError.MatchString(w.Errors[0]) {
				if len(errs) != 1 || !strings.HasPrefix(errs[0].Error(), name+": ") {
					t.Errorf("syntax error: got %q, reference %q", errTexts(errs), w.Errors)
				}
				return
			}
			var got []string
			for _, e := range errs {
				got = append(got, exprSuffix.ReplaceAllString(e.Error(), ""))
			}
			if note, known := knownLintDifferences[name]; known {
				t.Skip(note)
			}
			if strings.Join(got, "\n") != strings.Join(w.Errors, "\n") {
				t.Fatalf("errors differ\n got: %q\nwant: %q", got, w.Errors)
			}
		})
	}
}
