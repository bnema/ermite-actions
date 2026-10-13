// SPDX-License-Identifier: MIT
//
// Driver that runs the C# runner's action.yml loader on a directory of files.
// It is compiled into a copy of actions/runner src/Sdk (see build.sh), so that
// it can use the internal types the runner uses.
//
// Load, ConvertRuns and ConvertInputs below are
// Runner.Worker/ActionManifestManagerLegacy.cs at the commit in
// runner-commit.txt, with what needs an IExecutionContext removed (tracing,
// the file table, expression values: none of it influences loading) and the
// result written to JSON instead of ActionDefinitionData. Nothing else was
// changed: the flow of exceptions and errors is the runner's, because it
// decides which messages a user sees.
//
// Why the legacy loader: ActionManifestManagerWrapper.Load runs
// ActionManifestManagerLegacy and returns its result; the newer
// ActionManifestManager only runs next to it when the feature flag
// actions_runner_compare_workflow_parser or the environment variable
// ACTIONS_RUNNER_COMPARE_WORKFLOW_PARSER is set, and its result is discarded.

using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using GitHub.DistributedTask.ObjectTemplating;
using GitHub.DistributedTask.ObjectTemplating.Schema;
using GitHub.DistributedTask.ObjectTemplating.Tokens;
using GitHub.DistributedTask.Pipelines;
using GitHub.DistributedTask.Pipelines.ObjectTemplating;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;

namespace ActionOracle
{
    public static class Program
    {
        public static int Main(string[] args)
        {
            if (args.Length != 2 && args.Length != 3)
            {
                Console.Error.WriteLine("usage: Sdk.dll <action_yaml.json> <dir with *.yml> [<file with one uses value per line>]");
                return 2;
            }

            var schemaJson = File.ReadAllText(args[0]);
            var schema = TemplateSchema.Load(new JsonObjectReader(null, schemaJson));
            if (args.Length == 3)
            {
                // One-step composite action per "uses" value (with an id, so that
                // a value of the wrong form is reported and nothing else fails):
                // the answer is the error list and the reference the runner made.
                var uses = new JObject();
                foreach (var line in File.ReadAllLines(args[2]))
                {
                    var yaml = "name: a\ndescription: d\nruns:\n  using: composite\n  steps:\n    - uses: " + JsonConvert.ToString(line) + "\n      id: x\n";
                    uses[line] = Load(schema, "u.yml", yaml);
                }
                Console.Out.Write(uses.ToString(Formatting.Indented));
                Console.Out.WriteLine();
                return 0;
            }

            var result = new JObject();
            foreach (var file in Directory.GetFiles(args[1], "*.yml").OrderBy(x => x, StringComparer.Ordinal))
            {
                var name = Path.GetFileName(file);
                result[name] = Load(schema, name, File.ReadAllText(file));
            }

            Console.Out.Write(result.ToString(Formatting.Indented));
            Console.Out.WriteLine();
            return 0;
        }

        static JObject Load(TemplateSchema schema, string fileRelativePath, string fileContent)
        {
            var templateContext = new TemplateContext
            {
                CancellationToken = System.Threading.CancellationToken.None,
                Errors = new TemplateValidationErrors(10, int.MaxValue),
                Memory = new TemplateMemory(maxDepth: 100, maxEvents: 1000000, maxBytes: 10 * 1024 * 1024),
                Schema = schema,
                TraceWriter = new EmptyTraceWriter(),
            };

            var summary = new JObject();
            JObject execution = null;
            var inputs = new JArray();
            var deprecated = new JObject();

            try
            {
                var token = default(TemplateToken);
                var fileId = templateContext.GetFileId(fileRelativePath);
                using (var stringReader = new StringReader(fileContent))
                {
                    var yamlObjectReader = new YamlObjectReader(fileId, stringReader);
                    token = TemplateReader.Read(templateContext, "action-root", yamlObjectReader, fileId, out _);
                }

                var actionMapping = token.AssertMapping("action manifest root");
                var actionOutputs = default(MappingToken);
                var actionRunValueToken = default(TemplateToken);

                foreach (var actionPair in actionMapping)
                {
                    var propertyName = actionPair.Key.AssertString($"action.yml property key");

                    switch (propertyName.Value)
                    {
                        case "name":
                            summary["name"] = actionPair.Value.AssertString("name").Value;
                            break;
                        case "outputs":
                            actionOutputs = actionPair.Value.AssertMapping("outputs");
                            break;
                        case "description":
                            summary["description"] = actionPair.Value.AssertString("description").Value;
                            break;
                        case "inputs":
                            ConvertInputs(actionPair.Value, inputs, deprecated);
                            break;
                        case "runs":
                            actionRunValueToken = actionPair.Value;
                            break;
                        default:
                            break;
                    }
                }

                if (actionRunValueToken != null)
                {
                    execution = ConvertRuns(templateContext, actionRunValueToken, fileRelativePath, actionOutputs);
                }
            }
            catch (Exception ex)
            {
                templateContext.Errors.Add(ex);
            }

            var errors = new JArray();
            foreach (var error in templateContext.Errors)
            {
                // ex.ToString() ends with the stack trace, which names temporary
                // paths and line numbers of this driver: keep the message only.
                var message = error.Message;
                var at = message.IndexOf("\n   at ", StringComparison.Ordinal);
                errors.Add(at >= 0 ? message.Substring(0, at) : message);
            }

            if (errors.Count > 0)
            {
                summary["errors"] = errors;
                summary["failed"] = $"Failed to load {fileRelativePath}";
                return summary;
            }

            if (execution == null)
            {
                summary["errors"] = new JArray($"Top level 'runs:' section is required for {fileRelativePath}");
                return summary;
            }

            summary["inputs"] = inputs;
            if (deprecated.Count > 0)
            {
                summary["deprecated"] = deprecated;
            }
            summary["execution"] = execution;
            return summary;
        }

