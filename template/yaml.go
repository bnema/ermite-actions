// SPDX-License-Identifier: MIT

package template

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// MaxFileBytes is the largest document [ReadYAML] accepts: 1 MiB. The check
// comes before any parsing, so that a hostile file costs nothing more than
// its length; a file this size can still expand through aliases (see
// [MaxNodes]).
//
// TODO(verify with recording): GitHub's own limit and message.
const MaxFileBytes = 1 << 20

// MaxNodes is the number of YAML nodes a document may expand to, counting
// every use of an alias. The reference expands aliases without a limit, so a
// small document of nested aliases can allocate gigabytes.
const MaxNodes = 200_000

type rkind int

const (
	rLiteral rkind = iota
	rSeq
	rMap
)

// rnode is a YAML node after alias resolution: the tree the template reader
// walks.
type rnode struct {
	kind  rkind
	pos   Pos
	lit   Token // rLiteral: a *String, *Number, *Boolean or *Null
	items []*rnode
	keys  []*rnode // rMap: the keys, rLiteral nodes with string tokens
	vals  []*rnode // rMap: the values, parallel to keys
	// quoted and block describe how a string scalar was written; they only
	// matter to the position of expressions inside it.
	quoted bool
	block  bool
	// badKey is set on a key that is a collection.
	badKey bool
}

// yamlResult is the outcome of reading a YAML document.
type yamlResult struct {
	root *rnode // nil for an empty document
	errs []*Error
}

type yamlBuilder struct {
	file  string
	nodes int
	errs  []*Error
	stack map[*yaml.Node]bool
	over  bool
}

// readYAMLDocument parses content with yaml.v3 and builds the tree. Syntax
// errors are returned in errs, with root nil.
func readYAMLDocument(file string, content []byte) (res yamlResult) {
	defer func() {
		if r := recover(); r != nil {
			res = yamlResult{errs: []*Error{{File: file, Raw: fmt.Sprintf("%v", r)}}}
		}
	}()
	dec := yaml.NewDecoder(bytes.NewReader(content))
	var doc yaml.Node
	err := dec.Decode(&doc)
	if errors.Is(err, io.EOF) {
		return yamlResult{}
	}
	if err != nil {
		return yamlResult{errs: []*Error{yamlError(file, err)}}
	}
	var second yaml.Node
	if err := dec.Decode(&second); err == nil {
		e := &Error{File: file, Raw: "Source contains multiple documents; please use YAML.parseAllDocuments()"}
		e.Pos = Pos{second.Line, second.Column}
		e.fileOnly = true
		e.Raw += fmt.Sprintf(" at line %d, column %d", second.Line, second.Column)
		return yamlResult{errs: []*Error{e}}
	} else if !errors.Is(err, io.EOF) {
		return yamlResult{errs: []*Error{yamlError(file, err)}}
	}
	if len(doc.Content) == 0 {
		return yamlResult{}
	}
	b := &yamlBuilder{file: file, stack: map[*yaml.Node]bool{}}
	root := b.build(doc.Content[0], false)
	if b.over {
		return yamlResult{errs: b.errs}
	}
	return yamlResult{root: root, errs: b.errs}
}

var yamlLineRE = regexp.MustCompile(`^(?:yaml: )?(?:unmarshal errors:\n\s*)?(?:line (\d+): )?`)

// yamlError converts an error of yaml.v3 ("yaml: line 3: did not find ...")
// to an Error. The wording is yaml.v3's, not the JavaScript library's, and
// only the line is known.
func yamlError(file string, err error) *Error {
	msg := err.Error()
	e := &Error{File: file, fileOnly: true}
	if m := yamlLineRE.FindStringSubmatch(msg); m != nil {
		if m[1] != "" {
			e.Pos.Line, _ = strconv.Atoi(m[1])
		}
		msg = msg[len(m[0]):]
	}
	e.Raw = msg
	if e.Pos.Line > 0 {
		e.Raw += fmt.Sprintf(" at line %d", e.Pos.Line)
	}
	return e
}

func (b *yamlBuilder) count() bool {
	b.nodes++
	if b.nodes > MaxNodes {
		if !b.over {
			b.over = true
			b.errs = append(b.errs, &Error{File: b.file, Raw: fmt.Sprintf("The document is too large: it has more than %d nodes once aliases are expanded", MaxNodes)})
		}
		return false
	}
	return true
}

