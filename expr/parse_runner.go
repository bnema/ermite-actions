// SPDX-License-Identifier: MIT

package expr

import (
	"strings"
	"unicode/utf16"
)

// This file is a port of the runner's tokenizer and parser:
// src/Sdk/DTExpressions2/Expressions2/Tokens/LexicalAnalyzer.cs, Tokens/Token.cs
// and ExpressionParser.cs. They differ from the JavaScript reference's
// (lexer.go, parser.go) in ways users can see:
//
//   - the tokenizer is context sensitive: each token is checked against the
//     one before it, so "a b", "1 2" or "f(1,)" fail at the second token;
//   - tokens are produced on demand and the parser stops at the first
//     unexpected token or unknown name, in order of appearance (the JavaScript
//     lexes everything first, so a later lexical error wins there);
//   - white space is Char.IsWhiteSpace (U+0085 and U+00A0 included);
//   - "Unexpected end of expression" is reported for an unfinished expression
//     where the JavaScript says "Unexpected symbol: 'EOF'";
//   - error positions are absolute indexes in the expression, not columns;
//   - the depth limit applies to the shape of the finished tree, in which
//     parentheses do not count and a flat run of && or || is one node;
//   - the "too long" and "too deep" errors carry no token.
//
// Parse builds the same AST as the JavaScript-style parser did.

type csKind int

const (
	csStartGroup csKind = iota
	csStartIndex
	csStartParameters
	csEndGroup
	csEndIndex
	csEndParameters
	csSeparator
	csDereference
	csWildcard
	csLogicalOperator
	csNull
	csBoolean
	csNumber
	csString
	csPropertyName
	csFunction
	csNamedValue
	csUnexpected
)

type csToken struct {
	kind  csKind
	raw   string
	index int
	value Value // Boolean, Number or String for literals
}

func (t *csToken) isOperator() bool {
	switch t.kind {
	case csStartGroup, csStartIndex, csStartParameters, csEndGroup, csEndIndex, csEndParameters, csSeparator, csDereference, csLogicalOperator:
		return true
	}
	return false
}

// leftToRight reports whether the token is a left-associative operator.
func (t *csToken) leftToRight() bool {
	switch {
	case t.kind == csStartGroup:
		return false
	case t.kind == csLogicalOperator && t.raw == "!":
		return false
	}
	return t.isOperator()
}

func (t *csToken) precedence() int {
	switch t.kind {
	case csStartGroup:
		return 20
	case csStartIndex, csStartParameters, csDereference:
		return 19
	case csLogicalOperator:
		switch t.raw {
		case "!":
			return 16
		case ">", ">=", "<", "<=":
			return 11
		case "==", "!=":
			return 10
		case "&&":
			return 6
		case "||":
			return 5
		}
	case csEndGroup, csEndIndex, csEndParameters, csSeparator:
		return 1
	}
	return 0
}

func (t *csToken) operandCount() int {
	switch t.kind {
	case csStartIndex, csDereference:
		return 2
	case csLogicalOperator:
		if t.raw == "!" {
			return 1
		}
		return 2
	}
	return 0
}

// csLexer is LexicalAnalyzer.
type csLexer struct {
	units    []uint16
	index    int
	last     *csToken
	unclosed []*csToken // stack of "(", "[" and function "("
}

func (l *csLexer) substr(from, to int) string { return string(utf16.Decode(l.units[from:to])) }

func isCSWhiteSpace(c uint16) bool { return isNetSpace(rune(c)) }

func isCSBoundary(c uint16) bool {
	switch c {
	case '(', '[', ')', ']', ',', '.', '!', '>', '<', '=', '&', '|':
		return true
	}
	return isCSWhiteSpace(c)
}

func (l *csLexer) topUnclosed() csKind {
	if len(l.unclosed) == 0 {
		return -1
	}
	return l.unclosed[len(l.unclosed)-1].kind
}

