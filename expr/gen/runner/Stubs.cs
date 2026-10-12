// SPDX-License-Identifier: MIT

using System;
namespace GitHub.Services.Common {
  public static class ArgumentUtility {
    public static void CheckForNull(Object v, String name) { if (v == null) throw new ArgumentNullException(name); }
    public static void CheckStringForNullOrEmpty(String v, String name, bool trim = false) { if (String.IsNullOrEmpty(v)) throw new ArgumentException(name); }
  }
}
namespace GitHub.Services.WebApi.Internal {
  [AttributeUsage(AttributeTargets.All)] public sealed class ClientIgnoreAttribute : Attribute {}
}
namespace GitHub.Services.WebApi {
  public abstract class VssSecureJsonConverter : Newtonsoft.Json.JsonConverter { public override void WriteJson(Newtonsoft.Json.JsonWriter w, object v, Newtonsoft.Json.JsonSerializer s) {} }
  public static class JsonUtility {
    // Same as the runner's JsonUtility.ToString for a string: default JsonTextWriter settings.
    public static String ToString(Object o) { return Newtonsoft.Json.JsonConvert.SerializeObject(o); }
  }
}
namespace GitHub.DistributedTask.Expressions {
  internal static class ExpressionResources {
    public static String ExceededAllowedMemory(object e) { return "The maximum allowed memory size was exceeded while evaluating the following expression: " + e; }
    public static String InvalidFormatArgIndex(object f) { return "The following format string references more arguments than were supplied: " + f; }
    public static String InvalidFormatSpecifiers(object s, object k) { return "The format specifiers '" + s + "' are not valid for objects of type '" + k + "'"; }
    public static String InvalidFormatString(object f) { return "The following format string is invalid: " + f; }
  }
}
namespace GitHub.Actions.Expressions.Data { internal class Dummy {} }
