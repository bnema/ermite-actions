// SPDX-License-Identifier: MIT

package template

import (
	"fmt"
	"sort"
	"strings"
)

// ReadYAML parses a YAML document and reads it as a template of the schema
// definition rootType (for workflows, "workflow-root-strict"). Problems
// found in the document, such as syntax errors, unexpected keys or
// misplaced expressions, are recorded in ctx; the returned token holds what
// could be read and is nil when the document is not valid YAML.
//
// An empty document is an error, as in the reference. A document larger than
// [MaxFileBytes] is refused before it is parsed, with one error about the
// file.
func ReadYAML(ctx *Context, rootType, file string, content []byte) (root Token) {
	if len(content) > MaxFileBytes {
		e := &Error{File: file, fileOnly: true, Raw: fmt.Sprintf("The file is too large: %d bytes, the limit is %d bytes", len(content), MaxFileBytes)}
		ctx.add(e)
		return nil
	}
	res := readYAMLDocument(file, content)
	for _, e := range res.errs {
		ctx.add(e)
	}
	if len(res.errs) > 0 {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(fatal)
			if !ok {
				panic(r)
			}
			ctx.ErrorInFile(file, f.msg)
		}
	}()
	rd := &reader{ctx: ctx, schema: ctx.schema, file: file}
	if res.root == nil {
		fatalf("Expected a scalar value, a sequence, or a mapping")
	}
	def := newDefinitionInfo(ctx.schema, nil, ctx.schema.get(rootType))
	return rd.readValue(res.root, def)
}

type reader struct {
	ctx    *Context
	schema *Schema
	file   string
	// noExpressions is set when reading the result of an evaluation, whose
	// strings are text even if they contain ${{.
	noExpressions bool
}

func (r *reader) origin(n *rnode) Origin { return Origin{File: r.file, Pos: n.pos} }

func (r *reader) readValue(n *rnode, def *DefinitionInfo) Token {
	switch n.kind {
	case rLiteral:
		scalar := r.parseScalar(n, n.lit, def)
		return r.validate(scalar, def)
	case rSeq:
		seq := NewSequence(r.origin(n))
		defs := def.schema.definitionsOfType(def.Definition, DefSequence)
		if len(defs) > 0 {
			itemDef := def.child(defs[0].ItemType)
			for _, it := range n.items {
				seq.Items = append(seq.Items, r.readValue(it, itemDef))
			}
		} else {
			r.ctx.ErrorAt(seq, "A sequence was not expected")
		}
		seq.def = def
		return seq
	}
	m := NewMapping(r.origin(n))
	defs := def.schema.definitionsOfType(def.Definition, DefMapping)
	if len(defs) > 0 {
		if len(defs) > 1 || len(defs[0].Properties) > 0 || defs[0].LooseKeyType == "" {
			r.mappingWithProperties(n, m, def, defs)
		} else {
			keyDef := def.child(defs[0].LooseKeyType)
			valDef := def.child(defs[0].LooseValueType)
			r.mappingAllLoose(n, m, def, keyDef, valDef, defs[0])
		}
	} else {
		r.ctx.ErrorAt(m, "A mapping was not expected")
	}
	if m.def == nil {
		m.def = def
	}
	return m
}

// key reads the key of a mapping entry; a key that is not a scalar is an
// error and reads as an empty string.
func (r *reader) key(n *rnode, i int, def *DefinitionInfo) Token {
	k := n.keys[i]
	if k.badKey {
		r.ctx.ErrorAt(NewString(r.origin(k), ""), "Unexpected value ''")
	}
	return r.parseScalar(k, k.lit, def)
}

