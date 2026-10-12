// SPDX-License-Identifier: MIT

package expr

import "math"

// FuncInfo describes a function that expressions may call besides the
// built-in ones (case, contains, endsWith, format, fromJSON, join,
// startsWith, toJSON). The parser checks names and argument counts against
// it; the implementation is supplied at evaluation time in
// [EvalContext.Funcs].
type FuncInfo struct {
	// Name is matched case-insensitively.
	Name string
	// MinArgs and MaxArgs bound the number of arguments. A negative MaxArgs
	// means no upper bound.
	MinArgs int
	MaxArgs int
}

// builtinFuncs are the functions of ExpressionConstants.WellKnownFunctions, keyed
// by upper-cased name (see foldKey). They take precedence over caller-supplied FuncInfo for
// validation.
var builtinFuncs = map[string]FuncInfo{
	"CASE":       {"case", 3, math.MaxInt},
	"CONTAINS":   {"contains", 2, 2},
	"ENDSWITH":   {"endsWith", 2, 2},
	"FORMAT":     {"format", 1, 255},
	"FROMJSON":   {"fromJson", 1, 1},
	"JOIN":       {"join", 1, 2},
	"STARTSWITH": {"startsWith", 2, 2},
	"TOJSON":     {"toJson", 1, 1},
}

// Parse parses the text of one expression (the inside of ${{ }}).
//
// contexts lists the context names the expression may reference, such as
// "github" or "matrix"; funcs lists the extra functions it may call. Names
// are case-insensitive. A reference to any other context or function is an
// error, as is a call with a wrong number of arguments.
//
// The returned error is an [*Error]. An expression with no tokens, such as
// "" or "  ", is not an error: the reference returns nothing for it, and Parse
// returns a nil [Node], which [Evaluate] rejects.
func Parse(expr string, contexts []string, funcs []FuncInfo) (Node, error) {
	return parseRunner(expr, contexts, funcs)
}

// arityError reports whether argCount is wrong for f, and how. name is the
// upper-cased function name (see foldKey): case also needs an odd number of arguments.
func arityError(name string, f FuncInfo, argCount int) (ErrorType, bool) {
	switch {
	case argCount < f.MinArgs:
		return ErrorTooFewParameters, true
	case f.MaxArgs >= 0 && argCount > f.MaxArgs:
		return ErrorTooManyParameters, true
	case name == "CASE" && argCount%2 == 0:
		return ErrorEvenParameters, true
	}
	return 0, false
}
