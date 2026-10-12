// SPDX-License-Identifier: MIT

package workflow

import "strings"

// Limits GitHub applies to push events.
const (
	// MaxFilesEvaluated is the number of changed files a paths or paths-ignore
	// filter looks at; files after the 3000th are ignored. Recorded on
	// github.com (filter-paths-3000-boundary): a push of exactly 3000 files
	// whose only match is the last one runs the workflow, the same push with
	// 3001 files does not. It is not 300 (filter-paths-300-files and
	// filter-paths-1200-files: a match beyond the 300th file still runs).
	MaxFilesEvaluated = 3000
	// MaxTagsPerPush is the number of tags that can be pushed at once before
	// GitHub creates no event for them.
	MaxTagsPerPush = 3
)

// PushInfo describes a push to one ref.
type PushInfo struct {
	// Ref is the full name, "refs/heads/main" or "refs/tags/v1".
	Ref string
	// Created and Deleted say whether the push created or deleted the ref.
	Created bool
	Deleted bool
	// ChangedFiles are the paths changed by the push, in the order of the
	// diff. Only the first MaxFilesEvaluated are looked at.
	ChangedFiles []string
	// FilesTruncated is set when the list of changed files is not available:
	// the push has more than 1000 commits, or the diff could not be
	// computed. A paths filter is then considered satisfied.
	FilesTruncated bool
	// TagCount is the number of tags pushed in the same push.
	TagCount int
}

// MatchPush reports whether a push triggers the workflow, applying the
// "push" event's filters like GitHub:
//
//   - the deletion of a ref triggers nothing;
//   - a push of more than three tags creates no event for its tags;
//   - a workflow with only branches or branches-ignore does not run for a tag,
//     and one with only tags or tags-ignore does not run for a branch; with
//     neither it runs for both;
//   - when both a branches and a tags filter exist, a ref is checked against
//     the filter for its kind;
//   - paths and paths-ignore apply to branch pushes only, against the first
//     3000 changed files; when the list is truncated they pass;
//   - patterns are evaluated in order and the last one that matches wins.
//
// Commit-message directives are separate: see [SkipCI].
//
// Recorded on github.com: push-tags-limit, filter-tags, filter-branches-ignore,
// filter-tag-branches-only, filter-paths-tag, push-delete-branch and
// push-delete-tag, see TestMatchPushRecordedTags. Still assumed: the
// behaviour on refs that are neither a branch nor a tag, and on a push that
// changes no file.
func (w *Workflow) MatchPush(p PushInfo) bool {
	if w == nil || w.On == nil {
		return false
	}
	ev := w.On.Event("push")
	if ev == nil {
		return false
	}
	if p.Deleted {
		return false
	}
	switch {
	case strings.HasPrefix(p.Ref, "refs/heads/"):
		name := strings.TrimPrefix(p.Ref, "refs/heads/")
		if !ev.refFilterAllows(true, name) {
			return false
		}
		return ev.pathsAllow(p)
	case strings.HasPrefix(p.Ref, "refs/tags/"):
		if p.TagCount > MaxTagsPerPush {
			return false
		}
		return ev.refFilterAllows(false, strings.TrimPrefix(p.Ref, "refs/tags/"))
	}
	return false
}

// refFilterAllows applies the branches or tags filters to the name of a
// branch (branch is true) or a tag.
func (e *Event) refFilterAllows(branch bool, name string) bool {
	cf := e.filters()
	inc, exc := cf.branches, cf.branchesIgnore
	own, ownIgnore, other, otherIgnore := e.Branches, e.BranchesIgnore, e.Tags, e.TagsIgnore
	if !branch {
		inc, exc = cf.tags, cf.tagsIgnore
		own, ownIgnore, other, otherIgnore = e.Tags, e.TagsIgnore, e.Branches, e.BranchesIgnore
	}
	if own == nil && ownIgnore == nil && (other != nil || otherIgnore != nil) {
		return false
	}
	if own != nil && !matchList(inc, name) {
		return false
	}
	if ownIgnore != nil && matchList(exc, name) {
		return false
	}
	return true
}

// pathsAllow applies paths and paths-ignore to a branch push.
func (e *Event) pathsAllow(p PushInfo) bool {
	if e.Paths == nil && e.PathsIgnore == nil {
		return true
	}
	if p.FilesTruncated {
		return true
	}
	cf := e.filters()
	files := p.ChangedFiles
	if len(files) > MaxFilesEvaluated {
		files = files[:MaxFilesEvaluated]
	}
	if e.Paths != nil {
		any := false
		for _, f := range files {
			if matchList(cf.paths, f) {
				any = true
				break
			}
		}
		if !any {
			return false
		}
	}
	if e.PathsIgnore != nil && len(files) > 0 {
		any := false
		for _, f := range files {
			if !matchList(cf.pathsIgnore, f) {
				any = true
				break
			}
		}
		if !any {
			return false
		}
	}
	return true
}
