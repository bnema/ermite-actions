// SPDX-License-Identifier: MIT

package workflow

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Cron validation, ported from converter/cron.ts.

type cronRange struct {
	min, max int
	names    map[string]int
}

var (
	cronMinute = cronRange{0, 59, nil}
	cronHour   = cronRange{0, 23, nil}
	cronDOM    = cronRange{1, 31, nil}
	cronMonth  = cronRange{1, 12, map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}}
	cronDOW    = cronRange{0, 6, map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}}
)

func isValidCron(cron string) bool {
	parts := splitSpaces(cron)
	if len(parts) != 5 {
		return false
	}
	return validateCronPart(parts[0], cronMinute, true) && validateCronPart(parts[1], cronHour, true) &&
		validateCronPart(parts[2], cronDOM, true) && validateCronPart(parts[3], cronMonth, true) &&
		validateCronPart(parts[4], cronDOW, true)
}

// splitSpaces is JavaScript's split(/ +/): runs of spaces separate, other
// whitespace does not, and leading or trailing spaces give empty parts.
func splitSpaces(s string) []string {
	var parts []string
	start := 0
	i := 0
	for i < len(s) {
		if s[i] != ' ' {
			i++
			continue
		}
		parts = append(parts, s[start:i])
		for i < len(s) && s[i] == ' ' {
			i++
		}
		start = i
	}
	return append(parts, s[start:])
}

var jsDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// jsNumber is JavaScript's Number(string): surrounding white space is
// ignored, an empty string is 0, and decimal, hexadecimal, octal and binary
// literals and Infinity are numbers. ok is false for NaN.
func jsNumber(v string) (float64, bool) {
	t := strings.Trim(v, " \t\n\v\f\r\u00a0\u2028\u2029\ufeff")
	switch {
	case t == "":
		return 0, true
	case jsDecimal.MatchString(t):
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil || math.IsInf(f, 0)
	case t == "Infinity" || t == "+Infinity":
		return math.Inf(1), true
	case t == "-Infinity":
		return math.Inf(-1), true
	case len(t) > 2 && t[0] == '0':
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[t[1]]
		if base == 0 {
			return 0, false
		}
		n, err := strconv.ParseUint(t[2:], base, 64)
		if err != nil {
			if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
				return math.Inf(1), true
			}
			return 0, false
		}
		return float64(n), true
	}
	return 0, false
}

func cronNumber(v string) (float64, bool) { return jsNumber(v) }

func validateCronPart(value string, r cronRange, allowSeparators bool) bool {
	lower := strings.ToLower(value)
	if _, ok := r.names[lower]; ok {
		return true
	}
	if value == "*" {
		return true
	}
	if strings.Contains(value, ",") {
		if !allowSeparators {
			return false
		}
		for _, v := range strings.Split(value, ",") {
			if v == "" || !validateCronPart(v, r, true) {
				return false
			}
		}
		return true
	}
	if strings.Contains(value, "/") {
		if !allowSeparators {
			return false
		}
		parts := strings.Split(value, "/")
		start, step := parts[0], ""
		if len(parts) > 1 {
			step = parts[1]
		}
		n, ok := cronNumber(step)
		if len(parts) > 2 || !ok || n <= 0 || start == "" || step == "" {
			return false
		}
		return validateCronPart(start, r, true) && validateCronPart(step, r, false)
	}
	if strings.Contains(value, "-") {
		if !allowSeparators {
			return false
		}
		parts := strings.Split(value, "-")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return false
		}
		s, e := cronRangeValue(parts[0], r), cronRangeValue(parts[1], r)
		return validateCronPart(parts[0], r, false) && validateCronPart(parts[1], r, false) && e >= s
	}
	n, ok := cronNumber(value)
	return ok && n >= float64(r.min) && n <= float64(r.max)
}

func cronRangeValue(v string, r cronRange) float64 {
	if n, ok := r.names[strings.ToLower(v)]; ok {
		return float64(n)
	}
	n, ok := cronNumber(v)
	if !ok {
		return math.NaN()
	}
	return n
}
