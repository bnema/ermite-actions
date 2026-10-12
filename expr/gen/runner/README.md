# Running the runner's expression engine

`runner.csproj` compiles `src/Sdk/DTExpressions2` (the Expressions2 engine the
runner uses on github.com), its `ContextData` types and `DTLogging` from a clone
of https://github.com/actions/runner, replacing the few assemblies they need by
the stubs in `Stubs.cs`. `Program.cs` reads
`{"contexts": {...}, "cases": [{"e": "<expression>"}]}` and prints one JSON line
per case: `{"k": kind, "s": string form, "j": toJSON of the result}`, or
`{"err": {"t": "parse"|"eval", "m": message}}`.

Requirements (checked, not assumed):

- a clone of https://github.com/actions/runner at the commit in
  `runner-commit.txt` (`5f0b3ddcc6ee9fe9441e00d55e85b94ea4aad798`), clean in
  `src/Sdk`. `RunnerSrc` has no default: the build fails without it.
- the .NET SDK (the project targets `net9.0`) and a **9.0.x runtime**
  (`dotnet --list-runtimes`): number formatting and case folding come from the
  runtime, and `env.mjs` refuses to run on another line. No `global.json` pins
  the SDK because any SDK that can build `net9.0` works (the author's machine
  has SDK 10 with the 9.0 runtime); the runtime is what matters and is asserted.

Build and run:

    dotnet build -c Release expr/gen/runner -p:RunnerSrc=$RUNNER_CLONE/src/Sdk
    RUNNER_CLONE=<clone> node expr/gen/runner/answers.mjs | gzip -9 -n > expr/testdata/runner.json.gz

The other scripts (`goldens.mjs`, `ordinal_cases.mjs`, `number_cases.mjs`,
`fromjson_cases.mjs`) use the same environment (`env.mjs`); `runner.dll
input.json` can also be run by hand. `TestRunnerCommit` checks that the
recorded data, `runner-commit.txt` and `PORTING.md` name the same commit.

It is how the C# column of `expr/PORTING.md` was established: the 34 765
expressions of `testdata/oracle.json.gz` were run through it and compared with
this package. Requires the .NET SDK; nothing in `go test` depends on it.
The stubs are not the runner's code: `JsonUtility.ToString` is
`JsonConvert.SerializeObject` and the error resources are copied texts.
