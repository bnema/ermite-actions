#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Builds the runner-oracle for package action and runs it on the files in
# ../../testdata/cases, writing ../../testdata/runner.json, and on each line of
# ../../testdata/uses.txt as the "uses" of a one-step composite action, writing
# ../../testdata/uses.json.
#
#   RUNNER_CLONE=~/dev/clones/runner action/gen/runner/build.sh
#
# Requirements: a clone of https://github.com/actions/runner at the commit in
# runner-commit.txt (checked), the .NET SDK, network access for NuGet
# (restoring the runner's packages), and a .NET runtime of version 8 or newer
# (the Sdk project targets net8.0; DOTNET_ROLL_FORWARD=Major runs it on a newer
# runtime). The clone is copied to a temporary directory, never modified.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
: "${RUNNER_CLONE:?set RUNNER_CLONE to a clone of actions/runner}"
want=$(cat "$here/runner-commit.txt")
have=$(git -C "$RUNNER_CLONE" rev-parse HEAD)
[ "$have" = "$want" ] || { echo "runner clone is at $have, want $want" >&2; exit 1; }
git -C "$RUNNER_CLONE" diff --quiet -- src/Sdk src/Runner.Worker/action_yaml.json || { echo "runner clone is modified" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp -r "$RUNNER_CLONE/src/Sdk" "$work/Sdk"
cp "$RUNNER_CLONE/src/Directory.Build.props" "$RUNNER_CLONE/src/NuGet.Config" "$work/" 2>/dev/null || true
cp "$here/Driver.cs" "$work/Sdk/Driver.cs"
(cd "$work/Sdk" && dotnet build Sdk.csproj -c Release -p:OutputType=Exe -p:RuntimeIdentifiers= -v:q -nologo >&2)
out="$here/../../testdata/runner.json"
DOTNET_ROLL_FORWARD=Major dotnet "$work/Sdk/bin/Release/net8.0/Sdk.dll" \
	"$RUNNER_CLONE/src/Runner.Worker/action_yaml.json" "$here/../../testdata/cases" > "$out"
echo "wrote $out" >&2
out="$here/../../testdata/uses.json"
DOTNET_ROLL_FORWARD=Major dotnet "$work/Sdk/bin/Release/net8.0/Sdk.dll" \
	"$RUNNER_CLONE/src/Runner.Worker/action_yaml.json" "$here/../../testdata/cases" "$here/../../testdata/uses.txt" > "$out"
echo "wrote $out" >&2
