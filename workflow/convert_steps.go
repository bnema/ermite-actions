// SPDX-License-Identifier: MIT

package workflow

import (
	"strings"

	"bnema.dev/ermite-actions/template"
)

// convertSteps is a port of convertSteps: steps that fail to convert are
// dropped, ids written in the file are checked, and the others get a
// generated id.
func (c *converter) convertSteps(t template.Token) []*Step {
	seq, ok := t.(*template.Sequence)
	if !ok {
		c.ctx.ErrorAt(t, "Invalid format for steps")
		return nil
	}
	ids := newIDBuilder()
	var result []*Step
	for _, it := range seq.Items {
		c.handle(seq, func() {
			if s := c.convertStep(ids, it); s != nil {
				result = append(result, s)
			}
		})
	}
	for _, s := range result {
		if s.ID != "" {
			continue
		}
		id := ""
		if s.Uses != nil {
			id = actionStepID(s.Uses.Value)
		}
		if id == "" {
			id = "run"
		}
		ids.appendSegment("__" + id)
		s.ID = ids.build()
	}
	return result
}

func (c *converter) convertStep(ids *idBuilder, t template.Token) *Step {
	m := asMap(t, "steps item")
	s := &Step{Pos: t.Pos()}
	var uses *template.String
	var run template.Token
	for _, p := range m.Pairs {
		key := asString(p.Key, "steps item key")
		v := p.Value
		switch key.Value {
		case "id":
			id := asString(v, "steps item id")
			if msg := ids.tryAddKnownID(id.Value); msg != "" {
				c.ctx.ErrorAt(id, msg)
			}
			s.ID = id.Value
			s.Explicit = id.Value != ""
		case "name":
			s.Name = asScalar(v, "steps item name")
		case "run":
			run = asScalar(v, "steps item run")
		case "uses":
			uses = asString(v, "steps item uses")
		case "env":
			s.Env = asMap(v, "step env")
		case "if":
			s.If = c.convertIf(v, "step-if")
		case "continue-on-error":
			if !template.IsExpression(v) {
				asBool(v, "steps item continue-on-error")
			} else {
				asScalar(v, "steps item continue-on-error")
			}
			s.ContinueOnError = v
		case "shell":
			s.Shell = v
		case "working-directory":
			s.WorkingDirectory = v
		case "timeout-minutes":
			s.TimeoutMinutes = v
		case "with":
			if wm, ok := v.(*template.Mapping); ok {
				s.With = wm
			}
		}
	}
	switch {
	case run != nil:
		s.Run = run
	case uses != nil:
		s.Uses = uses
	default:
		c.ctx.ErrorAt(t, "Expected one of uses or run to be defined")
		return nil
	}
	if s.If == nil {
		s.If = c.defaultIf("step-if")
	}
	return s
}

// actionStepID is the id segment generated for a uses step without an id.
func actionStepID(uses string) string {
	switch {
	case strings.HasPrefix(uses, "docker://"):
		return uses[len("docker://"):]
	case strings.HasPrefix(uses, "./"), strings.HasPrefix(uses, ".\\"), strings.HasPrefix(uses, "$/"):
		return "self"
	}
	segments := strings.Split(uses, "@")
	if len(segments) != 2 {
		return ""
	}
	path := strings.FieldsFunc(segments[0], func(r rune) bool { return r == '/' || r == '\\' })
	if len(path) >= 2 && path[0] != "" && path[1] != "" && segments[1] != "" {
		return path[0] + "/" + path[1]
	}
	return ""
}
