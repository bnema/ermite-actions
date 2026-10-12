// SPDX-License-Identifier: MIT

package workflow

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"bnema.dev/ermite-actions/expr"
	"bnema.dev/ermite-actions/template"
)

// Strategy is an evaluated "strategy" key.
type Strategy struct {
	// FailFast is true unless the file says otherwise.
	FailFast bool
	// MaxParallel is the limit of concurrent jobs, 0 for no limit.
	MaxParallel int
	// HasMatrix reports whether the strategy has a matrix. Without one,
	// Combos holds a single empty combination: the job runs once.
	HasMatrix bool
	// Combos are the jobs, in the order GitHub creates them.
	Combos []MatrixCombo
}

// ParseStrategy reads an evaluated strategy value and expands its matrix,
// see [ExpandMatrix] for the rules and limits.
func ParseStrategy(ctx context.Context, v expr.Value) (*Strategy, error) {
	s := &Strategy{FailFast: true}
	var matrix expr.Value
	switch d := v.(type) {
	case nil, expr.Null:
	case *expr.Dictionary:
		if d == nil {
			break
		}
		for _, p := range d.Pairs() {
			switch strings.ToLower(p.Key) {
			case "fail-fast":
				b, err := strategyBool(p.Value)
				if err != nil {
					return nil, fmt.Errorf("unexpected value for 'fail-fast': %w", err)
				}
				s.FailFast = b
			case "max-parallel":
				n, err := strategyInt(p.Value)
				if err != nil {
					return nil, fmt.Errorf("unexpected value for 'max-parallel': %w", err)
				}
				s.MaxParallel = n
			case "matrix":
				matrix = p.Value
			}
		}
	default:
		return nil, errors.New("the strategy must be a mapping")
	}
	combos, has, err := buildMatrix(ctx, matrix)
	if err != nil {
		return nil, err
	}
	s.Combos, s.HasMatrix = combos, has
	return s, nil
}

// Context returns the "strategy" context of job number index (from 0).
func (s *Strategy) Context(index int) *expr.Dictionary {
	d := expr.NewDictionary()
	d.Add("fail-fast", expr.Boolean(s.FailFast))
	d.Add("job-index", expr.Number(index))
	d.Add("job-total", expr.Number(len(s.Combos)))
	max := s.MaxParallel
	if max == 0 {
		max = len(s.Combos)
	}
	d.Add("max-parallel", expr.Number(max))
	return d
}

func strategyBool(v expr.Value) (bool, error) {
	switch x := v.(type) {
	case expr.Boolean:
		return bool(x), nil
	case expr.String:
		switch strings.ToLower(string(x)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, fmt.Errorf("expected a boolean but got %s", kindName(v))
}

func strategyInt(v expr.Value) (int, error) {
	var f float64
	switch x := v.(type) {
	case expr.Number:
		f = float64(x)
	case expr.String:
		n, err := strconv.ParseFloat(strings.TrimSpace(string(x)), 64)
		if err != nil {
			return 0, fmt.Errorf("expected a number but got '%s'", string(x))
		}
		f = n
	default:
		return 0, fmt.Errorf("expected a number but got %s", kindName(v))
	}
	if f != float64(int(f)) || f < 1 || f > 1<<31 {
		return 0, errors.New("expected a whole number greater than 0")
	}
	return int(f), nil
}

func kindName(v expr.Value) string {
	if v == nil {
		return expr.KindNull.String()
	}
	return v.Kind().String()
}

// EvaluateStrategy evaluates the job's strategy token against env and
// expands its matrix. env must provide the contexts the schema allows in
// "strategy": github, inputs, vars and needs. A job without a strategy gives
// a Strategy with one empty combination.
func (j *Job) EvaluateStrategy(ctx context.Context, env template.Env) (*Strategy, error) {
	if j.Strategy == nil {
		return ParseStrategy(ctx, nil)
	}
	v, err := template.Evaluate(ctx, j.Strategy, env)
	if err != nil {
		return nil, err
	}
	st, err := ParseStrategy(ctx, v)
	var limit *configurationLimitError
	if errors.As(err, &limit) {
		return nil, &configurationLimitError{Job: j.ID, Count: limit.Count}
	}
	return st, err
}

// MatrixEnv returns env with the "matrix" and "strategy" contexts of
// combination index added, for evaluating the parts of a job that may use
// them: runs-on, name, env, container, services, steps... It fails when index
// is out of range.
func (s *Strategy) MatrixEnv(env template.Env, index int) (template.Env, error) {
	if index < 0 || index >= len(s.Combos) {
		return env, fmt.Errorf("matrix combination %d out of range (%d combinations)", index, len(s.Combos))
	}
	out := env
	out.Contexts = make(map[string]expr.Value, len(env.Contexts)+2)
	for k, v := range env.Contexts {
		out.Contexts[k] = v
	}
	out.Contexts["matrix"] = s.Combos[index].Values
	out.Contexts["strategy"] = s.Context(index)
	return out, nil
}

// DisplayName evaluates the job's name for combination index of its
// strategy: the name token (or the job id) with the matrix values added in
// parentheses, unless the name is set and reads the matrix context, in which
// case it is used as is. Null and empty values are not listed. env must
// already include the matrix and strategy contexts, see [Strategy.MatrixEnv].
//
// Recorded on github.com (matrix-job-name): "default-name (a, 1)", a custom
// name using the matrix as is, and a custom name that does not read the matrix
// followed by the values ("same name for every leg (1)").
func (j *Job) DisplayName(ctx context.Context, env template.Env, s *Strategy, index int) (string, error) {
	if index < 0 || index >= len(s.Combos) {
		return "", fmt.Errorf("matrix combination %d out of range (%d combinations)", index, len(s.Combos))
	}
	name, err := template.EvaluateString(ctx, j.Name, env)
	if err != nil {
		return "", err
	}
	if !s.HasMatrix || (j.HasName && usesMatrix(j.Name)) {
		return name, nil
	}
	return s.Combos[index].DisplayName(name), nil
}

// usesMatrix reports whether an expression in t reads the matrix context.
func usesMatrix(t template.Token) bool {
	found := false
	template.Walk(t, func(x template.Token) bool {
		if be, ok := x.(*template.BasicExpression); ok && be.UsesContext("matrix") {
			found = true
		}
		return !found
	})
	return found
}
