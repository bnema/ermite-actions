// SPDX-License-Identifier: MIT

package expr

import (
	"math"
	"strconv"
	"strings"
)

// builtinImpls are the implementations of the built-in functions, keyed by
// lower-cased name. Arity is validated by the parser, so each may assume its
// documented argument count.
var builtinImpls = map[string]Func{
	"case":       funcCase,
	"contains":   funcContains,
	"endswith":   funcEndsWith,
	"format":     funcFormat,
	"fromjson":   funcFromJSON,
	"join":       funcJoin,
	"startswith": funcStartsWith,
	"tojson":     funcToJSON,
}

// funcCase is case(pred1, val1, pred2, val2, ..., default).
//
// The JavaScript reference ships it behind an experimental feature flag
// (allowCaseFunction) that only affects editor completion; the parser always
// accepts it, and so does this package.
func funcCase(args []Value) (Value, error) {
	for i := 0; i < len(args)-1; i += 2 {
		pred, ok := normalize(args[i]).(Boolean)
		if !ok {
			return nil, evalErrorf("case predicate must evaluate to a boolean value")
		}
		if pred {
			return args[i+1], nil
		}
	}
	return args[len(args)-1], nil
}

// funcContains is contains(search, item): substring test, case-insensitive
// via toLowerCase, when both are primitive; membership by == in an array;
// false otherwise.
func funcContains(args []Value) (Value, error) {
	left, right := normalize(args[0]), normalize(args[1])
	switch {
	case isPrimitive(left):
		if isPrimitive(right) {
			return Boolean(strings.Contains(jsToLower(CoerceString(left)), jsToLower(CoerceString(right)))), nil
		}
	case left.Kind() == KindArray:
		for _, v := range left.(*Array).items {
			if equals(right, v) {
				return Boolean(true), nil
			}
		}
	}
	return Boolean(false), nil
}

func funcStartsWith(args []Value) (Value, error) {
	ls, rs, ok := primitivePair(args)
	return Boolean(ok && strings.HasPrefix(toUpperSpecial(ls), toUpperSpecial(rs))), nil
}

func funcEndsWith(args []Value) (Value, error) {
	ls, rs, ok := primitivePair(args)
	return Boolean(ok && strings.HasSuffix(toUpperSpecial(ls), toUpperSpecial(rs))), nil
}

// primitivePair returns the string forms of the two arguments, and false
// when either is an array or a dictionary.
func primitivePair(args []Value) (string, string, bool) {
	if !isPrimitive(args[0]) || !isPrimitive(args[1]) {
		return "", "", false
	}
	return CoerceString(args[0]), CoerceString(args[1]), true
}

// funcJoin is join(array, separator). A primitive first argument is just
// converted to a string, ignoring the separator; a dictionary gives "". A
// separator that is not primitive means ",".
func funcJoin(args []Value) (Value, error) {
	first := normalize(args[0])
	if isPrimitive(first) {
		return String(CoerceString(first)), nil
	}
	arr, ok := first.(*Array)
	if !ok {
		return String(""), nil
	}
	sep := ","
	if len(args) > 1 && isPrimitive(args[1]) {
		sep = CoerceString(args[1])
	}
	parts := make([]string, len(arr.items))
	for i, it := range arr.items {
		parts[i] = CoerceString(it)
	}
	return String(strings.Join(parts, sep)), nil
}

func funcToJSON(args []Value) (Value, error) {
	return String(ToJSON(args[0])), nil
}

// funcFromJSON is fromJSON(value). Blank input is "empty input"; anything
// that is not valid JSON is "Error parsing JSON when evaluating fromJson"
// wrapping the syntax error.
func funcFromJSON(args []Value) (Value, error) {
	in := CoerceString(args[0])
	if jsTrim(in) == "" {
		return nil, evalErrorf("empty input")
	}
	v, err := FromJSON([]byte(in))
	if err != nil {
		return nil, &EvalError{Message: "Error parsing JSON when evaluating fromJson", Err: err}
	}
	return v, nil
}

// funcFormat is format(fmt, args...): "{N}" inserts argument N, "{{" and "}}"
// are literal braces. Anything between the digits of "{N" and the closing
// brace is ignored ("{0:x}" is "{0}"). Every other use of a brace is an
// error, as is a reference to a missing argument.
func funcFormat(args []Value) (Value, error) {
	fs := CoerceString(args[0])
	var out strings.Builder
	index := 0
	for index < len(fs) {
		lbrace := indexByteFrom(fs, '{', index)
		rbrace := indexByteFrom(fs, '}', index)
		switch {
		case lbrace >= 0 && (rbrace < 0 || rbrace > lbrace):
			// Escaped left brace.
			if charAt(fs, lbrace+1) == '{' {
				out.WriteString(fs[index : lbrace+1])
				index = lbrace + 2
				continue
			}
			// Left brace, number, optional format specifiers, right brace.
			if rbrace > lbrace+1 {
				if argIndex, ok := readArgIndex(fs, lbrace+1); ok {
					if 1+argIndex > float64(len(args)-1) {
						return nil, evalErrorf("The following format string references more arguments than were supplied: %s", fs)
					}
					out.WriteString(fs[index:lbrace])
					out.WriteString(CoerceString(args[1+int(argIndex)]))
					index = rbrace + 1
					continue
				}
			}
			return nil, evalErrorf("The following format string is invalid: %s", fs)
		case rbrace >= 0:
			// Escaped right brace.
			if charAt(fs, rbrace+1) != '}' {
				return nil, evalErrorf("The following format string is invalid: %s", fs)
			}
			out.WriteString(fs[index : rbrace+1])
			index = rbrace + 2
		default:
			out.WriteString(fs[index:])
			index = len(fs)
		}
	}
	return String(out.String()), nil
}

func indexByteFrom(s string, c byte, from int) int {
	if from >= len(s) {
		return -1
	}
	i := strings.IndexByte(s[from:], c)
	if i < 0 {
		return -1
	}
	return from + i
}

// charAt returns the byte at i, or 0 past the end (safeCharAt).
func charAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

// readArgIndex reads the decimal digits at s[start:]. The index is a float
// because parseInt in the reference yields Infinity for absurdly long digit
// runs, which then fails the argument-count check.
func readArgIndex(s string, start int) (float64, bool) {
	n := 0
	for charAt(s, start+n) >= '0' && charAt(s, start+n) <= '9' {
		n++
	}
	if n == 0 {
		return 0, false
	}
	f, _ := strconv.ParseFloat(s[start:start+n], 64)
	if math.IsNaN(f) {
		return 0, false
	}
	return f, true
}
