// SPDX-License-Identifier: MIT

package expr

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

// builtinFunc implements a built-in function. It runs inside the evaluator so
// that it can charge the output budget and watch the context, and it receives
// the call, not the evaluated arguments: like the runner's functions
// (Sdk/Functions/*.cs) it evaluates only the arguments it needs, in its own
// order, so an argument that is never reached cannot fail.
type builtinFunc func(e *evaluator, c *FunctionCall) (Value, error)

// builtinImpls are the implementations of the built-in functions, keyed by
// upper-cased name (see foldKey). Arity is validated by the parser, so each
// may assume its documented argument count.
var builtinImpls map[string]builtinFunc

// The map is filled in init: the implementations call back into the evaluator,
// which would make a package-level initializer cyclic.
func init() {
	builtinImpls = map[string]builtinFunc{
		"CASE":       funcCase,
		"CONTAINS":   funcContains,
		"ENDSWITH":   funcEndsWith,
		"FORMAT":     funcFormat,
		"FROMJSON":   funcFromJSON,
		"JOIN":       funcJoin,
		"STARTSWITH": funcStartsWith,
		"TOJSON":     funcToJSON,
	}
}

// funcCase is case(pred1, val1, pred2, val2, ..., default): the predicates are
// evaluated in order up to the first true one, then only its value (Case.cs).
// A predicate that is not a boolean is an error. The JavaScript reference
// ships case behind an experimental feature flag that only affects editor
// completion; the parser always accepts it, and so does the runner.
func funcCase(e *evaluator, c *FunctionCall) (Value, error) {
	n := len(c.Args)
	if n%2 == 0 {
		return nil, evalErrorf("case requires an odd number of arguments")
	}
	for i := 0; i < n-1; i += 2 {
		p, err := e.eval(c.Args[i])
		if err != nil {
			return nil, err
		}
		pred, ok := normalize(p).(Boolean)
		if !ok {
			return nil, evalErrorf("case predicate must evaluate to a boolean value")
		}
		if pred {
			return e.eval(c.Args[i+1])
		}
	}
	return e.eval(c.Args[n-1])
}

// funcContains is contains(search, item) (Contains.cs): when search is a
// primitive and so is item, a case-insensitive (OrdinalIgnoreCase) substring
// test on their string forms; when search is a non-empty array, == against each
// element; false otherwise. item is only evaluated when it can matter.
func funcContains(e *evaluator, c *FunctionCall) (Value, error) {
	l, err := e.eval(c.Args[0])
	if err != nil {
		return nil, err
	}
	left := normalize(l)
	switch {
	case isPrimitive(left):
		r, err := e.eval(c.Args[1])
		if err != nil {
			return nil, err
		}
		if right := normalize(r); isPrimitive(right) {
			return Boolean(jsIncludes(ordinalFold(CoerceString(left)), ordinalFold(CoerceString(right)))), nil
		}
	case left.Kind() == KindArray && len(left.(*Array).items) > 0:
		right, err := e.eval(c.Args[1])
		if err != nil {
			return nil, err
		}
		for _, v := range left.(*Array).items {
			if err := e.tick(); err != nil {
				return nil, err
			}
			if equals(right, v) {
				return Boolean(true), nil
			}
		}
	}
	return Boolean(false), nil
}

func funcStartsWith(e *evaluator, c *FunctionCall) (Value, error) {
	ls, rs, ok, err := e.primitivePair(c)
	return Boolean(ok && jsHasPrefix(ordinalFold(ls), ordinalFold(rs))), err
}

func funcEndsWith(e *evaluator, c *FunctionCall) (Value, error) {
	ls, rs, ok, err := e.primitivePair(c)
	return Boolean(ok && jsHasSuffix(ordinalFold(ls), ordinalFold(rs))), err
}

// primitivePair evaluates the two arguments, the second only if the first is
// primitive, and returns their string forms; ok is false when either is an
// array or a dictionary.
func (e *evaluator) primitivePair(c *FunctionCall) (ls, rs string, ok bool, err error) {
	l, err := e.eval(c.Args[0])
	if err != nil || !isPrimitive(l) {
		return "", "", false, err
	}
	r, err := e.eval(c.Args[1])
	if err != nil || !isPrimitive(r) {
		return "", "", false, err
	}
	return CoerceString(l), CoerceString(r), true, nil
}

