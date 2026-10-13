// SPDX-License-Identifier: MIT

package action

import (
	"bnema.dev/ermite-actions/template"
	"bnema.dev/ermite-actions/workflow"
)

// Error is a problem found in an action manifest. See the package comment for
// the form of its message.
type Error = template.Error

// Action is a parsed action.yml.
type Action struct {
	// File is the name given to Parse.
	File string
	// Name, Description and Author are the keys of the same names; "" when
	// absent, or not a string. The runner does not read Author.
	Name, Description, Author string
	// Inputs are the declared inputs in file order.
	Inputs []Input
	// Outputs are the declared outputs in file order. Only a composite
	// action's outputs have a Value that is used.
	Outputs []Output
	// Runs says how the action runs. It is never nil in a returned Action.
	Runs *Runs
}

// Input returns the input called name, compared like the runner compares
// input names (ASCII case-insensitively), or nil.
func (a *Action) Input(name string) *Input {
	for i := range a.Inputs {
		if asciiEqualFold(a.Inputs[i].Name, name) {
			return &a.Inputs[i]
		}
	}
	return nil
}

// Input is an entry of "inputs".
type Input struct {
	// Name is the key, as written.
	Name string
	// Pos is where the key is.
	Pos template.Pos
	// Description is the "description" key, or "".
	Description string
	// Required is "required: true" (a boolean, or the string "true"). The
	// runner does not check it.
	Required bool
	// Default is the default value: a string, or an expression
	// (${{ github.ref }}) that the runner evaluates when the step does not
	// give the input; the context it may use is the one of
	// "input-default-context" in the schema (github, strategy, matrix, job,
	// runner and hashFiles). It is never nil: an input without a default has
	// the empty string, as in the runner. HasDefault tells them apart.
	Default    template.Token
	HasDefault bool
	// DeprecationMessage is "deprecationMessage", or "". The runner warns
	// "Input '<name>' has been deprecated with message: <message>" when a
	// step gives the input.
	DeprecationMessage string
}

// Deprecated reports whether the input has a deprecation message.
func (in *Input) Deprecated() bool { return in.DeprecationMessage != "" }

// Output is an entry of "outputs".
type Output struct {
	Name        string
	Pos         template.Pos
	Description string
	// Value is the "value" token of a composite action's output: a string or
	// an expression over github, strategy, matrix, steps, inputs, job, runner
	// and env. nil when the output has none.
	Value template.Token
}

// RunsKind is how an action runs.
type RunsKind int

// The kinds of action.
const (
	// RunsNode is a JavaScript action: using node12, node16, node20 or node24.
	RunsNode RunsKind = iota + 1
	// RunsDocker is a container action: using docker.
	RunsDocker
	// RunsComposite is a composite action: using composite.
	RunsComposite
	// RunsPlugin is "runs: {plugin: name}", an internal runner plugin. The
	// manifest loads, but only the runner's own plugins can run.
	RunsPlugin
)

// Runs is the "runs" key. Which fields are set depends on Kind.
type Runs struct {
	Kind RunsKind
	// Using is the runtime: "node12", "node16", "node20", "node24", "docker",
	// "composite", or "plugin". The runner compares "using" without regard to
	// case; Using is lower case.
	Using string
	// Pos is where "runs" is.
	Pos template.Pos

	// Node actions.
	//
	// Main is the script to run; Pre and Post are scripts that run before the
	// job's first step and after its last, or "". PreIf and PostIf are their
	// conditions: expressions written without ${{ }}, evaluated as they are
	// (the runner does not add "success() &&"), "always()" when absent.
	// Docker actions have the same four conditions for their entrypoints.
	Main, Pre, Post string
	PreIf, PostIf   string

	// Docker actions.
	//
	// Image is a path to a Dockerfile ("Dockerfile", "images/Dockerfile") or
	// "docker://image[:tag]"; see [ParseImage]. Entrypoint overrides the
	// image's ENTRYPOINT. Args is a sequence of strings and Env a mapping of
	// strings, both of which may hold ${{ inputs.x }} expressions (and no other
	// context); nil when absent. PreEntrypoint and PostEntrypoint are the
	// entrypoints that run before and after, or "".
	Image, Entrypoint, PreEntrypoint, PostEntrypoint string
	Args                                             *template.Sequence
	Env                                              *template.Mapping

	// Composite actions.
	//
	// Steps are the steps in file order. A run step has a shell; the contexts
	// of each key are those of the schema (github, inputs, strategy, matrix,
	// steps, job, runner, env), and "if" also takes vars and needs. A step
	// has no timeout-minutes and no working-directory on a uses step.
	Steps []*workflow.Step

	// Plugin actions.
	Plugin string
}

// HasPre reports whether the action has something to run before the job's
// steps: a node "pre" or a docker "pre-entrypoint".
func (r *Runs) HasPre() bool {
	switch r.Kind {
	case RunsNode:
		return r.Pre != ""
	case RunsDocker:
		return r.PreEntrypoint != ""
	}
	return false
}

// HasPost reports whether the action has something to run after the job's
// steps: a node "post", a docker "post-entrypoint" or a plugin.
func (r *Runs) HasPost() bool {
	switch r.Kind {
	case RunsNode:
		return r.Post != ""
	case RunsDocker:
		return r.PostEntrypoint != ""
	}
	return false
}
