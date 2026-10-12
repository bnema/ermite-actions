// SPDX-License-Identifier: MIT

package expr

// Node is a node of a parsed expression. The concrete types are [*Literal],
// [*Unary], [*FunctionCall], [*Binary], [*Logical], [*Grouping],
// [*ContextAccess], [*IndexAccess] and [*Star].
type Node interface {
	isNode()
}

// Literal is a null, boolean, number or string constant.
type Literal struct {
	Value Value
	Token Token
}

// Unary is a negation: "!x".
type Unary struct {
	Operator Token
	Expr     Node
}

// FunctionCall is a call such as "contains(a, b)". Name is the identifier
// token as written; function names are case-insensitive.
type FunctionCall struct {
	Name Token
	Args []Node
}

// Binary is a comparison: ==, !=, <, <=, > or >=.
type Binary struct {
	Left     Node
	Operator Token
	Right    Node
}

// Logical is a chain of && or || operands. The chain is flat: "a || b || c"
// is one Logical with three Args. Operator is the first operator token.
type Logical struct {
	Operator Token
	Args     []Node
}

// Grouping is a parenthesized expression.
type Grouping struct {
	Group Node
}

// ContextAccess is the name of a context, such as "github".
type ContextAccess struct {
	Name Token
}

// IndexAccess is "expr[index]" or "expr.name" (Index is then a string
// [Literal]) or "expr.*" (Index is a [Star]).
type IndexAccess struct {
	Expr  Node
	Index Node
}

// Star is the "*" of "expr.*" and "expr[*]".
type Star struct{}

func (*Literal) isNode()       {}
func (*Unary) isNode()         {}
func (*FunctionCall) isNode()  {}
func (*Binary) isNode()        {}
func (*Logical) isNode()       {}
func (*Grouping) isNode()      {}
func (*ContextAccess) isNode() {}
func (*IndexAccess) isNode()   {}
func (*Star) isNode()          {}
