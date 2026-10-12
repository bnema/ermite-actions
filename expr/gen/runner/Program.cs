// SPDX-License-Identifier: MIT

using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text;
using GitHub.DistributedTask.Expressions2;
using GitHub.DistributedTask.Expressions2.Sdk;
using GitHub.DistributedTask.Expressions2.Sdk.Functions;
using GitHub.DistributedTask.Pipelines.ContextData;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;

// Differential harness: runs expressions through the runner's own
// Expressions2 code (compiled from the runner repository) and prints one JSON
// line per case.
//
// Input (argv[0]): JSON {"contexts": {name: json}, "cases": [{"e": expr, "ctx": {name: json} | null}]}
// Output: JSON lines {"k": kind, "s": string form, "j": toJson, "err": {"t": "parse"|"eval", "m": message}}

internal sealed class CtxNode : NamedValue
{
    protected override Object EvaluateCore(EvaluationContext context, out ResultMemory resultMemory)
    {
        resultMemory = null;
        return (context.State as DictionaryContextData)[Name];
    }
}

internal sealed class Success : Function { protected override Object EvaluateCore(EvaluationContext c, out ResultMemory m) { m = null; return true; } }
internal sealed class Always : Function { protected override Object EvaluateCore(EvaluationContext c, out ResultMemory m) { m = null; return true; } }
internal sealed class HashFiles : Function
{
    protected override Object EvaluateCore(EvaluationContext c, out ResultMemory m)
    {
        m = null;
        var s = "hash:";
        for (var i = 0; i < Parameters.Count; i++)
        {
            if (i > 0) s += "|";
            s += Parameters[i].Evaluate(c).ConvertToString();
        }
        return s;
    }
}

internal static class P
{
    static DictionaryContextData Load(JObject ctx)
    {
        var d = new DictionaryContextData();
        foreach (var p in ctx.Properties())
        {
            var token = p.Value;
            d[p.Name] = token.ToPipelineContextData();
        }
        return d;
    }

    static int Main(string[] args)
    {
        if (args[0] == "--g15")
        {
            // Debug aid: one hexadecimal IEEE 754 bit pattern per input line, the G15 form per output line.
            foreach (var line in File.ReadLines(args[1]))
            {
                var d = BitConverter.Int64BitsToDouble(Convert.ToInt64(line.Trim(), 16));
                Console.WriteLine(d.ToString("G15", System.Globalization.CultureInfo.InvariantCulture));
            }
            return 0;
        }
        JObject root; using (var jr0 = new JsonTextReader(new StringReader(File.ReadAllText(args[0]))) { DateParseHandling = DateParseHandling.None }) root = JObject.Load(jr0);
        var defaultCtx = (JObject)root["contexts"];
        var funcs = new IFunctionInfo[]
        {
            new FunctionInfo<Success>("success", 0, 0),
            new FunctionInfo<Always>("always", 0, 0),
            new FunctionInfo<HashFiles>("hashFiles", 1, 255),
        };
        var output = new StreamWriter(Console.OpenStandardOutput(), new UTF8Encoding(false));
        var defaultData = Load(defaultCtx);
        foreach (var c in (JArray)root["cases"])
        {
            var e = (string)c["e"];
            var custom = c["ctx"] as JObject;
            var data = custom != null ? Load(custom) : defaultData;
            var names = (custom ?? defaultCtx).Properties().Select(p => p.Name);
            var res = new JObject();
            IExpressionNode tree = null;
            try
            {
                tree = new ExpressionParser().CreateTree(e, null, names.Select(n => (INamedValueInfo)new NamedValueInfo<CtxNode>(n)).ToList(), funcs);
            }
            catch (Exception ex)
            {
                res["err"] = new JObject { ["t"] = "parse", ["m"] = ex.Message, ["x"] = ex.GetType().Name };
            }
            if (res["err"] == null)
            {
                if (tree == null)
                {
                    res["empty"] = true;
                }
                else
                {
                    try
                    {
                        var r = tree.Evaluate(null, null, data, null);
                        res["k"] = r.Kind.ToString();
                        res["s"] = r.ConvertToString();
                        // toJson of the result
                        var tj = new ToJson();
                        tj.AddParameter(new Literal(r.Value));
                        try
                        {
                            var jr = ((IExpressionNode)tj).Evaluate(null, null, data, null);
                            res["j"] = jr.ConvertToString();
                        }
                        catch (Exception ex2)
                        {
                            res["jerr"] = ex2.Message;
                        }
                    }
                    catch (Exception ex)
                    {
                        res["err"] = new JObject { ["t"] = "eval", ["m"] = ex.Message, ["x"] = ex.GetType().Name };
                    }
                }
            }
            // ASCII-escaped, so that a lone surrogate (which UTF-8 cannot carry) survives.
            var sw = new StringWriter();
            using (var jw = new JsonTextWriter(sw) { StringEscapeHandling = StringEscapeHandling.EscapeNonAscii, Formatting = Formatting.None })
            {
                res.WriteTo(jw);
            }
            output.WriteLine(sw.ToString());
        }
        output.Flush();
        return 0;
    }
}
