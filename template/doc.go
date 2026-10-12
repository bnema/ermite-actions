// SPDX-License-Identifier: MIT

// Package template reads, validates and evaluates GitHub Actions workflow
// templates: YAML documents whose strings may contain ${{ }} expressions.
//
// It is a port of the template layer of the npm package
// @actions/workflow-parser 0.3.61 (MIT, GitHub, actions/languageservices):
// the same tokens, the same reading rules and, where feasible, the same error
// messages. The workflow schema (schema/workflow-v1.0.min.json) is vendored
// unmodified and drives both validation and the contexts and functions that
// each key may use in its expressions.
//
// [ReadYAML] turns a document into a tree of [Token] values, checked against
// a [Schema]; problems are collected in a [Context] as [Error] values. A
// string holding expressions becomes a [*BasicExpression] (a single ${{ }}) or
// a format() call that joins the literal text and the expressions; ${{ insert }}
// becomes an [*InsertExpression]. [Evaluate] later computes a token against
// named contexts and functions, using package expr.
//
// Differences from the reference are listed in PORTING.md.
package template
