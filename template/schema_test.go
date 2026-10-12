// SPDX-License-Identifier: MIT

package template

import (
	"reflect"
	"strings"
	"testing"
)

func TestWorkflowSchemaLoads(t *testing.T) {
	s, err := WorkflowSchema()
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "workflow-v1.0" {
		t.Errorf("version = %q", s.Version)
	}
	root := s.Definition("workflow-root-strict")
	if root == nil || !root.Properties["jobs"].Required || !root.Properties["on"].Required {
		t.Fatalf("workflow-root-strict = %+v", root)
	}
	if d := s.Definition("push-string"); d == nil || d.Constant != "push" {
		t.Errorf("push-string = %+v", d)
	}
}

func TestAllowedContext(t *testing.T) {
	s, err := WorkflowSchema()
	if err != nil {
		t.Fatal(err)
	}
	di := newDefinitionInfo(s, nil, s.Definition("step-if"))
	if got, want := di.NamedContexts(), []string{"github", "inputs", "vars", "needs", "strategy", "matrix", "steps", "job", "runner", "env"}; !reflect.DeepEqual(got, want) {
		t.Errorf("named = %v", got)
	}
	var fs []string
	for _, f := range di.Functions() {
		fs = append(fs, f.Name)
	}
	if got, want := strings.Join(fs, ","), "always,failure,cancelled,success,hashFiles"; got != want {
		t.Errorf("funcs = %s", got)
	}
	ji := newDefinitionInfo(s, nil, s.Definition("job-if"))
	for _, f := range ji.Functions() {
		if f.Name == "success" && f.MaxArgs != -1 {
			t.Errorf("job-if success max = %d", f.MaxArgs)
		}
	}
}

func TestParseFuncEntry(t *testing.T) {
	for in, ok := range map[string]bool{
		"hashFiles(1,255)": true, "always(0,0)": true, "failure(0,MAX)": true,
		"github": false, "f(1)": false, "f(a,2)": false, "(1,2)": false, "f(1,2) ": false,
	} {
		if _, got := parseFuncEntry(in); got != ok {
			t.Errorf("%q: %v", in, got)
		}
	}
}

func TestParseSchemaErrors(t *testing.T) {
	for in, want := range map[string]string{
		`{"definitions":{"a":{"sequence":{}}}}`:                                      "'a' does not defined 'item-type'",
		`{"definitions":{"a":{"mapping":{"loose-key-type":"string"}}}}`:              "Property 'loose-key-type' is defined but 'loose-value-type' is not defined on 'a'",
		`{"definitions":{"a":{"one-of":["zzz"]}}}`:                                   "Schema definition 'zzz' not found",
		`{"definitions":{"a":{"description":"x"}}}`:                                  "Not enough information to construct definition 'a'",
		`{"definitions":{"a b":{"string":{}}}}`:                                      "Invalid definition name 'a b'",
		`{"definitions":{"a":{"string":{"constant":"x","require-non-empty":true}}}}`: "cannot both be set",
		`{"foo":1}`: "Unexpected value 'foo'",
	} {
		_, err := ParseSchema([]byte(in))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", in, err, want)
		}
	}
}
