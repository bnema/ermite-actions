// SPDX-License-Identifier: MIT

package workflow

import (
	"bnema.dev/ermite-actions/template"
	"strings"
)

// On is the "on" key: the events that trigger a workflow.
type On struct {
	// Events are the events in file order.
	Events []*Event
}

// Event returns the configuration of the named event, or nil.
func (o *On) Event(name string) *Event {
	if o == nil {
		return nil
	}
	for _, e := range o.Events {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// Has reports whether the workflow is triggered by the named event.
func (o *On) Has(name string) bool { return o.Event(name) != nil }

// Event is one event of the "on" key with its filters. A filter that is
// absent from the file is nil; one written as an empty list is empty and
// non-nil.
type Event struct {
	Name string
	// Token is the position of the event name.
	Token template.Token

	Branches       []string
	BranchesIgnore []string
	Tags           []string
	TagsIgnore     []string
	Paths          []string
	PathsIgnore    []string
	Types          []string
	Versions       []string
	Names          []string
	Workflows      []string

	cf *compiledFilters

	// Schedule holds the entries of the "schedule" event.
	Schedule []Schedule
	// Inputs holds the inputs of workflow_dispatch and workflow_call.
	Inputs []Input
}

// Schedule is an entry of the "schedule" event.
type Schedule struct {
	Cron     string
	Timezone string
}

// Input is an input of workflow_dispatch or workflow_call.
type Input struct {
	Name        string
	Type        string // "string", "choice", "boolean", "number", "environment"
	Description string
	Required    bool
	// HasDefault reports whether the file gave a default; Default is its text
	// (an expression keeps its ${{ }} form).
	HasDefault bool
	Default    string
	Options    []string
}

// convertOn is a port of convertOn in converter/events.ts.
func (c *converter) convertOn(t template.Token) *On {
	on := &On{}
	switch t := t.(type) {
	case *template.String:
		on.Events = append(on.Events, &Event{Name: t.Value, Token: t})
	case *template.Sequence:
		for _, it := range t.Items {
			s := asString(it, "on")
			on.Events = append(on.Events, &Event{Name: s.Value, Token: s})
		}
	case *template.Mapping:
		for _, p := range t.Pairs {
			key := asString(p.Key, "event name")
			ev := &Event{Name: key.Value, Token: key}
			on.Events = append(on.Events, ev)
			if p.Value.Kind() == template.KindNull {
				continue
			}
			if key.Value == "schedule" {
				ev.Schedule = c.convertSchedule(asSeq(p.Value, "event schedule"))
				continue
			}
			m := asMap(p.Value, "event "+key.Value)
			switch key.Value {
			case "workflow_call":
				c.convertWorkflowCall(ev, m)
				continue
			case "workflow_dispatch":
				c.convertWorkflowDispatch(ev, m)
				continue
			}
			ev.Branches = patternFilter("branches", m)
			ev.BranchesIgnore = patternFilter("branches-ignore", m)
			ev.Tags = patternFilter("tags", m)
			ev.TagsIgnore = patternFilter("tags-ignore", m)
			ev.Paths = patternFilter("paths", m)
			ev.PathsIgnore = patternFilter("paths-ignore", m)
			ev.Types = patternFilter("types", m)
			ev.Versions = patternFilter("versions", m)
			ev.Names = patternFilter("names", m)
			ev.Workflows = patternFilter("workflows", m)
		}
	default:
		c.ctx.ErrorAt(t, "Invalid format for 'on'")
		return &On{}
	}
	return on
}

// patternFilter reads a filter that is a string or a list of strings; nil
// when the key is absent.
func patternFilter(name string, m *template.Mapping) []string {
	var out []string
	for _, p := range m.Pairs {
		key := asString(p.Key, name+" filter key")
		if key.Value != name {
			continue
		}
		if s, ok := p.Value.(*template.String); ok {
			out = []string{s.Value}
			continue
		}
		out = []string{}
		for _, it := range asSeq(p.Value, name+" list").Items {
			out = append(out, asString(it, name+" list item").Value)
		}
	}
	return out
}

func (c *converter) convertSchedule(seq *template.Sequence) []Schedule {
	var out []Schedule
	for _, it := range seq.Items {
		m := asMap(it, "event schedule")
		var cfg Schedule
		valid := true
		for _, e := range m.Pairs {
			key := asString(e.Key, "schedule key")
			switch key.Value {
			case "cron":
				cron := asString(e.Value, "schedule cron")
				if !isValidCron(cron.Value) {
					c.ctx.ErrorAt(cron, "Invalid cron expression. Expected format: '* * * * *' (minute hour day month weekday)")
				}
				cfg.Cron = cron.Value
			case "timezone":
				cfg.Timezone = asString(e.Value, "schedule timezone").Value
			default:
				c.ctx.ErrorAt(key, "Invalid schedule key")
				valid = false
			}
		}
		switch {
		case valid && cfg.Cron != "":
			out = append(out, cfg)
		case valid:
			c.ctx.ErrorAt(m, "Missing required key 'cron' in schedule entry")
		}
	}
	return out
}

func (c *converter) convertWorkflowDispatch(ev *Event, m *template.Mapping) {
	for _, p := range m.Pairs {
		if asString(p.Key, "workflow dispatch input key").Value == "inputs" {
			ev.Inputs = c.convertInputs(asMap(p.Value, "workflow dispatch inputs"), false)
		}
	}
}

func (c *converter) convertWorkflowCall(ev *Event, m *template.Mapping) {
	for _, p := range m.Pairs {
		switch asString(p.Key, "workflow dispatch input key").Value {
		case "inputs":
			ev.Inputs = c.convertInputs(asMap(p.Value, "workflow dispatch inputs"), true)
		case "secrets":
			for _, s := range asMap(p.Value, "workflow dispatch inputs").Pairs {
				asString(s.Key, "secret name")
				if sm, ok := s.Value.(*template.Mapping); ok {
					for _, kv := range sm.Pairs {
						switch asString(kv.Key, "workflow call secret key").Value {
						case "description":
							asString(kv.Value, "secret description")
						case "required":
							asBool(kv.Value, "secret required")
						}
					}
				}
			}
		}
	}
}

// convertInputs ports convertWorkflowDispatchInputs and convertWorkflowInputs.
// The two differ only in that call inputs skip the default check when the
// default is an expression.
func (c *converter) convertInputs(m *template.Mapping, call bool) []Input {
	var out []Input
	for _, p := range m.Pairs {
		name := asString(p.Key, "input name")
		im := asMap(p.Value, "input configuration")
		out = append(out, c.convertInput(name.Value, im, call))
	}
	return out
}

func (c *converter) convertInput(name string, m *template.Mapping, call bool) Input {
	in := Input{Name: name, Type: "string"}
	var def template.Token
	var options []string
	hasOptions := false
	for _, p := range m.Pairs {
		key := asString(p.Key, "workflow dispatch input key")
		switch key.Value {
		case "description":
			in.Description = asString(p.Value, "input description").Value
		case "required":
			in.Required = asBool(p.Value, "input required").Value
		case "default":
			def = asScalar(p.Value, "input default")
		case "type":
			in.Type = asString(p.Value, "input type").Value
		case "options":
			hasOptions = true
			options = []string{}
			for _, it := range asSeq(p.Value, "input options").Items {
				options = append(options, asString(it, "input options item").Value)
			}
			in.Options = options
		default:
			c.ctx.Errorf(p.Key, "Invalid key '%s'", key.Value)
		}
	}
	if def != nil {
		in.HasDefault = true
		in.Default = def.String()
		if !call || !template.IsExpression(def) {
			c.handle(def, func() {
				switch in.Type {
				case "boolean":
					asBool(def, "input default")
				case "string", "choice", "environment":
					asString(def, "input default")
				}
			})
		}
	}
	if in.Type == "choice" {
		if !hasOptions || len(options) == 0 {
			c.ctx.ErrorAt(m, "Missing 'options' for choice input")
		}
	} else if hasOptions {
		c.ctx.ErrorAt(m, "Input type is not 'choice', but 'options' is defined")
	}
	return in
}

// filterNames are the pattern filters of an event, in the order they are
// checked and reported.
var filterNames = []string{"branches", "branches-ignore", "tags", "tags-ignore", "paths", "paths-ignore"}

// filterLists returns the lists of the six pattern filters by name.
func (e *Event) filterLists() map[string][]string {
	return map[string][]string{
		"branches": e.Branches, "branches-ignore": e.BranchesIgnore,
		"tags": e.Tags, "tags-ignore": e.TagsIgnore,
		"paths": e.Paths, "paths-ignore": e.PathsIgnore,
	}
}

// compiledFilters holds the compiled pattern lists of an event.
type compiledFilters struct {
	branches, branchesIgnore, tags, tagsIgnore, paths, pathsIgnore []*Pattern
}

func (e *Event) compileFilters() (*compiledFilters, map[string]error) {
	errs := map[string]error{}
	var cf compiledFilters
	dst := map[string]*[]*Pattern{
		"branches": &cf.branches, "branches-ignore": &cf.branchesIgnore,
		"tags": &cf.tags, "tags-ignore": &cf.tagsIgnore,
		"paths": &cf.paths, "paths-ignore": &cf.pathsIgnore,
	}
	for name, list := range e.filterLists() {
		l, err := compileList(list)
		if err != nil {
			errs[name] = err
		}
		*dst[name] = l
	}
	return &cf, errs
}

// filters returns the compiled filters: those made by Parse, or, for an
// Event built by hand, a fresh compilation (a pattern that does not compile
// never matches).
func (e *Event) filters() *compiledFilters {
	if e.cf != nil {
		return e.cf
	}
	cf, _ := e.compileFilters()
	return cf
}

// validateFilters checks the pattern filters of every event, as GitHub does
// and @actions/workflow-parser does not:
//
//   - a filter and its "-ignore" counterpart (branches and branches-ignore,
//     tags and tags-ignore, paths and paths-ignore) cannot be used together;
//   - a list made only of "!" patterns is an error: such a list can match
//     nothing, and GitHub requires at least one pattern without "!";
//   - every pattern must compile.
//
// The messages are ours. The compiled patterns are kept in the events.
//
// TODO(verify with recording): the text and position of GitHub's errors,
// whether they apply to every event that has these filters, and whether a
// "!" pattern is refused in the "-ignore" lists too.
func (c *converter) validateFilters() {
	if c.w.On == nil {
		return
	}
	for _, ev := range c.w.On.Events {
		lists := ev.filterLists()
		for _, pr := range [][2]string{{"branches", "branches-ignore"}, {"tags", "tags-ignore"}, {"paths", "paths-ignore"}} {
			if lists[pr[0]] != nil && lists[pr[1]] != nil {
				c.ctx.Errorf(ev.Token, "Event '%s' cannot use both '%s' and '%s'. Use only one of them.", ev.Name, pr[0], pr[1])
			}
		}
		cf, errs := ev.compileFilters()
		for _, name := range filterNames {
			list := lists[name]
			if err := errs[name]; err != nil {
				c.ctx.Errorf(ev.Token, "Event '%s' filter '%s': %v", ev.Name, name, err)
			}
			if onlyNegated(list) {
				c.ctx.Errorf(ev.Token, "Event '%s' filter '%s' has only patterns that start with '!'. Add at least one pattern without '!'.", ev.Name, name)
			}
		}
		ev.cf = cf
	}
}

// onlyNegated reports whether a non-empty list has no positive pattern.
func onlyNegated(patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, p := range patterns {
		if !strings.HasPrefix(p, "!") {
			return false
		}
	}
	return true
}
