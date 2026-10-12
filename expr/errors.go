// SPDX-License-Identifier: MIT

package expr

import (
	"fmt"
)

// Limits of the reference implementation.
const (
	// MaxParserDepth is the deepest nesting of groupings, function arguments,
	// index accesses, negations and logical operators the parser accepts.
	MaxParserDepth = 50
	// MaxExpressionLength is the longest expression accepted, counted in
	// UTF-16 code units like the JavaScript string length it mirrors.
	MaxExpressionLength = 21000
)

// ErrorType classifies an [Error].
type ErrorType int

// The error types of the reference implementation, in its order.
const (
	ErrorUnexpectedSymbol ErrorType = iota
	// ErrorUnrecognizedNamedValue is declared by the reference but never
	// produced; ErrorUnrecognizedContext carries the same text.
	ErrorUnrecognizedNamedValue
	ErrorUnexpectedEndOfExpression
	ErrorExceededMaxDepth
	ErrorExceededMaxLength
	ErrorTooFewParameters
	ErrorTooManyParameters
	ErrorEvenParameters
	ErrorUnrecognizedContext
	ErrorUnrecognizedFunction
)

func (t ErrorType) description() string {
	switch t {
	case ErrorUnexpectedEndOfExpression:
		return "Unexpected end of expression"
	case ErrorUnexpectedSymbol:
		return "Unexpected symbol"
	case ErrorUnrecognizedNamedValue:
		return "Unrecognized named-value"
	case ErrorExceededMaxDepth:
		return fmt.Sprintf("Exceeded max expression depth %d", MaxParserDepth)
	case ErrorExceededMaxLength:
		return fmt.Sprintf("Exceeded max expression length %d", MaxExpressionLength)
	case ErrorTooFewParameters:
		return "Too few parameters supplied"
	case ErrorTooManyParameters:
		return "Too many parameters supplied"
	case ErrorEvenParameters:
		return "Even number of parameters supplied, requires an odd number of parameters"
	case ErrorUnrecognizedContext:
		return "Unrecognized named-value"
	case ErrorUnrecognizedFunction:
		return "Unrecognized function"
	}
	return "Unknown error"
}

// Position is a zero-based line and column in an expression. Columns count
// UTF-16 code units, as in the reference implementation.
type Position struct {
	Line   int
	Column int
}

// Error is a lexing or parsing error. Its message is exactly the one the
// reference implementation produces.
type Error struct {
	Type ErrorType
	// Message is the full text, e.g. "Unexpected symbol: ')'".
	Message string
	// Pos is where the offending token starts.
	Pos Position
	// Token is the text of the offending token.
	Token string
}

func (e *Error) Error() string { return e.Message }

// EvalError is an error raised while evaluating an expression: a failing
// function, such as fromJSON on invalid input, or an invalid format string.
type EvalError struct {
	Message string
	// Err is the underlying error, if any.
	Err error
}

func (e *EvalError) Error() string { return e.Message }
func (e *EvalError) Unwrap() error { return e.Err }

func evalErrorf(format string, a ...any) *EvalError {
	return &EvalError{Message: fmt.Sprintf(format, a...)}
}
