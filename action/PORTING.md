# Porting notes for package action

`Parse` reads an `action.yml` the way **the C# runner** does when it loads an
action (actions/runner at `5f0b3ddcc6ee9fe9441e00d55e85b94ea4aad798`), with the
vendored schema of `@actions/workflow-parser` 0.3.61 (MIT, GitHub,
actions/languageservices). `Lint` reproduces what that npm package reports.
Nothing here was recorded on github.com: phase A of the CI-2 plan has not
produced recordings of action manifests yet. What is checked is the runner's
own code, run by `gen/runner`, and the npm package's own code, run by
`gen/strict.mjs`.

## Which loader the runner runs

`ActionManifestManagerWrapper.Load` (the only caller) runs
`ActionManifestManagerLegacy` and returns its result. The newer
`ActionManifestManager` (on `Sdk/WorkflowParser`, the code the npm package is
a port of) runs next to it only when the feature flag
`actions_runner_compare_workflow_parser` or the environment variable
`ACTIONS_RUNNER_COMPARE_WORKFLOW_PARSER` is set, and its result is thrown away
(`EvaluateAndCompare`). So `Parse` follows the **legacy** loader and the legacy
converter `PipelineTemplateConverter.ConvertToSteps`. The two loaders agree on
most files and differ on a few (below); the new one is not reproduced.

## Checks

- `testdata/runner.json`: the answer of the runner's loader on every file of
  `testdata/cases` (299 files). `gen/runner/Driver.cs` is
  `ActionManifestManagerLegacy.Load/ConvertRuns/ConvertInputs` with what needs
  an `IExecutionContext` removed (tracing, the file table, expression values:
  none of it changes loading), compiled into a copy of `src/Sdk` with
  `gen/runner/build.sh`, which refuses a clone at another commit or with
  changes. `TestAgainstRunner` compares the error list (text and order) and the
  model (inputs and defaults, deprecations, every field of `runs`, every step's
  id, condition, name, env, with, shell, working directory, reference, and the
  outputs). The only thing not compared word for word is YamlDotNet's wording of
  YAML syntax errors (`While parsing a node, ...`), where yaml.v3's wording is
  used; only the shape is compared.
- `testdata/uses.json`: the same driver on 68 `uses:` values; `ParseUses` is
  compared on error text and on the reference the runner builds
  (`TestParseUsesAgainstRunner`).
- `testdata/strict.json`: `parseAction` + `convertActionTemplate` of the npm
  package on every case; `Lint` is compared on the error list
  (`TestLintAgainstReference`). One file is excused with its reason
  (`knownLintDifferences`).
- `FuzzParse` (no panic, an action iff no error, at most ten errors, every step
  has an id and a condition) and `FuzzParseUses`.

Regenerate with `gen/regen.sh` (`RUNNER_CLONE=<clone>` for the runner part); the
diff must be empty. The cases are: the 24 files of the runner's
`src/Test/TestData` that are action manifests (`runner_*`), the 96 manifests
inlined in the language service tests (`ls_*`: `action-parser.test.ts`,
`validate-action.test.ts`), and 179 written for this package. The
language service's own expectations (`toBeGreaterThan(0)`) are not ported: the
cases are replayed through the reference instead, which gives the messages.

## Model

`Action` has `Name`, `Description`, `Author`, ordered `Inputs` (with
`Description`, `Required`, `Default` as a token, `HasDefault`,
`DeprecationMessage`), ordered `Outputs` (with `Value`), and `Runs`:
`Kind` (`RunsNode`, `RunsDocker`, `RunsComposite`, `RunsPlugin`), `Using`
(lower case), node `Main/Pre/Post`, docker `Image/Entrypoint/PreEntrypoint/
PostEntrypoint/Args/Env`, `PreIf/PostIf`, composite `Steps` (`[]*workflow.Step`),
`Plugin`. Conditions default to `always()` for node and docker actions
(`ActionManifestManagerLegacy.cs`, `preIfToken?.Value ?? "always()"`); they are
kept as written, without `success() &&` (the schema's own description of
`pre-if` says the same). A composite action's conditions are `always()`.

Steps are `workflow.Step`, with two additions to that type:
`EnvExpression` and `WithExpression` hold `env: ${{ fromJSON(...) }}` and
`with: ${{ ... }}`, which the runner accepts on a step of an action and the
workflow converter does not. `Step.If` is the runner's: `success()` when empty,
`success() && (cond)` when the condition has no status function; the contexts
of `if` are the schema's `step-if` plus `vars` and `needs`, which the runner's
converter adds (`s_stepNamedValues`) and the schema lacks.

## What the runner does that the other readers do not

All verified by `TestAgainstRunner`.