// next returns the next token, or nil at the end of the expression.
func (l *csLexer) next() *csToken {
	for l.index < len(l.units) && isCSWhiteSpace(l.units[l.index]) {
		l.index++
	}
	if l.index >= len(l.units) {
		return nil
	}
	c := l.units[l.index]
	var tok *csToken
	switch c {
	case '(':
		if l.last != nil && l.last.kind == csFunction {
			tok = l.create(csStartParameters, "(", l.index, nil)
		} else {
			tok = l.create(csStartGroup, "(", l.index, nil)
		}
		l.index++
	case '[':
		tok = l.create(csStartIndex, "[", l.index, nil)
		l.index++
	case ')':
		if l.topUnclosed() == csStartParameters {
			tok = l.create(csEndParameters, ")", l.index, nil)
		} else {
			tok = l.create(csEndGroup, ")", l.index, nil)
		}
		l.index++
	case ']':
		tok = l.create(csEndIndex, "]", l.index, nil)
		l.index++
	case ',':
		tok = l.create(csSeparator, ",", l.index, nil)
		l.index++
	case '*':
		tok = l.create(csWildcard, "*", l.index, nil)
		l.index++
	case '\'':
		tok = l.readString()
	case '!', '>', '<', '=', '&', '|':
		tok = l.readOperator()
	default:
		switch {
		case c == '.':
			if l.last == nil || l.last.kind == csSeparator || l.last.kind == csStartGroup || l.last.kind == csStartIndex ||
				l.last.kind == csStartParameters || l.last.kind == csLogicalOperator {
				tok = l.readNumber()
			} else {
				tok = l.create(csDereference, ".", l.index, nil)
				l.index++
			}
		case c == '-' || c == '+' || (c >= '0' && c <= '9'):
			tok = l.readNumber()
		default:
			tok = l.readKeyword()
		}
	}
	l.last = tok
	return tok
}

func (l *csLexer) readNumber() *csToken {
	start := l.index
	for {
		l.index++
		if !(l.index < len(l.units) && (!isCSBoundary(l.units[l.index]) || l.units[l.index] == '.')) {
			break
		}
	}
	str := l.substr(start, l.index)
	d := parseNumber(str)
	if d != d { // NaN
		return l.create(csUnexpected, str, start, nil)
	}
	return l.create(csNumber, str, start, Number(d))
}

func isLegalKeyword(s string) bool { return isLegalIdentifier(s) }

func (l *csLexer) readKeyword() *csToken {
	start := l.index
	l.index++
	for l.index < len(l.units) && !isCSBoundary(l.units[l.index]) {
		l.index++
	}
	str := l.substr(start, l.index)
	if !isLegalKeyword(str) {
		return l.create(csUnexpected, str, start, nil)
	}
	if l.last != nil && l.last.kind == csDereference {
		return l.create(csPropertyName, str, start, nil)
	}
	switch str {
	case "null":
		return l.create(csNull, str, start, Null{})
	case "true":
		return l.create(csBoolean, str, start, Boolean(true))
	case "false":
		return l.create(csBoolean, str, start, Boolean(false))
	case "NaN":
		return l.create(csNumber, str, start, Number(nan))
	case "Infinity":
		return l.create(csNumber, str, start, Number(posInf))
	}
	// Lookahead: a function is a name followed by "(", white space allowed.
	t := l.index
	for t < len(l.units) && isCSWhiteSpace(l.units[t]) {
		t++
	}
	if t < len(l.units) && l.units[t] == '(' {
		return l.create(csFunction, str, start, nil)
	}
	return l.create(csNamedValue, str, start, nil)
}

func (l *csLexer) readString() *csToken {
	start := l.index
	var b strings.Builder
	closed := false
	l.index++ // the opening quote
	var run []uint16
	for l.index < len(l.units) {
		c := l.units[l.index]
		l.index++
		if c == '\'' {
			if l.index >= len(l.units) || l.units[l.index] != '\'' {
				closed = true
				break
			}
			l.index++ // an escaped quote
		}
		run = append(run, c)
	}
	b.WriteString(string(utf16.Decode(run)))
	raw := l.substr(start, l.index)
	if closed {
		return l.create(csString, raw, start, String(b.String()))
	}
	return l.create(csUnexpected, raw, start, nil)
}

