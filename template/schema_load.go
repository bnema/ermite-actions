// SPDX-License-Identifier: MIT

package template

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	_ "embed"
)

//go:embed schema/workflow-v1.0.min.json
var workflowSchemaJSON []byte

var (
	workflowSchemaOnce sync.Once
	workflowSchema     *Schema
	workflowSchemaErr  error
)

// WorkflowSchema returns the vendored workflow schema (workflow-v1.0). The
// schema is read once and shared; it must not be modified.
func WorkflowSchema() (*Schema, error) {
	workflowSchemaOnce.Do(func() {
		workflowSchema, workflowSchemaErr = ParseSchema(workflowSchemaJSON)
	})
	return workflowSchema, workflowSchemaErr
}

// jnode is a JSON value that keeps the order of object keys.
type jnode struct {
	kind byte // 'o'bject, 'a'rray, 's'tring, 'b'ool, 'n'umber, 'z' null
	keys []string
	vals []*jnode
	str  string
	b    bool
}

func (n *jnode) get(key string) *jnode {
	for i, k := range n.keys {
		if k == key {
			return n.vals[i]
		}
	}
	return nil
}

func readJNode(dec *json.Decoder) (*jnode, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := t.(type) {
	case json.Delim:
		switch v {
		case '{':
			n := &jnode{kind: 'o'}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := readJNode(dec)
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, kt.(string))
				n.vals = append(n.vals, val)
			}
			_, err := dec.Token()
			return n, err
		case '[':
			n := &jnode{kind: 'a'}
			for dec.More() {
				val, err := readJNode(dec)
				if err != nil {
					return nil, err
				}
				n.vals = append(n.vals, val)
			}
			_, err := dec.Token()
			return n, err
		}
	case string:
		return &jnode{kind: 's', str: v}, nil
	case bool:
		return &jnode{kind: 'b', b: v}, nil
	case nil:
		return &jnode{kind: 'z'}, nil
	case json.Number, float64:
		return &jnode{kind: 'n'}, nil
	}
	return nil, errors.New("unexpected JSON token")
}

// ParseSchema reads a schema in the JSON form of workflow-v1.0.min.json: an
// object with "version" and "definitions". The built-in definitions null,
// boolean, number, string, sequence, mapping and any are always present.
func ParseSchema(data []byte) (s *Schema, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(fatal)
			if !ok {
				panic(r)
			}
			s, err = nil, errors.New(f.msg)
		}
	}()
	dec := json.NewDecoder(bytes.NewReader(data))
	root, err := readJNode(dec)
	if err != nil {
		return nil, fmt.Errorf("invalid schema JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("invalid schema JSON: trailing data")
	}
	if root.kind != 'o' {
		fatalf("The schema must be an object")
	}
	s = newBuiltinSchema()
	for i, key := range root.keys {
		val := root.vals[i]
		switch key {
		case "version":
			if val.kind != 's' || val.str == "" {
				fatalf("Unexpected value in template-schema version")
			}
			s.Version = val.str
		case "definitions":
			if val.kind != 'o' {
				fatalf("template-schema definitions must be an object")
			}
			for j, name := range val.keys {
				s.loadDefinition(name, val.vals[j])
			}
		default:
			fatalf("Error while reading 'template-schema key'. Unexpected value '%s'", key)
		}
	}
	s.validate()
	return s, nil
}

func newBuiltinSchema() *Schema {
	s := &Schema{definitions: map[string]*Definition{}}
	for _, d := range []*Definition{
		{Name: "null", Type: DefNull},
		{Name: "boolean", Type: DefBoolean},
		{Name: "number", Type: DefNumber},
		{Name: "string", Type: DefString},
		{Name: "sequence", Type: DefSequence, ItemType: "any"},
		{Name: "mapping", Type: DefMapping, LooseKeyType: "string", LooseValueType: "any"},
		{Name: "any", Type: DefOneOf, OneOf: []string{"null", "boolean", "number", "string", "sequence", "mapping"}},
	} {
		s.definitions[d.Name] = d
	}
	return s
}

