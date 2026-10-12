// SPDX-License-Identifier: MIT

package expr

import (
	"math"
	"strconv"
	"strings"
)

var (
	nan    = math.NaN()
	posInf = math.Inf(1)
)

// numberToCoercedString is the string form of a number: .NET's
// Double.ToString("G15", InvariantCulture), which is what the C# runner uses
// for every number-to-string conversion (ExpressionConstants.NumberFormat,
// EvaluationResult.ConvertToString, hence format(), join(), toJSON() and the
// comparison with strings). The JavaScript reference computes
// (+x.toFixed(15)).toString() instead; they agree on ordinary values and
// differ on large, small and very precise ones. See PORTING.md.
//
// G15 rounds to 15 significant digits (ties to even on the exact binary
// value), then prints positionally when the decimal exponent X of the rounded
// value satisfies -5 < X < 15, and as d.dddE+XX otherwise, with the trailing
// zeros of the mantissa removed and at least two exponent digits. NaN is
// "NaN" and the infinities are "Infinity" and "-Infinity". Negative zero is
// "0": .NET Core prints "-0", which Sdk/Expressions/EvaluationResult.cs
// (ConvertToString, "keep our behavior consistent") and the shared fixtures
// (coerce_string.json, format('{0}', -0)) undo; the older
// DTExpressions2 copy of the runner lacks that fix and prints "-0".
func numberToCoercedString(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	case x == 0:
		return "0"
	}
	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}
	s := strconv.FormatFloat(x, 'e', 14, 64) // d.dddddddddddddde±xx, correctly rounded
	mant, expStr, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expStr)
	digits := strings.TrimRight(strings.Replace(mant, ".", "", 1), "0")
	if digits == "" {
		digits = "0"
	}
	switch {
	case exp >= 15 || exp <= -5:
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		esign := "+"
		if exp < 0 {
			esign = "-"
			exp = -exp
		}
		e := strconv.Itoa(exp)
		if len(e) < 2 {
			e = "0" + e
		}
		return sign + out + "E" + esign + e
	case exp < 0:
		return sign + "0." + strings.Repeat("0", -exp-1) + digits
	case len(digits) <= exp+1:
		return sign + digits + strings.Repeat("0", exp+1-len(digits))
	}
	return sign + digits[:exp+1] + "." + digits[exp+1:]
}

// isNetSpace reports whether r is white space for .NET's Char.IsWhiteSpace,
// which String.Trim() removes: U+0009..U+000D, U+0020, U+0085, U+00A0,
// U+1680, U+2000..U+200A, U+2028, U+2029, U+202F, U+205F and U+3000. (The
// JavaScript trim also removes U+FEFF and not U+0085.)
func isNetSpace(r rune) bool {
	switch {
	case r >= 0x09 && r <= 0x0D, r == 0x20, r == 0x85, r == 0xA0, r == 0x1680,
		r >= 0x2000 && r <= 0x200A, r == 0x2028, r == 0x2029, r == 0x202F, r == 0x205F, r == 0x3000:
		return true
	}
	return false
}

// parseNumber converts a string to a number the way the runner does,
// ExpressionUtility.ParseNumber (src/Sdk/DTExpressions2/Expressions2/Sdk/
// ExpressionUtility.cs), which is used for number literals in expressions and
// for every implicit string-to-number conversion:
//
//   - the string is trimmed (see isNetSpace); empty means 0;
//   - Double.TryParse with AllowLeadingSign | AllowDecimalPoint |
//     AllowExponent in the invariant culture: [sign] digits [. digits]
//     [e [sign] digits], at least one digit around the point, ASCII digits
//     only, an out-of-range exponent giving an infinity, "Infinity" and
//     "-Infinity" in any letter case, NaN, and any number of trailing NUL
//     characters, which .NET ignores;
//   - "0x" followed by hexadecimal digits and "0o" followed by octal digits
//     (lower-case prefix, no sign), read as a 32-bit two's-complement
//     integer: 0xffffffff is -1, and a value that does not fit in 32 bits is
//     NaN. There is no binary form;
//   - everything else is NaN.
//
// The JavaScript reference uses Number(), which differs: 0X, 0O and 0B
// prefixes, 64-bit exactness, no 32-bit wrap, and U+FEFF as white space.
func parseNumber(s string) float64 {
	s = strings.TrimFunc(s, isNetSpace)
	if s == "" {
		return 0
	}
	t := strings.TrimRight(s, "\x00")
	if body, neg := strings.CutPrefix(t, "-"); neg {
		if v, ok := parseDecimal(body); ok {
			return -v
		}
	} else {
		body, _ = strings.CutPrefix(t, "+")
		if v, ok := parseDecimal(body); ok {
			return v
		}
	}
	if len(s) > 2 && s[0] == '0' {
		switch s[1] {
		case 'x':
			if v, ok := parseRadix32(s[2:], 16); ok {
				return v
			}
		case 'o':
			if v, ok := parseRadix32(s[2:], 8); ok {
				return v
			}
		}
	}
	return nan
}

// parseDecimal parses an unsigned decimal literal or "Infinity".
func parseDecimal(body string) (float64, bool) {
	if len(body) == 8 && strings.EqualFold(body, "infinity") && isASCII(body) {
		return posInf, true
	}
	if !isDecimalLiteral(body) {
		return 0, false
	}
	v, _ := strconv.ParseFloat(body, 64) // out of range yields an infinity, as in .NET
	return v, true
}

// parseRadix32 reads digits in base 16 or 8 as an unsigned 32-bit value and
// returns it as a signed one (Int32.TryParse with AllowHexSpecifier,
// Convert.ToInt32(s, 8)). Leading zeros are free; digits beyond 32 bits fail.
func parseRadix32(digits string, base int) (float64, bool) {
	if digits == "" {
		return 0, false
	}
	var v uint64
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return 0, false
		}
		if d >= base {
			return 0, false
		}
		v = v*uint64(base) + uint64(d)
		if v > math.MaxUint32 {
			return 0, false
		}
	}
	return float64(int32(uint32(v))), true
}

// isDecimalLiteral reports whether s matches
// (digits [. digits?] | . digits) ([eE] [+-]? digits)?.
func isDecimalLiteral(s string) bool {
	i := 0
	digits := func() int {
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i - start
	}
	intDigits := digits()
	fracDigits := 0
	if i < len(s) && s[i] == '.' {
		i++
		fracDigits = digits()
	}
	if intDigits == 0 && fracDigits == 0 {
		return false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return i == len(s)
}