func (l *csLexer) readOperator() *csToken {
	start := l.index
	l.index++
	if l.index < len(l.units) {
		l.index++
		switch raw := l.substr(start, start+2); raw {
		case "!=", ">=", "<=", "==", "&&", "||":
			return l.create(csLogicalOperator, raw, start, nil)
		}
		l.index-- // back up
	}
	switch raw := l.substr(start, start+1); raw {
	case "!", ">", "<":
		return l.create(csLogicalOperator, raw, start, nil)
	}
	for l.index < len(l.units) && !isCSBoundary(l.units[l.index]) {
		l.index++
	}
	return l.create(csUnexpected, l.substr(start, l.index), start, nil)
}

// lastIs reports whether the previous token is one of kinds; a nil kind list
// entry (csUnexpected here) stands for "no previous token".
func (l *csLexer) lastIs(kinds ...csKind) bool {
	if l.last == nil {
		for _, k := range kinds {
			if k == -1 {
				return true
			}
		}
		return false
	}
	for _, k := range kinds {
		if k == l.last.kind {
			return true
		}
	}
	return false
}

const csFirst csKind = -1 // "is the first token"

// create builds a token after checking that it may follow the previous one
// and that brackets are balanced; an illegal one becomes csUnexpected.
func (l *csLexer) create(kind csKind, raw string, index int, v Value) *csToken {
	var legal bool
	switch kind {
	case csStartGroup:
		legal = l.lastIs(csFirst, csSeparator, csStartGroup, csStartParameters, csStartIndex, csLogicalOperator)
	case csStartIndex:
		legal = l.lastIs(csEndGroup, csEndParameters, csEndIndex, csWildcard, csPropertyName, csNamedValue)
	case csStartParameters:
		legal = l.lastIs(csFunction)
	case csEndGroup, csEndIndex:
		legal = l.lastIs(csEndGroup, csEndParameters, csEndIndex, csWildcard, csNull, csBoolean, csNumber, csString, csPropertyName, csNamedValue)
	case csEndParameters:
		legal = l.lastIs(csStartParameters, csEndGroup, csEndParameters, csEndIndex, csWildcard, csNull, csBoolean, csNumber, csString, csPropertyName, csNamedValue)
	case csSeparator:
		legal = l.lastIs(csEndGroup, csEndParameters, csEndIndex, csWildcard, csNull, csBoolean, csNumber, csString, csPropertyName, csNamedValue)
	case csDereference:
		legal = l.lastIs(csEndGroup, csEndParameters, csEndIndex, csWildcard, csPropertyName, csNamedValue)
	case csWildcard:
		legal = l.lastIs(csStartIndex, csDereference)
	case csLogicalOperator:
		if raw == "!" {
			legal = l.lastIs(csFirst, csSeparator, csStartGroup, csStartParameters, csStartIndex, csLogicalOperator)
		} else {
			legal = l.lastIs(csEndGroup, csEndParameters, csEndIndex, csWildcard, csNull, csBoolean, csNumber, csString, csPropertyName, csNamedValue)
		}
	case csNull, csBoolean, csNumber, csString, csFunction, csNamedValue:
		legal = l.lastIs(csFirst, csSeparator, csStartIndex, csStartGroup, csStartParameters, csLogicalOperator)
	case csPropertyName:
		legal = l.lastIs(csDereference)
	}
	if !legal {
		return &csToken{kind: csUnexpected, raw: raw, index: index}
	}
	tok := &csToken{kind: kind, raw: raw, index: index, value: v}
	switch kind {
	case csStartGroup, csStartIndex, csStartParameters:
		l.unclosed = append(l.unclosed, tok)
	case csEndGroup:
		if l.topUnclosed() != csStartGroup {
			return &csToken{kind: csUnexpected, raw: raw, index: index}
		}
		l.unclosed = l.unclosed[:len(l.unclosed)-1]
	case csEndIndex:
		if l.topUnclosed() != csStartIndex {
			return &csToken{kind: csUnexpected, raw: raw, index: index}
		}
		l.unclosed = l.unclosed[:len(l.unclosed)-1]
	case csEndParameters:
		if l.topUnclosed() != csStartParameters {
			return &csToken{kind: csUnexpected, raw: raw, index: index}
		}
		l.unclosed = l.unclosed[:len(l.unclosed)-1]
	case csSeparator:
		if l.topUnclosed() != csStartParameters {
			return &csToken{kind: csUnexpected, raw: raw, index: index}
		}
	}
	return tok
}

