// SPDX-License-Identifier: MIT

package action

import (
	_ "embed"
	"strings"
	"sync"

	"bnema.dev/ermite-actions/template"
	"bnema.dev/ermite-actions/workflow"
)

//go:embed schema/action-v1.0.min.json
var schemaJSON []byte

var (
	schemaOnce sync.Once
	schema     *template.Schema
	schemaErr  error
)

// Schema returns the vendored action schema (action-v1.0). It is read once and
// shared; it must not be modified.
func Schema() (*template.Schema, error) {
	schemaOnce.Do(func() { schema, schemaErr = template.ParseSchema(schemaJSON) })
	return schema, schemaErr
}

// rootType is the definition the runner reads an action.yml against: its
// action_yaml.json has "action-root", whose definitions the vendored schema
// keeps under the same names. ("action-root-strict" is the language service's
// linter, which also wants name and description and refuses unknown keys that
// GitHub accepts.)
const rootType = "action-root"

// The limits of the runner's TemplateContext for an action manifest
// (ActionManifestManagerLegacy.CreateTemplateContext).
const (
	maxDepth  = 100
	maxErrors = 10
)

// LoadFailed returns the line the runner ends with when [Parse] returned
// errors: it reports each error, then fails the step with this text.
func LoadFailed(file string) string { return "Failed to load " + file }

// Parse reads, validates and converts an action.yml (or action.yaml) the way
// GitHub's runner does. path is only used in messages, where the runner puts
// the manifest's path inside the actions directory ("actions/checkout/v4/
// action.yml").
//
// The errors are the runner's, in its order: first the ones the template
// reader and the converters record with a position, then the one exception
// that stopped the load, if any, worded like the runner prints it
// ("System.ArgumentNullException: Value cannot be null. (Parameter 'You are
// using a JavaScript Action but there is not an entry JavaScript file provided
// in action.yml.')", stack trace left out). At most ten errors are kept. When
// the list is not empty the action is nil, and the runner fails with
// [LoadFailed]. A manifest without "runs" gives the one error "Top level
// 'runs:' section is required for <path>".
func Parse(path string, data []byte) (*Action, []Error) {
	sch, err := Schema()
	if err != nil {
		return nil, []Error{{Raw: "internal error: " + err.Error()}}
	}
	ctx := template.NewContext(sch)
	ctx.MaxDepth, ctx.ErrorLimit, ctx.RunnerYAML = maxDepth, maxErrors, true
	l := &loader{ctx: ctx, schema: sch, file: path}
	a := l.load(data)
	if ctx.HasErrors() {
		return nil, collect(ctx)
	}
	if a == nil || a.Runs == nil {
		return nil, []Error{{Raw: "Top level 'runs:' section is required for " + path}}
	}
	return a, nil
}

func collect(ctx *template.Context) []Error {
	var out []Error
	for _, e := range ctx.Errors() {
		out = append(out, *e)
	}
	return out
}

type loader struct {
	ctx    *template.Context
	schema *template.Schema
	file   string
}

// load is ActionManifestManagerLegacy.Load: everything that is thrown is
// recorded in the context, like the runner's catch.
func (l *loader) load(data []byte) (a *Action) {
	defer l.catch()
	return l.loadRoot(template.ReadYAML(l.ctx, rootType, l.file, data))
}

func (l *loader) catch() {
	if r := recover(); r != nil {
		t, ok := r.(thrown)
		if !ok {
			panic(r)
		}
		l.ctx.ErrorException(t.class, t.msg)
	}
}

func (l *loader) loadRoot(root template.Token) (a *Action) {
	a = &Action{File: l.file}
	m := assertMapping(root, "action manifest root")
	var outputs *template.Mapping
	var runs template.Token
	for _, p := range m.Pairs {
		key := assertString(p.Key, "action.yml property key")
		switch key.Value {
		case "name":
			a.Name = assertString(p.Value, "name").Value
		case "outputs":
			outputs = assertMapping(p.Value, "outputs")
		case "description":
			a.Description = assertString(p.Value, "description").Value
		case "inputs":
			l.convertInputs(p.Value, a)
		case "runs":
			runs = p.Value
		case "author":
			if s, ok := p.Value.(*template.String); ok {
				a.Author = s.Value
			}
		}
	}
	if outputs != nil {
		a.Outputs = convertOutputs(outputs)
	}
	if runs != nil {
		a.Runs = l.convertRuns(runs, m)
	}
	return a
}

