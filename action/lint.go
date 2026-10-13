// SPDX-License-Identifier: MIT

package action

import (
	"bnema.dev/ermite-actions/expr"
	"bnema.dev/ermite-actions/template"
	"bnema.dev/ermite-actions/workflow"
)

// strictRootType is the language service's definition for an editor.
const strictRootType = "action-root-strict"

// Lint reports what the language service (@actions/workflow-parser
// parseAction and convertActionTemplate, the code behind an editor's
// squiggles) finds wrong in an action.yml, and returns no model.
//
// It reads the file against "action-root-strict": name, description and runs
// are required, "using" must be exactly one of docker, node12, node16, node20,
// node24 or composite, "branding" is checked, and unknown top-level keys such
// as "icon" and "color" and a "plugin" action are refused. GitHub's runner
// accepts several of those; [Parse] says what GitHub does, Lint what a linter
// should flag. The conversion adds the errors of the conditions: pre-if and
// post-if, and the "if" of composite steps, are parsed with the contexts the
// schema grants (a step condition may not use vars or needs here, though the
// runner lets it).
//
// The messages are the language service's: positioned, the expression errors
// without the runner's ". Located at position" suffix being the one
// difference of ours (package expr follows the runner). YAML syntax errors
// have yaml.v3's wording.
func Lint(path string, data []byte) []Error {
	sch, err := Schema()
	if err != nil {
		return []Error{{Raw: "internal error: " + err.Error()}}
	}
	ctx := template.NewContext(sch)
	root := template.ReadYAML(ctx, strictRootType, path, data)
	if !ctx.HasErrors() && root != nil {
		lintConditions(ctx, sch, root)
	}
	return collect(ctx)
}

// lintConditions is validateRunsIfCondition for pre-if and post-if and
// convertToIfCondition for the "if" of every step of "runs.steps".
func lintConditions(ctx *template.Context, sch *template.Schema, root template.Token) {
	m, ok := root.(*template.Mapping)
	if !ok {
		return
	}
	runs, ok := m.Find("runs").(*template.Mapping)
	if !ok {
		return
	}
	runsIf := sch.Info("runs-if")
	stepIf := sch.Info("step-if")
	for _, p := range runs.Pairs {
		switch p.Key.String() {
		case "pre-if", "post-if":
			if s, ok := p.Value.(*template.String); ok {
				if _, err := expr.Parse(s.Value, runsIf.NamedContexts(), runsIf.Functions()); err != nil {
					ctx.ErrorAt(s, err.Error())
				}
			}
		case "steps":
			seq, ok := p.Value.(*template.Sequence)
			if !ok {
				continue
			}
			for _, it := range seq.Items {
				if sm, ok := it.(*template.Mapping); ok {
					lintStepIf(ctx, stepIf, sm.Find("if"))
				}
			}
		}
	}
}

func lintStepIf(ctx *template.Context, def *template.DefinitionInfo, tok template.Token) {
	var condition string
	switch s := tok.(type) {
	case nil:
		return
	case *template.BasicExpression:
		condition = s.Expression
	case *template.String:
		condition = s.Value
	default:
		return
	}
	if isNullOrWhiteSpace(condition) {
		return
	}
	// Condition text is trimmed for a string; an expression is used as it is.
	if _, isString := tok.(*template.String); isString {
		condition = trimSpaceJS(condition)
	}
	final := condition
	if node, err := expr.Parse(condition, def.NamedContexts(), def.Functions()); err == nil && !workflow.HasStatusFunction(node) {
		final = "success() && (" + condition + ")"
	}
	if _, err := expr.Parse(final, def.NamedContexts(), def.Functions()); err != nil {
		ctx.ErrorAt(tok, err.Error())
	}
}
