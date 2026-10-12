// SPDX-License-Identifier: MIT

package workflow

import (
	"fmt"
	"testing"
)

func pushWorkflow(t testing.TB, on string) *Workflow {
	t.Helper()
	w, errs := Parse("w.yml", []byte(on+"\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n"))
	if len(errs) > 0 {
		t.Fatalf("Parse: %v", errs)
	}
	return w
}

func TestMatchPush(t *testing.T) {
	br := func(n string) PushInfo { return PushInfo{Ref: "refs/heads/" + n} }
	tg := func(n string) PushInfo { return PushInfo{Ref: "refs/tags/" + n, TagCount: 1} }
	files := func(n string, f ...string) PushInfo { return PushInfo{Ref: "refs/heads/" + n, ChangedFiles: f} }
	tests := []struct {
		name string
		on   string
		push PushInfo
		want bool
	}{
		{"push string, branch", "on: push", br("main"), true},
		{"push string, tag", "on: push", tg("v1"), true},
		{"push list", "on: [push, pull_request]", br("main"), true},
		{"other event only", "on: pull_request", br("main"), false},
		{"push null", "on:\n  push:", br("x/y"), true},
		{"deleted branch", "on: push", PushInfo{Ref: "refs/heads/main", Deleted: true}, false},
		{"deleted tag", "on: push", PushInfo{Ref: "refs/tags/v1", Deleted: true, TagCount: 1}, false},
		{"created branch", "on: push", PushInfo{Ref: "refs/heads/new", Created: true}, true},
		{"other ref kind", "on: push", PushInfo{Ref: "refs/notes/x"}, false},

		// branches
		{"branches match", "on:\n  push:\n    branches: [main]", br("main"), true},
		{"branches no match", "on:\n  push:\n    branches: [main]", br("dev"), false},
		{"branches glob", "on:\n  push:\n    branches: ['release/**']", br("release/1/x"), true},
		{"branches single string", "on:\n  push:\n    branches: main", br("main"), true},
		{"branches-ignore hit", "on:\n  push:\n    branches-ignore: ['wip/**']", br("wip/x"), false},
		{"branches-ignore miss", "on:\n  push:\n    branches-ignore: ['wip/**']", br("main"), true},
		{"branches negation order", "on:\n  push:\n    branches: ['releases/**', '!releases/**-alpha']", br("releases/1-alpha"), false},
		{"branches negation re-include", "on:\n  push:\n    branches: ['releases/**', '!releases/**-alpha', 'releases/**/ga']", br("releases/1-alpha/ga"), true},
		{"branches empty list", "on:\n  push:\n    branches: []", br("main"), false},

		// branches-only filter: tags don't run; tags-only filter: branches don't run.
		{"branches filter, tag push", "on:\n  push:\n    branches: [main]", tg("main"), false},
		{"branches-ignore filter, tag push", "on:\n  push:\n    branches-ignore: [dev]", tg("v1"), false},
		{"tags filter, branch push", "on:\n  push:\n    tags: ['v*']", br("v1"), false},
		{"tags-ignore filter, branch push", "on:\n  push:\n    tags-ignore: ['v*']", br("main"), false},
		{"tags match", "on:\n  push:\n    tags: ['v*']", tg("v1.2"), true},
		{"tags no match", "on:\n  push:\n    tags: ['v*']", tg("x1"), false},
		{"tags-ignore hit", "on:\n  push:\n    tags-ignore: ['v*-rc*']", tg("v1-rc1"), false},
		{"tags-ignore miss", "on:\n  push:\n    tags-ignore: ['v*-rc*']", tg("v1"), true},
		{"branches and tags: branch", "on:\n  push:\n    branches: [main]\n    tags: ['v*']", br("main"), true},
		{"branches and tags: other branch", "on:\n  push:\n    branches: [main]\n    tags: ['v*']", br("v1"), false},
		{"branches and tags: tag", "on:\n  push:\n    branches: [main]\n    tags: ['v*']", tg("v1"), true},
		{"branches and tags: other tag", "on:\n  push:\n    branches: [main]\n    tags: ['v*']", tg("main"), false},
		{"tags with paths ignores paths", "on:\n  push:\n    tags: ['v*']\n    paths: ['src/**']", tg("v1"), true},

		// tag count
		{"3 tags", "on: push", PushInfo{Ref: "refs/tags/v1", TagCount: 3}, true},
		{"4 tags", "on: push", PushInfo{Ref: "refs/tags/v1", TagCount: 4}, false},
		{"4 tags and a branch push", "on: push", PushInfo{Ref: "refs/heads/main", TagCount: 4}, true},

		// paths
		{"paths match", "on:\n  push:\n    paths: ['src/**']", files("main", "src/a.go"), true},
		{"paths no match", "on:\n  push:\n    paths: ['src/**']", files("main", "docs/a.md"), false},
		{"paths any file", "on:\n  push:\n    paths: ['src/**']", files("main", "docs/a.md", "src/b.go"), true},
		{"paths no files", "on:\n  push:\n    paths: ['src/**']", files("main"), false},
		{"paths-ignore all ignored", "on:\n  push:\n    paths-ignore: ['docs/**']", files("main", "docs/a.md", "docs/b.md"), false},
		{"paths-ignore one other", "on:\n  push:\n    paths-ignore: ['docs/**']", files("main", "docs/a.md", "x.go"), true},
		{"paths negation", "on:\n  push:\n    paths: ['sub-project/**', '!sub-project/docs/**']", files("main", "sub-project/docs/a.md"), false},
		{"paths negation, other file", "on:\n  push:\n    paths: ['sub-project/**', '!sub-project/docs/**']", files("main", "sub-project/docs/a.md", "sub-project/a.go"), true},
		{"paths with branches", "on:\n  push:\n    branches: [main]\n    paths: ['src/**']", files("dev", "src/a.go"), false},
		{"paths with branches both", "on:\n  push:\n    branches: [main]\n    paths: ['src/**']", files("main", "src/a.go"), true},
		{"paths truncated", "on:\n  push:\n    paths: ['src/**']", PushInfo{Ref: "refs/heads/main", ChangedFiles: []string{"docs/a.md"}, FilesTruncated: true}, true},
		{"paths-ignore truncated", "on:\n  push:\n    paths-ignore: ['**']", PushInfo{Ref: "refs/heads/main", FilesTruncated: true}, true},
		{"no paths filter, no files", "on: push", files("main"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := pushWorkflow(t, tc.on)
			if got := w.MatchPush(tc.push); got != tc.want {
				t.Errorf("MatchPush = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatchPushFirst300Files(t *testing.T) {
	w := pushWorkflow(t, "on:\n  push:\n    paths: ['src/**']")
	files := make([]string, 0, 301)
	for i := 0; i < MaxFilesEvaluated; i++ {
		files = append(files, fmt.Sprintf("docs/%d.md", i))
	}
	files = append(files, "src/late.go") // the 301st
	if w.MatchPush(PushInfo{Ref: "refs/heads/main", ChangedFiles: files}) {
		t.Error("a match beyond the 300th file must not run the workflow")
	}
	files[299] = "src/early.go"
	if !w.MatchPush(PushInfo{Ref: "refs/heads/main", ChangedFiles: files}) {
		t.Error("a match among the first 300 files must run the workflow")
	}
	wi := pushWorkflow(t, "on:\n  push:\n    paths-ignore: ['docs/**']")
	files = files[:0]
	for i := 0; i < MaxFilesEvaluated; i++ {
		files = append(files, fmt.Sprintf("docs/%d.md", i))
	}
	files = append(files, "src/late.go")
	// TODO(verify with recording): with paths-ignore, files beyond the 300th
	// are not considered either, so everything seen is ignored.
	if wi.MatchPush(PushInfo{Ref: "refs/heads/main", ChangedFiles: files}) {
		t.Error("paths-ignore: only the first 300 files are evaluated")
	}
}

func TestMatchPushNilSafe(t *testing.T) {
	var w *Workflow
	if w.MatchPush(PushInfo{Ref: "refs/heads/main"}) {
		t.Error("nil workflow matched")
	}
	if (&Workflow{}).MatchPush(PushInfo{Ref: "refs/heads/main"}) {
		t.Error("workflow without on matched")
	}
}

func TestFilterPairsAreRejected(t *testing.T) {
	for _, on := range []string{
		"on:\n  push:\n    branches: [a]\n    branches-ignore: [b]",
		"on:\n  push:\n    tags: [a]\n    tags-ignore: [b]",
		"on:\n  pull_request:\n    paths: [a]\n    paths-ignore: [b]",
	} {
		if _, errs := Parse("w.yml", []byte(on+"\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n")); len(errs) != 1 {
			t.Errorf("%q: errors = %v", on, errs)
		}
	}
}
