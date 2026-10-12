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

var pow10to15 = new(big.Int).Exp(big.NewInt(10), big.NewInt(15), nil)

// numberToCoercedString is NumberData.coerceString of the reference
// implementation: (+x.toFixed(15)).toString(). Rounding to 15 decimals
// removes binary noise (0.1+0.2 is "0.3") and ties round half up like
// toFixed.
func numberToCoercedString(x float64) string {
	if x == 0 {
		return "0"
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return jsNumberToString(x)
	}
	a := math.Abs(x)
	// toFixed returns Number::toString from 1e21 up, and for integers the
	// fixed form parses back to the same number.
	if a >= 1e21 || a == math.Trunc(a) {
		return jsNumberToString(x)
	}
	r := new(big.Rat).SetFloat64(a)
	r.Mul(r, new(big.Rat).SetInt(pow10to15))
	q, rem := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if rem.Lsh(rem, 1).Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	digits := q.String()
	if len(digits) < 16 {
		digits = strings.Repeat("0", 16-len(digits)) + digits
	}
	fixed := digits[:len(digits)-15] + "." + digits[len(digits)-15:]
	v, _ := strconv.ParseFloat(fixed, 64)
	if x < 0 {
		v = -v
	}
	return jsNumberToString(v)
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
