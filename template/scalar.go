// SPDX-License-Identifier: MIT

package template

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"bnema.dev/ermite-actions/expr"
)

const (
	openExpression  = "${{"
	insertDirective = "insert"
)

// parseScalar splits a string that holds ${{ }} expressions into literal
// text and expression tokens. Strings without an expression, other scalars
// and strings with an error in an expression are returned as they are.
//
// The reference works on the raw source of a multi-line YAML scalar, which
// keeps the indentation of a block scalar; this port always works on the
// value, as the runner does.
func (r *reader) parseScalar(_ *rnode, token Token, def *DefinitionInfo) Token {
	s, ok := token.(*String)
	if r.noExpressions || !ok || s.Value == "" {
		return token
	}
	raw := s.Value
	startExpression := strings.Index(raw, openExpression)
	if startExpression < 0 {
		return token
	}
	var segments []Token
	encounteredError := false
	i := 0
	for i < len(raw) {
		switch {
		case i == startExpression:
			end := -1
			inString := false
			for i += len(openExpression); i < len(raw); i++ {
				if raw[i] == '\'' {
					inString = !inString
				} else if !inString && raw[i] == '}' && raw[i-1] == '}' {
					end = i
					i++
					break
				}
			}
			if end < startExpression {
				r.ctx.ErrorAt(token, "The expression is not closed. An unescaped ${{ sequence was found, but the closing }} sequence was not found.")
				return token
			}
			rawExpression := raw[startExpression+len(openExpression) : end-1]
			e := r.parseIntoExpressionToken(token, rawExpression, def)
			if e == nil {
				encounteredError = true
			} else {
				if _, isInsert := e.(*InsertExpression); isInsert && (startExpression != 0 || i < len(raw)) {
					r.ctx.ErrorAt(token, "The directive 'insert' is not allowed in this context. Directives are not supported for expressions that are embedded within a string. Directives are only supported when the entire value is an expression.")
					return token
				}
				segments = append(segments, e)
			}
			if j := strings.Index(raw[i:], openExpression); j >= 0 {
				startExpression = i + j
			} else {
				startExpression = -1
			}
		case i < startExpression:
			segments = r.addString(segments, token, raw[i:startExpression], s.def)
			i = startExpression
		default:
			segments = r.addString(segments, token, raw[i:], s.def)
			i = len(raw)
		}
	}
	if encounteredError {
		return token
	}
	if len(segments) == 1 {
		if be, ok := segments[0].(*BasicExpression); ok {
			if str, ok := expressionString(be.Expression); ok {
				return &String{tokenBase: tokenBase{file: r.file, pos: token.Pos(), def: s.def}, Value: str}
			}
		}
		return segments[0]
	}
	var format strings.Builder
	var args strings.Builder
	var originals []*BasicExpression
	argIndex := 0
	for _, seg := range segments {
		if ss, ok := seg.(*String); ok {
			text := strings.ReplaceAll(ss.Value, "'", "''")
			text = strings.ReplaceAll(text, "{", "{{")
			text = strings.ReplaceAll(text, "}", "}}")
			format.WriteString(text)
			continue
		}
		be := seg.(*BasicExpression)
		fmt.Fprintf(&format, "{%d}", argIndex)
		argIndex++
		args.WriteString(", ")
		args.WriteString(be.Expression)
		originals = append(originals, be)
	}
	t := &BasicExpression{
		tokenBase:  tokenBase{file: r.file, pos: token.Pos(), def: def},
		Expression: "format('" + format.String() + "'" + args.String() + ")",
		Original:   originals,
		Source:     raw,
	}
	t.node, t.parseErr = parseWith(t.Expression, def)
	return t
}

func (r *reader) addString(segs []Token, at Token, value string, def *DefinitionInfo) []Token {
	if n := len(segs); n > 0 {
		if last, ok := segs[n-1].(*String); ok {
			segs[n-1] = &String{tokenBase: tokenBase{file: r.file, pos: at.Pos(), def: def}, Value: last.Value + value}
			return segs
		}
	}
	return append(segs, &String{tokenBase: tokenBase{file: r.file, pos: at.Pos(), def: def}, Value: value})
}

func (r *reader) parseIntoExpressionToken(token Token, rawExpression string, def *DefinitionInfo) Token {
	e, err := r.parseExpression(token, rawExpression, def)
	if err != nil {
		r.ctx.ErrorAt(token, err.Error())
		return nil
	}
	return e
}

func (r *reader) parseExpression(token Token, value string, def *DefinitionInfo) (Token, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, errors.New("An expression was expected")
	}
	isMatch, err := matchDirective(trimmed, insertDirective, 0)
	if err != nil {
		return nil, err
	}
	if isMatch {
		return &InsertExpression{tokenBase{file: r.file, pos: token.Pos(), def: def}}, nil
	}
	node, err := parseWith(trimmed, def)
	if err != nil {
		return nil, err
	}
	src, _ := token.(*String)
	t := &BasicExpression{
		tokenBase:  tokenBase{file: r.file, pos: token.Pos(), def: def},
		Expression: trimmed,
		node:       node,
	}
	if src != nil {
		t.Source = src.Value
	}
	return t, nil
}

// parseWith parses an expression against the contexts and functions allowed
// by def. A nil def allows nothing.
func parseWith(text string, def *DefinitionInfo) (expr.Node, error) {
	var named []string
	var funcs []expr.FuncInfo
	if def != nil {
		named, funcs = def.named, def.funcs
	}
	return expr.Parse(text, named, funcs)
}

// matchDirective reports whether trimmed is the directive followed by exactly
// expectedParameters parameters. It is a literal port of the reference,
// including the way it counts parameters.
func matchDirective(trimmed, directive string, expectedParameters int) (bool, error) {
	if !strings.HasPrefix(trimmed, directive) {
		return false, nil
	}
	rest := trimmed[len(directive):]
	if rest != "" {
		c := []rune(rest)[0]
		if !unicode.IsSpace(c) && c != '\ufeff' {
			return false, nil
		}
	}
	// The reference only counts the text after the last separator that is
	// outside quotes and parentheses; its loop never counts earlier ones.
	rs := []rune(trimmed)
	startIndex := len([]rune(directive))
	inString := false
	parens := 0
	for i := startIndex; i < len(rs); i++ {
		c := rs[i]
		switch {
		case unicode.IsSpace(c) && !inString && parens == 0:
			startIndex = i + 1
		case c == '\'':
			inString = !inString
		case c == '(' && !inString:
			parens++
		case c == ')' && !inString:
			parens--
		}
	}
	parameters := 0
	if startIndex < len(rs) {
		parameters = 1
	}
	if expectedParameters != parameters {
		return false, fmt.Errorf("Exactly %d parameter(s) were expected following the directive '%s'. Actual parameter count: %d", expectedParameters, directive, parameters)
	}
	return true, nil
}

// expressionString returns the text of an expression that is a single quoted
// string, such as ${{ '{{ literal }}' }}, which is how GitHub lets a workflow
// contain a literal ${{.
func expressionString(trimmed string) (string, bool) {
	var out []byte
	inString := false
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		switch {
		case c == '\'':
			inString = !inString
			if inString && i != 0 {
				out = append(out, c)
			}
		case !inString:
			return "", false
		default:
			out = append(out, c)
		}
	}
	return string(out), true
}
