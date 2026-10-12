// SPDX-License-Identifier: MIT

package expr

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
)

// MaxResultBytes is the default budget of an evaluation: the total size, in
// bytes, of the strings that format, join and toJSON build plus a nominal
// cost for every element of the arrays that the * filter builds. The
// reference implementation has no such limit; without one a few kilobytes of
// expression can allocate gigabytes (format('{0}{0}{0}...', format(...))),
// and arrays shared in a context can multiply under .* filters.
const MaxResultBytes = 1 << 20

// filterElementCost is what one element of a filtered array costs against the
// budget: the size of an interface value.
const filterElementCost = 16

// maxEvalDepth bounds the recursion of the evaluator. The parser never
// produces trees anywhere near it; it protects against hand-built ASTs.
const maxEvalDepth = 1000

// Func implements a function callable from expressions. It receives the
// evaluation's context, which it should honour if it blocks or loops, and the
// evaluated arguments in order, and returns the result or an error.
// [Evaluate] wraps the error in an [*EvalError] that unwraps to it. Arity is
// not checked at call time: declare it in a [FuncInfo] given to [Parse].
type Func func(ctx context.Context, args []Value) (Value, error)

// EvalContext is the environment an expression is evaluated in.
type EvalContext struct {
	// Contexts maps context names ("github", "env", "matrix"...) to their
	// values. Names are matched case-insensitively; when several keys differ
	// only by case, the first in sorted order wins. A name that is absent
	// evaluates to null.
	Contexts map[string]Value
	// Funcs implements the extra functions declared to [Parse], such as
	// hashFiles, success, always, cancelled and failure. Names are matched
	// case-insensitively. As in the reference implementation these take
	// precedence over the built-in functions.
	Funcs map[string]Func
	// MaxResultBytes is the output budget of the evaluation, see
	// [MaxResultBytes]. Zero means the default; a negative value removes the
	// limit. The budget is shared by the whole evaluation, not per call.
	MaxResultBytes int
}

// Evaluate computes the value of a parsed expression.
//
// Evaluation stops with an error that wraps ctx.Err() when ctx is done, and
// with an [*EvalError] when the output budget is exceeded. Values of the
// context are never modified, and results may share them with the context:
// treat the result as read-only. The error is an [*EvalError].
func Evaluate(ctx context.Context, node Node, ec EvalContext) (Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if node == nil {
		return nil, &EvalError{Message: "cannot evaluate an empty expression"}
	}
	if err := ctx.Err(); err != nil {
		return nil, cancelError(err)
	}
	ev := &evaluator{
		ctx:      ctx,
		contexts: make(map[string]Value, len(ec.Contexts)),
		funcs:    make(map[string]Func, len(ec.Funcs)),
		limit:    ec.MaxResultBytes,
	}
	if ev.limit == 0 {
		ev.limit = MaxResultBytes
	}
	names := make([]string, 0, len(ec.Contexts))
	for k := range ec.Contexts {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, dup := ev.contexts[foldKey(k)]; !dup {
			ev.contexts[foldKey(k)] = ec.Contexts[k]
		}
	}
	names = names[:0]
	for k := range ec.Funcs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, dup := ev.funcs[foldKey(k)]; !dup {
			ev.funcs[foldKey(k)] = ec.Funcs[k]
		}
	}
	v, err := ev.eval(node)
	if err != nil {
		return nil, err
	}
	return normalize(v), nil
}

func cancelError(err error) *EvalError {
	return &EvalError{Message: "evaluation canceled: " + err.Error(), Err: err}
}

type evaluator struct {
	ctx      context.Context
	contexts map[string]Value
	funcs    map[string]Func
	limit    int // output budget; negative: unlimited
	used     int
	depth    int
	ticks    int
}

// tick is called in loops and on every node: it checks the context once in a
// while.
func (e *evaluator) tick() error {
	e.ticks++
	if e.ticks&63 == 0 {
		if err := e.ctx.Err(); err != nil {
			return cancelError(err)
		}
	}
	return nil
}

// charge spends n bytes of the output budget.
func (e *evaluator) charge(n int) error {
	if e.limit < 0 {
		return nil
	}
	if n > e.limit-e.used { // written to be overflow-proof
		return e.budgetError()
	}
	e.used += n
	return nil
}

func (e *evaluator) budgetError() *EvalError {
	return &EvalError{Message: fmt.Sprintf("expression output exceeds the limit of %d bytes", e.limit)}
}

func (e *evaluator) eval(n Node) (Value, error) {
	if err := e.tick(); err != nil {
		return nil, err
	}
	e.depth++
	defer func() { e.depth-- }()
	if e.depth > maxEvalDepth {
		return nil, &EvalError{Message: fmt.Sprintf("expression tree deeper than %d levels", maxEvalDepth)}
	}
	switch n := n.(type) {
	case *Literal:
		return normalize(n.Value), nil
	case *Unary:
		r, err := e.eval(n.Expr)
		if err != nil {
			return nil, err
		}
		return Boolean(!Truthy(r)), nil
	case *Binary:
		return e.binary(n)
	case *Logical:
		return e.logical(n)
	case *Grouping:
		return e.eval(n.Group)
	case *ContextAccess:
		if v, ok := e.contexts[foldKey(n.Name.Lexeme)]; ok {
			return normalize(v), nil
		}
		return Null{}, nil
	case *IndexAccess:
		return e.indexAccess(n)
	case *FunctionCall:
		return e.call(n)
	}
	// *Star is only valid as an index and never evaluated on its own; the
	// reference throws "Method not implemented." for it.
	return nil, &EvalError{Message: "Method not implemented."}
}

