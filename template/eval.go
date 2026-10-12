// SPDX-License-Identifier: MIT

package template

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"bnema.dev/ermite-actions/expr"
)

// Env is what expressions are evaluated against: the values of the named
// contexts ("github", "needs", "matrix"...) and the implementations of the
// extra functions ("success", "hashFiles"...). Which of them an expression may
// use was decided when it was read, from the schema.
type Env struct {
	Contexts map[string]expr.Value
	Funcs    map[string]expr.Func
	// MaxResultBytes is the output budget of one expression, see
	// [expr.EvalContext]. Zero means the default.
	MaxResultBytes int
}

func (e Env) evalContext() expr.EvalContext {
	return expr.EvalContext{Contexts: e.Contexts, Funcs: e.Funcs, MaxResultBytes: e.MaxResultBytes}
}

// MaxEvalDepth bounds the nesting of tokens that [Evaluate] follows.
const MaxEvalDepth = 100

func (t *BasicExpression) errorf(format string, a ...any) error {
	return &Error{File: t.file, Pos: t.pos, Raw: fmt.Sprintf(format, a...)}
}

// EvaluateExpression computes one expression. Errors are [*Error] values
// positioned at the expression and wrap the underlying error of package expr.
func EvaluateExpression(ctx context.Context, t *BasicExpression, env Env) (expr.Value, error) {
	if t.parseErr != nil {
		return nil, &Error{File: t.file, Pos: t.pos, Raw: t.parseErr.Error(), cause: t.parseErr}
	}
	if t.node == nil {
		return nil, t.errorf("An expression was expected")
	}
	v, err := expr.Evaluate(ctx, t.node, env.evalContext())
	if err != nil {
		return nil, &Error{File: t.file, Pos: t.pos, Raw: err.Error(), cause: err}
	}
	return v, nil
}

// Evaluate converts a token to a value of package expr, computing the
// expressions in it: literals become the corresponding scalars, sequences
// arrays and mappings dictionaries. A mapping key that is ${{ insert }} must
// have a mapping as value, whose entries are merged in place; a key that is
// another expression is evaluated and must produce a scalar.
func Evaluate(ctx context.Context, t Token, env Env) (expr.Value, error) {
	return evaluate(ctx, t, env, 0)
}

func evaluate(ctx context.Context, t Token, env Env, depth int) (expr.Value, error) {
	if depth > MaxEvalDepth {
		return nil, &Error{File: t.File(), Pos: t.Pos(), Raw: "Maximum object depth exceeded"}
	}
	switch t := t.(type) {
	case *Null:
		return expr.Null{}, nil
	case *Boolean:
		return expr.Boolean(t.Value), nil
	case *Number:
		return expr.Number(t.Value), nil
	case *String:
		return expr.String(t.Value), nil
	case *BasicExpression:
		return EvaluateExpression(ctx, t, env)
	case *InsertExpression:
		return nil, &Error{File: t.file, Pos: t.pos, Raw: "The directive 'insert' is only allowed as a mapping key"}
	case *Sequence:
		out := expr.NewArray()
		for _, it := range t.Items {
			v, err := evaluate(ctx, it, env, depth+1)
			if err != nil {
				return nil, err
			}
			out.Append(v)
		}
		return out, nil
	case *Mapping:
		out := expr.NewDictionary()
		if err := evaluateMapping(ctx, t, env, depth, out); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, errors.New("unknown token")
}

func evaluateMapping(ctx context.Context, m *Mapping, env Env, depth int, out *expr.Dictionary) error {
	for _, p := range m.Pairs {
		if _, ok := p.Key.(*InsertExpression); ok {
			v, err := evaluate(ctx, p.Value, env, depth+1)
			if err != nil {
				return err
			}
			d, ok := v.(*expr.Dictionary)
			if !ok {
				return &Error{File: p.Key.File(), Pos: p.Key.Pos(), Raw: fmt.Sprintf("Expected a mapping for ${{ insert }} but got %s", v.Kind())}
			}
			for _, kv := range d.Pairs() {
				out.Add(kv.Key, kv.Value)
			}
			continue
		}
		kv, err := evaluate(ctx, p.Key, env, depth+1)
		if err != nil {
			return err
		}
		if k := kv.Kind(); k == expr.KindArray || k == expr.KindDictionary {
			return &Error{File: p.Key.File(), Pos: p.Key.Pos(), Raw: fmt.Sprintf("A mapping key must be a scalar but the expression produced %s", k)}
		}
		v, err := evaluate(ctx, p.Value, env, depth+1)
		if err != nil {
			return err
		}
		out.Add(expr.CoerceString(kv), v)
	}
	return nil
}

// EvaluateString computes a token that must produce a string. Numbers,
// booleans and null are converted as GitHub does (null is the empty string);
// a sequence or mapping is an error.
func EvaluateString(ctx context.Context, t Token, env Env) (string, error) {
	if s, ok := t.(*String); ok {
		return s.Value, nil
	}
	v, err := Evaluate(ctx, t, env)
	if err != nil {
		return "", err
	}
	if k := v.Kind(); k == expr.KindArray || k == expr.KindDictionary {
		return "", &Error{File: t.File(), Pos: t.Pos(), Raw: fmt.Sprintf("Unexpected type '%s' encountered while reading 'string'. The type 'StringToken' was expected.", tokenKindOf(v))}
	}
	return expr.CoerceString(v), nil
}

// EvaluateBool computes a token that must produce a boolean. A string
// "true" or "false" (any case) is accepted, as is the result of an
// expression of that kind; anything else is an error.
func EvaluateBool(ctx context.Context, t Token, env Env) (bool, error) {
	if b, ok := t.(*Boolean); ok {
		return b.Value, nil
	}
	v, err := Evaluate(ctx, t, env)
	if err != nil {
		return false, err
	}
	switch x := v.(type) {
	case expr.Boolean:
		return bool(x), nil
	case expr.String:
		switch lowerASCII(string(x)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, &Error{File: t.File(), Pos: t.Pos(), Raw: fmt.Sprintf("Unexpected type '%s' encountered while reading 'boolean'. The type 'BooleanToken' was expected.", tokenKindOf(v))}
}

// EvaluateNumber computes a token that must produce a number. Numeric strings
// are accepted.
func EvaluateNumber(ctx context.Context, t Token, env Env) (float64, error) {
	if n, ok := t.(*Number); ok {
		return n.Value, nil
	}
	v, err := Evaluate(ctx, t, env)
	if err != nil {
		return 0, err
	}
	switch x := v.(type) {
	case expr.Number:
		return float64(x), nil
	case expr.String:
		if f, ok := parseDecimal(string(x)); ok {
			return f, nil
		}
	}
	return 0, &Error{File: t.File(), Pos: t.Pos(), Raw: fmt.Sprintf("Unexpected type '%s' encountered while reading 'number'. The type 'NumberToken' was expected.", tokenKindOf(v))}
}

func tokenKindOf(v expr.Value) string {
	switch v.Kind() {
	case expr.KindNull:
		return KindNull.String()
	case expr.KindBoolean:
		return KindBoolean.String()
	case expr.KindNumber:
		return KindNumber.String()
	case expr.KindString:
		return KindString.String()
	case expr.KindArray:
		return KindSequence.String()
	}
	return KindMapping.String()
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func parseDecimal(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}
