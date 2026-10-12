# Running the runner's expression engine

`runner.csproj` compiles `src/Sdk/DTExpressions2` (the Expressions2 engine the
runner uses on github.com), its `ContextData` types and `DTLogging` from a clone
of https://github.com/actions/runner, replacing the few assemblies they need by
the stubs in `Stubs.cs`. `Program.cs` reads
`{"contexts": {...}, "cases": [{"e": "<expression>"}]}` and prints one JSON line
per case: `{"k": kind, "s": string form, "j": toJSON of the result}`, or
`{"err": {"t": "parse"|"eval", "m": message}}`.

    dotnet build -c Release expr/gen/runner -p:RunnerSrc=/path/to/runner/src/Sdk
    dotnet expr/gen/runner/bin/Release/net9.0/runner.dll input.json

It is how the C# column of `expr/PORTING.md` was established: the 34 765
expressions of `testdata/oracle.json.gz` were run through it and compared with
this package. Requires the .NET SDK; nothing in `go test` depends on it.
The stubs are not the runner's code: `JsonUtility.ToString` is
`JsonConvert.SerializeObject` and the error resources are copied texts.
