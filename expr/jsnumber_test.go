// SPDX-License-Identifier: MIT

package expr

import (
	"math"
	"strconv"
	"strings"
)

// The helpers below reproduce what the JavaScript reference prints for a
// number. The package itself follows the C# runner (numberToCoercedString);
// the oracle tests need the JavaScript form to read the recorded tokens.

// jsNumberToString is Number.prototype.toString: the shortest decimal that
// round-trips, in positional notation for exponents from -7 to 20 and in
// exponent notation ("1e+21", "1.5e-7") beyond.
func jsNumberToString(x float64) string {
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
	s := strconv.FormatFloat(x, 'e', -1, 64) // d.ddde±xx
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
