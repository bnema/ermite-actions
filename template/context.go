// SPDX-License-Identifier: MIT

package template

import (
	"fmt"
	"strings"
)

// MaxErrors is the number of errors a [Context] keeps; later ones are
// dropped. The reference keeps all of them, which lets a hostile file make the
// list as long as the file.
const MaxErrors = 1000

// Error is a problem found while reading or evaluating a template.
type Error struct {
	// File is the name of the file the problem is in, or "".
	File string
	// Pos is where the problem is; the zero value means no position.
	Pos Pos
	// Raw is the message without the "file (Line: l, Col: c): " prefix.
	Raw string

	// fileOnly makes the prefix the file name alone, for errors of the YAML
	// parser, which the reference reports without a position.
	fileOnly bool

	cause error
}

// Unwrap returns the error of package expr behind an evaluation error, if any.
func (e *Error) Unwrap() error { return e.cause }

// Error returns the message with its prefix, as the reference does:
// "file.yml (Line: 3, Col: 5): message".
func (e *Error) Error() string {
	pos := e.Pos
	if e.fileOnly {
		pos = Pos{}
	}
	if p := errorPrefix(e.File, pos); p != "" {
		return p + ": " + e.Raw
	}
	return e.Raw
}

func errorPrefix(file string, p Pos) string {
	switch {
	case file != "" && p.Line > 0:
		return fmt.Sprintf("%s (Line: %d, Col: %d)", file, p.Line, p.Col)
	case file != "":
		return file
	case p.Line > 0:
		return fmt.Sprintf("(Line: %d, Col: %d)", p.Line, p.Col)
	}
	return ""
}

// Context collects the errors of a read and carries its schema.
type Context struct {
	schema *Schema
	errors []*Error
	// Dropped counts the errors beyond [MaxErrors].
	dropped int
}

// NewContext returns a context that validates against schema.
func NewContext(schema *Schema) *Context { return &Context{schema: schema} }

// Schema returns the schema the context validates against.
func (c *Context) Schema() *Schema { return c.schema }

// Errors returns the errors recorded so far.
func (c *Context) Errors() []*Error { return append([]*Error(nil), c.errors...) }

// HasErrors reports whether an error was recorded.
func (c *Context) HasErrors() bool { return len(c.errors) > 0 }

// Errorf records an error located at tok, which may be nil.
func (c *Context) Errorf(tok Token, format string, a ...any) {
	c.ErrorAt(tok, fmt.Sprintf(format, a...))
}

// ErrorAt records an error with message msg located at tok, which may be nil.
func (c *Context) ErrorAt(tok Token, msg string) {
	e := &Error{Raw: msg}
	if tok != nil {
		e.File, e.Pos = tok.File(), tok.Pos()
	}
	c.add(e)
}

// ErrorInFile records an error that is about a whole file.
func (c *Context) ErrorInFile(file, msg string) { c.add(&Error{File: file, Raw: msg}) }

// ErrorAtPos records an error at an explicit position of file.
func (c *Context) ErrorAtPos(file string, p Pos, msg string) {
	c.add(&Error{File: file, Pos: p, Raw: msg})
}

func (c *Context) add(e *Error) {
	if len(c.errors) >= MaxErrors {
		c.dropped++
		return
	}
	c.errors = append(c.errors, e)
}

// Messages returns the full text of the errors.
func (c *Context) Messages() []string {
	out := make([]string, len(c.errors))
	for i, e := range c.errors {
		out[i] = e.Error()
	}
	return out
}

// Join returns the errors as one text, one per line.
func (c *Context) Join() string { return strings.Join(c.Messages(), "\n") }
