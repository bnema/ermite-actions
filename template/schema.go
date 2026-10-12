// SPDX-License-Identifier: MIT

package template

import (
	"fmt"
	"strings"
)

// DefinitionType is the type of a schema [Definition].
type DefinitionType int

// The definition types.
const (
	DefNull DefinitionType = iota
	DefBoolean
	DefNumber
	DefString
	DefSequence
	DefMapping
	DefOneOf
)

// Property is a well-known key of a mapping definition.
type Property struct {
	// Type is the name of the definition of the value.
	Type        string
	Required    bool
	Description string
}

// Definition is a named type of the schema. Which fields are meaningful
// depends on Type.
type Definition struct {
	Name        string
	Type        DefinitionType
	Description string
	// Context lists the named contexts and functions ("github",
	// "success(0,MAX)") expressions in values of this type may use.
	Context []string

	// String definitions.
	Constant        string
	IgnoreCase      bool
	RequireNonEmpty bool
	IsExpression    bool

	// Sequence definitions.
	ItemType string

	// Mapping definitions: Properties in schema order.
	PropertyNames  []string
	Properties     map[string]Property
	LooseKeyType   string
	LooseValueType string

	// One-of definitions: the names of the alternatives.
	OneOf []string
}

// Schema is a set of named definitions.
type Schema struct {
	Version     string
	definitions map[string]*Definition
}

// Definition returns the definition called name, or nil.
func (s *Schema) Definition(name string) *Definition { return s.definitions[name] }

// get is the reference's getDefinition, which throws when the name is unknown.
func (s *Schema) get(name string) *Definition {
	d := s.definitions[name]
	if d == nil {
		fatalf("Schema definition '%s' not found", name)
	}
	return d
}

// scalarDefinitions expands one-of definitions and returns all scalar ones.
func (s *Schema) scalarDefinitions(d *Definition) []*Definition {
	switch d.Type {
	case DefNull, DefBoolean, DefNumber, DefString:
		return []*Definition{d}
	case DefOneOf:
		var out []*Definition
		for _, n := range d.OneOf {
			out = append(out, s.scalarDefinitions(s.get(n))...)
		}
		return out
	}
	return nil
}

// definitionsOfType expands a one-of definition one level and returns the
// alternatives of type t.
func (s *Schema) definitionsOfType(d *Definition, t DefinitionType) []*Definition {
	if d.Type == t {
		return []*Definition{d}
	}
	var out []*Definition
	if d.Type == DefOneOf {
		for _, n := range d.OneOf {
			if nd := s.get(n); nd.Type == t {
				out = append(out, nd)
			}
		}
	}
	return out
}

// isMatch reports whether a literal token is accepted by a scalar definition.
func (d *Definition) isMatch(t Token) bool {
	switch d.Type {
	case DefNull:
		return t.Kind() == KindNull
	case DefBoolean:
		return t.Kind() == KindBoolean
	case DefNumber:
		return t.Kind() == KindNumber
	case DefString:
		s, ok := t.(*String)
		if !ok {
			return false
		}
		switch {
		case d.Constant != "":
			if d.IgnoreCase {
				return strings.EqualFold(d.Constant, s.Value)
			}
			return d.Constant == s.Value
		case d.RequireNonEmpty:
			return s.Value != ""
		}
		return true
	}
	return false
}

// matchPropertyAndFilter looks for a property called name in any of defs; when
// found in some only, defs is narrowed to those. It returns the property, if
// any, and the possibly narrowed list.
func matchPropertyAndFilter(defs []*Definition, name string) (*Property, []*Definition) {
	var result *Property
	notFoundInSome := false
	for _, d := range defs {
		if p, ok := d.Properties[name]; ok {
			p := p
			result = &p
		} else {
			notFoundInSome = true
		}
	}
	if result != nil && notFoundInSome {
		kept := defs[:0:0]
		for _, d := range defs {
			if _, ok := d.Properties[name]; ok {
				kept = append(kept, d)
			}
		}
		defs = kept
	}
	return result, defs
}

// fatal is the panic value used for errors that abort a read, which the
// reference throws as exceptions.
type fatal struct{ msg string }

func fatalf(format string, a ...any) { panic(fatal{fmt.Sprintf(format, a...)}) }
