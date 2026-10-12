// SPDX-License-Identifier: MIT

package workflow

import (
	"bnema.dev/ermite-actions/template"
)

// rootType is the schema definition a workflow file is read against; its
// "on" key is required.
const rootType = "workflow-root-strict"

// Parse reads, validates and converts a workflow file. path is only used in
// error messages, which read "path (Line: l, Col: c): message".
//
// The errors are the reference's: syntax errors, schema violations, unknown
// contexts and functions in expressions, and the checks of the converter (job
// and step ids, needs, filters, inputs). When there is any error the workflow
// is nil. As in the reference, the converter only runs when the file passes
// the schema. One check is not the reference's: an event that uses both a
// filter and its "-ignore" counterpart (branches and branches-ignore, tags and
// tags-ignore, paths and paths-ignore) is an error, as on GitHub.
func Parse(path string, content []byte) (*Workflow, []Error) {
	schema, err := template.WorkflowSchema()
	if err != nil {
		return nil, []Error{{Raw: "internal error: " + err.Error()}}
	}
	ctx := template.NewContext(schema)
	root := template.ReadYAML(ctx, rootType, path, content)
	w := &Workflow{File: path}
	if root != nil && !ctx.HasErrors() {
		c := &converter{ctx: ctx, schema: schema, w: w, jobIDs: newIDBuilder()}
		c.convertRoot(root)
		c.validateFilters()
	}
	errs := collect(ctx)
	if len(errs) > 0 {
		return nil, errs
	}
	return w, nil
}

func collect(ctx *template.Context) []Error {
	var out []Error
	for _, e := range ctx.Errors() {
		out = append(out, *e)
	}
	return out
}
