// SPDX-License-Identifier: MIT

// Package actions is the root of ermite-actions, an implementation of the
// GitHub Actions workflow language in Go: expressions (package expr),
// template tokens and the workflow schema (package template), and the
// workflow model with matrices, job graphs and trigger filters (package
// workflow), and action.yml manifests and "uses:" values (package action).
// ermite plans runs with it and ermite-runner executes jobs with it, so both
// evaluate workflows the same way.
//
// The target is GitHub's exact behaviour. Semantics come from GitHub's MIT
// sources (actions/languageservices, actions/runner) and from recordings of
// runs on github.com.
package actions
