// SPDX-License-Identifier: MIT

package workflow

import "testing"

// TestSkipCI replays the commit messages pushed by the recorded scenarios
// skip-directives, skip-directives-placement, skip-checks-trailer and
// skip-directive-tag. skip is true when GitHub started no run.
func TestSkipCI(t *testing.T) {
	tests := []struct {
		scenario, msg string
		skip          bool
	}{
		// skip-directives
		{"skip-directives 1", "change one [skip ci]", true},
		{"skip-directives 2", "change two [ci skip]", true},
		{"skip-directives 3", "change three [no ci]", true},
		{"skip-directives 4", "change four [skip actions]", true},
		{"skip-directives 5", "change five [actions skip]", true},
		{"skip-directives 6", "change six, no directive", false},

		// skip-directives-placement
		{"placement 1 body", "subject\n\nbody line [skip ci] more body", true},
		{"placement 2 mid-subject", "prefix [skip ci] suffix", true},
		{"placement 3 upper case", "upper case [SKIP CI]", false},
		{"placement 4 mixed case", "mixed case [Skip Ci]", false},
		{"placement 5 near miss dash", "near miss [skip-ci]", false},
		{"placement 6 no brackets", "near miss skip ci without brackets", false},
		{"placement 7 extra word", "near miss [skip ci please]", false},
		{"placement 8 NO_CI", "marker ***NO_CI*** in subject", false},
		{"placement 9 SKIP_CI", "marker ***SKIP_CI*** in subject", false},

		// skip-checks-trailer
		{"trailer 1 one blank line", "trailer true\n\nskip-checks: true", false},
		{"trailer 2 false, one blank line", "trailer false\n\nskip-checks: false", false},
		{"trailer 3 upper, one blank line", "trailer upper\n\nSkip-Checks: true", false},
		{"trailer 4 other trailer first", "trailer after other trailer\n\nSigned-off-by: A Person <a@example.invalid>\nskip-checks: true", false},
		{"trailer 5 in the subject", "not a trailer, in the subject skip-checks: true", false},
		{"trailer 6 no blank line", "trailer without blank line\nskip-checks: true", false},
		{"trailer 7 two blank lines", "two blank lines true\n\n\nskip-checks: true", true},
		{"trailer 8 no space", "two blank lines no space\n\n\nskip-checks:true", true},
		{"trailer 9 false", "two blank lines false\n\n\nskip-checks: false", false},
		{"trailer 10 value upper", "two blank lines upper\n\n\nSkip-Checks: TRUE", false},
		{"trailer 11 text after", "two blank lines then more text\n\n\nskip-checks: true\nand more", false},
		{"trailer 12 after other trailer", "two blank lines after other trailer\n\n\nSigned-off-by: A Person <a@example.invalid>\nskip-checks: true", false},
		{"trailer 13 trailing spaces", "two blank lines trailing spaces\n\n\nskip-checks: true  ", true},
		{"trailer 14 yes", "two blank lines yes\n\n\nskip-checks: yes", false},
		{"trailer 15 key upper", "two blank lines key upper value lower\n\n\nSkip-Checks: true", false},
		{"trailer 16 value upper", "two blank lines key lower value upper\n\n\nskip-checks: TRUE", false},
		{"trailer 17 key mixed", "two blank lines key mixed\n\n\nSkip-checks: true", false},
		{"trailer 18 plain", "plain commit", false},

		// skip-directive-tag: the message of the commit a tag points at.
		{"tag 1", "tagged [skip ci]", true},
		{"tag 2", "tagged normally", false},

		// Not recorded: see the doc comment of SkipCI.
		{"trailing newline", "x\n\n\nskip-checks: true\n", true},
		{"crlf", "x\r\n\r\n\r\nskip-checks: true\r\n", true},
		{"three blank lines", "x\n\n\n\nskip-checks: true", true},
		{"trailer alone", "\n\n\nskip-checks: true", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		if got := SkipCI(tc.msg); got != tc.skip {
			t.Errorf("%s: SkipCI(%q) = %v, want %v", tc.scenario, tc.msg, got, tc.skip)
		}
	}
}
