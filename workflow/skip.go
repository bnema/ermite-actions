// SPDX-License-Identifier: MIT

package workflow

import "strings"

var skipMarkers = []string{"[skip ci]", "[ci skip]", "[no ci]", "[skip actions]", "[actions skip]"}

// SkipCI reports whether the message of the head commit of a push tells
// GitHub not to run workflows for it: it contains [skip ci], [ci skip],
// [no ci], [skip actions] or [actions skip], or it ends with a
// "skip-checks: true" (or "skip-checks:true") trailer in its last paragraph.
//
// TODO(verify with recording): the following is assumed. Markers are matched
// without regard to case, anywhere in the message, subject or body. GitHub
// documents the trailer as "two empty lines followed by skip-checks: true";
// here one blank line is enough, the trailer must be in the last paragraph
// and not the first one, its key and value are case-insensitive, and other
// trailers may share its paragraph. Whether GitHub looks at the head commit
// only, or at every commit of the push, is not settled by its documentation.
func SkipCI(headCommitMessage string) bool {
	lower := strings.ToLower(headCommitMessage)
	for _, m := range skipMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return hasSkipChecksTrailer(headCommitMessage)
}

func hasSkipChecksTrailer(msg string) bool {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	var paras [][]string
	var cur []string
	for _, line := range strings.Split(msg, "\n") {
		if strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				paras = append(paras, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		paras = append(paras, cur)
	}
	if len(paras) < 2 {
		return false
	}
	for _, line := range paras[len(paras)-1] {
		key, val, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "skip-checks") && strings.EqualFold(strings.TrimSpace(val), "true") {
			return true
		}
	}
	return false
}
