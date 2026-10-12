// SPDX-License-Identifier: MIT

package expr_test

import (
	"context"
	"errors"
	"fmt"

	"bnema.dev/ermite-actions/expr"
)

func ExampleParse() {
	node, err := expr.Parse("contains(github.ref, 'main') && !failure()",
		[]string{"github"},
		[]expr.FuncInfo{{Name: "failure", MinArgs: 0, MaxArgs: 0}})
	fmt.Println(node != nil, err)

	// Unknown names and wrong argument counts are rejected when parsing,
	// with the messages of GitHub's implementation.
	for _, src := range []string{"secrets.TOKEN", "contains('a')", "github.ref ==", "nope()"} {
		_, err := expr.Parse(src, []string{"github"}, nil)
		var pe *expr.Error
		if errors.As(err, &pe) {
			fmt.Printf("%s -> %s (line %d, column %d)\n", src, pe.Message, pe.Pos.Line, pe.Pos.Column)
		}
	}
	// Output:
	// true <nil>
	// secrets.TOKEN -> Unrecognized named-value: 'secrets' (line 0, column 0)
	// contains('a') -> Too few parameters supplied: 'contains' (line 0, column 0)
	// github.ref == -> Unexpected end of expression: '==' (line 0, column 11)
	// nope() -> Unrecognized function: 'nope' (line 0, column 0)
}

func ExampleEvaluate() {
	event, err := expr.FromJSON([]byte(`{"ref": "refs/heads/main", "labels": [{"name": "Bug"}, {"name": "docs"}]}`))
	if err != nil {
		panic(err)
	}
	ec := expr.EvalContext{
		Contexts: map[string]expr.Value{"github": event},
		Funcs: map[string]expr.Func{
			"always": func(context.Context, []expr.Value) (expr.Value, error) { return expr.Boolean(true), nil },
		},
	}
	funcs := []expr.FuncInfo{{Name: "always", MinArgs: 0, MaxArgs: 0}}

	for _, src := range []string{
		"github.ref == 'refs/heads/main' && 'release' || 'dev'",
		"contains(github.labels.*.name, 'bug')",
		"join(github.labels.*.name, ', ')",
		"format('{0} has {1} labels', github.ref, 2)",
		"always() && github.missing",
		"fromJSON('{')",
	} {
		node, err := expr.Parse(src, []string{"github"}, funcs)
		if err != nil {
			panic(err)
		}
		v, err := expr.Evaluate(context.Background(), node, ec)
		if err != nil {
			fmt.Printf("%s -> error: %v\n", src, err)
			continue
		}
		fmt.Printf("%s -> %s (truthy: %t)\n", src, expr.CoerceString(v), expr.Truthy(v))
	}
	// Output:
	// github.ref == 'refs/heads/main' && 'release' || 'dev' -> release (truthy: true)
	// contains(github.labels.*.name, 'bug') -> true (truthy: true)
	// join(github.labels.*.name, ', ') -> Bug, docs (truthy: true)
	// format('{0} has {1} labels', github.ref, 2) -> refs/heads/main has 2 labels (truthy: true)
	// always() && github.missing ->  (truthy: false)
	// fromJSON('{') -> error: Error parsing JSON when evaluating fromJson
}

func ExampleToJSON() {
	d := expr.NewDictionary()
	d.Add("name", expr.String("octo"))
	d.Add("tags", expr.NewArray(expr.Number(1), expr.Null{}, expr.Number(0.5)))
	fmt.Println(expr.ToJSON(d))
	// Output:
	// {
	//   "name": "octo",
	//   "tags": [
	//     1,
	//     null,
	//     0.5
	//   ]
	// }
}
