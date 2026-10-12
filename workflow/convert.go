// SPDX-License-Identifier: MIT

package workflow

import (
	"bnema.dev/ermite-actions/template"
)

// converter turns the token tree of a workflow into the model. It is a port
// of the reference's converter, which reports problems by throwing an error
// that the caller of each conversion step catches and records; here panics of
// type *template.TokenError play the part of those exceptions.
type converter struct {
	ctx    *template.Context
	schema *template.Schema
	w      *Workflow
	jobIDs *idBuilder
}

// handle runs fn and records a *template.TokenError panic as an error
// located at the token it names, like handleTemplateTokenErrors.
func (c *converter) handle(root template.Token, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			te, ok := r.(*template.TokenError)
			if !ok {
				panic(r)
			}
			if te.Token != nil {
				c.ctx.ErrorAt(te.Token, te.Message)
			} else {
				c.ctx.ErrorAt(root, te.Message)
			}
		}
	}()
	fn()
}

func throw(t template.Token, format string, a ...any) {
	panic(&template.TokenError{Token: t, Message: sprintf(format, a...)})
}

func ck[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func asString(t template.Token, desc string) *template.String {
	return ck(template.AssertString(t, desc))
}

func asBool(t template.Token, desc string) *template.Boolean {
	return ck(template.AssertBoolean(t, desc))
}

func asSeq(t template.Token, desc string) *template.Sequence {
	return ck(template.AssertSequence(t, desc))
}

func asMap(t template.Token, desc string) *template.Mapping {
	return ck(template.AssertMapping(t, desc))
}

func asScalar(t template.Token, desc string) template.Token {
	return ck(template.AssertScalar(t, desc))
}

func origin(t template.Token) template.Origin {
	return template.Origin{File: t.File(), Pos: t.Pos()}
}

// convertRoot converts the root mapping.
func (c *converter) convertRoot(root template.Token) {
	defer func() {
		if r := recover(); r != nil {
			te, ok := r.(*template.TokenError)
			if !ok {
				panic(r)
			}
			if te.Token != nil {
				c.ctx.ErrorAt(te.Token, te.Message)
			} else {
				c.ctx.ErrorAt(root, te.Message)
			}
		}
	}()
	m := asMap(root, "root")
	for _, p := range m.Pairs {
		key := asString(p.Key, "root key")
		switch key.Value {
		case "on":
			c.handle(root, func() { c.w.On = c.convertOn(p.Value) })
		case "jobs":
			c.w.Jobs = nil
			c.handle(root, func() { c.w.Jobs = c.convertJobs(p.Value) })
		case "concurrency":
			c.handle(root, func() { c.convertConcurrency(p.Value) })
			c.w.Concurrency = p.Value
		case "env":
			if em, ok := p.Value.(*template.Mapping); ok {
				c.w.Env = em
			}
		case "name":
			if s, ok := p.Value.(*template.String); ok {
				c.w.Name = s.Value
			}
		case "run-name":
			c.w.RunName = p.Value
		case "defaults":
			c.w.Defaults = convertDefaults(p.Value)
		case "permissions":
			c.w.Permissions = convertPermissions(p.Value)
		}
	}
}

func convertDefaults(t template.Token) *Defaults {
	m, ok := t.(*template.Mapping)
	if !ok {
		return nil
	}
	d := &Defaults{}
	if run, ok := m.Find("run").(*template.Mapping); ok {
		d.Run = &RunDefaults{Shell: run.Find("shell"), WorkingDirectory: run.Find("working-directory")}
	}
	return d
}

func convertPermissions(t template.Token) *Permissions {
	switch t := t.(type) {
	case *template.String:
		return &Permissions{All: t.Value}
	case *template.Mapping:
		p := &Permissions{}
		for _, kv := range t.Pairs {
			p.Scopes = append(p.Scopes, PermissionScope{Scope: kv.Key.String(), Level: kv.Value.String()})
		}
		return p
	}
	return nil
}
