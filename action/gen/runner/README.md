# The runner's action.yml loader, as an oracle

`Driver.cs` is `ActionManifestManagerLegacy.Load`, `ConvertRuns` and
`ConvertInputs` of actions/runner (the loader the runner runs; see
`../../PORTING.md`) with what needs an `IExecutionContext` removed, writing its
result as JSON. `build.sh` copies `src/Sdk` of a clone at the commit in
`runner-commit.txt` to a temporary directory, adds the driver and builds it.

    RUNNER_CLONE=~/dev/clones/runner action/gen/runner/build.sh

It writes `testdata/runner.json` (the files of `testdata/cases`) and
`testdata/uses.json` (each line of `testdata/uses.txt` as the `uses` of a
one-step composite action).

Needs the .NET SDK (any version that builds `net8.0`; the output was made with
SDK 10 and the 9.0 runtime through `DOTNET_ROLL_FORWARD=Major`), network access
for NuGet, and a clone that is clean in `src/Sdk`. The driver prints the message
of each recorded error without its stack trace.