func (e *evaluator) binary(b *Binary) (Value, error) {
	// Both operands are always evaluated: comparisons do not short-circuit.
	left, err := e.eval(b.Left)
	if err != nil {
		return nil, err
	}
	right, err := e.eval(b.Right)
	if err != nil {
		return nil, err
	}
	switch b.Operator.Type {
	case TokenEqualEqual:
		return Boolean(equals(left, right)), nil
	case TokenBangEqual:
		return Boolean(!equals(left, right)), nil
	case TokenGreater:
		return Boolean(greaterThan(left, right)), nil
	case TokenGreaterEqual:
		return Boolean(equals(left, right) || greaterThan(left, right)), nil
	case TokenLess:
		return Boolean(lessThan(left, right)), nil
	case TokenLessEqual:
		return Boolean(equals(left, right) || lessThan(left, right)), nil
	}
	return nil, evalErrorf("unknown binary operator: %s", b.Operator.Lexeme)
}

// logical returns the operand that decided the chain, not a boolean:
// "a || b" is a when a is truthy, else b; "a && b" is a when a is falsy, else b.
func (e *evaluator) logical(l *Logical) (Value, error) {
	var result Value
	for _, arg := range l.Args {
		r, err := e.eval(arg)
		if err != nil {
			return nil, err
		}
		result = r
		if (l.Operator.Type == TokenAnd && !Truthy(r)) || (l.Operator.Type == TokenOr && Truthy(r)) {
			break
		}
	}
	if result == nil {
		return Null{}, nil // a Logical without operands, possible only in a hand-built tree
	}
	return result, nil
}