func (b *yamlBuilder) build(n *yaml.Node, asKey bool) *rnode {
	if b.over || !b.count() {
		return &rnode{kind: rLiteral, lit: NewNull(Origin{})}
	}
	pos := Pos{n.Line, n.Column}
	o := Origin{File: b.file, Pos: pos}
	switch n.Kind {
	case yaml.AliasNode:
		target := n.Alias
		if target == nil || b.stack[target] {
			b.errs = append(b.errs, &Error{File: b.file, Pos: pos, Raw: fmt.Sprintf("Circular reference through the YAML alias '*%s'", n.Value)})
			return &rnode{kind: rLiteral, pos: pos, lit: NewNull(o)}
		}
		b.stack[target] = true
		r := b.build(target, asKey)
		delete(b.stack, target)
		return r
	case yaml.SequenceNode:
		if asKey {
			return &rnode{kind: rLiteral, pos: pos, lit: NewString(o, ""), badKey: true}
		}
		r := &rnode{kind: rSeq, pos: pos}
		b.stack[n] = true
		for _, c := range n.Content {
			r.items = append(r.items, b.build(c, false))
		}
		delete(b.stack, n)
		return r
	case yaml.MappingNode:
		if asKey {
			return &rnode{kind: rLiteral, pos: pos, lit: NewString(o, ""), badKey: true}
		}
		r := &rnode{kind: rMap, pos: pos}
		b.stack[n] = true
		for i := 0; i+1 < len(n.Content); i += 2 {
			r.keys = append(r.keys, b.build(n.Content[i], true))
			r.vals = append(r.vals, b.build(n.Content[i+1], false))
		}
		delete(b.stack, n)
		return r
	case yaml.ScalarNode:
		r := &rnode{kind: rLiteral, pos: pos}
		if asKey {
			// Keys are always strings, whatever they look like.
			r.lit = NewString(o, n.Value)
		} else {
			r.lit = resolveScalar(o, n)
		}
		r.quoted = n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0
		r.block = n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0
		return r
	}
	return &rnode{kind: rLiteral, pos: pos, lit: NewNull(o)}
}

// resolveScalar gives a scalar its type the way the YAML 1.2 core schema of
// the JavaScript yaml library does: only plain scalars are resolved, and an
// explicit !!tag overrides the guess.
func resolveScalar(o Origin, n *yaml.Node) Token {
	if n.Style&yaml.TaggedStyle != 0 {
		switch n.ShortTag() {
		case "!!str":
			return NewString(o, n.Value)
		case "!!null", "!!bool", "!!int", "!!float":
			if t := resolvePlain(o, n.Value); t.Kind() != KindString {
				return t
			}
			if n.ShortTag() == "!!null" && n.Value == "" {
				return NewNull(o)
			}
		}
		return NewString(o, n.Value)
	}
	if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return NewString(o, n.Value)
	}
	return resolvePlain(o, n.Value)
}

var (
	reInt   = regexp.MustCompile(`^[-+]?[0-9]+$`)
	reOct   = regexp.MustCompile(`^0o[0-7]+$`)
	reHex   = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
	reFloat = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?$`)
	reInf   = regexp.MustCompile(`^[-+]?\.(?:inf|Inf|INF)$`)
	reNaN   = regexp.MustCompile(`^\.(?:nan|NaN|NAN)$`)
)

func resolvePlain(o Origin, s string) Token {
	switch s {
	case "", "~", "null", "Null", "NULL":
		return NewNull(o)
	case "true", "True", "TRUE":
		return NewBoolean(o, true)
	case "false", "False", "FALSE":
		return NewBoolean(o, false)
	}
	switch {
	case reInt.MatchString(s):
		f, _ := strconv.ParseFloat(s, 64)
		return NewNumber(o, f)
	case reOct.MatchString(s):
		return NewNumber(o, parseBase(s[2:], 8))
	case reHex.MatchString(s):
		return NewNumber(o, parseBase(s[2:], 16))
	case reFloat.MatchString(s):
		f, _ := strconv.ParseFloat(s, 64)
		return NewNumber(o, f)
	case reInf.MatchString(s):
		if s[0] == '-' {
			return NewNumber(o, math.Inf(-1))
		}
		return NewNumber(o, math.Inf(1))
	case reNaN.MatchString(s):
		return NewNumber(o, math.NaN())
	}
	return NewString(o, s)
}

// parseBase converts digits in base 8 or 16 to a float64 without overflowing,
// like JavaScript's parseInt.
func parseBase(digits string, base int) float64 {
	var f float64
	for _, c := range strings.ToLower(digits) {
		d := int(c - '0')
		if c >= 'a' {
			d = int(c-'a') + 10
		}
		f = f*float64(base) + float64(d)
	}
	return f
}