// funcJoin is join(array, separator) (Join.cs). A primitive first argument is
// just converted to a string, ignoring the separator; a dictionary or an empty
// array gives "". The separator is evaluated only when there are at least two
// items to separate, and a separator that is not primitive means ",".
func funcJoin(e *evaluator, c *FunctionCall) (Value, error) {
	f, err := e.eval(c.Args[0])
	if err != nil {
		return nil, err
	}
	first := normalize(f)
	if isPrimitive(first) {
		return String(CoerceString(first)), nil
	}
	arr, ok := first.(*Array)
	if !ok || len(arr.items) == 0 {
		return String(""), nil
	}
	sep := ","
	if len(arr.items) > 1 && len(c.Args) > 1 {
		sv, err := e.eval(c.Args[1])
		if err != nil {
			return nil, err
		}
		if isPrimitive(sv) {
			sep = CoerceString(sv)
		}
	}
	var out strings.Builder
	for i, it := range arr.items {
		if err := e.tick(); err != nil {
			return nil, err
		}
		part := CoerceString(it)
		n := len(part)
		if i > 0 {
			n += len(sep)
		}
		if err := e.charge(n); err != nil {
			return nil, err
		}
		if i > 0 {
			out.WriteString(sep)
		}
		out.WriteString(part)
	}
	return String(out.String()), nil
}

// funcToJSON is toJSON(value). The output counts against the budget and is
// cut off as soon as it exceeds what is left.
func funcToJSON(e *evaluator, c *FunctionCall) (Value, error) {
	v, err := e.eval(c.Args[0])
	if err != nil {
		return nil, err
	}
	max := -1
	if e.limit >= 0 {
		max = e.limit - e.used
	}
	out, err := EncodeJSON(v, max)
	switch {
	case errors.Is(err, errJSONTooBig):
		return nil, e.budgetError()
	case err != nil:
		return nil, &EvalError{Message: "toJSON: " + err.Error(), Err: err}
	}
	if err := e.charge(len(out)); err != nil {
		return nil, err
	}
	return String(out), nil
}

// funcFromJSON is fromJSON(value). Blank input is "empty input"; anything
// that is not readable by Json.NET is "Error parsing JSON when evaluating
// fromJson" wrapping the reader's error.
func funcFromJSON(e *evaluator, c *FunctionCall) (Value, error) {
	a, err := e.eval(c.Args[0])
	if err != nil {
		return nil, err
	}
	in := CoerceString(a)
	if strings.TrimFunc(in, isNetSpace) == "" {
		return nil, evalErrorf("empty input")
	}
	v, err := FromJSON([]byte(in))
	if err != nil {
		return nil, &EvalError{Message: "Error parsing JSON when evaluating fromJson", Err: err}
	}
	return v, nil
}

// formatPiece is a segment of the result of format(): literal text, or the
// index of an argument (evaluated when the result is assembled) with the
// format specifiers written after the colon.
type formatPiece struct {
	text  string
	arg   int
	specs string
	isArg bool
}

