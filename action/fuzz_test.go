// SPDX-License-Identifier: MIT

package action

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzParse(f *testing.F) {
	files, _ := filepath.Glob("testdata/cases/*.yml")
	for _, name := range files {
		if data, err := os.ReadFile(name); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("name: a\ndescription: d\nruns:\n  using: node20\n  main: m.js\n"))
	f.Add([]byte(""))
	f.Add([]byte("\x00\xff"))
	f.Add([]byte("runs: &a\n  using: *a\n"))
	f.Add([]byte("runs:\n  using: composite\n  steps:\n    - uses: $/x@\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		a, errs := Parse("action.yml", data)
		if (a == nil) == (len(errs) == 0) {
			t.Fatalf("action nil = %v with %d errors", a == nil, len(errs))
		}
		if len(errs) > maxErrors {
			t.Fatalf("%d errors, the limit is %d", len(errs), maxErrors)
		}
		for _, e := range errs {
			_ = e.Error()
		}
		if a != nil {
			if a.Runs == nil {
				t.Fatal("no runs")
			}
			for _, s := range a.Runs.Steps {
				if s.ID == "" || s.If == nil {
					t.Fatalf("step without id or condition: %+v", s)
				}
				if s.Uses != nil {
					_, _ = ParseUses(s.Uses.Value)
				}
			}
			_ = a.Runs.HasPre()
			_ = a.Runs.HasPost()
			if a.Runs.Kind == RunsDocker {
				_, _ = ParseImage(a.Runs.Image)
			}
		}
		_ = Lint("action.yml", data)
	})
}

func FuzzParseUses(f *testing.F) {
	for _, s := range []string{"actions/checkout@v4", "./x", "docker://a", "$/a", "a@b@c", "", "//", `\@`, "a/b@"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		u, err := ParseUses(s)
		if (err == nil) != (u.Kind != 0) {
			t.Fatalf("%q: kind %d with error %v", s, u.Kind, err)
		}
		if u.Kind == UsesRepository && (u.Owner == "" || u.Repo == "" || u.Ref == "") {
			t.Fatalf("%q: %+v", s, u)
		}
		_, _ = ParseImage(s)
	})
}
