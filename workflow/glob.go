// SPDX-License-Identifier: MIT

package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

// Pattern is a compiled filter pattern in the syntax GitHub documents as the
// "filter pattern cheat sheet" for branches, tags and paths:
//
//   - zero or more characters, but not /
//     **     zero or more of any character; "**/" at the start of a pattern or
//     after a "/" may also match nothing, so "**/README.md" matches
//     "README.md" and "docs/**/*.md" matches "docs/a.md"
//     ?      zero or one of the preceding character
//   - one or more of the preceding character
//     [abc]  one character listed or in a range (a-z, A-Z, 0-9)
//     !      at the start of a pattern: negates the patterns before it
//     \      escapes the next character
//
// Every other character, "." included, stands for itself. Matching is
// case-sensitive and covers the whole name.
//
// Recorded on github.com (filter-branches, filter-paths, filter-paths-ignore,
// see TestMatchPushRecordedGlobs): "*", "**", "scn/filter-*/**", "**.md",
// "*.md" and negation order. TODO(verify with recording): "**/" matching
// nothing, "\" escapes and how ? and + behave after a wildcard or a bracket
// are reconstructed.
type Pattern struct {
	// Negated is set for a pattern that started with "!".
	Negated bool
	// Source is the pattern as written.
	Source string
	re     *regexp.Regexp
}

// CompilePattern compiles one pattern. A pattern that cannot be compiled never
// matches; the error says why.
func CompilePattern(p string) (*Pattern, error) {
	pat := &Pattern{Source: p}
	body := p
	if strings.HasPrefix(body, "!") {
		pat.Negated = true
		body = body[1:]
	}
	re, err := regexp.Compile("^(?s:" + translateGlob(body) + ")$")
	if err != nil {
		return pat, err
	}
	pat.re = re
	return pat, nil
}

// Match reports whether name matches the pattern, ignoring any "!" prefix.
func (p *Pattern) Match(name string) bool { return p.re != nil && p.re.MatchString(name) }

// translateGlob converts a pattern to an RE2 expression.
func translateGlob(p string) string {
	var b strings.Builder
	rs := []rune(p)
	// quantifiable is true when the last thing written is a single character
	// or a class that ? and + can apply to.
	quantifiable := false
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case '\\':
			if i+1 < len(rs) {
				i++
				b.WriteString(regexp.QuoteMeta(string(rs[i])))
			} else {
				b.WriteString(`\\`)
			}
			quantifiable = true
		case '*':
			if i+1 < len(rs) && rs[i+1] == '*' {
				for i+1 < len(rs) && rs[i+1] == '*' {
					i++
				}
				atStart := b.Len() == 0 || (i-2 >= 0 && rs[i-2] == '/')
				if atStart && i+1 < len(rs) && rs[i+1] == '/' {
					i++
					b.WriteString(`(?:.*/)?`)
				} else {
					b.WriteString(`.*`)
				}
			} else {
				b.WriteString(`[^/]*`)
			}
			quantifiable = false
		case '?', '+':
			if quantifiable {
				b.WriteRune(c)
				quantifiable = false
			} else {
				b.WriteString(regexp.QuoteMeta(string(c)))
				quantifiable = true
			}
		case '[':
			if class, n, ok := parseClass(rs[i:]); ok {
				b.WriteString(class)
				i += n - 1
				quantifiable = true
			} else {
				b.WriteString(`\[`)
				quantifiable = true
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
			quantifiable = true
		}
	}
	return b.String()
}

// parseClass reads "[...]" at the start of rs: letters, digits and ranges of
// them. It returns the RE2 class and the number of runes consumed.
func parseClass(rs []rune) (string, int, bool) {
	var b strings.Builder
	b.WriteByte('[')
	n := 0
	for i := 1; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == ']':
			if n == 0 {
				return "", 0, false
			}
			b.WriteByte(']')
			return b.String(), i + 1, true
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteRune(c)
			n++
		case c == '-' && n > 0 && i+1 < len(rs) && rs[i+1] != ']':
			b.WriteByte('-')
		default:
			return "", 0, false
		}
	}
	return "", 0, false
}

// matchList applies compiled patterns to name in order. A pattern that
// matches includes the name, a "!" pattern that matches excludes it again, and
// the last pattern that matches decides. A list with no positive pattern
// therefore matches nothing.
func matchList(patterns []*Pattern, name string) bool {
	matched := false
	for _, p := range patterns {
		if p.Match(name) {
			matched = !p.Negated
		}
	}
	return matched
}

// compileList compiles a filter list. It reports the first pattern that does
// not compile.
func compileList(patterns []string) ([]*Pattern, error) {
	out := make([]*Pattern, 0, len(patterns))
	for _, s := range patterns {
		p, err := CompilePattern(s)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern '%s': %w", s, err)
		}
		out = append(out, p)
	}
	return out, nil
}