// funcFormat is format(fmt, args...) (Format.cs): "{N}" inserts argument N,
// "{{" and "}}" are literal braces. The format string is scanned completely
// first (so a syntax error or a reference to a missing argument is reported
// before any argument is evaluated), then the arguments are evaluated, once
// each, in the order they appear. N is at most 255 (a byte). Format specifiers
// ("{0:x}") are not supported for any value: they are an error.
func funcFormat(e *evaluator, c *FunctionCall) (Value, error) {
	f, err := e.eval(c.Args[0])
	if err != nil {
		return nil, err
	}
	fs := CoerceString(f)
	var pieces []formatPiece
	text := func(t string) error {
		if t == "" {
			return nil
		}
		if err := e.charge(utf16Len(t)); err != nil {
			return err
		}
		pieces = append(pieces, formatPiece{text: t})
		return nil
	}
	index := 0
	for index < len(fs) {
		lbrace := indexByteFrom(fs, '{', index)
		rbrace := indexByteFrom(fs, '}', index)
		switch {
		case lbrace >= 0 && (rbrace < 0 || rbrace > lbrace):
			// Escaped left brace.
			if charAt(fs, lbrace+1) == '{' {
				if err := text(fs[index : lbrace+1]); err != nil {
					return nil, err
				}
				index = lbrace + 2
				continue
			}
			// Left brace, number, optional format specifiers, right brace.
			if rbrace > lbrace+1 {
				if argIndex, end, ok := readArgIndex(fs, lbrace+1); ok {
					if specs, closing, ok := readFormatSpecifiers(fs, end+1); ok {
						if argIndex > len(c.Args)-2 {
							return nil, evalErrorf("The following format string references more arguments than were supplied: %s", fs)
						}
						if err := text(fs[index:lbrace]); err != nil {
							return nil, err
						}
						pieces = append(pieces, formatPiece{arg: argIndex, specs: specs, isArg: true})
						index = closing + 1
						continue
					}
				}
			}
			return nil, evalErrorf("The following format string is invalid: %s", fs)
		case rbrace >= 0:
			// Escaped right brace.
			if charAt(fs, rbrace+1) != '}' {
				return nil, evalErrorf("The following format string is invalid: %s", fs)
			}
			if err := text(fs[index : rbrace+1]); err != nil {
				return nil, err
			}
			index = rbrace + 2
		default:
			if err := text(fs[index:]); err != nil {
				return nil, err
			}
			index = len(fs)
		}
	}
	// Assemble: arguments are evaluated lazily, once each, in the order of
	// their first appearance.
	type argValue struct {
		str  string
		kind Kind
		done bool
	}
	cache := make([]argValue, len(c.Args)-1)
	var out strings.Builder
	for _, p := range pieces {
		if !p.isArg {
			out.WriteString(p.text)
			continue
		}
		av := &cache[p.arg]
		if !av.done {
			v, err := e.eval(c.Args[1+p.arg])
			if err != nil {
				return nil, err
			}
			*av = argValue{str: CoerceString(v), kind: normalize(v).Kind(), done: true}
		}
		if p.specs != "" {
			return nil, evalErrorf("The format specifiers '%s' are not valid for objects of type '%s'", p.specs, av.kind)
		}
		if av.str != "" {
			if err := e.charge(utf16Len(av.str)); err != nil {
				return nil, err
			}
		}
		out.WriteString(av.str)
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

// readArgIndex reads the digits at s[start:] as the index of a format
// argument: a byte, so at most 255 (Byte.TryParse). It returns the index of the
// last digit. Digits are Unicode decimal digits for the scan but only ASCII
// ones parse, so any other digit makes the format string invalid.
func readArgIndex(s string, start int) (value, end int, ok bool) {
	n := 0
	for i := start; i < len(s); {
		r, w := decodeRune(s[i:])
		if !unicode.IsDigit(r) {
			break
		}
		i += w
		n = i - start
	}
	if n == 0 {
		return 0, 0, false
	}
	v, err := strconv.ParseUint(s[start:start+n], 10, 8)
	if err != nil || !isASCII(s[start:start+n]) {
		return 0, 0, false
	}
	return int(v), start + n - 1, true
}

// readFormatSpecifiers reads what follows the digits of "{N": either the
// closing brace, or ":" and specifiers up to a closing brace, where "}}" is a
// literal brace inside the specifiers. It returns the specifiers and the index
// of the closing brace.
func readFormatSpecifiers(s string, start int) (specs string, closing int, ok bool) {
	switch charAt(s, start) {
	case '}':
		return "", start, true
	case ':':
	default:
		return "", 0, false
	}
	var b strings.Builder
	for i := start + 1; i < len(s); {
		if s[i] != '}' {
			b.WriteByte(s[i])
			i++
		} else if charAt(s, i+1) == '}' {
			b.WriteByte('}')
			i += 2
		} else {
			return b.String(), i, true
		}
	}
	return "", 0, false
}

// utf16Len is the length of s in UTF-16 code units, the length of a .NET string.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}
