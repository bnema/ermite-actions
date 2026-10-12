// SPDX-License-Identifier: MIT

package expr

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf16"
)

// TokenType is the kind of a lexical token.
type TokenType int

// The token types, in the order of the reference implementation.
const (
	TokenUnknown TokenType = iota
	TokenLeftParen
	TokenRightParen
	TokenLeftBracket
	TokenRightBracket
	TokenComma
	TokenDot
	TokenBang
	TokenBangEqual
	TokenEqualEqual
	TokenGreater
	TokenGreaterEqual
	TokenLess
	TokenLessEqual
	TokenAnd
	TokenOr
	TokenStar
	TokenNumber
	TokenString
	TokenIdentifier
	TokenTrue
	TokenFalse
	TokenNull
	TokenEOF
)

var tokenTypeNames = [...]string{
	"UNKNOWN", "LEFT_PAREN", "RIGHT_PAREN", "LEFT_BRACKET", "RIGHT_BRACKET",
	"COMMA", "DOT", "BANG", "BANG_EQUAL", "EQUAL_EQUAL", "GREATER",
	"GREATER_EQUAL", "LESS", "LESS_EQUAL", "AND", "OR", "STAR", "NUMBER",
	"STRING", "IDENTIFIER", "TRUE", "FALSE", "NULL", "EOF",
}

func (t TokenType) String() string {
	if t >= 0 && int(t) < len(tokenTypeNames) {
		return tokenTypeNames[t]
	}
	return fmt.Sprintf("TokenType(%d)", int(t))
}

// Range is the extent of a token. Both positions are on the line where the
// token ends, as in the reference implementation.
type Range struct {
	Start, End Position
}

// Token is a lexical token.
type Token struct {
	Type   TokenType
	Lexeme string
	Range  Range
	// Value is the number of a TokenNumber, as a [Number], or the unescaped
	// text of a TokenString, as a [String]; nil for other tokens.
	Value Value
}

// tokenString is the text an error message shows for a token.
func tokenString(t Token) string {
	switch t.Type {
	case TokenEOF:
		return "EOF"
	case TokenString:
		return string(t.Value.(String))
	}
	return t.Lexeme
}

// Lex splits an expression into tokens. The result always ends with a
// TokenEOF token.
func Lex(input string) ([]Token, error) {
	if exceedsUTF16Len(input, MaxExpressionLength) {
		// The reference throws a bare Error("ErrorExceededMaxLength") here,
		// not the descriptive ExpressionError the parser would build. The
		// length is checked before the input is converted so that a huge
		// input costs no allocation.
		return nil, &Error{Type: ErrorExceededMaxLength, Message: "ErrorExceededMaxLength"}
	}
	units := utf16.Encode([]rune(input))
	lx := &lexer{input: string(utf16.Decode(units)), units: units}
	if err := lx.lex(); err != nil {
		return nil, err
	}
	return lx.tokens, nil
}

// lexer is a line-by-line port of Lexer in lexer.js. It works on UTF-16 code
// units so that positions match the reference.
type lexer struct {
	input          string
	units          []uint16
	start          int
	offset         int
	line           int
	lastLineOffset int
	tokens         []Token
}

func (l *lexer) lex() error {
	for !l.atEnd() {
		l.start = l.offset
		c := l.next()
		var err error
		switch c {
		case '(':
			l.addToken(TokenLeftParen, nil)
		case ')':
			l.addToken(TokenRightParen, nil)
		case '[':
			l.addToken(TokenLeftBracket, nil)
		case ']':
			l.addToken(TokenRightBracket, nil)
		case ',':
			l.addToken(TokenComma, nil)
		case '.':
			p := l.previous()
			if p != TokenIdentifier && p != TokenRightBracket && p != TokenRightParen && p != TokenStar {
				err = l.consumeNumber()
			} else {
				l.addToken(TokenDot, nil)
			}
		case '-', '+':
			err = l.consumeNumber()
		case '!':
			l.addToken(l.pick('=', TokenBangEqual, TokenBang), nil)
		case '=':
			if !l.match('=') {
				// Illegal; continue reading until a boundary and report it.
				err = l.consumeIdentifier()
				break
			}
			l.addToken(TokenEqualEqual, nil)
		case '<':
			l.addToken(l.pick('=', TokenLessEqual, TokenLess), nil)
		case '>':
			l.addToken(l.pick('=', TokenGreaterEqual, TokenGreater), nil)
		case '&':
			if !l.match('&') {
				err = l.consumeIdentifier()
				break
			}
			l.addToken(TokenAnd, nil)
		case '|':
			if !l.match('|') {
				err = l.consumeIdentifier()
				break
			}
			l.addToken(TokenOr, nil)
		case '*':
			l.addToken(TokenStar, nil)
		case ' ', '\r', '\t':
			// Whitespace is ignored. Only these three characters and the
			// newline are: other Unicode spaces (U+00A0, \v, \f...) are not
			// skipped here and fall through to the identifier rule.
		case '\n':
			l.line++
			l.lastLineOffset = l.offset
		case '\'':
			err = l.consumeString()
		default:
			if c >= '0' && c <= '9' {
				err = l.consumeNumber()
			} else {
				err = l.consumeIdentifier()
			}
		}
		if err != nil {
			return err
		}
	}
	l.tokens = append(l.tokens, Token{Type: TokenEOF, Range: l.rng()})
	return nil
}

