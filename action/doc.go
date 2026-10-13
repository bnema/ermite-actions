// SPDX-License-Identifier: MIT

// Package action reads GitHub Actions action.yml manifests the way GitHub
// does: the model it returns is what the runner's ActionManifestManager gets
// out of the same file, with the same errors.
//
// [Parse] follows the C# runner (actions/runner, Runner.Worker/
// ActionManifestManagerLegacy.cs, which is what ActionManifestManagerWrapper
// runs; the newer ActionManifestManager only runs next to it, behind a
// comparison flag, and its result is thrown away). The runner reads the file
// with its own schema, action_yaml.json, which is the "action-root" definition
// of the schema that this package vendors, action-v1.0.json of the npm package
// @actions/workflow-parser 0.3.61 (MIT, GitHub, actions/languageservices): that
// definition accepts unknown top-level keys (icon, color, branding...), needs
// no name and no description, and checks the keys under "runs" and the
// contexts that expressions may use. [ParseStrict] reads the file against the
// same schema's "action-root-strict" definition instead, which is the language
// service's linter: name, description and runs are required, branding and
// "using" are checked and unknown top-level keys are refused. GitHub accepts
// files that the strict form refuses; use it to lint, not to run.
//
// The steps of a composite action are [workflow.Step] values, converted the way
// the runner converts them (which is not the way package workflow converts the
// steps of a workflow file). Expressions stay unevaluated [template.Token]
// values; package template evaluates them.
//
// Errors read like the runner's: positioned messages ("action.yml (Line: 3,
// Col: 5): Unexpected value 'x'") and, after them, the exceptions that the
// runner reports with their .NET class name ("System.ArgumentNullException:
// ..."). The runner prints exception stack traces after them; those are not
// reproduced. PORTING.md lists what differs from the runner and from the
// language service, and what was checked against which source.
package action
