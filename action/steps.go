// SPDX-License-Identifier: MIT

package action

import (
	"strings"

	"bnema.dev/ermite-actions/expr"
	"bnema.dev/ermite-actions/template"
	"bnema.dev/ermite-actions/workflow"
)

// convertSteps is the runner's PipelineTemplateConverter.ConvertToSteps
// (Sdk/DTPipelines/Pipelines/ObjectTemplating/PipelineTemplateConverter.cs).
// It is not the conversion of package workflow, which reads the steps of a
// workflow file: this one throws at the first token of the wrong type, keeps
// a step whose "uses" has the wrong form (and then fails on it later if it has
// no id), accepts ids that start with "__", and names a "$/" action
// "selfRepository". See the runner for each of those.
func (l *loader) convertSteps(items *template.Sequence) []*workflow.Step {
	names := newRefNames()
	var result []*workflow.Step
	var refs []*stepRef
	for _, it := range items.Items {
		s, ref := l.convertStep(it, names)
		if s != nil {
			result = append(result, s)
			refs = append(refs, ref)
		}
	}
	for i, s := range result {
		if s.ID != "" {
			continue
		}
		ref := refs[i]
		if ref == nil {
			// The runner dereferences the Reference of a step whose "uses" had
			// the wrong form.
			throwf(exNullRef, "Object reference not set to an instance of an object.")
		}
		name := ref.name
		if name == "" {
			name = "run"
		}
		names.appendSegment("__" + name)
		s.ID = names.build()
	}
	return result
}

// stepRef is what the runner makes of a step's action reference that decides
// the generated id: the image of a docker:// action, "owner/repo", "self" for
// a local action, "selfRepository" for a $/ action, "" for a run step.
type stepRef struct{ name string }

func (l *loader) convertStep(t template.Token, names *refNames) (*workflow.Step, *stepRef) {
	m := assertMapping(t, "steps item")
	step := &workflow.Step{Pos: t.Pos()}
	var run template.Token
	var ifToken template.Token
	var uses *template.String
	for _, p := range m.Pairs {
		key := assertString(p.Key, "steps item key")
		v := p.Value
		switch key.Value {
		case "continue-on-error":
			if !template.IsExpression(v) {
				assertBoolean(v, "step continue-on-error")
			}
			step.ContinueOnError = assertScalar(v, "steps continue-on-error")
		case "env":
			if !template.IsExpression(v) {
				for _, kv := range assertMapping(v, "environment").Pairs {
					if template.IsExpression(kv.Key) {
						continue
					}
					assertString(kv.Key, "environment key")
					if template.IsExpression(kv.Value) {
						continue
					}
					assertString(kv.Value, "environment value")
				}
			}
			if em, ok := v.(*template.Mapping); ok {
				step.Env = em
			} else {
				step.EnvExpression = v
			}
		case "id":
			id := assertString(v, "steps item id")
			if id.Value != "" {
				if msg := names.tryAddKnownName(id.Value); msg != "" {
					l.ctx.ErrorAt(id, msg)
				}
			}
			step.ID, step.Explicit = id.Value, id.Value != ""
		case "if":
			ifToken = assertScalar(v, "steps item if")
		case "name":
			step.Name = assertScalar(v, "steps item name")
		case "run":
			run = assertScalar(v, "steps item run")
		case "shell":
			step.Shell = assertScalar(v, "steps item shell")
		case "timeout-minutes":
			if !template.IsExpression(v) {
				if _, ok := v.(*template.Number); !ok {
					assertFail(v, "step timeout-minutes", "NumberToken")
				}
			}
			step.TimeoutMinutes = assertScalar(v, "steps item timeout-minutes")
		case "uses":
			uses = assertString(v, "steps item uses")
		case "with":
			if !template.IsExpression(v) {
				for _, kv := range assertMapping(v, "inputs").Pairs {
					if template.IsExpression(kv.Key) {
						continue
					}
					assertString(kv.Key, "inputs key")
					if template.IsExpression(kv.Value) {
						continue
					}
					assertString(kv.Value, "inputs value")
				}
			}
			if wm, ok := v.(*template.Mapping); ok {
				step.With = wm
			} else {
				step.WithExpression = v
			}
		case "working-directory":
			step.WorkingDirectory = assertScalar(v, "steps item working-directory")
		case "clean", "fetch-depth", "lfs", "path", "submodules":
			assertScalar(v, "steps item "+key.Value)
		default:
			throwf(exArgument, "Error while reading 'steps item key'. Unexpected value '%s'", key.Value)
		}
	}
	step.If = l.convertIf(ifToken)

	if run != nil {
		step.Run = run
		return step, &stepRef{}
	}
	if uses == nil {
		assertFail(nil, "steps item uses", "StringToken")
	}
	step.Uses = uses
	u, err := ParseUses(uses.Value)
	if err != nil {
		l.ctx.ErrorAt(uses, err.Error())
		if strings.HasPrefix(uses.Value, "$/") {
			// A $/ value with a ref drops the step.
			return nil, nil
		}
		return step, nil
	}
	ref := &stepRef{name: "self"}
	switch u.Kind {
	case UsesDocker:
		ref.name = u.Image
	case UsesRepository:
		ref.name = u.Name
	case UsesSelfRepository:
		ref.name = "selfRepository"
	}
	return step, ref
}

// stepIfContexts are the contexts the runner's converter grants the "if" of a
// step on top of the ones the schema's step-if gives (which lacks vars and
// needs): s_stepNamedValues.
var stepIfContexts = []string{"vars", "needs"}

// convertIf is ConvertToIfCondition for a step: a condition without a status
// function is wrapped as "success() && (cond)", an empty one is "success()".
func (l *loader) convertIf(tok template.Token) *template.BasicExpression {
	def := l.schema.Info("step-if").WithContexts(stepIfContexts...)
	var condition string
	switch s := tok.(type) {
	case nil:
	case *template.BasicExpression:
		condition = s.Expression
	default:
		condition = assertString(tok, "step if").Value
	}
	final := "success()"
	if !isNullOrWhiteSpace(condition) {
		node, err := expr.Parse(condition, def.NamedContexts(), def.Functions())
		if err != nil {
			l.ctx.ErrorAt(tok, err.Error())
			return nil
		}
		final = condition
		if node != nil && !workflow.HasStatusFunction(node) {
			final = "success() && (" + condition + ")"
		}
	}
	var o template.Origin
	if tok != nil {
		o = template.Origin{File: tok.File(), Pos: tok.Pos()}
	}
	return template.NewExpression(o, final, def)
}
