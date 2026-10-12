// SPDX-License-Identifier: MIT

package template

import "testing"

func FuzzReadYAML(f *testing.F) {
	for _, s := range []string{
		"on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: echo ${{ github.ref }}\n",
		"a: &x [1, *x]\n",
		"on: [push\n",
		"${{ insert }}: 1\n",
		"x: ${{ '${{' }}\n",
		"\x00",
		"a: !!str 1\nb: !!int x\n",
		"? [a]\n: b\n",
		"on: push\njobs:\n  a:\n    runs-on: ${{ matrix.os }}\n    strategy:\n      matrix:\n        ${{ insert }}: ${{ fromJSON(needs.x.outputs.y) }}\n",
	} {
		f.Add(s)
	}
	schema, err := WorkflowSchema()
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, src string) {
		ctx := NewContext(schema)
		ReadYAML(ctx, "workflow-root-strict", "f.yml", []byte(src))
		for _, e := range ctx.Errors() {
			_ = e.Error()
		}
	})
}
