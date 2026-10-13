#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Regenerates the two files that are the answer of a reference implementation:
#   action/testdata/strict.json   (@actions/workflow-parser, from gen/package-lock.json)
#   action/testdata/runner.json   (the C# runner; needs RUNNER_CLONE, see gen/runner/build.sh)
# Run from anywhere; `git diff` afterwards must be empty unless the cases changed.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root/gen"
npm ci --ignore-scripts --no-audit --no-fund
cd "$root"
node --import ./gen/register.mjs action/gen/strict.mjs > action/testdata/strict.json
if [ -n "${RUNNER_CLONE:-}" ]; then
	action/gen/runner/build.sh
else
	echo "RUNNER_CLONE not set: runner.json left as it is" >&2
fi
