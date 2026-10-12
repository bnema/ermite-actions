// SPDX-License-Identifier: MIT

package workflow

import (
	"reflect"
	"testing"
)

// TestRecordedInvalidWorkflows parses the workflow files of the recorded
// scenarios expr-status-in-env, expr-status-in-run, invalid-needs and
// invalid-schema, which GitHub refused (a failed run without jobs), and checks
// the messages. The valid sibling of each scenario is in TestRecordedValid.
func TestRecordedInvalidWorkflows(t *testing.T) {
	const on = "on:\n  push:\n    branches: ['scn/**']\n"
	tests := []struct {
		name, src string
		want      []string
	}{
		{"expr-status-in-env", on + `jobs:
  main:
    runs-on: ubuntu-24.04
    steps:
      - name: status functions in env
        env:
          ST_SUCCESS: ${{ success() }}
          ST_FAILURE: ${{ failure() }}
          ST_ALWAYS: ${{ always() }}
          ST_CANCELLED: ${{ cancelled() }}
        run: env | grep '^ST_' | sort
`, []string{
			"w.yml (Line: 10, Col: 23): Unrecognized function: 'success'. Located at position 1 within expression: success()",
			"w.yml (Line: 11, Col: 23): Unrecognized function: 'failure'. Located at position 1 within expression: failure()",
			"w.yml (Line: 12, Col: 22): Unrecognized function: 'always'. Located at position 1 within expression: always()",
			"w.yml (Line: 13, Col: 25): Unrecognized function: 'cancelled'. Located at position 1 within expression: cancelled()",
		}},
		{"expr-status-in-run", on + `jobs:
  main:
    runs-on: ubuntu-24.04
    steps:
      - name: status functions in run
        run: |
          echo "success=${{ success() }}"
          echo "failure=${{ failure() }}"
`, []string{
			"w.yml (Line: 9, Col: 14): Unrecognized function: 'success'. Located at position 1 within expression: success()",
			"w.yml (Line: 9, Col: 14): Unrecognized function: 'failure'. Located at position 1 within expression: failure()",
		}},
		{"invalid-needs unknown-need", on + `jobs:
  main:
    needs: nowhere
    runs-on: ubuntu-24.04
    steps:
      - run: echo SHOULD-NOT-RUN
`, []string{"w.yml (Line: 5, Col: 3): The workflow must contain at least one job with no dependencies."}},
		{"invalid-needs unknown-need-with-root", on + `jobs:
  root:
    runs-on: ubuntu-24.04
    steps:
      - run: echo SHOULD-NOT-RUN
  dependent:
    needs: [root, nowhere]
    runs-on: ubuntu-24.04
    steps:
      - run: echo SHOULD-NOT-RUN
`, []string{"w.yml (Line: 10, Col: 19): Job 'dependent' depends on unknown job 'nowhere'."}},
		{"invalid-needs cycle", on + `jobs:
  a:
    needs: b
    runs-on: ubuntu-24.04
    steps:
      - run: echo SHOULD-NOT-RUN
  b:
    needs: a
    runs-on: ubuntu-24.04
    steps:
      - run: echo SHOULD-NOT-RUN
`, []string{"w.yml (Line: 5, Col: 3): The workflow must contain at least one job with no dependencies."}},
		{"invalid-needs cycle-with-root", on + `jobs:
  root:
    runs-on: ubuntu-24.04
    steps:
      - run: echo SHOULD-NOT-RUN
  a:
    needs: [root, b]
    runs-on: ubuntu-24.04
    steps:
      - run: echo SHOULD-NOT-RUN
  b:
    needs: a
    runs-on: ubuntu-24.04
    steps:
      - run: echo SHOULD-NOT-RUN
`, []string{
			"w.yml (Line: 10, Col: 19): Job 'a' depends on job 'b' which creates a cycle in the dependency graph.",
			"w.yml (Line: 15, Col: 12): Job 'b' depends on job 'a' which creates a cycle in the dependency graph.",
		}},
		{"invalid-schema unknown-key", on + `jobs:
  main:
    runs-on: ubuntu-24.04
    unknown-key: true
    steps:
      - run: echo SHOULD-NOT-RUN
`, []string{"w.yml (Line: 7, Col: 5): Unexpected value 'unknown-key'"}},
		{"invalid-schema no-runs-on", on + `jobs:
  main:
    steps:
      - run: echo SHOULD-NOT-RUN
`, []string{"w.yml (Line: 6, Col: 5): Required property is missing: runs-on"}},
		{"invalid-schema empty-step", on + `jobs:
  main:
    runs-on: ubuntu-24.04
    steps:
      - name: nothing to run
`, []string{"w.yml (Line: 8, Col: 9): There's not enough info to determine what you meant. Add one of these properties: run, shell, uses, with, working-directory"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := Parse("w.yml", []byte(tc.src))
			var got []string
			for _, e := range errs {
				got = append(got, e.Error())
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("errors:\n  got  %q\n  want %q", got, tc.want)
			}
		})
	}
}

// TestRecordedValid checks that the valid siblings of the invalid scenarios
// and the status functions in conditions, which GitHub accepted, parse.
func TestRecordedValid(t *testing.T) {
	for name, src := range map[string]string{
		"valid sibling":          "name: valid-sibling\non:\n  push:\n    branches: ['scn/**']\n    tags: ['scn-*']\njobs:\n  main:\n    runs-on: ubuntu-24.04\n    steps:\n      - run: echo valid-sibling-ran\n",
		"status functions in if": "on: push\njobs:\n  main:\n    runs-on: x\n    if: success() || failure()\n    steps:\n      - if: always() && !cancelled()\n        run: echo\n",
	} {
		if _, errs := Parse("w.yml", []byte(src)); len(errs) > 0 {
			t.Errorf("%s: %v", name, errs)
		}
	}
}
