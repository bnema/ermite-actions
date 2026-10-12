// SPDX-License-Identifier: MIT

package template

import (
	"strings"

	"bnema.dev/ermite-actions/expr"
)

// Kind is the type of a [Token], numbered as in the reference.
type Kind int

// The token kinds.
const (
	KindString Kind = iota
	KindSequence
	KindMapping
	KindBasicExpression
	KindInsertExpression
	KindBoolean
	KindNumber
	KindNull
)

// String returns the reference's name for the kind, such as "StringToken".
func (k Kind) String() string {
	switch k {
	case KindString:
		return "StringToken"
	case KindSequence:
		return "SequenceToken"
	case KindMapping:
		return "MappingToken"
	case KindBasicExpression:
		return "BasicExpressionToken"
	case KindInsertExpression:
		return "InsertExpressionToken"
	case KindBoolean:
		return "BooleanToken"
	case KindNumber:
		return "NumberToken"
	case KindNull:
		return "NullToken"
	}
	return "unknown"
}

// Pos is a position in a source file. Line and Col are 1-based; the zero
// value means the position is unknown (a token made by the program).
type Pos struct{ Line, Col int }

// Token is a node of a template: a scalar, a sequence or a mapping. The
// concrete types are [*String], [*Number], [*Boolean], [*Null],
// [*Sequence], [*Mapping], [*BasicExpression] and [*InsertExpression].
type Token interface {
	// Kind reports the type of the token.
	Kind() Kind
	// File is the name of the file the token was read from, or "".
	File() string
	// Pos is where the token starts in File.
	Pos() Pos
	// Definition is the schema definition the token was read against, or nil.
	Definition() *DefinitionInfo
	// String is the text the reference shows in messages: the value of a
	// literal, "${{ expr }}" for an expression.
	String() string
	base() *tokenBase
}

type tokenBase struct {
	file string
	pos  Pos
	def  *DefinitionInfo
}

func (m *tokenBase) File() string                { return m.file }
func (m *tokenBase) Pos() Pos                    { return m.pos }
func (m *tokenBase) Definition() *DefinitionInfo { return m.def }
func (m *tokenBase) base() *tokenBase            { return m }

// Origin is where a token comes from; the zero value is a token made by the
// program.
type Origin struct {
	File string
	Pos  Pos
}

func mk(o Origin) tokenBase { return tokenBase{file: o.File, pos: o.Pos} }

// String is a string literal.
type String struct {
	tokenBase
	Value string
}

// Number is a number literal.
type Number struct {
	tokenBase
	Value float64
}

// Boolean is a true or false literal.
type Boolean struct {
	tokenBase
	Value bool
}

// Null is a null literal.
type Null struct{ tokenBase }

// Sequence is a list of tokens.
type Sequence struct {
	tokenBase
	Items []Token
}

// Pair is a key and its value in a [Mapping]. The key is a scalar: a
// [*String] or an expression.
type Pair struct {
	Key, Value Token
}

// Mapping is an ordered list of key-value pairs. Duplicate keys are rejected
// when reading, comparing case-insensitively.
type Mapping struct {
	tokenBase
	Pairs []Pair
}

// BasicExpression is a ${{ }} expression. Expression is the text between the
// braces, trimmed, or a format() call when a string mixes text and
// expressions.
type BasicExpression struct {
	tokenBase
	Expression string
	// Original holds the individual expressions when Expression is the
	// format() call built from a string with several parts.
	Original []*BasicExpression
	// Source is the unprocessed text of the value the token was read from.
	Source string

	node     expr.Node
	parseErr error
}

// InsertExpression is the ${{ insert }} directive.
type InsertExpression struct{ tokenBase }

func (*String) Kind() Kind           { return KindString }
func (*Number) Kind() Kind           { return KindNumber }
func (*Boolean) Kind() Kind          { return KindBoolean }
func (*Null) Kind() Kind             { return KindNull }
func (*Sequence) Kind() Kind         { return KindSequence }
func (*Mapping) Kind() Kind          { return KindMapping }
func (*BasicExpression) Kind() Kind  { return KindBasicExpression }
func (*InsertExpression) Kind() Kind { return KindInsertExpression }

