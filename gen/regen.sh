#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Regenerates every file that is the answer of a reference implementation:
#   workflow/testdata/convert.json   (@actions/workflow-parser)
#   template/testdata/scalars.json   (yaml)
# from the exact versions pinned in gen/package-lock.json, installed with
# `npm ci`. Run from anywhere; `git diff` afterwards must be empty unless the
# pins or the corpus changed. (expr/gen has its own generators, run the same
# way with `--import ./gen/register.mjs` and the dist path of
# gen/node_modules/@actions/expressions.)
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root/gen"
npm ci --ignore-scripts --no-audit --no-fund
cd "$root"
node --import ./gen/register.mjs workflow/gen/convert.mjs > workflow/testdata/convert.json
node --import ./gen/register.mjs template/gen/scalars.mjs > template/testdata/scalars.json