func (e *evaluator) call(c *FunctionCall) (Value, error) {
	name := foldKey(c.Name.Lexeme)
	ext, isExt := e.funcs[name]
	isExt = isExt && ext != nil
	var impl builtinFunc
	if !isExt {
		// Arity of the built-ins is checked by the parser, and again here
		// so that a hand-built tree cannot reach an implementation with the
		// wrong number of arguments.
		f, ok := builtinFuncs[name]
		if !ok {
			return nil, evalErrorf("function %s is not implemented", c.Name.Lexeme)
		}
		if t, bad := arityError(name, f, len(c.Args)); bad {
			return nil, &EvalError{Message: t.description() + ": '" + c.Name.Lexeme + "'"}
		}
		impl = builtinImpls[name]
	}
	args := make([]Value, len(c.Args))
	for i, a := range c.Args {
		v, err := e.eval(a)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	if isExt {
		v, err := ext(e.ctx, args)
		if err != nil {
			var ee *EvalError
			if errors.As(err, &ee) {
				return nil, err
			}
			return nil, &EvalError{Message: err.Error(), Err: err}
		}
		return v, nil
	}
	return impl(e, args)
}

// indexer is idxHelper of the reference: how an index value is read, as a
// star, as a string key and as an array position.
type indexer struct {
	star   bool
	hasStr bool
	str    string
	hasInt bool
	// intv is only meaningful when hasInt; it is a float because positions
	// beyond the int range must simply compare as out of bounds.
	intv float64
}

func newIndexer(star bool, v Value) indexer {
	ix := indexer{star: star}
	if star || v == nil {
		return ix
	}
	v = normalize(v)
	if isPrimitive(v) {
		ix.hasStr = true
		ix.str = CoerceString(v)
	}
	f := toNumber(v)
	if !math.IsNaN(f) && !math.IsInf(f, 0) && f >= 0 {
		ix.hasInt = true
		ix.intv = math.Floor(f)
	}
	return ix
}

// pos returns the position as an int when it is within [0, n).
func (ix indexer) pos(n int) (int, bool) {
	if !ix.hasInt || ix.intv >= float64(n) {
		return 0, false
	}
	return int(ix.intv), true
}

func (e *evaluator) indexAccess(ia *IndexAccess) (Value, error) {
	var ix indexer
	if _, isStar := ia.Index.(*Star); isStar {
		ix = newIndexer(true, nil)
	} else {
		v, err := e.eval(ia.Index)
		if err != nil {
			return nil, &EvalError{Message: "could not evaluate index for index access: " + err.Error(), Err: err}
		}
		ix = newIndexer(false, v)
	}
	obj, err := e.eval(ia.Expr)
	if err != nil {
		return nil, err
	}
	switch o := normalize(obj).(type) {
	case *Array:
		if o.filtered {
			return e.filteredArrayAccess(o, ix)
		}
		return e.arrayAccess(o, ix)
	case *Dictionary:
		return e.objectAccess(o, ix)
	}
	if ix.star {
		return &Array{filtered: true}, nil
	}
	return Null{}, nil
}

// filteredArrayAccess applies an index to each element of a filtered array
// and collects the hits: "a.*.b" is the b of every element that has one.
func (e *evaluator) filteredArrayAccess(fa *Array, ix indexer) (Value, error) {
	result := &Array{filtered: true}
	for _, item := range fa.items {
		if err := e.tick(); err != nil {
			return nil, err
		}
		switch it := normalize(item).(type) {
		case *Dictionary:
			if ix.star {
				if err := e.charge(len(it.vals) * filterElementCost); err != nil {
					return nil, err
				}
				result.items = append(result.items, it.vals...)
			} else if ix.hasStr {
				if v, ok := it.Get(ix.str); ok {
					if err := e.charge(filterElementCost); err != nil {
						return nil, err
					}
					result.items = append(result.items, v)
				}
			}
		case *Array:
			if ix.star {
				if err := e.charge(len(it.items) * filterElementCost); err != nil {
					return nil, err
				}
				result.items = append(result.items, it.items...)
			} else if i, ok := ix.pos(len(it.items)); ok {
				if err := e.charge(filterElementCost); err != nil {
					return nil, err
				}
				result.items = append(result.items, it.items[i])
			}
		}
	}
	return result, nil
}

func (e *evaluator) arrayAccess(a *Array, ix indexer) (Value, error) {
	if ix.star {
		if err := e.charge(len(a.items) * filterElementCost); err != nil {
			return nil, err
		}
		return &Array{items: append([]Value(nil), a.items...), filtered: true}, nil
	}
	if i, ok := ix.pos(len(a.items)); ok {
		return normalize(a.items[i]), nil
	}
	return Null{}, nil
}

func (e *evaluator) objectAccess(d *Dictionary, ix indexer) (Value, error) {
	if ix.star {
		if err := e.charge(len(d.vals) * filterElementCost); err != nil {
			return nil, err
		}
		return &Array{items: append([]Value(nil), d.vals...), filtered: true}, nil
	}
	if ix.hasStr {
		if v, ok := d.Get(ix.str); ok {
			return v, nil
		}
	}
	return Null{}, nil
}

// coerceTypes converts two values of different kinds to a common kind the
// way the reference does, which is like the JavaScript abstract equality
// algorithm except that arrays and dictionaries are never converted: null
// and booleans become numbers, and a string compared with a number becomes
// a number.
func coerceTypes(l, r Value) (Value, Value) {
	l, r = normalize(l), normalize(r)
	if l.Kind() == r.Kind() {
		return l, r
	}
	switch l.Kind() {
	case KindNumber:
		if r.Kind() == KindString {
			return l, Number(toNumber(r))
		}
	case KindString:
		if r.Kind() == KindNumber {
			return Number(toNumber(l)), r
		}
	case KindNull, KindBoolean:
		return coerceTypes(Number(toNumber(l)), r)
	}
	switch r.Kind() {
	case KindNull, KindBoolean:
		return coerceTypes(l, Number(toNumber(r)))
	}
	return l, r
}

// equals is the == operator: loose equality, case-insensitive for strings
// (see toUpperSpecial), false for NaN, and by identity for arrays and
// dictionaries.
func equals(lhs, rhs Value) bool {
	l, r := coerceTypes(lhs, rhs)
	if l.Kind() != r.Kind() {
		return false
	}
	switch lv := l.(type) {
	case Null:
		return true
	case Number:
		return float64(lv) == float64(r.(Number)) // false when either is NaN
	case String:
		return toUpperSpecial(string(lv)) == toUpperSpecial(string(r.(String)))
	case Boolean:
		return lv == r.(Boolean)
	case *Array:
		return lv == r.(*Array)
	case *Dictionary:
		return lv == r.(*Dictionary)
	}
	return false
}

// compare implements < and >: it returns -1, 0 or 1, and ok false when the
// values are not ordered (different kinds after coercion, NaN, null, arrays,
// dictionaries).
func compare(lhs, rhs Value) (c int, ok bool) {
	l, r := coerceTypes(lhs, rhs)
	if l.Kind() != r.Kind() {
		return 0, false
	}
	switch lv := l.(type) {
	case Number:
		lf, rf := float64(lv), float64(r.(Number))
		switch {
		case math.IsNaN(lf) || math.IsNaN(rf):
			return 0, false
		case lf < rf:
			return -1, true
		case lf > rf:
			return 1, true
		}
		return 0, true
	case String:
		return compareUTF16(toUpperSpecial(string(lv)), toUpperSpecial(string(r.(String)))), true
	case Boolean:
		// false < true
		switch rb := r.(Boolean); {
		case lv == rb:
			return 0, true
		case !bool(lv):
			return -1, true
		}
		return 1, true
	}
	return 0, false
}

func greaterThan(l, r Value) bool {
	c, ok := compare(l, r)
	return ok && c > 0
}

func lessThan(l, r Value) bool {
	c, ok := compare(l, r)
	return ok && c < 0
}
