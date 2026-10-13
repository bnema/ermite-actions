// SPDX-License-Identifier: MIT

package workflow

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"bnema.dev/ermite-actions/expr"
	"bnema.dev/ermite-actions/template"
)

// StatusFuncs is the state the status functions of conditions report:
// success(), failure(), cancelled() and always().
type StatusFuncs struct {
	// Success is the result of success(): true while nothing before has
	// failed or been cancelled.
	Success bool
	// Failure is the result of failure().
	Failure bool
	// Cancelled is the result of cancelled().
	Cancelled bool
	// always() is true, whatever the state.
}

// Funcs returns the implementations to put in [template.Env.Funcs]. The
// functions ignore their arguments.
func (s StatusFuncs) Funcs() map[string]expr.Func {
	fixed := func(b bool) expr.Func {
		return func(context.Context, []expr.Value) (expr.Value, error) { return expr.Boolean(b), nil }
	}
	return map[string]expr.Func{
		"success":   fixed(s.Success),
		"failure":   fixed(s.Failure),
		"cancelled": fixed(s.Cancelled),
		"always":    fixed(true),
	}
}

// evaluateIf evaluates a condition with the status functions added to env and
// converts the result to a boolean the way GitHub does (see expr.Truthy).
func evaluateIf(ctx context.Context, cond *template.BasicExpression, env template.Env, st StatusFuncs) (bool, error) {
	out := env
	out.Funcs = make(map[string]expr.Func, len(env.Funcs)+4)
	for k, f := range env.Funcs {
		out.Funcs[k] = f
	}
	for k, f := range st.Funcs() {
		out.Funcs[k] = f
	}
	v, err := template.EvaluateExpression(ctx, cond, out)
	if err != nil {
		return false, err
	}
	return expr.Truthy(v), nil
}

// EvaluateIf evaluates the job's condition. env must provide the contexts
// the schema allows in a job's "if": github, inputs, vars and needs.
func (j *Job) EvaluateIf(ctx context.Context, env template.Env, st StatusFuncs) (bool, error) {
	return evaluateIf(ctx, j.If, env, st)
}

// EvaluateIf evaluates the step's condition. env must provide the contexts
// the schema allows in a step's "if" and may provide hashFiles.
func (s *Step) EvaluateIf(ctx context.Context, env template.Env, st StatusFuncs) (bool, error) {
	return evaluateIf(ctx, s.If, env, st)
}

// EvaluateRunsOn evaluates the job's runs-on and returns the labels and the
// runner group. runs-on may be a string, a list of strings, or a mapping with
// "group" and "labels" (a string or a list), each part possibly an
// expression. A job that calls a reusable workflow has no runs-on.
//
// The checks are those of the reference's converter: a group is a name, or
// "org/name", "organization/name", "ent/name" or "enterprise/name".
func (j *Job) EvaluateRunsOn(ctx context.Context, env template.Env) (labels []string, group string, err error) {
	if j.RunsOn == nil {
		return nil, "", fmt.Errorf("job '%s' has no runs-on", j.ID)
	}
	v, err := template.Evaluate(ctx, j.RunsOn, env)
	if err != nil {
		return nil, "", err
	}
	fail := func(format string, a ...any) ([]string, string, error) {
		return nil, "", &template.Error{File: j.RunsOn.File(), Pos: j.RunsOn.Pos(), Raw: fmt.Sprintf(format, a...)}
	}
	switch v := v.(type) {
	case *expr.Dictionary:
		for _, p := range v.Pairs() {
			switch strings.ToLower(p.Key) {
			case "group":
				g, ok := p.Value.(expr.String)
				if !ok {
					return fail("Unexpected type '%s' encountered while reading 'job runs-on group name'. The type 'StringToken' was expected.", tokenKind(p.Value))
				}
				if msg := checkGroup(string(g)); msg != "" {
					return fail("%s", msg)
				}
				group = string(g)
			case "labels":
				l, bad := labelList(p.Value)
				if bad != "" {
					return fail("%s", bad)
				}
				labels = append(labels, l...)
			}
		}
		return labels, group, nil
	}
	labels, bad := labelList(v)
	if bad != "" {
		return fail("%s", bad)
	}
	return labels, "", nil
}

