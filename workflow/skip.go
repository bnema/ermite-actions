// SPDX-License-Identifier: MIT

package workflow

import "strings"

var skipMarkers = []string{"[skip ci]", "[ci skip]", "[no ci]", "[skip actions]", "[actions skip]"}

// SkipCI reports whether the message of the head commit of a push (or of the
// commit a tag points at) tells GitHub not to run workflows for it. As
// recorded on github.com (scenarios skip-directives, skip-directives-placement,
// skip-checks-trailer and skip-directive-tag):
//
//   - the markers [skip ci], [ci skip], [no ci], [skip actions] and
//     [actions skip] skip anywhere in the subject or the body, and are
//     case-sensitive: [SKIP CI] and [Skip Ci] do not skip. Near misses do
//     not skip either: [skip-ci], "skip ci" without brackets, [skip ci please]
//     and the markers ***NO_CI*** and ***SKIP_CI***;
//   - the trailer "skip-checks: true" skips only as the end of the message,
//     preceded by two blank lines ("x\n\n\nskip-checks: true"). The space
//     after the colon is optional and trailing white space is ignored. It does
//     not skip with one blank line or none, with text after it, after another
//     trailer, with another value ("false", "yes"), or with any change of case
//     in the key or the value.
//
// Not recorded, so assumed: only the head commit of a push is looked at (the
// scenarios push one commit at a time); more than two blank lines before the
// trailer count like two; a line holding only spaces or tabs is not blank
// (the two lines before the trailer must be empty); trailing blank lines and
// CRLF line ends are ignored; more than one space after the colon is not
// accepted; and a message made of the trailer alone does not skip.
func SkipCI(headCommitMessage string) bool {
	for _, m := range skipMarkers {
		if strings.Contains(headCommitMessage, m) {
			return true
		}
	}
	return hasSkipChecksTrailer(headCommitMessage)
}

// hasSkipChecksTrailer reports whether the message ends with a
// "skip-checks: true" line preceded by two empty lines and some text.
func hasSkipChecksTrailer(msg string) bool {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	msg = strings.TrimRight(msg, " \t\n")
	lines := strings.Split(msg, "\n")
	n := len(lines)
	if n < 4 {
		return false
	}
	val, ok := strings.CutPrefix(lines[n-1], "skip-checks:")
	if !ok || strings.TrimPrefix(val, " ") != "true" {
		return false
	}
	return lines[n-2] == "" && lines[n-3] == "" && strings.TrimSpace(strings.Join(lines[:n-3], "")) != ""
}
