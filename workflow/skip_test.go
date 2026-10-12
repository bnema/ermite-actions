// SPDX-License-Identifier: MIT

package workflow

import "testing"

func TestSkipCI(t *testing.T) {
	tests := []struct {
		name, msg string
		want      bool
	}{
		{"empty", "", false},
		{"plain", "fix: a bug", false},
		{"skip ci", "docs: typo [skip ci]", true},
		{"ci skip", "docs: typo [ci skip]", true},
		{"no ci", "docs: typo [no ci]", true},
		{"skip actions", "docs: typo [skip actions]", true},
		{"actions skip", "docs: typo [actions skip]", true},
		{"in body", "docs: typo\n\nlong text [skip ci] more", true},
		{"in subject start", "[skip ci] docs", true},
		{"no brackets", "skip ci", false},
		{"other brackets", "[skip tests]", false},
		{"partial", "[skip  ci]", false},
		// TODO(verify with recording): case of the markers.
		{"upper case marker", "docs [SKIP CI]", true},
		{"mixed case marker", "docs [Skip Ci]", true},

		// skip-checks trailer.
		{"trailer", "feat: x\n\nskip-checks: true", true},
		{"trailer no space", "feat: x\n\nskip-checks:true", true},
		{"trailer with body", "feat: x\n\nbody text\n\nskip-checks: true", true},
		{"trailer after two blank lines", "feat: x\n\n\nskip-checks: true\n", true},
		{"trailer crlf", "feat: x\r\n\r\nskip-checks: true\r\n", true},
		{"trailer with other trailers", "feat: x\n\nSigned-off-by: A <a@x>\nskip-checks: true", true},
		{"trailer false", "feat: x\n\nskip-checks: false", false},
		{"trailer not last paragraph", "feat: x\n\nskip-checks: true\n\nmore text", false},
		{"trailer in first paragraph only", "skip-checks: true", false},
		{"trailer in subject", "feat: x skip-checks: true", false},
		// TODO(verify with recording): case of key and value.
		{"trailer upper", "feat: x\n\nSkip-Checks: TRUE", true},
	}
	for _, tc := range tests {
		if got := SkipCI(tc.msg); got != tc.want {
			t.Errorf("%s: SkipCI(%q) = %v, want %v", tc.name, tc.msg, got, tc.want)
		}
	}
}