func (t *String) String() string  { return t.Value }
func (t *Number) String() string  { return JSNumberString(t.Value) }
func (*Null) String() string      { return "" }
func (*Sequence) String() string  { return "" }
func (*Mapping) String() string   { return "" }
func (t *Boolean) String() string { return boolString(t.Value) }

func (t *BasicExpression) String() string { return "${{ " + t.Expression + " }}" }
func (*InsertExpression) String() string  { return "${{ insert }}" }

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// NewString returns a string token.
func NewString(o Origin, v string) *String { return &String{tokenBase: mk(o), Value: v} }

// NewNumber returns a number token.
func NewNumber(o Origin, v float64) *Number { return &Number{tokenBase: mk(o), Value: v} }

// NewBoolean returns a boolean token.
func NewBoolean(o Origin, v bool) *Boolean { return &Boolean{tokenBase: mk(o), Value: v} }

// NewNull returns a null token.
func NewNull(o Origin) *Null { return &Null{tokenBase: mk(o)} }

// NewSequence returns a sequence token holding items.
func NewSequence(o Origin, items ...Token) *Sequence {
	return &Sequence{tokenBase: mk(o), Items: items}
}

// NewMapping returns an empty mapping token.
func NewMapping(o Origin) *Mapping { return &Mapping{tokenBase: mk(o)} }

// NewExpression returns an expression token. The text is parsed at once
// against the contexts and functions allowed by def; with a nil def no
// context and no function besides the built-in ones is allowed. A parse error
// is returned by [Evaluate] and [BasicExpression.Err].
func NewExpression(o Origin, expression string, def *DefinitionInfo) *BasicExpression {
	t := &BasicExpression{tokenBase: mk(o), Expression: expression}
	t.def = def
	t.node, t.parseErr = parseWith(expression, def)
	return t
}

// Err returns the error of parsing the expression, or nil.
func (t *BasicExpression) Err() error { return t.parseErr }

// Add appends a pair.
func (m *Mapping) Add(key, value Token) { m.Pairs = append(m.Pairs, Pair{key, value}) }

// Find returns the value of the first pair whose key's text is exactly key,
// or nil.
func (m *Mapping) Find(key string) Token {
	for _, p := range m.Pairs {
		if p.Key.String() == key {
			return p.Value
		}
	}
	return nil
}

// IsScalar reports whether t is neither a sequence nor a mapping.
func IsScalar(t Token) bool {
	switch t.Kind() {
	case KindSequence, KindMapping:
		return false
	}
	return true
}

// IsLiteral reports whether t is a string, number, boolean or null.
func IsLiteral(t Token) bool {
	switch t.Kind() {
	case KindString, KindNumber, KindBoolean, KindNull:
		return true
	}
	return false
}

// IsExpression reports whether t is a basic or insert expression.
func IsExpression(t Token) bool {
	k := t.Kind()
	return k == KindBasicExpression || k == KindInsertExpression
}

// Walk calls fn for t and for every token below it, depth first, mapping keys
// included. It stops descending into a token when fn returns false.
func Walk(t Token, fn func(Token) bool) {
	if t == nil || !fn(t) {
		return
	}
	switch t := t.(type) {
	case *Sequence:
		for _, it := range t.Items {
			Walk(it, fn)
		}
	case *Mapping:
		for _, p := range t.Pairs {
			Walk(p.Key, fn)
			Walk(p.Value, fn)
		}
	}
}

// HasExpression reports whether t, or anything below it, is an expression.
func HasExpression(t Token) bool {
	found := false
	Walk(t, func(x Token) bool {
		if IsExpression(x) {
			found = true
		}
		return !found
	})
	return found
}

func trimDisplay(s string) string {
	s = strings.TrimLeft(s, " \t\r\n\v\f")
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return s
}