// csParser is ExpressionParser.CreateTree: a shunting-yard parser.
type csParser struct {
	expression string
	units      []uint16
	lex        *csLexer
	contexts   map[string]bool
	funcs      map[string]FuncInfo
	operands   []Node
	operators  []*csToken
	last       *csToken // the previous token
	cur        *csToken
}

// fail builds the ParseException of the runner: with a token, the message names
// it and its absolute position; without, only the description.
func (p *csParser) fail(t ErrorType, tok *csToken) *Error {
	if tok == nil {
		return &Error{Type: t, Message: t.description()}
	}
	return &Error{
		Type:    t,
		Message: t.description() + ": '" + tok.raw + "'. Located at position " + itoa(tok.index+1) + " within expression: " + p.expression,
		Pos:     p.position(tok.index),
		Token:   tok.raw,
	}
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + intString(n)) }

func intString(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// position converts an absolute index into the line and column of the
// JavaScript-style Position (line breaks are U+000A).
func (p *csParser) position(index int) Position {
	line, lineStart := 0, 0
	for i := 0; i < index && i < len(p.units); i++ {
		if p.units[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return Position{Line: line, Column: index - lineStart}
}

func (p *csParser) token(t *csToken, typ TokenType) Token {
	end := t.index + len(utf16.Encode([]rune(t.raw)))
	return Token{Type: typ, Lexeme: t.raw, Range: Range{Start: p.position(t.index), End: p.position(end)}, Value: t.value}
}

func (p *csParser) wellKnownOrExtension(name string) (FuncInfo, bool) {
	k := foldKey(name)
	if f, ok := builtinFuncs[k]; ok {
		return f, true
	}
	f, ok := p.funcs[k]
	return f, ok
}

// parseRunner parses expression the way the runner does.
func parseRunner(expression string, contexts []string, funcs []FuncInfo) (Node, error) {
	p := &csParser{contexts: make(map[string]bool, len(contexts)), funcs: make(map[string]FuncInfo, len(funcs))}
	if exceedsUTF16Len(expression, MaxExpressionLength) {
		return nil, p.fail(ErrorExceededMaxLength, nil)
	}
	p.units = utf16.Encode([]rune(expression))
	p.expression = string(utf16.Decode(p.units))
	for _, c := range contexts {
		p.contexts[foldKey(c)] = true
	}
	for _, f := range funcs {
		p.funcs[foldKey(f.Name)] = f
	}
	p.lex = &csLexer{units: p.units}
	return p.createTree()
}

func (p *csParser) createTree() (Node, error) {
	for {
		tok := p.lex.next()
		if tok == nil {
			break
		}
		p.cur = tok
		switch {
		case tok.kind == csUnexpected:
			return nil, p.fail(ErrorUnexpectedSymbol, tok)
		case tok.isOperator():
			if err := p.pushOperator(tok); err != nil {
				return nil, err
			}
		default:
			if err := p.pushOperand(tok); err != nil {
				return nil, err
			}
		}
		p.last = tok
	}
	if p.last == nil {
		return nil, nil
	}
	if len(p.operators) > 0 {
		unexpected := false
		switch p.last.kind {
		case csEndGroup, csEndIndex, csEndParameters:
		case csFunction:
			unexpected = true
		default:
			unexpected = p.last.isOperator()
		}
		if unexpected || len(p.lex.unclosed) > 0 {
			return nil, p.fail(ErrorUnexpectedEndOfExpression, p.last)
		}
	}
	for len(p.operators) > 0 {
		if err := p.flushTop(); err != nil {
			return nil, err
		}
	}
	if len(p.operands) != 1 {
		return nil, p.fail(ErrorUnexpectedSymbol, p.last)
	}
	root := p.operands[0]
	if exceedsDepth(root, 1) {
		return nil, p.fail(ErrorExceededMaxDepth, nil)
	}
	return root, nil
}

func (p *csParser) pushOperand(tok *csToken) error {
	var n Node
	switch tok.kind {
	case csFunction:
		if _, ok := p.wellKnownOrExtension(tok.raw); !ok {
			return p.fail(ErrorUnrecognizedFunction, tok)
		}
		n = &FunctionCall{Name: p.token(tok, TokenIdentifier)}
	case csNamedValue:
		if !p.contexts[foldKey(tok.raw)] {
			return p.fail(ErrorUnrecognizedContext, tok)
		}
		n = &ContextAccess{Name: p.token(tok, TokenIdentifier)}
	case csNull:
		n = &Literal{Value: Null{}, Token: p.token(tok, TokenNull)}
	case csBoolean:
		typ := TokenFalse
		if tok.value == Boolean(true) {
			typ = TokenTrue
		}
		n = &Literal{Value: tok.value, Token: p.token(tok, typ)}
	case csNumber:
		n = &Literal{Value: tok.value, Token: p.token(tok, TokenNumber)}
	case csString:
		n = &Literal{Value: tok.value, Token: p.token(tok, TokenString)}
	case csPropertyName:
		n = &Literal{Value: String(tok.raw), Token: p.token(tok, TokenIdentifier)}
	case csWildcard:
		n = &Star{}
	default:
		return p.fail(ErrorUnexpectedSymbol, tok)
	}
	p.operands = append(p.operands, n)
	return nil
}

func (p *csParser) pushOperator(tok *csToken) error {
	if tok.leftToRight() {
		prec := tok.precedence()
		for len(p.operators) > 0 {
			top := p.operators[len(p.operators)-1]
			if prec <= top.precedence() && top.kind != csStartGroup && top.kind != csStartIndex &&
				top.kind != csStartParameters && top.kind != csSeparator {
				if err := p.flushTop(); err != nil {
					return err
				}
				continue
			}
			break
		}
	}
	p.operators = append(p.operators, tok)
	switch tok.kind {
	case csEndGroup, csEndIndex, csEndParameters:
		return p.flushTop()
	}
	return nil
}

func (p *csParser) popOperator(want csKind) (*csToken, error) {
	if len(p.operators) == 0 || p.operators[len(p.operators)-1].kind != want {
		return nil, p.fail(ErrorUnexpectedSymbol, p.cur)
	}
	t := p.operators[len(p.operators)-1]
	p.operators = p.operators[:len(p.operators)-1]
	return t, nil
}

// popOperands pops n operands, returned in their written order.
func (p *csParser) popOperands(n int) ([]Node, error) {
	if len(p.operands) < n {
		return nil, p.fail(ErrorUnexpectedSymbol, p.cur)
	}
	out := append([]Node(nil), p.operands[len(p.operands)-n:]...)
	p.operands = p.operands[:len(p.operands)-n]
	return out, nil
}

func (p *csParser) flushTop() error {
	top := p.operators[len(p.operators)-1]
	switch top.kind {
	case csEndIndex:
		if _, err := p.popOperator(csEndIndex); err != nil {
			return err
		}
		op, err := p.popOperator(csStartIndex)
		if err != nil {
			return err
		}
		args, err := p.popOperands(op.operandCount())
		if err != nil {
			return err
		}
		p.operands = append(p.operands, &IndexAccess{Expr: args[0], Index: args[1]})
		return nil
	case csEndGroup:
		if _, err := p.popOperator(csEndGroup); err != nil {
			return err
		}
		if _, err := p.popOperator(csStartGroup); err != nil {
			return err
		}
		if len(p.operands) == 0 {
			return p.fail(ErrorUnexpectedSymbol, p.cur)
		}
		g := &Grouping{Group: p.operands[len(p.operands)-1]}
		p.operands[len(p.operands)-1] = g
		return nil
	case csEndParameters:
		return p.flushEndParameters()
	}
	op, _ := p.popOperator(top.kind)
	args, err := p.popOperands(op.operandCount())
	if err != nil {
		return err
	}
	switch {
	case op.kind == csStartIndex || op.kind == csDereference:
		p.operands = append(p.operands, &IndexAccess{Expr: args[0], Index: args[1]})
	case op.kind == csLogicalOperator && op.raw == "!":
		p.operands = append(p.operands, &Unary{Operator: p.token(op, TokenBang), Expr: args[0]})
	case op.kind == csLogicalOperator && (op.raw == "&&" || op.raw == "||"):
		typ := TokenAnd
		if op.raw == "||" {
			typ = TokenOr
		}
		l := &Logical{Operator: p.token(op, typ)}
		for _, a := range args {
			// A nested chain of the same operator is merged, as the runner does.
			if nested, ok := a.(*Logical); ok && nested.Operator.Type == typ {
				l.Args = append(l.Args, nested.Args...)
			} else {
				l.Args = append(l.Args, a)
			}
		}
		p.operands = append(p.operands, l)
	case op.kind == csLogicalOperator:
		var typ TokenType
		switch op.raw {
		case "!=":
			typ = TokenBangEqual
		case "==":
			typ = TokenEqualEqual
		case ">":
			typ = TokenGreater
		case ">=":
			typ = TokenGreaterEqual
		case "<":
			typ = TokenLess
		default:
			typ = TokenLessEqual
		}
		p.operands = append(p.operands, &Binary{Left: args[0], Operator: p.token(op, typ), Right: args[1]})
	default:
		return p.fail(ErrorUnexpectedSymbol, op)
	}
	return nil
}

func (p *csParser) flushEndParameters() error {
	end, err := p.popOperator(csEndParameters)
	if err != nil {
		return err
	}
	if end != p.cur || p.last == nil {
		return p.fail(ErrorUnexpectedSymbol, p.cur)
	}
	count := 0
	if p.last.kind != csStartParameters {
		count = 1
		for len(p.operators) > 0 && p.operators[len(p.operators)-1].kind == csSeparator {
			count++
			p.operators = p.operators[:len(p.operators)-1]
		}
	}
	args, err := p.popOperands(count)
	if err != nil {
		return err
	}
	if len(p.operands) == 0 {
		return p.fail(ErrorUnexpectedSymbol, p.cur)
	}
	call, ok := p.operands[len(p.operands)-1].(*FunctionCall)
	if !ok {
		return p.fail(ErrorUnexpectedSymbol, p.cur)
	}
	call.Args = args
	open, err := p.popOperator(csStartParameters)
	if err != nil {
		return err
	}
	info, _ := p.wellKnownOrExtension(call.Name.Lexeme)
	switch {
	case len(args) < info.MinArgs:
		return p.fail(ErrorTooFewParameters, open)
	case info.MaxArgs >= 0 && len(args) > info.MaxArgs:
		return p.fail(ErrorTooManyParameters, open)
	case strings.EqualFold(info.Name, "case") && len(args)%2 == 0:
		return p.fail(ErrorEvenParameters, open)
	}
	return nil
}

// exceedsDepth reports whether the tree is deeper than MaxParserDepth, counted
// as the runner does (CheckMaxDepth): the root is at depth 1, the operands of
// an operator, the arguments of a call and the two parts of an index access
// are one deeper, parentheses add nothing, and a run of && or || is one node,
// also when parenthesized.
func exceedsDepth(n Node, depth int) bool {
	for {
		g, ok := n.(*Grouping)
		if !ok {
			break
		}
		n = g.Group
	}
	if depth > MaxParserDepth {
		return true
	}
	switch n := n.(type) {
	case *Unary:
		return exceedsDepth(n.Expr, depth+1)
	case *Binary:
		return exceedsDepth(n.Left, depth+1) || exceedsDepth(n.Right, depth+1)
	case *Logical:
		for _, a := range flatOperands(n, nil) {
			if exceedsDepth(a, depth+1) {
				return true
			}
		}
	case *FunctionCall:
		for _, a := range n.Args {
			if exceedsDepth(a, depth+1) {
				return true
			}
		}
	case *IndexAccess:
		return exceedsDepth(n.Expr, depth+1) || exceedsDepth(n.Index, depth+1)
	}
	return false
}

// flatOperands lists the operands of a && or || chain with nested chains of
// the same operator (parenthesized or not) merged into it.
func flatOperands(l *Logical, out []Node) []Node {
	for _, a := range l.Args {
		inner := a
		for {
			g, ok := inner.(*Grouping)
			if !ok {
				break
			}
			inner = g.Group
		}
		if nested, ok := inner.(*Logical); ok && nested.Operator.Type == l.Operator.Type {
			out = flatOperands(nested, out)
		} else {
			out = append(out, a)
		}
	}
	return out
}
