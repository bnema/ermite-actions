// SPDX-License-Identifier: MIT

package workflow

import (
	"strings"

	"bnema.dev/ermite-actions/expr"
	"bnema.dev/ermite-actions/template"
)

// convertConcurrency validates a concurrency token. Expressions are skipped.
func (c *converter) convertConcurrency(t template.Token) {
	if template.IsExpression(t) {
		return
	}
	if _, ok := t.(*template.String); ok {
		return
	}
	m := asMap(t, "concurrency group")
	for _, p := range m.Pairs {
		name := asString(p.Key, "concurrency group key")
		if template.IsExpression(p.Key) || template.IsExpression(p.Value) {
			continue
		}
		switch name.Value {
		case "group":
			asString(p.Value, "concurrency group")
		case "cancel-in-progress":
			asBool(p.Value, "cancel-in-progress")
		case "queue":
			asString(p.Value, "queue")
		default:
			c.ctx.Errorf(name, "Invalid property name: %s", name.Value)
		}
	}
}

// convertEnvironment validates a job environment token.
func (c *converter) convertEnvironment(t template.Token) {
	if template.IsExpression(t) || template.IsScalar(t) {
		return
	}
	m := asMap(t, "job environment")
	for _, p := range m.Pairs {
		name := asString(p.Key, "job environment key")
		if template.IsExpression(p.Key) || template.IsExpression(p.Value) {
			continue
		}
		switch name.Value {
		case "name":
			asScalar(p.Value, "job environment name key")
		case "deployment":
			asBool(p.Value, "job environment deployment")
		}
	}
}

// convertRunsOn validates a runs-on token.
func (c *converter) convertRunsOn(t template.Token) {
	labels := runsOnLabels(t)
	_ = labels
	m, ok := t.(*template.Mapping)
	if !ok {
		return
	}
	for _, p := range m.Pairs {
		key := asString(p.Key, "job runs-on property name")
		switch key.Value {
		case "group":
			if template.IsExpression(p.Value) {
				continue
			}
			group := asString(p.Value, "job runs-on group name").Value
			names := strings.Split(group, "/")
			switch len(names) {
			case 1:
			case 2:
				switch names[0] {
				case "org", "organization", "ent", "enterprise":
				default:
					c.ctx.Errorf(p.Value, "Invalid runs-on group name '%s. Please use 'organization/' or 'enterprise/' prefix to target a single runner group.'", group)
					continue
				}
				if names[1] == "" {
					c.ctx.Errorf(p.Value, "Invalid runs-on group name '%s'.", group)
				}
			default:
				c.ctx.Errorf(p.Value, "Invalid runs-on group name '%s. Please use 'organization/' or 'enterprise/' prefix to target a single runner group.'", group)
			}
		case "labels":
			runsOnLabels(p.Value)
		}
	}
}

// runsOnLabels collects the literal labels of a runs-on token, asserting that
// the items are strings.
func runsOnLabels(t template.Token) []string {
	if template.IsExpression(t) {
		return nil
	}
	switch t := t.(type) {
	case *template.String:
		return []string{t.Value}
	case *template.Sequence:
		var out []string
		for _, it := range t.Items {
			if template.IsExpression(it) {
				continue
			}
			out = append(out, asString(it, "job runs-on label sequence item").Value)
		}
		return out
	}
	return nil
}

// statusFunctions are the functions that make a condition a status check.
var statusFunctions = map[string]bool{"success": true, "failure": true, "cancelled": true, "always": true}

func hasStatusFunction(n expr.Node) bool {
	switch n := n.(type) {
	case *expr.FunctionCall:
		if statusFunctions[strings.ToLower(n.Name.Lexeme)] {
			return true
		}
		for _, a := range n.Args {
			if hasStatusFunction(a) {
				return true
			}
		}
	case *expr.Binary:
		return hasStatusFunction(n.Left) || hasStatusFunction(n.Right)
	case *expr.Unary:
		return hasStatusFunction(n.Expr)
	case *expr.Logical:
		for _, a := range n.Args {
			if hasStatusFunction(a) {
				return true
			}
		}
	case *expr.Grouping:
		return hasStatusFunction(n.Group)
	case *expr.IndexAccess:
		return hasStatusFunction(n.Expr) || hasStatusFunction(n.Index)
	}
	return false
}

// convertIf converts an if condition: the result always contains a status
// function, wrapping the condition as "success() && (cond)" when it has none.
// A condition that does not parse is recorded as an error and returns nil.
func (c *converter) convertIf(t template.Token, defName string) *template.BasicExpression {
	scalar := asScalar(t, "if condition")
	def := t.Definition()
	if def == nil {
		def = c.schema.Info(defName)
	}
	var condition string
	switch s := scalar.(type) {
	case *template.BasicExpression:
		condition = s.Expression
	default:
		condition = strings.TrimSpace(asString(scalar, "if condition").Value)
	}
	final := "success()"
	if condition != "" {
		final = condition
		if node, err := expr.Parse(condition, def.NamedContexts(), def.Functions()); err == nil && !hasStatusFunction(node) {
			final = "success() && (" + condition + ")"
		}
	}
	if _, err := expr.Parse(final, def.NamedContexts(), def.Functions()); err != nil {
		c.ctx.ErrorAt(t, err.Error())
		return nil
	}
	return template.NewExpression(origin(t), final, def)
}

// defaultIf is the condition of a job or step that has none.
func (c *converter) defaultIf(defName string) *template.BasicExpression {
	return template.NewExpression(template.Origin{}, "success()", c.schema.Info(defName))
}