func (l *lexer) pos() Position {
	return Position{Line: l.line, Column: l.start - l.lastLineOffset}
}

func (l *lexer) rng() Range {
	return Range{
		Start: l.pos(),
		End:   Position{Line: l.line, Column: l.offset - l.lastLineOffset},
	}
}

func (l *lexer) atEnd() bool { return l.offset >= len(l.units) }

// peek returns the next unit without consuming it, or 0 at the end.
func (l *lexer) peek() uint16 {
	if l.atEnd() {
		return 0
	}
	return l.units[l.offset]
}

func (l *lexer) peekNext() uint16 {
	if l.offset+1 >= len(l.units) {
		return 0
	}
	return l.units[l.offset+1]
}

func (l *lexer) previous() TokenType {
	if len(l.tokens) == 0 {
		return TokenEOF
	}
	return l.tokens[len(l.tokens)-1].Type
}

func (l *lexer) next() uint16 {
	c := l.units[l.offset]
	l.offset++
	return c
}

func (l *lexer) match(expected uint16) bool {
	if l.atEnd() || l.units[l.offset] != expected {
		return false
	}
	l.offset++
	return true
}

// pick consumes expected if it is next and returns yes, otherwise no.
func (l *lexer) pick(expected uint16, yes, no TokenType) TokenType {
	if l.match(expected) {
		return yes
	}
	return no
}

func (l *lexer) substring(from, to int) string {
	return string(utf16.Decode(l.units[from:to]))
}

func (l *lexer) addToken(t TokenType, v Value) {
	l.tokens = append(l.tokens, Token{
		Type:   t,
		Lexeme: l.substring(l.start, l.offset),
		Range:  l.rng(),
		Value:  v,
	})
}

func (l *lexer) unexpected(lexeme string) *Error {
	return &Error{
		Type:    ErrorUnexpectedSymbol,
		Message: fmt.Sprintf("Unexpected symbol: '%s'. Located at position %d within expression: %s", lexeme, l.start+1, l.input),
		Pos:     l.pos(),
		Token:   lexeme,
	}
}

func (l *lexer) consumeNumber() error {
	for !l.atEnd() && (!isBoundary(l.peek()) || l.peek() == '.') {
		l.next()
	}
	lexeme := l.substring(l.start, l.offset)
	v := jsToNumber(lexeme)
	if math.IsNaN(v) {
		return l.unexpected(lexeme)
	}
	l.addToken(TokenNumber, Number(v))
	return nil
}

func (l *lexer) consumeString() error {
	for (l.peek() != '\'' || l.peekNext() == '\'') && !l.atEnd() {
		if l.peek() == '\n' {
			l.line++
		}
		if l.peek() == '\'' && l.peekNext() == '\'' {
			// Escaped quote: consume the first of the pair here.
			l.next()
		}
		l.next()
	}
	if l.atEnd() {
		return l.unexpected(l.substring(l.start, len(l.units)))
	}
	l.next() // closing quote
	value := l.substring(l.start+1, l.offset-1)
	// The reference calls value.replace("''", "'") with a string pattern,
	// which replaces only the FIRST doubled quote: 'a''b''c' is a'b''c.
	// GitHub's C# lexer unescapes every pair. Followed as written; see
	// PORTING.md.
	value = strings.Replace(value, "''", "'", 1)
	l.addToken(TokenString, String(value))
	return nil
}

func (l *lexer) consumeIdentifier() error {
	for !l.atEnd() && !isBoundary(l.peek()) {
		l.next()
	}
	tokenType := TokenIdentifier
	var tokenValue Value
	lexeme := l.substring(l.start, l.offset)
	if l.previous() != TokenDot {
		switch lexeme {
		case "true":
			tokenType = TokenTrue
		case "false":
			tokenType = TokenFalse
		case "null":
			tokenType = TokenNull
		case "NaN":
			tokenType = TokenNumber
			tokenValue = Number(nan)
		case "Infinity":
			tokenType = TokenNumber
			tokenValue = Number(posInf)
		}
	}
	if !isLegalIdentifier(lexeme) {
		return l.unexpected(lexeme)
	}
	l.addToken(tokenType, tokenValue)
	return nil
}

// isBoundary reports whether c ends a number or identifier.
func isBoundary(c uint16) bool {
	switch c {
	case '(', '[', ')', ']', ',', '.', '!', '>', '<', '=', '&', '|':
		return true
	}
	return isJSSpace(rune(c))
}

// isLegalIdentifier reports whether s matches [A-Za-z_][A-Za-z0-9_-]*.
func isLegalIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case i > 0 && (c >= '0' && c <= '9' || c == '-'):
		default:
			return false
		}
	}
	return true
}
