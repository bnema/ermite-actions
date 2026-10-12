// SPDX-License-Identifier: MIT

package expr

import (
	"math"
	"math/big"
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

// jsToNumber is Number(string): the StringToNumber algorithm of ECMAScript.
// Surrounding whitespace is ignored, the empty string is 0, and the accepted
// forms are decimal literals with optional sign, fraction and exponent,
// [+-]Infinity, and unsigned 0x, 0o and 0b integers. Everything else, including
// digit separators and "inf" or "nan", is NaN.
func jsToNumber(s string) float64 {
	s = jsTrim(s)
	if s == "" {
		return 0
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, ok := new(big.Int).SetString(s[2:], base)
			if !ok || strings.ContainsAny(s[2:], "_+-") {
				return nan
			}
			f, _ := new(big.Float).SetInt(n).Float64()
			return f
		}
	}
	body := s
	neg := false
	if body[0] == '+' || body[0] == '-' {
		neg = body[0] == '-'
		body = body[1:]
	}
	if body == "Infinity" {
		if neg {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	if !isDecimalLiteral(body) {
		return nan
	}
	v, _ := strconv.ParseFloat(body, 64) // out of range yields ±Inf, which is what JS does too
	if neg {
		v = -v
	}
	return v
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
