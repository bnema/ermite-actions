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
        if (args[0] == "--fold")
        {
            // Debug aid: the case folding of StringComparison.OrdinalIgnoreCase. Prints "XXXX YYYY" (hexadecimal
            // code points) for every code point c whose class representative m(c) differs from c, after checking
            // that string.Equals(a, b, OrdinalIgnoreCase) holds exactly when m(a) == m(b) over all the affected
            // code points. m(c) is ToUpperInvariant(c) when OrdinalIgnoreCase agrees that they are equal.
            var map = new SortedDictionary<int, int>();
            for (var c = 0; c <= 0x10FFFF; c++)
            {
                if (c >= 0xD800 && c <= 0xDFFF) continue;
                var str = char.ConvertFromUtf32(c);
                var up = str.ToUpperInvariant();
                if (up != str && char.ConvertToUtf32(up, 0) is var u && up.Length == char.ConvertFromUtf32(u).Length
                    && string.Equals(str, up, StringComparison.OrdinalIgnoreCase))
                {
                    map[c] = u;
                }
            }
            var members = new SortedSet<int>(map.Keys);
            foreach (var v in map.Values) members.Add(v);
            var list = new List<int>(members);
            Func<int, int> m = c => map.TryGetValue(c, out var x) ? x : c;
            var bad = 0;
            foreach (var a in list)
            {
                foreach (var b in list)
                {
                    var eq = string.Equals(char.ConvertFromUtf32(a), char.ConvertFromUtf32(b), StringComparison.OrdinalIgnoreCase);
                    if (eq != (m(a) == m(b))) { bad++; if (bad < 10) Console.Error.WriteLine($"inconsistent: {a:X} {b:X}"); }
                }
            }
            // Nothing outside the table may be equal to anything but itself.
            for (var c = 0; c <= 0x10FFFF; c++)
            {
                if (c >= 0xD800 && c <= 0xDFFF) continue;
                if (members.Contains(c)) continue;
                var str = char.ConvertFromUtf32(c);
                foreach (var d in new[] { c ^ 1, c + 1, c - 1 })
                {
                    if (d < 0 || d > 0x10FFFF || (d >= 0xD800 && d <= 0xDFFF)) continue;
                    if (string.Equals(str, char.ConvertFromUtf32(d), StringComparison.OrdinalIgnoreCase)) { bad++; if (bad < 10) Console.Error.WriteLine($"outside: {c:X} {d:X}"); }
                }
            }
            Console.Error.WriteLine($"{map.Count} mappings, {bad} inconsistencies");
            foreach (var kv in map) Console.WriteLine($"{kv.Key:X} {kv.Value:X}");
            return bad == 0 ? 0 : 1;
        }
        if (args[0] == "--cmp")
        {
            // Debug aid: each input line is two space-separated strings written as dot-separated hexadecimal UTF-16 code
            // units; prints Equals, Compare sign, Contains, StartsWith and EndsWith, all OrdinalIgnoreCase.
            Func<string, string> parse = h => { var sb = new StringBuilder(); if (h != "-") foreach (var u in h.Split('.')) sb.Append((char)Convert.ToInt32(u, 16)); return sb.ToString(); };
            foreach (var line in File.ReadLines(args[1]))
            {
                var parts = line.Split(' ');
                var a = parse(parts[0]);
                var b = parse(parts[1]);
                Console.WriteLine($"{(string.Equals(a, b, StringComparison.OrdinalIgnoreCase) ? 1 : 0)} {Math.Sign(string.Compare(a, b, StringComparison.OrdinalIgnoreCase))} {(a.IndexOf(b, StringComparison.OrdinalIgnoreCase) >= 0 ? 1 : 0)} {(a.StartsWith(b, StringComparison.OrdinalIgnoreCase) ? 1 : 0)} {(a.EndsWith(b, StringComparison.OrdinalIgnoreCase) ? 1 : 0)}");
            }
            return 0;
        }
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
