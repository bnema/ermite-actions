// SPDX-License-Identifier: MIT

package workflow

import (
	"fmt"
	"strings"
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

// numberedFiles returns n paths "dir/f-00000.txt" ... in the order of the diff,
// which for GitHub is sorted by path.
func numberedFiles(dir string, n int) []string {
	files := make([]string, 0, n+1)
	for i := 0; i < n; i++ {
		files = append(files, fmt.Sprintf("%s/f-%05d.txt", dir, i))
	}
	return files
}

// TestMatchPushFileLimit follows the recordings filter-paths-300-files,
// filter-paths-1200-files, filter-paths-3000-boundary and
// filter-paths-3100-files: a paths or paths-ignore filter looks at the first
// 3000 changed files, whose only match (or only file not ignored) sorts last.
func TestMatchPushFileLimit(t *testing.T) {
	if MaxFilesEvaluated != 3000 {
		t.Fatalf("MaxFilesEvaluated = %d, want 3000", MaxFilesEvaluated)
	}
	paths := pushWorkflow(t, "on:\n  push:\n    paths: ['zzz/**']")
	early := pushWorkflow(t, "on:\n  push:\n    paths: ['gen/**']")
	ignore := pushWorkflow(t, "on:\n  push:\n    paths-ignore: ['gen/**']")
	for _, tc := range []struct {
		scenario string
		total    int // changed files, the last one is zzz/late.txt
		late     bool
		early    bool
		ignore   bool
	}{
		// filter-paths-300-files, push 2 (301 files): late-match, early-match
		// and ignore-late all ran.
		{"filter-paths-300-files", 301, true, true, true},
		// filter-paths-1200-files, push 2 (1201 files): same.
		{"filter-paths-1200-files", 1201, true, true, true},
		// filter-paths-3000-boundary, push 2 (3000 files, match last): ran.
		{"filter-paths-3000-boundary 3000", 3000, true, true, true},
		// filter-paths-3000-boundary, push 3 (3001 files): the match is the
		// 3001st file, the workflow did not run.
		{"filter-paths-3000-boundary 3001", 3001, false, true, false},
		// filter-paths-3100-files, push 2 (3101 files): late-match and
		// ignore-late did not run, early-match did.
		{"filter-paths-3100-files", 3101, false, true, false},
	} {
		files := append(numberedFiles("gen", tc.total-1), "zzz/late.txt")
		p := PushInfo{Ref: "refs/heads/scn/x", ChangedFiles: files}
		if got := paths.MatchPush(p); got != tc.late {
			t.Errorf("%s: paths with the match last = %v, want %v", tc.scenario, got, tc.late)
		}
		if got := early.MatchPush(p); got != tc.early {
			t.Errorf("%s: paths with an early match = %v, want %v", tc.scenario, got, tc.early)
		}
		if got := ignore.MatchPush(p); got != tc.ignore {
			t.Errorf("%s: paths-ignore with the last file not ignored = %v, want %v", tc.scenario, got, tc.ignore)
		}
	}
}

// TestMatchPushRecordedTags replays what github.com did in the scenarios
// push-tags-limit, filter-tags, filter-branches-ignore,
// filter-tag-branches-only and filter-paths-tag. The branches are named
// scn/<scenario>/<run> and the tags scn-<scenario>-<run>-<n>.
func TestMatchPushRecordedTags(t *testing.T) {
	tag := func(n, count int) PushInfo {
		return PushInfo{Ref: fmt.Sprintf("refs/tags/scn-filter-tags-1-%d", n), Created: true, TagCount: count}
	}
	branch := PushInfo{Ref: "refs/heads/scn/filter-tags/1", Created: true, ChangedFiles: []string{"a.txt"}}
	tests := []struct {
		scenario string
		on       string
		push     PushInfo
		want     bool
	}{
		// push-tags-limit: three tags at once start three runs, four tags none.
		{"push-tags-limit 3 tags", "on: push", tag(1, 3), true},
		{"push-tags-limit 3 tags (third)", "on: push", tag(3, 3), true},
		{"push-tags-limit 4 tags (first)", "on: push", tag(1, 4), false},
		{"push-tags-limit 4 tags (last)", "on: push", tag(4, 4), false},
		{"push-tags-limit 4 tags, tags filter", "on:\n  push:\n    tags: ['scn-*']", tag(2, 4), false},

		// filter-tags, push 1 (a branch): t-both and t-branches-only ran, the
		// tags-only workflows did not.
		{"filter-tags branch t-both", "on:\n  push:\n    branches: ['scn/**']\n    tags: ['scn-*']", branch, true},
		{"filter-tags branch t-branches-only", "on:\n  push:\n    branches: ['scn/**']", branch, true},
		{"filter-tags branch t-match", "on:\n  push:\n    tags: ['scn-*']", branch, false},
		{"filter-tags branch t-ignore-other", "on:\n  push:\n    tags-ignore: ['v*']", branch, false},
		{"filter-tags branch t-negated", "on:\n  push:\n    tags:\n      - 'scn-*'\n      - '!scn-*-1'", branch, false},
		// filter-tags, push 2 (tag scn-...-1): ran for t-both, t-match,
		// t-ignore-other and t-negated-other; not for t-branches-only, t-ignore,
		// t-negated and t-nomatch.
		{"filter-tags tag t-both", "on:\n  push:\n    branches: ['scn/**']\n    tags: ['scn-*']", tag(1, 1), true},
		{"filter-tags tag t-match", "on:\n  push:\n    tags: ['scn-*']", tag(1, 1), true},
		{"filter-tags tag t-ignore-other", "on:\n  push:\n    tags-ignore: ['v*']", tag(1, 1), true},
		{"filter-tags tag t-negated-other", "on:\n  push:\n    tags:\n      - 'scn-*'\n      - '!scn-*-2'", tag(1, 1), true},
		{"filter-tags tag t-branches-only", "on:\n  push:\n    branches: ['scn/**']", tag(1, 1), false},
		{"filter-tags tag t-ignore", "on:\n  push:\n    tags-ignore: ['scn-*']", tag(1, 1), false},
		{"filter-tags tag t-negated", "on:\n  push:\n    tags:\n      - 'scn-*'\n      - '!scn-*-1'", tag(1, 1), false},
		{"filter-tags tag t-nomatch", "on:\n  push:\n    tags: ['v*']", tag(1, 1), false},

		// filter-branches-ignore: the branch push ran bi-other and
		// bi-single-star, not bi-ignored and not bi-tags-too; the tag push ran
		// only bi-tags-too (branches-ignore alone ignores tags).
		{"filter-branches-ignore branch bi-ignored", "on:\n  push:\n    branches-ignore: ['scn/**']", branch, false},
		{"filter-branches-ignore branch bi-other", "on:\n  push:\n    branches-ignore: ['main', 'release/**']", branch, true},
		{"filter-branches-ignore branch bi-single-star", "on:\n  push:\n    branches-ignore: ['scn/*']", branch, true},
		{"filter-branches-ignore branch bi-tags-too", "on:\n  push:\n    branches-ignore: ['scn/**']\n    tags: ['scn-*']", branch, false},
		{"filter-branches-ignore tag bi-tags-too", "on:\n  push:\n    branches-ignore: ['scn/**']\n    tags: ['scn-*']", tag(1, 1), true},
		{"filter-branches-ignore tag bi-ignored", "on:\n  push:\n    branches-ignore: ['scn/**']", tag(1, 1), false},
		{"filter-branches-ignore tag bi-other", "on:\n  push:\n    branches-ignore: ['main']", tag(1, 1), false},

		// filter-tag-branches-only: a tag pushed to a repository whose only
		// workflow filters on branches starts nothing.
		{"filter-tag-branches-only", "on:\n  push:\n    branches: ['scn/**']", tag(1, 1), false},

		// filter-paths-tag: paths are not evaluated for tags.
		{"filter-paths-tag paths-only", "on:\n  push:\n    paths: ['nothing/**']", tag(1, 1), true},
		{"filter-paths-tag tag-and-paths-ignore", "on:\n  push:\n    tags: ['scn-*']\n    paths-ignore: ['**']", tag(1, 1), true},
		{"filter-paths-tag tag-and-paths-nomatch", "on:\n  push:\n    tags: ['scn-*']\n    paths: ['nothing/**']", tag(1, 1), true},
		{"filter-paths-tag branches-and-paths", "on:\n  push:\n    branches: ['scn/**']\n    paths: ['nothing/**']", tag(1, 1), false},
		{"filter-paths-tag branches-and-paths, branch", "on:\n  push:\n    branches: ['scn/**']\n    paths: ['nothing/**']", branch, false},

		// push-delete-branch, push-delete-tag: a deletion starts nothing,
		// whatever the filters.
		{"push-delete-branch", "on: push", PushInfo{Ref: "refs/heads/scn/push-delete-branch/1", Deleted: true}, false},
		{"push-delete-tag", "on: push", PushInfo{Ref: "refs/tags/scn-push-delete-tag-1-1", Deleted: true, TagCount: 1}, false},
	}
	for _, tc := range tests {
		t.Run(tc.scenario, func(t *testing.T) {
			if got := pushWorkflow(t, tc.on).MatchPush(tc.push); got != tc.want {
				t.Errorf("MatchPush = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMatchPushRecordedGlobs replays filter-branches (branch
// scn/filter-branches/<run>: ran for b-match, b-negated-then-readded,
// b-prefix-only, b-star-star and b-two-levels; not for b-negated, b-one-level
// and b-other), filter-paths and filter-paths-ignore.
func TestMatchPushRecordedGlobs(t *testing.T) {
	br := PushInfo{Ref: "refs/heads/scn/filter-branches/1"}
	for _, tc := range []struct {
		name, filter string
		want         bool
	}{
		{"b-match", "branches: ['scn/filter-branches/**']", true},
		{"b-negated-then-readded", "branches: ['**', '!scn/**', 'scn/filter-branches/**']", true},
		{"b-prefix-only", "branches: ['scn/filter-*/**']", true},
		{"b-star-star", "branches: ['**']", true},
		{"b-two-levels", "branches: ['scn/*/*']", true},
		{"b-negated", "branches: ['scn/**', '!scn/filter-branches/**']", false},
		{"b-one-level", "branches: ['scn/*']", false},
		{"b-other", "branches: [main, 'release/**']", false},
	} {
		if got := pushWorkflow(t, "on:\n  push:\n    "+tc.filter).MatchPush(br); got != tc.want {
			t.Errorf("filter-branches %s: %v, want %v", tc.name, got, tc.want)
		}
	}

	// filter-paths: the files of each push.
	pushes := [][]string{
		{".github/workflows/p-docs.yml", "docs/readme.md", "other/x.txt", "src/a.txt", "top.md"}, // 1: the workflow files are new too
		{"docs/readme.md"},             // 2
		{"src/a.txt"},                  // 3
		{"top.md"},                     // 4
		{"docs/new.md", "other/x.txt"}, // 5
	}
	for _, tc := range []struct {
		name, filter string
		ran          [5]bool
	}{
		{"p-docs", "paths: ['docs/**']", [5]bool{true, true, false, false, true}},
		{"p-double-star-md", "paths: ['**.md']", [5]bool{true, true, false, true, true}},
		{"p-exact-file", "paths: ['src/a.txt']", [5]bool{true, false, true, false, false}},
		{"p-negated", "paths: ['**', '!docs/**']", [5]bool{true, false, true, true, true}},
		{"p-no-match", "paths: ['nothing/**']", [5]bool{}},
		{"p-single-star-md", "paths: ['*.md']", [5]bool{true, false, false, true, false}},
		{"p-src", "paths: ['src/**']", [5]bool{true, false, true, false, false}},
	} {
		w := pushWorkflow(t, "on:\n  push:\n    branches: ['scn/**']\n    "+tc.filter)
		for n, files := range pushes {
			got := w.MatchPush(PushInfo{Ref: "refs/heads/scn/filter-paths/1", ChangedFiles: files})
			if got != tc.ran[n] {
				t.Errorf("filter-paths %s push %d: %v, want %v", tc.name, n+1, got, tc.ran[n])
			}
		}
	}

	// filter-paths-ignore: pushes 1 to 4 changed these files.
	ignorePushes := [][]string{
		{".github/workflows/pi-docs.yml", "docs/readme.md", "src/a.txt"}, // 1: the workflow files are new too
		{"docs/readme.md"},              // 2
		{"docs/readme.md", "src/a.txt"}, // 3
		{"src/a.txt"},                   // 4
	}
	for _, tc := range []struct {
		name, filter string
		ran          [4]bool
	}{
		{"pi-docs", "paths-ignore: ['docs/**']", [4]bool{true, false, true, true}},
		{"pi-md", "paths-ignore: ['**.md']", [4]bool{true, false, true, true}},
		{"pi-src-docs", "paths-ignore: ['docs/**', 'src/**']", [4]bool{true, false, false, false}},
		{"pi-everything", "paths-ignore: ['**']", [4]bool{}},
	} {
		w := pushWorkflow(t, "on:\n  push:\n    branches: ['scn/**']\n    "+tc.filter)
		for n, files := range ignorePushes {
			got := w.MatchPush(PushInfo{Ref: "refs/heads/scn/filter-paths-ignore/1", ChangedFiles: files})
			if got != tc.ran[n] {
				t.Errorf("filter-paths-ignore %s push %d: %v, want %v", tc.name, n+1, got, tc.ran[n])
			}
		}
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

func TestFilterValidation(t *testing.T) {
	tail := "\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n"
	tests := []struct{ name, on, want string }{
		{"only negated branches", "on:\n  push:\n    branches: ['!main']", "Event 'push' filter 'branches' has only patterns that start with '!'"},
		{"only negated paths", "on:\n  push:\n    paths: ['!docs/**', '!x']", "filter 'paths' has only patterns"},
		{"negated with positive", "on:\n  push:\n    branches: ['**', '!main']", ""},
		{"empty list", "on:\n  push:\n    branches: []", ""},
		{"pull_request too", "on:\n  pull_request:\n    branches: ['!x']", "Event 'pull_request' filter 'branches'"},
	}
	for _, tc := range tests {
		_, errs := Parse("w.yml", []byte(tc.on+tail))
		switch {
		case tc.want == "" && len(errs) > 0:
			t.Errorf("%s: unexpected errors %v", tc.name, errs)
		case tc.want != "" && (len(errs) != 1 || !strings.Contains(errs[0].Error(), tc.want)):
			t.Errorf("%s: errors = %v, want %q", tc.name, errs, tc.want)
		}
	}
}

func TestFiltersAreCompiledOnce(t *testing.T) {
	w := pushWorkflow(t, "on:\n  push:\n    branches: ['release/**']")
	ev := w.On.Event("push")
	if ev.cf == nil {
		t.Fatal("Parse did not compile the filters")
	}
	first := ev.filters()
	if ev.filters() != first || len(first.branches) != 1 {
		t.Error("filters are recompiled on each use")
	}
	// An Event built by hand still works.
	hand := &Workflow{On: &On{Events: []*Event{{Name: "push", Branches: []string{"main"}}}}}
	if !hand.MatchPush(PushInfo{Ref: "refs/heads/main"}) || hand.MatchPush(PushInfo{Ref: "refs/heads/dev"}) {
		t.Error("hand-built workflow does not filter")
	}
}
