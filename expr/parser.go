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

// builtinFuncs are the functions of the reference's wellKnownFunctions, keyed
// by lower-cased name. They take precedence over caller-supplied FuncInfo for
// validation.
var builtinFuncs = map[string]FuncInfo{
	"case":       {"case", 3, math.MaxInt},
	"contains":   {"contains", 2, 2},
	"endswith":   {"endsWith", 2, 2},
	"format":     {"format", 1, 255},
	"fromjson":   {"fromJson", 1, 1},
	"join":       {"join", 1, 2},
	"startswith": {"startsWith", 2, 2},
	"tojson":     {"toJson", 1, 1},
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
	tokens, err := Lex(expr)
	if err != nil {
		return nil, err
	}
	p := &parser{
		tokens:   tokens,
		contexts: make(map[string]bool, len(contexts)),
		funcs:    make(map[string]FuncInfo, len(funcs)),
	}
	for _, c := range contexts {
		p.contexts[foldKey(c)] = true
	}
	for _, f := range funcs {
		p.funcs[foldKey(f.Name)] = f
	}
	return p.parse()
}

// parser is a port of Parser in parser.js: recursive descent, with the same
// precedence (lowest first): ||, &&, == !=, < <= > >=, !, then index and
// call.
type parser struct {
	tokens   []Token
	offset   int
	depth    int
	contexts map[string]bool
	funcs    map[string]FuncInfo
}

func (p *parser) parse() (Node, error) {
	if p.atEnd() {
		return nil, nil
	}
	n, err := p.expression()
	if err != nil {
		return nil, err
	}
	if !p.atEnd() {
		return nil, newTokenError(ErrorUnexpectedSymbol, p.peek())
	}
	return n, nil
}

func (p *parser) expression() (Node, error) {
	if err := p.incrDepth(); err != nil {
		return nil, err
	}
	n, err := p.logicalOr()
	p.depth--
	return n, err
}

func (p *parser) logicalOr() (Node, error) {
	n, err := p.logicalAnd()
	if err != nil || !p.check(TokenOr) {
		return n, err
	}
	return p.logicalChain(TokenOr, n, p.logicalAnd)
}

func (p *parser) logicalAnd() (Node, error) {
	n, err := p.equality()
	if err != nil || !p.check(TokenAnd) {
		return n, err
	}
	return p.logicalChain(TokenAnd, n, p.equality)
}

// logicalChain parses the rest of a flat chain of operator operands, the
// first of which is already parsed.
func (p *parser) logicalChain(op TokenType, first Node, operand func() (Node, error)) (Node, error) {
	if err := p.incrDepth(); err != nil {
		return nil, err
	}
	logical := &Logical{Operator: p.peek(), Args: []Node{first}}
	for p.match(op) {
		right, err := operand()
		if err != nil {
			return nil, err
		}
		logical.Args = append(logical.Args, right)
	}
	p.depth--
	return logical, nil
}

func (p *parser) equality() (Node, error) {
	n, err := p.comparison()
	if err != nil {
		return nil, err
	}
	for p.match(TokenBangEqual, TokenEqualEqual) {
		op := p.previous()
		right, err := p.comparison()
		if err != nil {
			return nil, err
		}
		n = &Binary{Left: n, Operator: op, Right: right}
	}
	return n, nil
}

func (p *parser) comparison() (Node, error) {
	n, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.match(TokenGreater, TokenGreaterEqual, TokenLess, TokenLessEqual) {
		op := p.previous()
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		n = &Binary{Left: n, Operator: op, Right: right}
	}
	return n, nil
}

func (p *parser) unary() (Node, error) {
	if p.match(TokenBang) {
		if err := p.incrDepth(); err != nil {
			return nil, err
		}
		op := p.previous()
		operand, err := p.unary()
		if err != nil {
			return nil, err
		}
		p.depth--
		return &Unary{Operator: op, Expr: operand}, nil
	}
	return p.index()
}

func (p *parser) index() (Node, error) {
	n, err := p.call()
	if err != nil {
		return nil, err
	}
	depthIncreased := 0
	switch n.(type) {
	case *Grouping, *FunctionCall, *ContextAccess:
	loop:
		for {
			switch {
			case p.match(TokenLeftBracket):
				var idx Node
				if p.match(TokenStar) {
					idx = &Star{}
				} else if idx, err = p.expression(); err != nil {
					return nil, err
				}
				if err := p.consume(TokenRightBracket, ErrorUnexpectedSymbol); err != nil {
					return nil, err
				}
				if err := p.incrDepth(); err != nil {
					return nil, err
				}
				depthIncreased++
				n = &IndexAccess{Expr: n, Index: idx}
			case p.match(TokenDot):
				if err := p.incrDepth(); err != nil {
					return nil, err
				}
				depthIncreased++
				switch {
				case p.match(TokenIdentifier):
					prop := p.previous()
					n = &IndexAccess{Expr: n, Index: &Literal{Value: String(prop.Lexeme), Token: prop}}
				case p.match(TokenStar):
					n = &IndexAccess{Expr: n, Index: &Star{}}
				default:
					return nil, newTokenError(ErrorUnexpectedSymbol, p.peek())
				}
			default:
				break loop
			}
		}
	}
	p.depth -= depthIncreased
	return n, nil
}