        static JObject ConvertRuns(
            TemplateContext templateContext,
            TemplateToken inputsToken,
            String fileRelativePath,
            MappingToken outputs = null)
        {
            var runsMapping = inputsToken.AssertMapping("runs");
            var usingToken = default(StringToken);
            var imageToken = default(StringToken);
            var argsToken = default(SequenceToken);
            var entrypointToken = default(StringToken);
            var envToken = default(MappingToken);
            var mainToken = default(StringToken);
            var pluginToken = default(StringToken);
            var preToken = default(StringToken);
            var preEntrypointToken = default(StringToken);
            var preIfToken = default(StringToken);
            var postToken = default(StringToken);
            var postEntrypointToken = default(StringToken);
            var postIfToken = default(StringToken);
            var steps = default(List<Step>);

            foreach (var run in runsMapping)
            {
                var runsKey = run.Key.AssertString("runs key").Value;
                switch (runsKey)
                {
                    case "using":
                        usingToken = run.Value.AssertString("using");
                        break;
                    case "image":
                        imageToken = run.Value.AssertString("image");
                        break;
                    case "args":
                        argsToken = run.Value.AssertSequence("args");
                        break;
                    case "entrypoint":
                        entrypointToken = run.Value.AssertString("entrypoint");
                        break;
                    case "env":
                        envToken = run.Value.AssertMapping("env");
                        break;
                    case "main":
                        mainToken = run.Value.AssertString("main");
                        break;
                    case "plugin":
                        pluginToken = run.Value.AssertString("plugin");
                        break;
                    case "post":
                        postToken = run.Value.AssertString("post");
                        break;
                    case "post-entrypoint":
                        postEntrypointToken = run.Value.AssertString("post-entrypoint");
                        break;
                    case "post-if":
                        postIfToken = run.Value.AssertString("post-if");
                        break;
                    case "pre":
                        preToken = run.Value.AssertString("pre");
                        break;
                    case "pre-entrypoint":
                        preEntrypointToken = run.Value.AssertString("pre-entrypoint");
                        break;
                    case "pre-if":
                        preIfToken = run.Value.AssertString("pre-if");
                        break;
                    case "steps":
                        var stepsToken = run.Value.AssertSequence("steps");
                        steps = PipelineTemplateConverter.ConvertToSteps(templateContext, stepsToken);
                        templateContext.Errors.Check();
                        break;
                    default:
                        break;
                }
            }

            if (usingToken != null)
            {
                if (string.Equals(usingToken.Value, "docker", StringComparison.OrdinalIgnoreCase))
                {
                    if (string.IsNullOrEmpty(imageToken?.Value))
                    {
                        throw new ArgumentNullException($"You are using a Container Action but an image is not provided in {fileRelativePath}.");
                    }
                    else
                    {
                        return new JObject
                        {
                            ["type"] = "docker",
                            ["using"] = usingToken.Value,
                            ["image"] = imageToken.Value,
                            ["args"] = argsToken == null ? null : new JArray(argsToken.Select(x => x.ToString())),
                            ["entrypoint"] = entrypointToken?.Value,
                            ["env"] = envToken == null ? null : Pairs(envToken),
                            ["pre"] = preEntrypointToken?.Value,
                            ["preIf"] = preIfToken?.Value ?? "always()",
                            ["post"] = postEntrypointToken?.Value,
                            ["postIf"] = postIfToken?.Value ?? "always()",
                        };
                    }
                }
                else if (string.Equals(usingToken.Value, "node12", StringComparison.OrdinalIgnoreCase) ||
                         string.Equals(usingToken.Value, "node16", StringComparison.OrdinalIgnoreCase) ||
                         string.Equals(usingToken.Value, "node20", StringComparison.OrdinalIgnoreCase) ||
                         string.Equals(usingToken.Value, "node24", StringComparison.OrdinalIgnoreCase))
                {
                    if (string.IsNullOrEmpty(mainToken?.Value))
                    {
                        throw new ArgumentNullException($"You are using a JavaScript Action but there is not an entry JavaScript file provided in {fileRelativePath}.");
                    }
                    else
                    {
                        return new JObject
                        {
                            ["type"] = "node",
                            ["using"] = usingToken.Value,
                            ["main"] = mainToken.Value,
                            ["pre"] = preToken?.Value,
                            ["preIf"] = preIfToken?.Value ?? "always()",
                            ["post"] = postToken?.Value,
                            ["postIf"] = postIfToken?.Value ?? "always()",
                        };
                    }
                }
                else if (string.Equals(usingToken.Value, "composite", StringComparison.OrdinalIgnoreCase))
                {
                    if (steps == null)
                    {
                        throw new ArgumentNullException($"You are using a composite action but there are no steps provided in {fileRelativePath}.");
                    }
                    else
                    {
                        return new JObject
                        {
                            ["type"] = "composite",
                            ["using"] = usingToken.Value,
                            ["steps"] = new JArray(steps.Cast<ActionStep>().Select(Summarize)),
                            ["outputs"] = outputs == null ? null : new JArray(outputs.Select(x => new JArray(x.Key.ToString(), OutputValue(x.Value)))),
                        };
                    }
                }
                else
                {
                    throw new ArgumentOutOfRangeException($"'using: {usingToken.Value}' is not supported, use 'docker', 'node12', 'node16', 'node20' or 'node24' instead.");
                }
            }
            else if (pluginToken != null)
            {
                return new JObject { ["type"] = "plugin", ["plugin"] = pluginToken.Value };
            }

            throw new NotSupportedException("Missing 'using' value. 'using' requires 'composite', 'docker', 'node12', 'node16', 'node20' or 'node24'.");
        }