- **Schema.** The runner reads `action-root` (all keys but `name`,
  `description`, `inputs`, `outputs`, `runs` are accepted, nothing is
  required); the language service reads `action-root-strict`. `using` is any
  non-empty string for the schema and compared with `OrdinalIgnoreCase` by the
  loader: `Node20` and `DOCKER` run on GitHub, the strict schema refuses them.
  `branding` is not read at all: `branding: {icon: nope}` loads.
- **Errors are exceptions.** After the positioned errors, the exception that
  ended the load is printed as `Exception.ToString()` (class name, `.NET`
  message format, then a stack trace that is not reproduced): `using: node99`
  is `System.ArgumentOutOfRangeException: Specified argument was out of the
  range of valid values. (Parameter ''using: node99' is not supported, use
  'docker', 'node12', 'node16', 'node20' or 'node24' instead.')`. `Error.Exception`
  holds the class; `Error.Error()` formats it. At most ten errors are kept;
  `Failed to load <path>` is the runner's final message (`LoadFailed`).
- **Missing runs, using, main, image, steps** each have their text
  (`Top level 'runs:' section is required for ...`, `Missing 'using' value. ...`,
  `You are using a JavaScript Action but there is not an entry JavaScript file
  provided in ...`, `... Container Action but an image is not provided in ...`,
  `... composite action but there are no steps provided in ...`), after the
  schema's own errors when it has some (`There's not enough info to determine
  what you meant. Add one of these properties: ...`).
- **YAML.** Anchors and aliases are refused (`Anchors are not currently
  supported. Remove the anchor 'x'`); an explicit tag other than `!!bool`,
  `!!float`, `!!int`, `!!null` is `Unexpected tag '...'` (the runner's string tag
  is `tag:yaml.org,2002:string`, which no document spells, so `!!str` fails);
  a tagged quoted scalar, an out-of-range tagged number and a plain `0x`/`0o`
  integer over 32 bits (`Int32.TryParse`) have their messages; a second document
  is `Expected stream end parse event`, an empty file `Expected document start
  parse event`, a collection used as a key `Expected mapping end`. Such an error
  stops the read where the runner meets it, so errors found earlier in the
  document stay (`template.Context.RunnerYAML`).
- **Depth.** `TemplateMemory(maxDepth: 100)`: `Maximum object depth exceeded`
  from the 100th nested collection, the root mapping being the first. Skipped
  values (unknown keys) count (`template.Context.MaxDepth`).
- **Numbers as strings.** A number read where a string is wanted shows as .NET's
  `G15` (`1E+21`, `0.3` for `0.30000000000000004`, `-0`), not as JavaScript's
  (`template.DotNetNumberString`).
- **Steps** (legacy `ConvertToSteps`): the first token of the wrong type ends the
  conversion (`Unexpected type 'StringToken' encountered while reading 'steps'.
  ...`) instead of being recorded and skipped; an unknown key in a step is
  `Error while reading 'steps item key'. Unexpected value 'k'`; the id check
  (`ReferenceNameBuilder`) accepts ids that start with `__`, lets an id of 100
  characters or more through, compares ids with OrdinalIgnoreCase, and words its
  message `... IDs must start with a letter or '_' and must be less than 100
  characters.` (the language service has `and and`); generated ids are
  `__run`, `__owner_repo`, `__<image>`, `__self`, `__selfRepository`, made unique
  with `_2`, `_3`, ... A step whose `uses` is malformed is recorded as an error
  and **kept**, which makes the load fail later with
  `System.NullReferenceException` if it has no id (the runner dereferences its
  reference); a `$/x@ref` step is dropped instead.
- **`uses`.** `Expected format {org}/{repo}[/path]@ref. Actual 'x'` and
  `Self-repository references do not support an '@ref' suffix. Actual '$/x@v1'`.
  Segments are split on `/` and `\` and empty ones dropped, so `/a/b@c` and
  `a//b@c` are `a/b@c`; `docker://` is case-sensitive (`Docker://x` is an error)
  although the image of a docker *action* is not (`ParseImage`).

## Where the sources disagree

1. **Runner (legacy) / runner (new) / language service**, on the same file:
   - Step id text: legacy `... '_' and must be ...`; new loader adds
     `IDs starting with '__' are reserved.` and refuses them; language service
     has `and and` and the reserved check too. `Parse` follows legacy (what runs).
   - `$/` steps: legacy generates `__selfRepository`; new generates the same;
     language service `__self` (`createActionStepId`).
   - Conditions of `runs.pre-if`, `post-if`: the runner reads them as plain
     strings and never parses them when loading (an unknown context fails at run
     time); the language service parses them with the contexts of `runs-if`
     (`Unrecognized named-value: 'foo'`). `pre-if: ${{ always() }}` makes the
     runner fail (`Unrecognized function: 'always'`, the schema type there is
     `non-empty-string`, which allows no context), while the language service
     accepts the schema and flags the markers in its own pass.
