// SPDX-License-Identifier: MIT

package expr

import (
	"math"
	"sort"
)

// Func implements a function callable from expressions. It receives the
// evaluated arguments in order and returns the result or an error, which
// [Evaluate] returns as is. Arity is not checked at call time: declare it in
// a [FuncInfo] given to [Parse].
type Func func(args []Value) (Value, error)

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
}

// Evaluate computes the value of a parsed expression.
//
// Values of the context are never modified, and results may share them with
// the context: treat the result as read-only. The error is an [*EvalError].
func Evaluate(node Node, ctx EvalContext) (Value, error) {
	if node == nil {
		return nil, &EvalError{Message: "cannot evaluate an empty expression"}
	}
	ev := &evaluator{
		contexts: make(map[string]Value, len(ctx.Contexts)),
		funcs:    make(map[string]Func, len(ctx.Funcs)),
	}
	names := make([]string, 0, len(ctx.Contexts))
	for k := range ctx.Contexts {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, dup := ev.contexts[foldKey(k)]; !dup {
			ev.contexts[foldKey(k)] = ctx.Contexts[k]
		}
	}
	names = names[:0]
	for k := range ctx.Funcs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, dup := ev.funcs[foldKey(k)]; !dup {
			ev.funcs[foldKey(k)] = ctx.Funcs[k]
		}
	}
	v, err := ev.eval(node)
	if err != nil {
		return nil, err
	}
	return normalize(v), nil
}

type evaluator struct {
	contexts map[string]Value
	funcs    map[string]Func
}

func (e *evaluator) eval(n Node) (Value, error) {
	switch n := n.(type) {
	case *Literal:
		return n.Value, nil
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
	return result, nil
}

func (e *evaluator) call(c *FunctionCall) (Value, error) {
	args := make([]Value, len(c.Args))
	for i, a := range c.Args {
		v, err := e.eval(a)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	name := foldKey(c.Name.Lexeme)
	if f, ok := e.funcs[name]; ok && f != nil {
		return f(args)
	}
	if f, ok := builtinImpls[name]; ok {
		return f(args)
	}
	return nil, evalErrorf("function %s is not implemented", c.Name.Lexeme)
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
			return filteredArrayAccess(o, ix), nil
		}
		return arrayAccess(o, ix), nil
	case *Dictionary:
		return objectAccess(o, ix), nil
	}
	if ix.star {
		return &Array{filtered: true}, nil
	}
	return Null{}, nil
}

// filteredArrayAccess applies an index to each element of a filtered array
// and collects the hits: "a.*.b" is the b of every element that has one.
func filteredArrayAccess(fa *Array, ix indexer) *Array {
	result := &Array{filtered: true}
	for _, item := range fa.items {
		switch it := normalize(item).(type) {
		case *Dictionary:
			if ix.star {
				result.items = append(result.items, it.vals...)
			} else if ix.hasStr {
				if v, ok := it.Get(ix.str); ok {
					result.items = append(result.items, v)
				}
			}
		case *Array:
			if ix.star {
				result.items = append(result.items, it.items...)
			} else if i, ok := ix.pos(len(it.items)); ok {
				result.items = append(result.items, it.items[i])
			}
		}
	}
	return result
}

func arrayAccess(a *Array, ix indexer) Value {
	if ix.star {
		return &Array{items: append([]Value(nil), a.items...), filtered: true}
	}
	if i, ok := ix.pos(len(a.items)); ok {
		return normalize(a.items[i])
	}
	return Null{}
}

func objectAccess(d *Dictionary, ix indexer) Value {
	if ix.star {
		return &Array{items: append([]Value(nil), d.vals...), filtered: true}
	}
	if ix.hasStr {
		if v, ok := d.Get(ix.str); ok {
			return v
		}
	}
	return Null{}
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