        static string OutputValue(TemplateToken value)
        {
            if (value is MappingToken m)
            {
                foreach (var p in m)
                {
                    if (p.Key.ToString() == "value") return p.Value.ToString();
                }
            }
            return null;
        }

        static JObject Summarize(ActionStep a)
        {
            var o = new JObject { ["id"] = a.ContextName, ["if"] = a.Condition };
            o["name"] = a.DisplayNameToken?.ToString();
            o["continueOnError"] = a.ContinueOnError?.ToString();
            o["timeout"] = a.TimeoutInMinutes?.ToString();
            if (a.Environment is MappingToken)
            {
                o["env"] = Pairs(a.Environment);
            }
            else if (a.Environment != null)
            {
                o["envExpr"] = a.Environment.ToString();
            }
            if (a.Reference is ScriptReference)
            {
                o["kind"] = "run";
                var inputs = (MappingToken)a.Inputs;
                foreach (var p in inputs)
                {
                    o[p.Key.ToString()] = p.Value.ToString();
                }
            }
            else
            {
                o["kind"] = "uses";
                if (a.Inputs is MappingToken)
                {
                    o["with"] = Pairs(a.Inputs);
                }
                else if (a.Inputs != null)
                {
                    o["withExpr"] = a.Inputs.ToString();
                }
                if (a.Reference is ContainerRegistryReference c)
                {
                    o["reference"] = new JObject { ["type"] = "docker", ["image"] = c.Image };
                }
                else if (a.Reference is RepositoryPathReference r)
                {
                    o["reference"] = new JObject { ["type"] = r.RepositoryType, ["name"] = r.Name, ["ref"] = r.Ref, ["path"] = r.Path };
                }
            }
            return o;
        }

        static JArray Pairs(TemplateToken t)
        {
            return t is MappingToken m ? new JArray(m.Select(x => new JArray(x.Key.ToString(), x.Value.ToString()))) : new JArray(t.ToString());
        }

        static void ConvertInputs(TemplateToken inputsToken, JArray inputs, JObject deprecated)
        {
            var inputsMapping = inputsToken.AssertMapping("inputs");
            foreach (var input in inputsMapping)
            {
                bool hasDefault = false;
                var inputName = input.Key.AssertString("input name");
                var inputMetadata = input.Value.AssertMapping("input metadata");
                var entry = new JObject { ["name"] = inputName.Value };
                foreach (var metadata in inputMetadata)
                {
                    var metadataName = metadata.Key.AssertString("input metadata").Value;
                    if (string.Equals(metadataName, "default", StringComparison.OrdinalIgnoreCase))
                    {
                        hasDefault = true;
                        entry["default"] = metadata.Value.ToString();
                    }
                    else if (string.Equals(metadataName, "deprecationMessage", StringComparison.OrdinalIgnoreCase))
                    {
                        var message = metadata.Value.AssertString("input deprecationMessage");
                        deprecated.Add(inputName.Value, message.Value);
                    }
                }

                if (!hasDefault)
                {
                    entry["default"] = string.Empty;
                }
                inputs.Add(entry);
            }
        }
    }
}