func tokenKind(v expr.Value) string {
	switch v.(type) {
	case expr.String:
		return template.KindString.String()
	case expr.Number:
		return template.KindNumber.String()
	case expr.Boolean:
		return template.KindBoolean.String()
	case *expr.Array:
		return template.KindSequence.String()
	case *expr.Dictionary:
		return template.KindMapping.String()
	}
	return template.KindNull.String()
}

// labelList reads a string or a list of strings. It returns the problem as a
// message.
func labelList(v expr.Value) ([]string, string) {
	switch v := v.(type) {
	case expr.String:
		return []string{string(v)}, ""
	case *expr.Array:
		var out []string
		for _, it := range v.Items() {
			s, ok := it.(expr.String)
			if !ok {
				return nil, fmt.Sprintf("Unexpected type '%s' encountered while reading 'job runs-on label sequence item'. The type 'StringToken' was expected.", tokenKind(it))
			}
			out = append(out, string(s))
		}
		return out, ""
	}
	return nil, fmt.Sprintf("Unexpected type '%s' encountered while reading 'job runs-on label sequence item'. The type 'StringToken' was expected.", tokenKind(v))
}

// checkGroup returns the reference's message for an invalid runner group.
func checkGroup(group string) string {
	names := strings.Split(group, "/")
	switch len(names) {
	case 1:
		return ""
	case 2:
		switch names[0] {
		case "org", "organization", "ent", "enterprise":
		default:
			return fmt.Sprintf("Invalid runs-on group name '%s. Please use 'organization/' or 'enterprise/' prefix to target a single runner group.'", group)
		}
		if names[1] == "" {
			return fmt.Sprintf("Invalid runs-on group name '%s'.", group)
		}
		return ""
	}
	return fmt.Sprintf("Invalid runs-on group name '%s. Please use 'organization/' or 'enterprise/' prefix to target a single runner group.'", group)
}

// JobLevels groups the jobs by dependency depth: the first level holds the
// jobs without needs, and every later job is in the level after the last of
// its dependencies. Within a level the jobs keep the order of the file. A
// job whose needs name an unknown job, or that is part of a cycle, is left
// out; Parse rejects such workflows, so this only happens for a Workflow
// built by hand.
func (w *Workflow) JobLevels() [][]*Job {
	level := make(map[string]int, len(w.Jobs))
	var levels [][]*Job
	placed := 0
	for placed < len(w.Jobs) {
		progressed := false
		for _, j := range w.Jobs {
			if _, done := level[j.ID]; done {
				continue
			}
			depth, ready := 0, true
			for _, n := range j.Needs {
				l, ok := level[n]
				if !ok {
					ready = false
					break
				}
				depth = max(depth, l+1)
			}
			if !ready {
				continue
			}
			level[j.ID] = depth
			for len(levels) <= depth {
				levels = append(levels, nil)
			}
			levels[depth] = append(levels[depth], j)
			placed++
			progressed = true
		}
		if !progressed {
			break
		}
	}
	// A job placed in a later pass of the file may belong to an earlier level
	// than one placed before it; restore file order inside each level.
	pos := make(map[*Job]int, len(w.Jobs))
	for i, j := range w.Jobs {
		pos[j] = i
	}
	for _, l := range levels {
		slices.SortFunc(l, func(a, b *Job) int { return pos[a] - pos[b] })
	}
	return levels
}

// Ancestors returns the ids of every job the job with the given id depends on,
// directly or through other jobs, in the order of the file. It is empty for a
// job without needs and nil for an unknown id.
//
// GitHub's default condition of a job, success(), looks at this whole chain
// and not only at the direct needs: a job that needs a job which ran through
// always() after a failure is skipped, although its only direct need
// succeeded. Recorded (needs-failed-dependency): chain-after-always needs
// with-always, which needs broken; broken failed, with-always (if: always())
// succeeded, and chain-after-always was skipped. Likewise failure() is true
// when any ancestor failed, as documented.
func (w *Workflow) Ancestors(id string) []string {
	start := w.Job(id)
	if start == nil {
		return nil
	}
	seen := map[string]bool{id: true}
	stack := slices.Clone(start.Needs)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		if j := w.Job(n); j != nil {
			stack = append(stack, j.Needs...)
		}
	}
	delete(seen, id)
	out := []string{}
	for _, j := range w.Jobs {
		if seen[j.ID] {
			out = append(out, j.ID)
		}
	}
	return out
}
