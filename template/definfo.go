// SPDX-License-Identifier: MIT

package template

import (
	"strconv"
	"strings"

	"bnema.dev/ermite-actions/expr"
)

// DefinitionInfo is a definition together with the contexts and functions
// expressions at that place may use: those of the definition and of every
// definition it was reached through.
type DefinitionInfo struct {
	schema *Schema
	// Definition is the schema definition.
	Definition *Definition

	allowed []string
	named   []string
	funcs   []expr.FuncInfo
}

func newDefinitionInfo(s *Schema, parent *DefinitionInfo, d *Definition) *DefinitionInfo {
	di := &DefinitionInfo{schema: s, Definition: d}
	if len(d.Context) > 0 {
		seen := map[string]bool{}
		if parent != nil {
			for _, c := range parent.allowed {
				di.allowed = append(di.allowed, c)
				seen[strings.ToUpper(c)] = true
			}
		}
		for _, c := range d.Context {
			if u := strings.ToUpper(c); !seen[u] {
				di.allowed = append(di.allowed, c)
				seen[u] = true
			}
		}
	} else if parent != nil {
		di.allowed, di.named, di.funcs = parent.allowed, parent.named, parent.funcs
		return di
	}
	di.named, di.funcs = splitAllowedContext(di.allowed)
	return di
}

// child returns the info of the definition called name, reached through di.
func (di *DefinitionInfo) child(name string) *DefinitionInfo {
	return newDefinitionInfo(di.schema, di, di.schema.get(name))
}

// AllowedContext returns the raw entries: context names and function
// signatures such as "hashFiles(1,255)".
func (di *DefinitionInfo) AllowedContext() []string { return di.allowed }

// NamedContexts returns the names of the contexts expressions may use.
func (di *DefinitionInfo) NamedContexts() []string { return di.named }

// Functions returns the extra functions expressions may call, with their
// arity.
func (di *DefinitionInfo) Functions() []expr.FuncInfo { return di.funcs }

// splitAllowedContext separates "name(min,max)" entries, with max a number or
// MAX, from plain context names.
func splitAllowedContext(allowed []string) ([]string, []expr.FuncInfo) {
	var named []string
	var funcs []expr.FuncInfo
	for _, item := range allowed {
		if f, ok := parseFuncEntry(item); ok {
			funcs = append(funcs, f)
		} else {
			named = append(named, item)
		}
	}
	return named, funcs
}

// parseFuncEntry matches ^([a-zA-Z0-9_]+)\(([0-9]+),([0-9]+|MAX)\)$.
func parseFuncEntry(s string) (expr.FuncInfo, bool) {
	open := strings.IndexByte(s, '(')
	if open <= 0 || !strings.HasSuffix(s, ")") {
		return expr.FuncInfo{}, false
	}
	name := s[:open]
	for _, r := range name {
		if r != '_' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return expr.FuncInfo{}, false
		}
	}
	minS, maxS, ok := strings.Cut(s[open+1:len(s)-1], ",")
	if !ok || !isDigits(minS) || (!isDigits(maxS) && maxS != "MAX") {
		return expr.FuncInfo{}, false
	}
	lo, err := strconv.Atoi(minS)
	if err != nil {
		return expr.FuncInfo{}, false
	}
	hi := -1
	if maxS != "MAX" {
		if hi, err = strconv.Atoi(maxS); err != nil {
			return expr.FuncInfo{}, false
		}
	}
	return expr.FuncInfo{Name: name, MinArgs: lo, MaxArgs: hi}, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Info returns the info of the schema definition called name, with the
// contexts and functions that definition allows, or nil when there is none.
func (s *Schema) Info(name string) *DefinitionInfo {
	d := s.definitions[name]
	if d == nil {
		return nil
	}
	return newDefinitionInfo(s, nil, d)
}
