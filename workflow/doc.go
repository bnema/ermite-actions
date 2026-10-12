// SPDX-License-Identifier: MIT

// Package workflow parses GitHub Actions workflow files into a model and
// implements the planning rules that depend only on the file: validation,
// job matrices, push trigger filters and skip directives in commit messages.
//
// Parse is a port of the converter of the npm package
// @actions/workflow-parser 0.3.61 (MIT, GitHub, actions/languageservices) on
// top of package template. Values that GitHub computes later, such as
// runs-on, strategy, names and conditions, stay unevaluated template tokens
// in the model; package template evaluates them.
//
// The trigger rules (MatchPush, SkipCI) and the matrix expansion
// (ExpandMatrix) are written from GitHub's documentation and from the
// behaviour of the official runner; PORTING.md lists what to verify against
// recordings of github.com.
package workflow
