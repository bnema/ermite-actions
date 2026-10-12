// SPDX-License-Identifier: MIT

package workflow

import (
	"context"
	"strings"

	"bnema.dev/ermite-actions/expr"
	"bnema.dev/ermite-actions/template"
)

// EvaluateStrategy evaluates the job's strategy token against env and
// expands its matrix. env must provide the contexts the schema allows in
// "strategy": github, inputs, vars and needs. A job without a strategy gives
// a Strategy with one empty combination.
func (j *Job) EvaluateStrategy(ctx context.Context, env template.Env) (*Strategy, error) {
	if j.Strategy == nil {
		return ParseStrategy(nil)
	}
	v, err := template.Evaluate(ctx, j.Strategy, env)
	if err != nil {
		return nil, err
	}
	return ParseStrategy(v)
}

// MatrixEnv returns env with the "matrix" and "strategy" contexts of one
// combination added, for evaluating the parts of a job that may use them:
// runs-on, name, env, container, services, steps...
func (s *Strategy) MatrixEnv(env template.Env, index int) template.Env {
	out := env
	out.Contexts = make(map[string]expr.Value, len(env.Contexts)+2)
	for k, v := range env.Contexts {
		out.Contexts[k] = v
	}
	out.Contexts["matrix"] = s.Combos[index].Values
	out.Contexts["strategy"] = s.Context(index)
	return out
}

// DisplayName evaluates the job's name for combination index of its
// strategy: the name token (or the job id) with the matrix values added in
// parentheses when the name does not use the matrix context. env must
// already include the matrix and strategy contexts, see [Strategy.MatrixEnv].
//
// TODO(verify with recording): GitHub adds the values to the name unless the
// name is set and refers to the matrix; here a name is considered to refer to
// the matrix when its expression text contains "matrix.".
func (j *Job) DisplayName(ctx context.Context, env template.Env, s *Strategy, index int) (string, error) {
	name, err := template.EvaluateString(ctx, j.Name, env)
	if err != nil {
		return "", err
	}
	if !s.HasMatrix {
		return name, nil
	}
	if j.HasName && usesMatrix(j.Name) {
		return name, nil
	}
	return s.Combos[index].DisplayName(name), nil
}

func usesMatrix(t template.Token) bool {
	found := false
	template.Walk(t, func(x template.Token) bool {
		if be, ok := x.(*template.BasicExpression); ok && containsFold(be.Expression, "matrix.") {
			found = true
		}
		return !found
	})
	return found
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), sub)
}
