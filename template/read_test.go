// SPDX-License-Identifier: MIT

package template

import (
	"strings"
	"testing"
)

func readWorkflow(t testing.TB, src string) (Token, []string) {
	t.Helper()
	s, err := WorkflowSchema()
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewContext(s)
	tok := ReadYAML(ctx, "workflow-root-strict", "a.yml", []byte(src))
	return tok, ctx.Messages()
}

const head = "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n"

func TestReadErrors(t *testing.T) {
	// Expected messages come from @actions/workflow-parser 0.3.61 run on the
	// same inputs.
	tests := []struct {
		name, src string
		want      []string
	}{
		{"unknown context, block scalar", head + "      - run: |\n          echo hi\n          echo ${{ foo.bar }}\n",
			[]string{"a.yml (Line: 6, Col: 14): Unrecognized named-value: 'foo'"}},
		{"unknown context, plain", head + "      - run: echo ${{ foo.bar }} abc\n",
			[]string{"a.yml (Line: 6, Col: 14): Unrecognized named-value: 'foo'"}},
		{"unknown context, double quoted", head + "      - run: \"echo ${{ foo.bar }} abc\"\n",
			[]string{"a.yml (Line: 6, Col: 14): Unrecognized named-value: 'foo'"}},
		{"bad expression and insert in a name", head + "      - run: echo ${{ 1 +  }}\n      - name: ${{ insert }}\n        run: x\n",
			[]string{"a.yml (Line: 6, Col: 14): Unexpected symbol: '+'. Located at position 3 within expression: 1 +", "a.yml (Line: 7, Col: 15): Unexpected value '${{ insert }}'"}},
		{"not closed", head + "      - run: echo ${{ 1\n",
			[]string{"a.yml (Line: 6, Col: 14): The expression is not closed. An unescaped ${{ sequence was found, but the closing }} sequence was not found."}},
		{"empty document", "", []string{"a.yml: Expected a scalar value, a sequence, or a mapping"}},
		{"comment only", "# c\n", []string{"a.yml: Expected a scalar value, a sequence, or a mapping"}},
		{"sequence root", "[1,2]", []string{"a.yml (Line: 1, Col: 1): A sequence was not expected"}},
		{"duplicate key", "on: push\non: pull_request\njobs: {}", []string{"a.yml (Line: 2, Col: 1): 'on' is already defined"}},
		{"alias is resolved", "on: push\njobs:\n  a: &x\n    runs-on: x\n    steps: [{run: a}]\n  b: *x\n", nil},
		{"valid", head + "      - run: echo ${{ github.ref }}\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got := readWorkflow(t, tc.src)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("errors:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestReadSyntaxErrorsHaveNoTokens(t *testing.T) {
	for _, src := range []string{"on: [push\n", "a: b: c", "on: push\n---\nx: 1\n", "\t"} {
		tok, errs := readWorkflow(t, src)
		if tok != nil || len(errs) == 0 {
			t.Errorf("%q: tok=%v errs=%q", src, tok, errs)
		}
	}
}

func TestMixedStringBecomesFormat(t *testing.T) {
	tok, errs := readWorkflow(t, head+"      - run: it's ${{ github.ref }} {x} ${{ 1 }}\n")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	run := tok.(*Mapping).Find("jobs").(*Mapping).Find("a").(*Mapping).Find("steps").(*Sequence).Items[0].(*Mapping).Find("run")
	be, ok := run.(*BasicExpression)
	if !ok {
		t.Fatalf("run is %T", run)
	}
	if want := `format('it''s {0} {{x}} {1}', github.ref, 1)`; be.Expression != want {
		t.Errorf("expression = %s, want %s", be.Expression, want)
	}
	if len(be.Original) != 2 {
		t.Errorf("originals = %d", len(be.Original))
	}
}

func TestEscapedLiteralExpression(t *testing.T) {
	tok, errs := readWorkflow(t, head+"      - run: ${{ '{{ literal }}' }}\n")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	run := tok.(*Mapping).Find("jobs").(*Mapping).Find("a").(*Mapping).Find("steps").(*Sequence).Items[0].(*Mapping).Find("run")
	if s, ok := run.(*String); !ok || s.Value != "{{ literal }}" {
		t.Errorf("run = %#v", run)
	}
}

func TestScalarTypes(t *testing.T) {
	tok, errs := readWorkflow(t, "on: push\njobs:\n  a:\n    runs-on: x\n    timeout-minutes: 0x10\n    continue-on-error: yes\n    steps: [{run: 1}]\n")
	// yes is a string in YAML 1.2, so continue-on-error is rejected.
	if len(errs) == 0 || !strings.Contains(errs[0], "Unexpected value 'yes'") {
		t.Errorf("errs = %q", errs)
	}
	job := tok.(*Mapping).Find("jobs").(*Mapping).Find("a").(*Mapping)
	if n, ok := job.Find("timeout-minutes").(*Number); !ok || n.Value != 16 {
		t.Errorf("timeout-minutes = %#v", job.Find("timeout-minutes"))
	}
	step := job.Find("steps").(*Sequence).Items[0].(*Mapping)
	if s, ok := step.Find("run").(*String); !ok || s.Value != "1" {
		t.Errorf("run = %#v", step.Find("run"))
	}
}

func TestAliasBombIsRejected(t *testing.T) {
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 8; i++ {
		b.WriteString("a")
		b.WriteString(string(rune('0' + i)))
		b.WriteString(": &a")
		b.WriteString(string(rune('0' + i)))
		b.WriteString(" [")
		for j := 0; j < 10; j++ {
			b.WriteString("*a")
			b.WriteString(string(rune('0' + i - 1)))
			b.WriteString(",")
		}
		b.WriteString("]\n")
	}
	_, errs := readWorkflow(t, b.String())
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, "\n"), "too large") {
		t.Errorf("errs = %q", errs)
	}
}
