// SPDX-License-Identifier: MIT

package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseWorkflow(f *testing.F) {
	files, _ := filepath.Glob("testdata/convert/*.yml")
	for _, name := range files {
		if data, err := os.ReadFile(name); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("on: push\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n"))
	f.Add([]byte(""))
	f.Add([]byte("\x00\xff"))
	f.Fuzz(func(t *testing.T, data []byte) {
		w, errs := Parse("f.yml", data)
		if (w == nil) == (len(errs) == 0) {
			t.Fatalf("workflow nil = %v with %d errors", w == nil, len(errs))
		}
		for _, e := range errs {
			_ = e.Error()
		}
		if w != nil {
			w.MatchPush(PushInfo{Ref: "refs/heads/main", ChangedFiles: []string{"a/b"}})
			w.MatchPush(PushInfo{Ref: "refs/tags/v1", TagCount: 1})
		}
	})
}

func FuzzExpandMatrix(f *testing.F) {
	f.Add(`{"matrix": {"a": [1, 2], "include": [{"a": 1, "b": 2}], "exclude": [{"a": 2}]}}`)
	f.Add(`{"matrix": {"a": [1], "b": [[1], {"c": 2}]}}`)
	f.Fuzz(func(t *testing.T, s string) {
		v := jsonValue2(s)
		if v == nil {
			return
		}
		_, _ = ExpandMatrix(v)
	})
}

func FuzzSkipCI(f *testing.F) {
	f.Add("fix\n\nskip-checks: true")
	f.Fuzz(func(t *testing.T, s string) { SkipCI(s) })
}