func (l *loader) check() {
	errs := l.ctx.Errors()
	if len(errs) == 0 {
		return
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	throwf(exTemplate, "The template is not valid. %s", strings.Join(msgs, ","))
}

func (l *loader) convertInputs(t template.Token, a *Action) {
	for _, p := range assertMapping(t, "inputs").Pairs {
		name := assertString(p.Key, "input name")
		in := Input{Name: name.Value, Pos: name.Pos()}
		for _, md := range assertMapping(p.Value, "input metadata").Pairs {
			mdName := assertString(md.Key, "input metadata").Value
			switch {
			case asciiEqualFold(mdName, "default"):
				in.Default, in.HasDefault = md.Value, true
			case asciiEqualFold(mdName, "deprecationMessage"):
				// The runner's Dictionary.Add would throw on a repeated input
				// name, but the template reader has already refused one
				// (names compare case-insensitively there).
				in.DeprecationMessage = assertString(md.Value, "input deprecationMessage").Value
			case mdName == "description":
				if s, ok := md.Value.(*template.String); ok {
					in.Description = s.Value
				}
			case mdName == "required":
				switch v := md.Value.(type) {
				case *template.Boolean:
					in.Required = v.Value
				case *template.String:
					in.Required = v.Value == "true"
				}
			}
		}
		if !in.HasDefault {
			in.Default = template.NewString(template.Origin{}, "")
		}
		a.Inputs = append(a.Inputs, in)
	}
}

func convertOutputs(m *template.Mapping) []Output {
	var out []Output
	for _, p := range m.Pairs {
		o := Output{Name: p.Key.String(), Pos: p.Key.Pos()}
		if om, ok := p.Value.(*template.Mapping); ok {
			for _, kv := range om.Pairs {
				switch kv.Key.String() {
				case "description":
					if s, ok := kv.Value.(*template.String); ok {
						o.Description = s.Value
					}
				case "value":
					if template.IsScalar(kv.Value) {
						o.Value = kv.Value
					}
				}
			}
		}
		out = append(out, o)
	}
	return out
}

// convertRuns is ConvertRuns.
func (l *loader) convertRuns(t template.Token, _ *template.Mapping) *Runs {
	rm := assertMapping(t, "runs")
	var (
		usingTok, imageTok, entrypointTok, mainTok, pluginTok *template.String
		preTok, preEntrypointTok, preIfTok                    *template.String
		postTok, postEntrypointTok, postIfTok                 *template.String
		argsTok                                               *template.Sequence
		envTok                                                *template.Mapping
		steps                                                 []*workflow.Step
		haveSteps                                             bool
	)
	for _, p := range rm.Pairs {
		key := assertString(p.Key, "runs key").Value
		v := p.Value
		switch key {
		case "using":
			usingTok = assertString(v, "using")
		case "image":
			imageTok = assertString(v, "image")
		case "args":
			argsTok = assertSequence(v, "args")
		case "entrypoint":
			entrypointTok = assertString(v, "entrypoint")
		case "env":
			envTok = assertMapping(v, "env")
		case "main":
			mainTok = assertString(v, "main")
		case "plugin":
			pluginTok = assertString(v, "plugin")
		case "post":
			postTok = assertString(v, "post")
		case "post-entrypoint":
			postEntrypointTok = assertString(v, "post-entrypoint")
		case "post-if":
			postIfTok = assertString(v, "post-if")
		case "pre":
			preTok = assertString(v, "pre")
		case "pre-entrypoint":
			preEntrypointTok = assertString(v, "pre-entrypoint")
		case "pre-if":
			preIfTok = assertString(v, "pre-if")
		case "steps":
			steps, haveSteps = l.convertSteps(assertSequence(v, "steps")), true
			l.check()
		}
	}
	str := func(s *template.String) string {
		if s == nil {
			return ""
		}
		return s.Value
	}
	orAlways := func(s *template.String) string {
		if s == nil {
			return "always()"
		}
		return s.Value
	}
	r := &Runs{Pos: rm.Pos()}
	if usingTok != nil {
		using := usingTok.Value
		switch {
		case asciiEqualFold(using, "docker"):
			if str(imageTok) == "" {
				throwf(exArgumentNull, "You are using a Container Action but an image is not provided in %s.", l.file)
			}
			r.Kind, r.Using = RunsDocker, "docker"
			r.Image, r.Entrypoint = imageTok.Value, str(entrypointTok)
			r.Args, r.Env = argsTok, envTok
			r.PreEntrypoint, r.PostEntrypoint = str(preEntrypointTok), str(postEntrypointTok)
			r.PreIf, r.PostIf = orAlways(preIfTok), orAlways(postIfTok)
		case asciiEqualFold(using, "node12"), asciiEqualFold(using, "node16"), asciiEqualFold(using, "node20"), asciiEqualFold(using, "node24"):
			if str(mainTok) == "" {
				throwf(exArgumentNull, "You are using a JavaScript Action but there is not an entry JavaScript file provided in %s.", l.file)
			}
			r.Kind, r.Using = RunsNode, strings.ToLower(using)
			r.Main, r.Pre, r.Post = mainTok.Value, str(preTok), str(postTok)
			r.PreIf, r.PostIf = orAlways(preIfTok), orAlways(postIfTok)
		case asciiEqualFold(using, "composite"):
			if !haveSteps {
				throwf(exArgumentNull, "You are using a composite action but there are no steps provided in %s.", l.file)
			}
			r.Kind, r.Using, r.Steps = RunsComposite, "composite", steps
			r.PreIf, r.PostIf = "always()", "always()"
		default:
			throwf(exArgumentRng, "'using: %s' is not supported, use 'docker', 'node12', 'node16', 'node20' or 'node24' instead.", using)
		}
		return r
	}
	if pluginTok != nil {
		r.Kind, r.Using, r.Plugin = RunsPlugin, "plugin", pluginTok.Value
		return r
	}
	throwf(exNotSupported, "Missing 'using' value. 'using' requires 'composite', 'docker', 'node12', 'node16', 'node20' or 'node24'.")
	return nil
}
