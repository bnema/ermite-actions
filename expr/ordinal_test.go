// SPDX-License-Identifier: MIT

package expr

import (
	"os"
	"strings"
	"testing"
	"unicode/utf16"
)

// TestOrdinalFoldMatchesRunner replays a few hundred strings through .NET's
// StringComparison.OrdinalIgnoreCase, recorded by `runner.dll --cmp` (see
// gen/runner), and checks equality, ordering, contains, startsWith and
// endsWith. The cases are chosen around what differs from JavaScript: ß, ŉ,
// ı, İ, ſ, the Kelvin and Angstrom signs, final sigma, titlecase digraphs,
// supplementary letters, and code points on both sides of U+E000.
func TestOrdinalFoldMatchesRunner(t *testing.T) {
	// "a b want" where a and b are dot-separated UTF-16 code units in hex and
	// want is "equal order contains startsWith endsWith" as printed by --cmp.
	data, err := os.ReadFile("testdata/ordinal_cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	decode := func(h string) string {
		if h == "-" {
			return ""
		}
		var units []uint16
		for _, u := range strings.Split(h, ".") {
			var n uint16
			for _, c := range u {
				n = n<<4 | uint16(strings.IndexRune("0123456789ABCDEF", c))
			}
			units = append(units, n)
		}
		return utf16String(units)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		f := strings.Fields(line)
		if len(f) != 7 {
			t.Fatalf("bad line %q", line)
		}
		a, b := decode(f[0]), decode(f[1])
		want := strings.Join(f[2:], " ")
		b2i := func(v bool) string {
			if v {
				return "1"
			}
			return "0"
		}
		fa, fb := ordinalFold(a), ordinalFold(b)
		got := strings.Join([]string{
			b2i(ordinalEqual(a, b)),
			map[int]string{-1: "-1", 0: "0", 1: "1"}[ordinalCompare(a, b)],
			b2i(jsIncludes(fa, fb)),
			b2i(jsHasPrefix(fa, fb)),
			b2i(jsHasSuffix(fa, fb)),
		}, " ")
		if got != want {
			t.Errorf("%q vs %q (%s %s): got %s, runner %s", a, b, f[0], f[1], got, want)
		}
		n++
	}
	if n < 300 {
		t.Errorf("only %d cases", n)
	}
}

// utf16String decodes UTF-16 code units. The cases here hold only paired
// surrogates.
func utf16String(units []uint16) string { return string(utf16.Decode(units)) }
