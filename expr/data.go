// SPDX-License-Identifier: MIT

package expr

import "math"

// Kind is the type of a [Value].
type Kind int

// The kinds of value an expression can produce.
const (
	KindNull Kind = iota
	KindBoolean
	KindNumber
	KindString
	KindArray
	KindDictionary
)

// String returns the name GitHub uses for the kind when a non-primitive
// value is converted to a string: "Array" and "Object" for the two compound
// kinds.
func (k Kind) String() string {
	switch k {
	case KindNull:
		return "Null"
	case KindBoolean:
		return "Boolean"
	case KindNumber:
		return "Number"
	case KindString:
		return "String"
	case KindArray:
		return "Array"
	case KindDictionary:
		return "Object"
	}
	return "unknown"
}

// Value is the result of an expression or an element of a context: one of
// [Null], [Boolean], [Number], [String], [*Array] and [*Dictionary]. The set
// is closed. A nil Value, or a nil *Array or *Dictionary, behaves as [Null].
type Value interface {
	// Kind reports the type of the value.
	Kind() Kind
	isValue()
}

// Null is the null value.
type Null struct{}

// Boolean is a true or false value.
type Boolean bool

// Number is a double-precision floating-point number. NaN and the infinities
// are legal values.
type Number float64

// String is a text value.
type String string

func (Null) Kind() Kind        { return KindNull }
func (Boolean) Kind() Kind     { return KindBoolean }
func (Number) Kind() Kind      { return KindNumber }
func (String) Kind() Kind      { return KindString }
func (*Array) Kind() Kind      { return KindArray }
func (*Dictionary) Kind() Kind { return KindDictionary }

func (Null) isValue()        {}
func (Boolean) isValue()     {}
func (Number) isValue()      {}
func (String) isValue()      {}
func (*Array) isValue()      {}
func (*Dictionary) isValue() {}

// Array is an ordered list of values. Two arrays are equal in an expression
// only if they are the same *Array.
type Array struct {
	items []Value
	// filtered is set on arrays produced by the * filter ("a.*", "a[*]"),
	// which flatten one level when indexed again ("a.*.b").
	filtered bool
}

// NewArray returns an array holding items.
func NewArray(items ...Value) *Array {
	return &Array{items: append([]Value(nil), items...)}
}

// Len returns the number of elements.
func (a *Array) Len() int {
	if a == nil {
		return 0
	}
	return len(a.items)
}

// At returns the element at index i, or [Null] when i is out of range.
func (a *Array) At(i int) Value {
	if a == nil || i < 0 || i >= len(a.items) {
		return Null{}
	}
	return normalize(a.items[i])
}

// Items returns a copy of the elements.
func (a *Array) Items() []Value {
	if a == nil {
		return nil
	}
	out := make([]Value, len(a.items))
	for i, v := range a.items {
		out[i] = normalize(v)
	}
	return out
}

// Append adds v at the end of the array.
func (a *Array) Append(v Value) { a.items = append(a.items, v) }

// Filtered reports whether the array was produced by a * filter.
func (a *Array) Filtered() bool { return a != nil && a.filtered }

// Pair is a key and its value in a [Dictionary].
type Pair struct {
	Key   string
	Value Value
}

// Dictionary is an ordered map from string keys to values. Keys are looked up
// case-insensitively and keep the spelling and position of their first
// insertion. Two dictionaries are equal in an expression only if they are
// the same *Dictionary.
type Dictionary struct {
	keys  []string
	vals  []Value
	index map[string]int
}

// NewDictionary returns an empty dictionary.
func NewDictionary() *Dictionary {
	return &Dictionary{index: map[string]int{}}
}

// Add inserts key with value v and reports whether it did. When a key that
// differs only by case is already present it does nothing and returns false:
// the first insertion wins.
func (d *Dictionary) Add(key string, v Value) bool {
	if d.index == nil {
		d.index = map[string]int{}
	}
	folded := foldKey(key)
	if _, ok := d.index[folded]; ok {
		return false
	}
	d.index[folded] = len(d.vals)
	d.keys = append(d.keys, key)
	d.vals = append(d.vals, v)
	return true
}

// Get returns the value stored under key, compared case-insensitively.
func (d *Dictionary) Get(key string) (Value, bool) {
	if d == nil {
		return nil, false
	}
	i, ok := d.index[foldKey(key)]
	if !ok {
		return nil, false
	}
	return normalize(d.vals[i]), true
}

// Len returns the number of entries.
func (d *Dictionary) Len() int {
	if d == nil {
		return 0
	}
	return len(d.vals)
}

// Keys returns the keys in insertion order.
func (d *Dictionary) Keys() []string {
	if d == nil {
		return nil
	}
	return append([]string(nil), d.keys...)
}

// Values returns the values in insertion order.
func (d *Dictionary) Values() []Value {
	if d == nil {
		return nil
	}
	out := make([]Value, len(d.vals))
	for i, v := range d.vals {
		out[i] = normalize(v)
	}
	return out
}

// Pairs returns the entries in insertion order.
func (d *Dictionary) Pairs() []Pair {
	if d == nil {
		return nil
	}
	out := make([]Pair, len(d.keys))
	for i, k := range d.keys {
		out[i] = Pair{Key: k, Value: normalize(d.vals[i])}
	}
	return out
}

// foldKey is the case-insensitive form of a dictionary key. GitHub lower-cases
// keys with JavaScript's toLowerCase.
func foldKey(key string) string { return jsToLower(key) }

// normalize maps nil values to Null so that callers can switch on Kind.
func normalize(v Value) Value {
	switch x := v.(type) {
	case nil:
		return Null{}
	case *Array:
		if x == nil {
			return Null{}
		}
	case *Dictionary:
		if x == nil {
			return Null{}
		}
	}
	return v
}

// isPrimitive reports whether v is a Null, Boolean, Number or String.
func isPrimitive(v Value) bool {
	switch normalize(v).Kind() {
	case KindNull, KindBoolean, KindNumber, KindString:
		return true
	}
	return false
}

// toNumber converts v to a number the way GitHub does: null is 0, booleans
// are 1 and 0, strings are parsed like JavaScript's Number(), and arrays and
// dictionaries are NaN.
func toNumber(v Value) float64 {
	switch x := normalize(v).(type) {
	case Null:
		return 0
	case Boolean:
		if x {
			return 1
		}
		return 0
	case Number:
		return float64(x)
	case String:
		return jsToNumber(string(x))
	}
	return nan
}

// CoerceString converts v to the string GitHub shows for it: null is "", booleans
// are "true" and "false", numbers are formatted with at most 15 decimals
// ("0.1" for 0.1+0.2) and arrays and dictionaries are "Array" and "Object".
func CoerceString(v Value) string {
	switch x := normalize(v).(type) {
	case Null:
		return ""
	case Boolean:
		if x {
			return "true"
		}
		return "false"
	case Number:
		return numberToCoercedString(float64(x))
	case String:
		return string(x)
	default:
		return x.Kind().String()
	}
}

// Truthy reports whether v is true when used as a condition. Null, false, 0,
// NaN and the empty string are falsy; everything else, including every array
// and dictionary, is truthy.
func Truthy(v Value) bool {
	switch x := normalize(v).(type) {
	case Null:
		return false
	case Boolean:
		return bool(x)
	case Number:
		return x != 0 && !math.IsNaN(float64(x))
	case String:
		return len(x) != 0
	}
	return true
}