func (r *reader) mappingWithProperties(n *rnode, m *Mapping, def *DefinitionInfo, defs []*Definition) {
	var looseKey, looseVal string
	if defs[0].LooseKeyType != "" {
		looseKey, looseVal = defs[0].LooseKeyType, defs[0].LooseValueType
	}
	var looseKeyDef, looseValDef *DefinitionInfo
	upperKeys := map[string]bool{}
	hasExpressionKey := false
	for i := range n.keys {
		keyScalar := r.key(n, i, def)
		val := n.vals[i]
		if IsExpression(keyScalar) {
			hasExpressionKey = true
			if len(def.allowed) > 0 {
				m.Add(keyScalar, r.readValue(val, def.child("any")))
			} else {
				r.ctx.ErrorAt(keyScalar, "A template expression is not allowed in this context")
			}
			continue
		}
		nextKey, ok := keyScalar.(*String)
		if !ok {
			nextKey = &String{tokenBase: *keyScalar.base(), Value: keyScalar.String()}
		}
		if nextKey.Value != "" {
			up := strings.ToUpper(nextKey.Value)
			if upperKeys[up] {
				r.ctx.Errorf(nextKey, "'%s' is already defined", nextKey.Value)
				continue
			}
			upperKeys[up] = true
		}
		var prop *Property
		prop, defs = matchPropertyAndFilter(defs, nextKey.Value)
		if prop != nil {
			if backgroundStepProperties[nextKey.Value] {
				r.ctx.Errorf(nextKey, "Unexpected value '%s'", nextKey.Value)
				continue
			}
			nd := def.child(prop.Type)
			nextKey.def = nd
			m.Add(nextKey, r.readValue(val, nd))
			continue
		}
		if looseKey != "" {
			if looseKeyDef == nil {
				looseKeyDef = def.child(looseKey)
				looseValDef = def.child(looseVal)
			}
			r.validate(nextKey, looseKeyDef)
			nextKey.def = def.child(defs[0].LooseValueType)
			m.Add(nextKey, r.readValue(val, looseValDef))
			continue
		}
		r.ctx.Errorf(nextKey, "Unexpected value '%s'", nextKey.Value)
	}
	if len(defs) == 1 {
		m.def = newDefinitionInfo(def.schema, def, defs[0])
	}
	switch {
	case len(defs) > 1:
		hits := map[string]int{}
		for _, d := range defs {
			for _, k := range d.PropertyNames {
				hits[k]++
			}
		}
		var props []string
		for k, c := range hits {
			if c == 1 && !backgroundStepOnlyProperties[k] {
				props = append(props, k)
			}
		}
		sort.Strings(props)
		r.ctx.Errorf(m, "There's not enough info to determine what you meant. Add one of these properties: %s", strings.Join(props, ", "))
	case len(defs) == 1 && !hasExpressionKey:
		for _, name := range defs[0].PropertyNames {
			if defs[0].Properties[name].Required && !upperKeys[strings.ToUpper(name)] {
				r.ctx.Errorf(m, "Required property is missing: %s", name)
			}
		}
	}
}

var (
	backgroundStepProperties     = map[string]bool{"background": true, "wait": true, "wait-all": true, "cancel": true}
	backgroundStepOnlyProperties = map[string]bool{"wait": true, "wait-all": true, "cancel": true}
)

func (r *reader) mappingAllLoose(n *rnode, m *Mapping, def, keyDef, valDef *DefinitionInfo, md *Definition) {
	upperKeys := map[string]bool{}
	for i := range n.keys {
		keyScalar := r.key(n, i, def)
		keyScalar.base().def = keyDef
		val := n.vals[i]
		if IsExpression(keyScalar) {
			if len(def.allowed) > 0 {
				m.Add(keyScalar, r.readValue(val, valDef))
			} else {
				r.ctx.ErrorAt(keyScalar, "A template expression is not allowed in this context")
			}
			continue
		}
		nextKey, ok := keyScalar.(*String)
		if !ok {
			nextKey = &String{tokenBase: *keyScalar.base(), Value: keyScalar.String()}
		}
		if nextKey.Value != "" {
			up := strings.ToUpper(nextKey.Value)
			if upperKeys[up] {
				r.ctx.Errorf(nextKey, "'%s' is already defined", nextKey.Value)
				continue
			}
			upperKeys[up] = true
		}
		r.validate(nextKey, keyDef)
		nextKey.def = def.child(md.LooseValueType)
		m.Add(nextKey, r.readValue(val, valDef))
	}
}

// validate checks a scalar against the scalar definitions of def. A literal
// that is not a string may be converted to one.
func (r *reader) validate(scalar Token, def *DefinitionInfo) Token {
	switch scalar.Kind() {
	case KindNull, KindBoolean, KindNumber, KindString:
		for _, d := range def.schema.scalarDefinitions(def.Definition) {
			if d.isMatch(scalar) {
				scalar.base().def = newDefinitionInfo(def.schema, def, d)
				return scalar
			}
		}
		if scalar.Kind() != KindString {
			sl := &String{tokenBase: *scalar.base(), Value: scalar.String()}
			for _, d := range def.schema.scalarDefinitions(def.Definition) {
				if d.isMatch(sl) {
					sl.def = newDefinitionInfo(def.schema, def, d)
					return sl
				}
			}
		}
		r.ctx.ErrorAt(scalar, fmt.Sprintf("Unexpected value '%s'", scalar.String()))
	case KindBasicExpression:
		if len(def.allowed) == 0 {
			r.ctx.ErrorAt(scalar, "A template expression is not allowed in this context")
		}
	default:
		r.ctx.ErrorAt(scalar, fmt.Sprintf("Unexpected value '%s'", scalar.String()))
	}
	return scalar
}
