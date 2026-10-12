// SPDX-License-Identifier: MIT

package workflow

import (
	"bnema.dev/ermite-actions/template"
)

// Error is a problem found in a workflow file. Its message has the form
// "file.yml (Line: 3, Col: 5): message".
type Error = template.Error

// Workflow is a parsed and validated workflow file.
type Workflow struct {
	// File is the name given to Parse.
	File string
	// Name is the "name" key, or "".
	Name string
	// RunName is the "run-name" token (an expression is allowed), or nil.
	RunName template.Token
	// On lists the events that trigger the workflow, in file order.
	On *On
	// Env is the workflow-level "env" mapping, or nil. Values may hold
	// expressions.
	Env *template.Mapping
	// Defaults holds the workflow-level "defaults", or nil.
	Defaults *Defaults
	// Concurrency is the "concurrency" token (a string or a mapping), or nil.
	Concurrency template.Token
	// Permissions is the "permissions" key, or nil.
	Permissions *Permissions
	// Jobs are the jobs in file order.
	Jobs []*Job
}

// Job returns the job with the given id, or nil.
func (w *Workflow) Job(id string) *Job {
	for _, j := range w.Jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// Defaults is the "defaults" key.
type Defaults struct {
	// Run holds defaults.run; nil when absent.
	Run *RunDefaults
}

// RunDefaults is defaults.run: the shell and working directory of run steps.
type RunDefaults struct {
	Shell            template.Token
	WorkingDirectory template.Token
}

// Permissions is the "permissions" key: either all permissions read or
// write, or a level per scope.
type Permissions struct {
	// All is "read-all" or "write-all", or "" when Scopes is used.
	All string
	// Scopes lists the scopes in file order ("contents": "read").
	Scopes []PermissionScope
}

// PermissionScope is one entry of a permissions mapping.
type PermissionScope struct {
	Scope string
	Level string // "read", "write" or "none"
}

// Job is a job of a workflow.
type Job struct {
	// ID is the key of the job.
	ID string
	// IDToken is the key token, with its position.
	IDToken template.Token
	// Name is the "name" token; when the file gives none it is the id.
	Name template.Token
	// HasName reports whether the file gave a non-empty name.
	HasName bool
	// Needs are the ids of the jobs this one depends on, with their tokens.
	Needs      []string
	NeedTokens []*template.String
	// If is the condition, always with a status function: a condition
	// without one is wrapped as "success() && (cond)", and a missing one is
	// "success()".
	If *template.BasicExpression
	// RunsOn is the "runs-on" token (string, sequence, mapping or an
	// expression); nil for a reusable workflow job.
	RunsOn template.Token
	// Strategy is the "strategy" token, or nil.
	Strategy template.Token
	// Env is the job's "env" mapping, or nil.
	Env *template.Mapping
	// Defaults holds the job-level "defaults", or nil.
	Defaults *Defaults
	// Steps are the steps in file order.
	Steps []*Step
	// Outputs is the "outputs" mapping, or nil.
	Outputs *template.Mapping
	// TimeoutMinutes, CancelTimeoutMinutes and ContinueOnError are tokens
	// (a literal or an expression), or nil.
	TimeoutMinutes       template.Token
	CancelTimeoutMinutes template.Token
	ContinueOnError      template.Token
	// Container, Services, Environment, Concurrency and Snapshot are
	// the raw tokens, or nil.
	Container   template.Token
	Services    template.Token
	Environment template.Token
	Concurrency template.Token
	Snapshot    template.Token
	// Permissions is the job's "permissions", or nil.
	Permissions *Permissions

	// Uses is set for a job that calls a reusable workflow ("uses"); such a
	// job has no steps and no runs-on.
	Uses *template.String
	// With and Secrets are the inputs and secrets passed to the called
	// workflow. InheritSecrets is "secrets: inherit".
	With           *template.Mapping
	Secrets        *template.Mapping
	InheritSecrets bool
}

// IsReusable reports whether the job calls a reusable workflow.
func (j *Job) IsReusable() bool { return j.Uses != nil }

// Step is a step of a job: a run step or a uses step.
type Step struct {
	// ID is the id of the step. When the file gives none, one is generated
	// the way the reference does: "__run", "__run_2", "__actions_checkout".
	ID string
	// Explicit reports whether the id came from the file.
	Explicit bool
	// Pos is where the step starts.
	Pos template.Pos
	// Name is the "name" token, or nil.
	Name template.Token
	// If is the condition, with a status function, as for jobs.
	If *template.BasicExpression
	// Run is the script of a run step, or nil.
	Run template.Token
	// Shell and WorkingDirectory are the run step's options, or nil.
	Shell            template.Token
	WorkingDirectory template.Token
	// Uses is the action of a uses step, or nil.
	Uses *template.String
	// With holds the inputs of a uses step, or nil.
	With *template.Mapping
	// Env is the step's "env" mapping, or nil.
	Env *template.Mapping
	// ContinueOnError and TimeoutMinutes are tokens, or nil.
	ContinueOnError template.Token
	TimeoutMinutes  template.Token
}

// IsRun reports whether the step runs a script.
func (s *Step) IsRun() bool { return s.Run != nil }
