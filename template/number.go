// SPDX-License-Identifier: MIT

package template

import (
	"math"
	"strconv"
	"strings"
)

// JSNumberString formats x like JavaScript's Number.prototype.toString: the
// shortest decimal that round-trips, positional for decimal exponents from -7
// to 20, exponent notation beyond. The reference uses it for the text of a
// number token.
func JSNumberString(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case x == 0:
		return "0"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	}
	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}
	s := strconv.FormatFloat(x, 'e', -1, 64)
	mant, expStr, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expStr)
	digits := strings.Replace(mant, ".", "", 1)
	k := len(digits)
	n := exp + 1
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	e := n - 1
	esign := "+"
	if e < 0 {
		esign = "-"
		e = -e
	}
	if k == 1 {
		return sign + digits + "e" + esign + strconv.Itoa(e)
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + esign + strconv.Itoa(e)
}

// DotNetNumberString formats x like .NET's Double.ToString("G15",
// CultureInfo.InvariantCulture), which is what the C# runner's NumberToken
// shows (and what a number becomes when it is read as a string): at most 15
// significant digits, positional when the decimal exponent is at least -4 and
// below 15, "E+XX"/"E-XX" with at least two exponent digits otherwise, "-0" for
// negative zero.
func DotNetNumberString(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	case x == 0:
		if math.Signbit(x) {
			return "-0"
		}
		return "0"
	}
	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}
	s := strconv.FormatFloat(x, 'e', 14, 64)
	mant, expStr, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expStr)
	digits := strings.TrimRight(strings.Replace(mant, ".", "", 1), "0")
	if digits == "" {
		digits = "0"
	}
	if exp >= 15 || exp < -4 {
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
	}
	if exp < 0 {
		return sign + "0." + strings.Repeat("0", -exp-1) + digits
	}
	if len(digits) <= exp+1 {
		return sign + digits + strings.Repeat("0", exp+1-len(digits))
	}
	return sign + digits[:exp+1] + "." + digits[exp+1:]
}