func (s *Schema) loadDefinition(name string, n *jnode) {
	if n.kind != 'o' {
		fatalf("Error while reading 'definitions value'. Unexpected value")
	}
	d := &Definition{Name: name}
	var body *jnode
	typed := false
	for i, k := range n.keys {
		v := n.vals[i]
		switch k {
		case "description":
			d.Description = v.str
		case "context":
			seen := map[string]bool{}
			for _, it := range v.vals {
				u := strings.ToUpper(it.str)
				if seen[u] {
					fatalf("Duplicate context item '%s'", it.str)
				}
				seen[u] = true
				d.Context = append(d.Context, it.str)
			}
		case "null", "boolean", "number", "string", "sequence", "mapping", "one-of", "allowed-values":
			if typed {
				continue
			}
			typed = true
			body = v
			d.Type = map[string]DefinitionType{
				"null": DefNull, "boolean": DefBoolean, "number": DefNumber, "string": DefString,
				"sequence": DefSequence, "mapping": DefMapping, "one-of": DefOneOf, "allowed-values": DefOneOf,
			}[k]
			if k == "allowed-values" {
				for _, it := range v.vals {
					key := name + "-" + it.str
					s.definitions[key] = &Definition{Name: key, Type: DefString, Constant: trimDisplay(it.str)}
					d.OneOf = append(d.OneOf, key)
				}
			}
		default:
			fatalf("Error while reading 'definition mapping key'. Unexpected value '%s'", k)
		}
	}
	if !typed {
		fatalf("Not enough information to construct definition '%s'", name)
	}
	switch d.Type {
	case DefString:
		for i, k := range body.keys {
			v := body.vals[i]
			switch k {
			case "constant":
				d.Constant = v.str
			case "ignore-case":
				d.IgnoreCase = v.b
			case "require-non-empty":
				d.RequireNonEmpty = v.b
			case "is-expression":
				d.IsExpression = v.b
			default:
				fatalf("Error while reading 'definition string key'. Unexpected value '%s'", k)
			}
		}
	case DefSequence:
		if t := body.get("item-type"); t != nil {
			d.ItemType = t.str
		}
	case DefMapping:
		d.Properties = map[string]Property{}
		for i, k := range body.keys {
			v := body.vals[i]
			switch k {
			case "properties":
				for j, pn := range v.keys {
					d.PropertyNames = append(d.PropertyNames, pn)
					d.Properties[pn] = loadProperty(v.vals[j])
				}
			case "loose-key-type":
				d.LooseKeyType = v.str
			case "loose-value-type":
				d.LooseValueType = v.str
			default:
				fatalf("Error while reading 'definition mapping key'. Unexpected value '%s'", k)
			}
		}
	case DefOneOf:
		if body != nil && body.kind == 'a' && len(d.OneOf) == 0 {
			for _, it := range body.vals {
				d.OneOf = append(d.OneOf, it.str)
			}
		}
	}
	s.definitions[name] = d
}

func loadProperty(n *jnode) Property {
	if n.kind == 's' {
		return Property{Type: n.str}
	}
	var p Property
	for i, k := range n.keys {
		v := n.vals[i]
		switch k {
		case "type":
			p.Type = v.str
		case "required":
			p.Required = v.b
		case "description":
			p.Description = v.str
		default:
			fatalf("Error while reading 'mapping-property-value key'. Unexpected value '%s'", k)
		}
	}
	return p
}

// validate checks that every reference resolves and the one-of definitions
// are unambiguous, as TemplateSchema.validate does.
func (s *Schema) validate() {
	for name, d := range s.definitions {
		if !validDefinitionName(name) {
			fatalf("Invalid definition name '%s'", name)
		}
		switch d.Type {
		case DefString:
			if d.Constant != "" && d.RequireNonEmpty {
				fatalf("Properties 'constant' and 'require-non-empty' cannot both be set")
			}
		case DefSequence:
			if d.ItemType == "" {
				fatalf("'%s' does not defined 'item-type'", name)
			}
			s.get(d.ItemType)
		case DefMapping:
			switch {
			case d.LooseKeyType != "":
				s.get(d.LooseKeyType)
				if d.LooseValueType == "" {
					fatalf("Property 'loose-key-type' is defined but 'loose-value-type' is not defined on '%s'", name)
				}
				s.get(d.LooseValueType)
			case d.LooseValueType != "":
				fatalf("Property 'loose-value-type' is defined but 'loose-key-type' is not defined on '%s'", name)
			}
			for pn, p := range d.Properties {
				if p.Type == "" {
					fatalf("Type not specified for the property '%s' on '%s'", pn, name)
				}
				s.get(p.Type)
			}
		case DefOneOf:
			if len(d.OneOf) == 0 {
				fatalf("'%s' does not contain any references", name)
			}
			seen := map[string]bool{}
			for _, n := range d.OneOf {
				if seen[n] {
					fatalf("'%s' contains duplicate nested type '%s'", name, n)
				}
				seen[n] = true
				if len(s.get(n).Context) > 0 {
					fatalf("'%s' is a one-of definition and references another definition that defines context. This is currently not supported.", name)
				}
			}
		}
	}
}

func validDefinitionName(n string) bool {
	if n == "" {
		return false
	}
	for i, r := range n {
		switch {
		case r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		case i > 0 && (r == '-' || r >= '0' && r <= '9'):
		default:
			return false
		}
	}
	return true
}