func (p *parser) call() (Node, error) {
	if !p.check(TokenIdentifier) {
		return p.primary()
	}
	ident := p.next()
	if !p.match(TokenLeftParen) {
		if !p.contexts[foldKey(ident.Lexeme)] {
			return nil, newTokenError(ErrorUnrecognizedContext, ident)
		}
		return &ContextAccess{Name: ident}, nil
	}
	var args []Node
	for !p.match(TokenRightParen) {
		arg, err := p.expression()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if !p.check(TokenRightParen) {
			// A trailing comma before ")" is accepted, as in the reference.
			if err := p.consume(TokenComma, ErrorUnexpectedSymbol); err != nil {
				return nil, err
			}
		}
	}
	if err := p.validateFunction(ident, len(args)); err != nil {
		return nil, err
	}
	return &FunctionCall{Name: ident, Args: args}, nil
}

// validateFunction checks that the function exists and accepts argCount
// arguments (funcs.js validateFunction).
func (p *parser) validateFunction(ident Token, argCount int) error {
	name := foldKey(ident.Lexeme)
	f, ok := builtinFuncs[name]
	if !ok {
		f, ok = p.funcs[name]
		if !ok {
			return newTokenError(ErrorUnrecognizedFunction, ident)
		}
	}
	if argCount < f.MinArgs {
		return newTokenError(ErrorTooFewParameters, ident)
	}
	if f.MaxArgs >= 0 && argCount > f.MaxArgs {
		return newTokenError(ErrorTooManyParameters, ident)
	}
	if name == "case" && argCount%2 == 0 {
		return newTokenError(ErrorEvenParameters, ident)
	}
	return nil
}

func (p *parser) primary() (Node, error) {
	switch {
	case p.match(TokenFalse):
		return &Literal{Value: Boolean(false), Token: p.previous()}, nil
	case p.match(TokenTrue):
		return &Literal{Value: Boolean(true), Token: p.previous()}, nil
	case p.match(TokenNull):
		return &Literal{Value: Null{}, Token: p.previous()}, nil
	case p.match(TokenNumber):
		return &Literal{Value: p.previous().Value, Token: p.previous()}, nil
	case p.match(TokenString):
		return &Literal{Value: p.previous().Value, Token: p.previous()}, nil
	case p.match(TokenLeftParen):
		n, err := p.expression()
		if err != nil {
			return nil, err
		}
		if p.atEnd() {
			// Back up to the last token before the EOF.
			return nil, newTokenError(ErrorUnexpectedEndOfExpression, p.previous())
		}
		if err := p.consume(TokenRightParen, ErrorUnexpectedSymbol); err != nil {
			return nil, err
		}
		return &Grouping{Group: n}, nil
	case p.atEnd():
		return nil, newTokenError(ErrorUnexpectedEndOfExpression, p.previous())
	}
	return nil, newTokenError(ErrorUnexpectedSymbol, p.peek())
}

// match consumes the next token if it has one of the given types.
func (p *parser) match(types ...TokenType) bool {
	for _, t := range types {
		if p.check(t) {
			p.next()
			return true
		}
	}
	return false
}

func (p *parser) check(t TokenType) bool {
	if p.atEnd() {
		return false
	}
	return p.peek().Type == t
}

func (p *parser) atEnd() bool { return p.peek().Type == TokenEOF }

func (p *parser) next() Token {
	if !p.atEnd() {
		p.offset++
	}
	return p.previous()
}

func (p *parser) peek() Token { return p.tokens[p.offset] }

// previous returns the token before the current one. At offset 0 the
// reference reads tokens[-1], which is undefined and crashes; here it
// returns the zero Token. That is unreachable: previous is only used after
// a token was consumed.
func (p *parser) previous() Token {
	if p.offset == 0 {
		return Token{}
	}
	return p.tokens[p.offset-1]
}

func (p *parser) consume(t TokenType, errType ErrorType) error {
	if p.check(t) {
		p.next()
		return nil
	}
	return newTokenError(errType, p.peek())
}

func (p *parser) incrDepth() error {
	p.depth++
	if p.depth > MaxParserDepth {
		return newTokenError(ErrorExceededMaxDepth, p.peek())
	}
	return nil
}
