// SPDX-License-Identifier: MIT

package workflow

import "testing"

func TestPatternMatch(t *testing.T) {
	// Cases from GitHub's "Filter pattern cheat sheet" and the examples
	// around it.
	tests := []struct {
		pattern, name string
		want          bool
	}{
		// feature/*: the * matches any character except /.
		{"feature/*", "feature/my-branch", true},
		{"feature/*", "feature/your-branch", true},
		{"feature/*", "feature", false},
		{"feature/*", "feature/a/b", false},
		// feature/**: matches any branch with feature/ and slashes.
		{"feature/**", "feature/beta-a/my-branch", true},
		{"feature/**", "feature/your-branch", true},
		{"feature/**", "feature/mona/the/octocat", true},
		// main, releases/mona-the-octocat: exact.
		{"main", "main", true},
		{"main", "main2", false},
		{"main", "xmain", false},
		{"releases/mona-the-octocat", "releases/mona-the-octocat", true},
		// '*': all names that do not contain a slash.
		{"*", "main", true},
		{"*", "releases", true},
		{"*", "releases/10", false},
		// '**': all names.
		{"**", "all/the/branches", true},
		{"**", "every/tag", true},
		{"**", "", true},
		// '*feature': * is a special character in YAML, quoted here.
		{"*feature", "mona-feature", true},
		{"*feature", "feature", true},
		{"*feature", "ver-10-feature", true},
		{"*feature", "a/feature", false},
		// v2*
		{"v2*", "v2", true},
		{"v2*", "v2.0", true},
		{"v2*", "v2.9", true},
		{"v2*", "v2/x", false},
		// v[12].[0-9]+.[0-9]+
		{"v[12].[0-9]+.[0-9]+", "v1.10.1", true},
		{"v[12].[0-9]+.[0-9]+", "v2.0.0", true},
		{"v[12].[0-9]+.[0-9]+", "v3.0.0", false},
		{"v[12].[0-9]+.[0-9]+", "v1.x.1", false},
		// releases/**-alpha
		{"releases/**-alpha", "releases/beta/3-alpha", true},
		{"releases/**-alpha", "releases/v10/3-alpha", true},
		{"releases/**-alpha", "releases/3-alpha", true},
		{"releases/**-alpha", "releases/3-beta", false},
		// ? : zero or one of the preceding character.
		{"colou?r", "color", true},
		{"colou?r", "colour", true},
		{"colou?r", "colouur", false},
		// + : one or more.
		{"a+b", "ab", true},
		{"a+b", "aaab", true},
		{"a+b", "b", false},
		// paths
		{"**.js", "app.js", true},
		{"**.js", "src/app.js", true},
		{"**.js", "src/app.ts", false},
		{"*.js", "app.js", true},
		{"*.js", "src/app.js", false},
		{"docs/**", "docs/readme.md", true},
		{"docs/**", "docs/a/b/c.md", true},
		{"docs/**", "docs", false},
		{"docs/**/*.md", "docs/a.md", true},
		{"docs/**/*.md", "docs/x/y/a.md", true},
		{"docs/**/*.md", "docs/a.txt", false},
		{"**/README.md", "README.md", true},
		{"**/README.md", "a/b/README.md", true},
		{"**/README.md", "a/XREADME.md", false},
		{"sub-project/**", "sub-project/src/main.go", true},
		{"/README.md", "README.md", false},
		// Special characters stand for themselves.
		{"a.b", "a.b", true},
		{"a.b", "axb", false},
		{"a(b)", "a(b)", true},
		{`a\*b`, "a*b", true},
		{`a\*b`, "axb", false},
		// Case-sensitive.
		{"Main", "main", false},
	}
	for _, tc := range tests {
		p, err := CompilePattern(tc.pattern)
		if err != nil {
			t.Errorf("%q: %v", tc.pattern, err)
			continue
		}
		if got := p.Match(tc.name); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestMatchListOrder(t *testing.T) {
	// "The order that you define patterns matters": a later negative pattern
	// excludes, a later positive one includes again.
	tests := []struct {
		patterns []string
		name     string
		want     bool
	}{
		{[]string{"releases/**", "!releases/**-alpha"}, "releases/v1", true},
		{[]string{"releases/**", "!releases/**-alpha"}, "releases/v1-alpha", false},
		{[]string{"releases/**", "!releases/**-alpha", "releases/**/ga"}, "releases/v1-alpha/ga", true},
		{[]string{"!releases/**-alpha", "releases/**"}, "releases/v1-alpha", true},
		{[]string{"!docs/**"}, "src/a.go", false}, // only negatives: matches nothing
		{[]string{"**", "!docs/**"}, "src/a.go", true},
		{[]string{"**", "!docs/**"}, "docs/a.md", false},
		{nil, "x", false},
	}
	for _, tc := range tests {
		if got := matchList(mustList(t, tc.patterns), tc.name); got != tc.want {
			t.Errorf("matchList(%q, %q) = %v, want %v", tc.patterns, tc.name, got, tc.want)
		}
	}
}

func mustList(t testing.TB, patterns []string) []*Pattern {
	t.Helper()
	l, err := compileList(patterns)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestUnclosedBracketIsLiteral(t *testing.T) {
	if !matchList(mustList(t, []string{"[abc"}), "[abc") {
		t.Error("an unclosed bracket should be literal")
	}
}

func FuzzPattern(f *testing.F) {
	for _, s := range []string{"*", "**", "a/**/b", "[a-z]+", "!x", `\`, "?+", "[", "]]"} {
		f.Add(s, "a/b")
	}
	f.Fuzz(func(t *testing.T, pattern, name string) {
		if p, err := CompilePattern(pattern); err == nil {
			p.Match(name)
		}
	})
}
