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
	// fault is set in runner mode (Context.RunnerYAML) on a node the runner's
	// YAML reader refuses. The template reader stops with this message when it
	// reaches the node, so the errors found before it in the document are kept.
	fault string
}

// yamlResult is the outcome of reading a YAML document.
type yamlResult struct {
	root *rnode // nil for an empty document
	errs []*Error
	// after holds errors to record once the document is read (runner mode:
	// the runner notices a second document only after reading the first).
	after []*Error
}

type yamlBuilder struct {
	file  string
	nodes int
	errs  []*Error
	stack map[*yaml.Node]bool
	over  bool
	// runnerYAML selects the runner's restrictions, see Context.RunnerYAML.
	runnerYAML bool
}

// readYAMLDocument parses content with yaml.v3 and builds the tree. Syntax
// errors are returned in errs, with root nil.
func readYAMLDocument(file string, content []byte, runnerYAML bool) (res yamlResult) {
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
	var after []*Error
	var second yaml.Node
	if err := dec.Decode(&second); err == nil {
		if runnerYAML {
			// The runner reads the first document, then fails on the event
			// that is not the end of the stream.
			after = []*Error{{File: file, fileOnly: true, Raw: "Expected stream end parse event"}}
		} else {
			e := &Error{File: file, Raw: "Source contains multiple documents; please use YAML.parseAllDocuments()"}
			e.Pos = Pos{second.Line, second.Column}
			e.fileOnly = true
			e.Raw += fmt.Sprintf(" at line %d, column %d", second.Line, second.Column)
			return yamlResult{errs: []*Error{e}}
		}
	} else if !errors.Is(err, io.EOF) {
		return yamlResult{errs: []*Error{yamlError(file, err)}}
	}
	if len(doc.Content) == 0 {
		return yamlResult{}
	}
	b := &yamlBuilder{file: file, stack: map[*yaml.Node]bool{}, runnerYAML: runnerYAML}
	root := b.build(doc.Content[0], false)
	if b.over {
		return yamlResult{errs: b.errs}
	}
	return yamlResult{root: root, errs: b.errs, after: after}
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
	r := b.build1(n, asKey)
	if b.runnerYAML && r.fault == "" && n.Kind != yaml.AliasNode {
		r.fault = runnerYAMLError(n)
	}
	return r
}

func (b *yamlBuilder) build1(n *yaml.Node, asKey bool) *rnode {
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

// runnerYAMLError returns the message of the runner's YAML reader
// (Sdk/DTPipelines/Pipelines/ObjectTemplating/YamlObjectReader.cs) for a node
// it refuses, or "". It checks anchors, and the tag and style of scalars.
func runnerYAMLError(n *yaml.Node) string {
	if n.Anchor != "" {
		return fmt.Sprintf("Anchors are not currently supported. Remove the anchor '%s'", n.Anchor)
	}
	if n.Kind == yaml.ScalarNode && n.Style&(yaml.TaggedStyle|yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
		if msg := runnerIntegerError(n); msg != "" {
			return msg
		}
	}
	if n.Kind != yaml.ScalarNode || n.Style&yaml.TaggedStyle == 0 {
		return ""
	}
	tag := n.LongTag()
	const yamlTag = "tag:yaml.org,2002:"
	// The runner's string tag is "tag:yaml.org,2002:string", which no YAML
	// document spells (!!str is "tag:yaml.org,2002:str").
	if tag == yamlTag+"string" {
		return ""
	}
	plain := n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) == 0
	if !plain {
		style := "SingleQuoted"
		switch {
		case n.Style&yaml.DoubleQuotedStyle != 0:
			style = "DoubleQuoted"
		case n.Style&yaml.LiteralStyle != 0:
			style = "Literal"
		case n.Style&yaml.FoldedStyle != 0:
			style = "Folded"
		}
		return fmt.Sprintf("The scalar style '%s' on line %d and column %d is not valid with the tag '%s'", style, n.Line, n.Column, tag)
	}
	switch tag {
	case yamlTag + "bool", yamlTag + "float", yamlTag + "int", yamlTag + "null":
	default:
		return fmt.Sprintf("Unexpected tag '%s'", tag)
	}
	t := resolvePlain(Origin{}, n.Value)
	ok := false
	switch tag {
	case yamlTag + "bool":
		ok = t.Kind() == KindBoolean
	case yamlTag + "null":
		ok = t.Kind() == KindNull
	case yamlTag + "float":
		ok = t.Kind() == KindNumber
	case yamlTag + "int":
		ok = t.Kind() == KindNumber && (reInt.MatchString(n.Value) || reOct.MatchString(n.Value) || reHex.MatchString(n.Value))
	}
	if !ok {
		return fmt.Sprintf("The value '%s' on line %d and column %d is invalid for the type '%s'", n.Value, n.Line, n.Column, tag)
	}
	return ""
}

// runnerIntegerError is the runner's refusal of a plain 0x or 0o integer that
// does not fit 32 bits (Int32.TryParse with AllowHexSpecifier, which accepts up
// to FFFFFFFF as a negative number, and Convert.ToInt32(s, 8)). The tag is
// empty in the message, as the plain scalar has none.
func runnerIntegerError(n *yaml.Node) string {
	v := n.Value
	var limit uint64 = 1<<32 - 1
	var base int
	switch {
	case reHex.MatchString(v):
		base = 16
	case reOct.MatchString(v):
		base = 8
	default:
		return ""
	}
	var acc uint64
	for _, c := range strings.ToLower(v[2:]) {
		d := uint64(c - '0')
		if c >= 'a' {
			d = uint64(c-'a') + 10
		}
		acc = acc*uint64(base) + d
		if acc > limit {
			return fmt.Sprintf("The value '%s' on line %d and column %d is invalid for the type ''", v, n.Line, n.Column)
		}
	}
	return ""
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
