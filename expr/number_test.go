// SPDX-License-Identifier: MIT

package expr

import (
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestParseNumberMatchesRunner replays testdata/number_cases.txt, the answers
// of the runner's ExpressionUtility.ParseNumber (gen/runner/number_cases.mjs)
// on 443 strings: hexadecimal, octal and binary forms, signs, exponents,
// Infinity and NaN spellings, Unicode digits and spaces.
func TestParseNumberMatchesRunner(t *testing.T) {
	data, err := os.ReadFile("testdata/number_cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			t.Fatalf("bad line %q", line)
		}
		in := decodeUnits(f[0])
		var want float64
		if f[1] == "NaN" {
			want = math.NaN()
		} else {
			bits, err := strconv.ParseUint(f[1], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			want = math.Float64frombits(bits)
		}
		got := parseNumber(in)
		if math.Float64bits(got) != math.Float64bits(want) && !(math.IsNaN(got) && math.IsNaN(want)) {
			t.Errorf("parseNumber(%q [%s]) = %v (%x), runner %v (%x)", in, f[0], got, math.Float64bits(got), want, math.Float64bits(want))
		}
		n++
	}
	if n < 400 {
		t.Errorf("only %d cases", n)
	}
}

// decodeUnits decodes dot-separated hexadecimal UTF-16 code units ("-" is the
// empty string).
func decodeUnits(h string) string {
	if h == "-" {
		return ""
	}
	var units []uint16
	for _, u := range strings.Split(h, ".") {
		n, _ := strconv.ParseUint(u, 16, 16)
		units = append(units, uint16(n))
	}
	return utf16String(units)
}
