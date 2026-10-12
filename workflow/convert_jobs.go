// SPDX-License-Identifier: MIT

package workflow

import (
	"bnema.dev/ermite-actions/template"
)

type needsNode struct {
	name  string
	needs []*template.String
}

// convertJobs is a port of convertJobs: every job is converted on its own, a
// job that fails is dropped after its error is recorded, then the needs graph
// is checked.
func (c *converter) convertJobs(t template.Token) []*Job {
	m, ok := t.(*template.Mapping)
	if !ok {
		c.ctx.ErrorAt(t, "Invalid format for jobs")
		return nil
	}
	var result []*Job
	var satisfied, unsatisfied []*needsNode
	for _, p := range m.Pairs {
		key := asString(p.Key, "job name")
		var job *Job
		c.handle(m, func() {
			job = c.convertJob(key, asMap(p.Value, "job "+key.Value))
		})
		if job == nil {
			continue
		}
		result = append(result, job)
		n := &needsNode{name: job.ID, needs: append([]*template.String(nil), job.NeedTokens...)}
		if len(n.needs) > 0 {
			unsatisfied = append(unsatisfied, n)
		} else {
			satisfied = append(satisfied, n)
		}
	}
	c.validateNeeds(m, result, satisfied, unsatisfied)
	return result
}

// validateNeeds reports jobs that can never start: it removes satisfied
// dependencies in the order the reference does and complains about what is
// left.
func (c *converter) validateNeeds(t template.Token, jobs []*Job, satisfied, unsatisfied []*needsNode) {
	if len(satisfied) == 0 {
		c.ctx.ErrorAt(t, "The workflow must contain at least one job with no dependencies.")
		return
	}
	for len(satisfied) > 0 {
		current := satisfied[0]
		satisfied = satisfied[1:]
		for i := len(unsatisfied) - 1; i >= 0; i-- {
			u := unsatisfied[i]
			for j := len(u.needs) - 1; j >= 0; j-- {
				if u.needs[j].Value != current.name {
					continue
				}
				u.needs = append(u.needs[:j], u.needs[j+1:]...)
				if len(u.needs) == 0 {
					satisfied = append(satisfied, u)
					unsatisfied = append(unsatisfied[:i], unsatisfied[i+1:]...)
				}
			}
		}
	}
	if len(unsatisfied) == 0 {
		return
	}
	known := map[string]bool{}
	for _, j := range jobs {
		known[j.ID] = true
	}
	for _, u := range unsatisfied {
		for _, need := range u.needs {
			if known[need.Value] {
				c.ctx.Errorf(need, "Job '%s' depends on job '%s' which creates a cycle in the dependency graph.", u.name, need.Value)
			} else {
				c.ctx.Errorf(need, "Job '%s' depends on unknown job '%s'.", u.name, need.Value)
			}
		}
	}
}

// convertJob is a port of convertJob.
func (c *converter) convertJob(key *template.String, m *template.Mapping) *Job {
	if msg := c.jobIDs.tryAddKnownID(key.Value); msg != "" {
		c.ctx.ErrorAt(key, msg)
	}
	j := &Job{ID: key.Value, IDToken: key}
	var name template.Token
	for _, p := range m.Pairs {
		prop := asString(p.Key, "job property name")
		v := p.Value
		switch prop.Value {
		case "concurrency":
			c.handle(v, func() { c.convertConcurrency(v) })
			j.Concurrency = v
		case "container":
			c.convertContainer(v, false)
			j.Container = v
		case "env":
			c.handle(v, func() { j.Env = asMap(v, "job env") })
		case "environment":
			c.handle(v, func() { c.convertEnvironment(v) })
			j.Environment = v
		case "if":
			j.If = c.convertIf(v, "job-if")
		case "name":
			name = asScalar(v, "job name")
		case "needs":
			j.NeedTokens = nil
			if s, ok := v.(*template.String); ok {
				j.NeedTokens = append(j.NeedTokens, asString(s, "job needs id"))
			}
			if seq, ok := v.(*template.Sequence); ok {
				for _, it := range seq.Items {
					j.NeedTokens = append(j.NeedTokens, asString(it, "job needs id"))
				}
			}
		case "outputs":
			c.handle(v, func() { j.Outputs = asMap(v, "job outputs") })
		case "runs-on":
			c.handle(v, func() { c.convertRunsOn(v) })
			j.RunsOn = v
		case "services":
			c.convertServices(v)
			j.Services = v
		case "snapshot":
			j.Snapshot = v
		case "steps":
			j.Steps = c.convertSteps(v)
		case "strategy":
			j.Strategy = v
		case "uses":
			j.Uses = asString(v, "job uses value")
		case "with":
			c.handle(v, func() { j.With = asMap(v, "uses-with value") })
		case "secrets":
			if s, ok := v.(*template.String); ok && s.Value == "inherit" {
				j.InheritSecrets = true
			} else {
				c.handle(v, func() { j.Secrets = asMap(v, "uses-secrets value") })
			}
		case "timeout-minutes":
			j.TimeoutMinutes = v
		case "cancel-timeout-minutes":
			j.CancelTimeoutMinutes = v
		case "continue-on-error":
			j.ContinueOnError = v
		case "defaults":
			j.Defaults = convertDefaults(v)
		case "permissions":
			j.Permissions = convertPermissions(v)
		}
	}
	for _, n := range j.NeedTokens {
		j.Needs = append(j.Needs, n.Value)
	}
	if j.If == nil {
		j.If = c.defaultIf("job-if")
	}
	switch {
	case name == nil:
		j.Name = key
	case isEmptyString(name):
		j.Name = key
	default:
		j.Name, j.HasName = name, true
	}
	return j
}

func isEmptyString(t template.Token) bool {
	s, ok := t.(*template.String)
	return ok && s.Value == ""
}