2. **Schema vs runner's `action_yaml.json`.** Equal in meaning for every
   definition `Parse` reads (`action-root` and below); the vendored file adds
   `-strict` variants, `branding`, `using`, `runs-if` and descriptions, orders
   the `runs` alternatives differently and writes `{"type": x}` where the
   runner writes `x`. Checked by diffing the two after removing descriptions.
3. **Schema vs plan/task text.** The task lists the composite step keys as
   `uses, run, shell, with, env, name, id, if, working-directory,
   continue-on-error`. The schema: a `run` step takes `name, id, if, run, shell,
   env, continue-on-error, working-directory`; a `uses` step takes `name, id, if,
   uses, with, env, continue-on-error`. `shell` and `working-directory` on a
   `uses` step and `with` on a `run` step are refused; so is `timeout-minutes`,
   although the runner's converter would read it.
4. **`required` inputs: the plan vs the runner.** The CI-2 plan says "required
   inputs warned like GitHub". The runner has no such warning for an action:
   `ActionRunner.cs` warns `Unexpected input(s) 'x', valid inputs are ['a', 'b']`
   (only for actions that have a manifest, `entryPoint` and `args` being valid
   for docker actions) and `Input 'x' has been deprecated with message: ...`,
   and `inputs.<id>.required` is not read by the loader nor checked later (a
   `grep` of `Runner.Worker`, `Runner.Common`, `Sdk/WorkflowParser` and
   `Sdk/DTPipelines` finds `required` checks only for reusable workflow inputs
   and secrets, `WorkflowTemplateConverter.cs:2166,2223`). `Missing required
   input` is the language service's editor message
   (`validate-action-reference.ts`: `Missing required input \`x\`` / `Missing
   required inputs: \`x\`, \`y\``), shown when no `with:` gives the input and it
   has no default. `Input.Required` is parsed so that an editor can do the same;
   the executor should not warn.
5. **Language service TypeScript vs its own schema.** `convertRuns` falls back to
   an empty composite when `using` or `main` is missing; the model here follows
   the runner (an error instead).
6. **`uses` messages.** Runner `{org}/{repo}[/path]@ref`; language service
   `{owner}/{repo}[/path]@{ref}`, `'uses' value in action cannot be blank`, and
   it rejects `/repo@v4` where the runner accepts `/a/b@c`. `ParseUses` is the
   runner's.

## Differences from the runner

- **Stack traces** are not reproduced.
- **YAML syntax errors** have yaml.v3's wording (`file.yml: did not find expected
  key at line 5`); YamlDotNet's is `file.yml: (Line: 6, Col: 34, Idx: 122) -
  (Line: 6, Col: 44, Idx: 132): While parsing a block mapping, ...`.
- **Limits that the runner does not state**: a manifest over `template.MaxFileBytes`
  (1 MiB) and a document that expands to more than `template.MaxNodes` nodes are
  refused before reading. The runner reads the file with
  `TemplateMemory(maxBytes: 10 MiB, maxEvents: 1 000 000)`; those two are not
  reproduced.
- **Not ported**: what happens after loading (`ActionManager`: choosing between
  `action.yml`, `action.yaml` and `Dockerfile`, `Can't find 'action.yml',
  'action.yaml' or 'Dockerfile'`, the `Dockerfile` image check `ParseImage`
  reproduces, input defaults evaluated at run time, `Input '<n>' has been
  deprecated with message: ...`, composite depth). `ParseImage` and
  `Input.DeprecationMessage` carry what is needed.
- **Expression errors** come from package `expr`, which follows the runner's
  expression engine (`... Located at position N within expression: ...`). `Lint`
  strips nothing: the language service's expression messages lack that suffix, so
  `TestLintAgainstReference` strips it from ours before comparing.
- **`Lint` ordering of schema errors** for files with several (`Required property
  is missing` after `Unexpected value`) is the reference's and is replayed.
- A mapping key that is a collection: the reference reads `undefined`
  (`Unexpected value 'undefined'`); `Lint` reports `Unexpected value ''`
  (`knownLintDifferences`). `Parse` is the runner's (`Expected mapping end`).

## To verify against recordings

- [ ] What github.com prints for a manifest that fails to load (the runner's
      messages are the ones above; whether the hosted service wraps or reorders
      them is unknown).
- [ ] That the hosted runners run the legacy loader (the flag
      `actions_runner_compare_workflow_parser` may be set there; its result is
      still discarded in this commit).
- [ ] `using: node12`/`node16` after the migration to node20 (`HandlerFactory`),
      which belongs to the executor.
