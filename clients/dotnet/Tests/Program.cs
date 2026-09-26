using System;
using System.Collections.Generic;
using System.IO;
using System.Text;
using Mobium;

namespace Mobium.Tests
{
    /// <summary>
    /// The .NET client's tests, with no test framework.
    /// </summary>
    /// <remarks>
    /// Run with <c>dotnet run --project Tests</c>, or by <c>make dotnet</c>.
    /// Mirrors the Java client's Tests.java, which exists for the same reason:
    /// a framework would mean this client cannot be checked without a NuGet
    /// restore and a network.
    /// </remarks>
    internal static class Program
    {
        private static int _checks;
        private static int _failures;

        private static int Main(string[] args)
        {
            if (args.Length > 0 && args[0] == "e2e") return E2E.Run();
            JsonRoundTripsStrings();
            JsonEscapesControlCharacters();
            JsonKeepsNonAsciiIntact();
            JsonReadsIntegersAsIntegers();
            JsonRejectsMalformedInput();
            JsonParsesTheShapesTheWireUses();
            JsonWritesNumbersWithoutTheCulturesOpinion();
            StdioIsUtf8WhateverTheConsoleThinks();
            ElementReadsAToolResult();
            BoundsComputeTheirCentre();
            UntilBuildsItsArguments();
            FindBinaryRejectsSomethingThatIsNotOne();
            ArgumentsSurviveSpacesAndQuotes();
            EveryErrorCodeHasItsException();
            AToolFailureKeepsCodeRemedyAndDetails();
            AnUnknownOrMissingCodeIsTheBaseException();

            Console.WriteLine();
            Console.WriteLine($"{_checks} checks, {_failures} failed");
            return _failures > 0 ? 1 : 0;
        }

        // -- the JSON layer, which is the only hand-written parsing here -----

        private static void JsonRoundTripsStrings()
        {
            foreach (var s in new[] { "", "plain", "with \"quotes\"", "back\\slash",
                                      "new\nline", "tab\there", "emoji 🐛" })
            {
                var back = Json.Parse(Json.Write(new Dictionary<string, object> { ["k"] = s }));
                Eq("round trip " + Describe(s), s, Json.Str(Json.AsObject(back), "k"));
            }
        }

        private static void JsonEscapesControlCharacters()
        {
            // A raw control character in a JSON string is invalid, and mobium's
            // own error messages can carry one.
            var written = Json.Write(new Dictionary<string, object> { ["k"] = "a\u0001b" });
            Yes("control character escaped", written.Contains("\\u0001"));
            Eq("control character survives", "a\u0001b",
                Json.Str(Json.AsObject(Json.Parse(written)), "k"));
        }

        private static void JsonKeepsNonAsciiIntact()
        {
            // Android's own Settings has a non-breaking hyphen in "Wi‑Fi", and
            // a locator containing one must survive unchanged or it matches
            // nothing.
            const string wifi = "text=Wi\u2011Fi";
            Eq("non-breaking hyphen", wifi,
                Json.Str(Json.AsObject(Json.Parse(Json.Write(new Dictionary<string, object> { ["k"] = wifi }))), "k"));
            // And it must arrive as \u2011 or literal UTF-8, either of which parses.
            Eq("escaped form parses", wifi,
                Json.Str(Json.AsObject(Json.Parse("{\"k\":\"text=Wi\\u2011Fi\"}")), "k"));
        }

        private static void JsonReadsIntegersAsIntegers()
        {
            // A coordinate that arrives as 540.0 prints wrongly and compares
            // wrongly. Whole numbers must stay whole.
            var m = Json.AsObject(Json.Parse("{\"x\":540,\"y\":-12,\"s\":1.5,\"e\":1e3}"));
            Eq("integer stays integer", 540, Json.Integer(m, "x"));
            Eq("negative integer", -12, Json.Integer(m, "y"));
            Yes("integer is not floating point", m["x"] is long);
            Yes("real number is floating point", m["s"] is double);
            Yes("exponent is floating point", m["e"] is double);
        }

        private static void JsonRejectsMalformedInput()
        {
            foreach (var bad in new[] { "{", "{\"a\"}", "[1,]", "tru", "{\"a\":1}x", "", "\"unterminated", "{\"a\":}" })
            {
                var threw = false;
                try { Json.Parse(bad); } catch (MobiumException) { threw = true; }
                Yes("rejects " + Describe(bad), threw);
            }
        }

        private static void JsonParsesTheShapesTheWireUses()
        {
            const string wire = "{\"elements\":[{\"ref\":\"@e1\",\"label\":\"Sign in\",\"role\":\"button\","
                + "\"locator\":{\"kind\":\"text\",\"value\":\"Sign in\",\"exact\":true},"
                + "\"bounds\":{\"x1\":100,\"y1\":200,\"x2\":300,\"y2\":280}}],"
                + "\"count\":1,\"truncated\":false}";
            var d = Json.AsObject(Json.Parse(wire));
            var elements = Json.AsArray(d["elements"]);
            Eq("one element", 1, elements.Count);
            var e = Element.From(Json.AsObject(elements[0]));
            Eq("ref", "@e1", e.Ref);
            Eq("label", "Sign in", e.Label);
            Eq("role", "button", e.Role);
            Eq("locator flattened", "text=Sign in", e.Locator);
            Eq("bounds", "[100,200][300,280]", e.Bounds.ToString());
            Yes("false is false", !Json.Bool(d, "truncated"));
        }

        private static void JsonWritesNumbersWithoutTheCulturesOpinion()
        {
            // A machine set to a locale that writes 1,5 for 1.5 would emit
            // invalid JSON, and a coordinate would arrive as two arguments.
            var previous = System.Globalization.CultureInfo.CurrentCulture;
            try
            {
                System.Globalization.CultureInfo.CurrentCulture =
                    new System.Globalization.CultureInfo("de-DE");
                var written = Json.Write(new Dictionary<string, object> { ["x"] = 540, ["s"] = 1.5 });
                Yes("integer unaffected by culture", written.Contains("\"x\":540"));
                Yes("decimal point stays a point", written.Contains("\"s\":1.5"));
            }
            finally
            {
                System.Globalization.CultureInfo.CurrentCulture = previous;
            }
        }

        private static void StdioIsUtf8WhateverTheConsoleThinks()
        {
            // netstandard2.0 has no ProcessStartInfo.StandardInputEncoding, so
            // Connection wraps the raw stream itself. This checks the wrapping
            // it uses: UTF-8, and no byte-order mark. A BOM would be the first
            // thing mobium read on the line, and the line would not parse.
            var buffer = new MemoryStream();
            using (var w = new StreamWriter(buffer, new UTF8Encoding(false)))
            {
                w.Write(Json.Write(new Dictionary<string, object> { ["k"] = "Wi\u2011Fi" }));
                w.Flush();
            }
            var bytes = buffer.ToArray();
            Yes("no byte-order mark", !(bytes.Length >= 3 && bytes[0] == 0xEF && bytes[1] == 0xBB && bytes[2] == 0xBF));
            Eq("round trips through UTF-8 bytes", "Wi\u2011Fi",
                Json.Str(Json.AsObject(Json.Parse(new UTF8Encoding(false).GetString(bytes))), "k"));
        }

        // -- the value types ------------------------------------------------

        private static void ElementReadsAToolResult()
        {
            var e = Element.From(Json.AsObject(Json.Parse(
                "{\"ref\":\"@e2\",\"label\":\"Search\",\"role\":\"\",\"bounds\":{\"x1\":0,\"y1\":0,\"x2\":10,\"y2\":10}}")));
            Eq("missing role is empty, not null", "", e.Role);
            Eq("missing locator is empty", "", e.Locator);
            Eq("toString without a role", "@e2 Search", e.ToString());
        }

        private static void BoundsComputeTheirCentre()
        {
            var b = new Bounds(100, 200, 300, 280);
            Eq("center x", 200, b.CenterX);
            Eq("center y", 240, b.CenterY);
            Eq("width", 200, b.Width);
            Eq("height", 80, b.Height);
        }

        private static void UntilBuildsItsArguments()
        {
            var visible = Until.Visible().Args("@e1");
            Eq("target", "@e1", visible["target"]);
            Eq("condition", "visible", visible["condition"]);
            Yes("no text when not asked for", !visible.ContainsKey("text"));
            Yes("no timeout when not asked for", !visible.ContainsKey("timeout_ms"));

            var timed = Until.Text("Sent").Timeout(TimeSpan.FromSeconds(30)).Args("@e4");
            Eq("text condition", "text", timed["condition"]);
            Eq("expected text", "Sent", timed["text"]);
            Eq("timeout in milliseconds", 30000L, timed["timeout_ms"]);
        }

        private static void FindBinaryRejectsSomethingThatIsNotOne()
        {
            var threw = false;
            try
            {
                Connection.FindBinary(Path.Combine(Path.GetTempPath(), "definitely-not-mobium-" + Guid.NewGuid()));
            }
            catch (MobiumException)
            {
                threw = true;
            }
            Yes("a path that is not a binary is refused", threw);
        }

        private static void ArgumentsSurviveSpacesAndQuotes()
        {
            // netstandard2.0 has no ArgumentList, so Connection joins the
            // command line itself. Getting this wrong splits a device serial
            // or a path at its spaces, which is how a notification body
            // became one word when it went through `adb shell` unquoted.
            Eq("plain arguments are not quoted", "pipe --device emulator-5554",
                Connection.BuildArguments(new[] { "pipe", "--device", "emulator-5554" }));
            Eq("a space is quoted", "pipe \"my device\"",
                Connection.BuildArguments(new[] { "pipe", "my device" }));
            Eq("an empty argument survives", "pipe \"\"",
                Connection.BuildArguments(new[] { "pipe", "" }));
            Eq("a quote is escaped", "\"a\\\"b\"",
                Connection.BuildArguments(new[] { "a\"b" }));
            // A backslash only escapes when it runs into a quote, so a run
            // before the closing quote has to be doubled and one in the
            // middle must not be.
            Eq("a backslash in the middle stays single", "\"a\\b c\"",
                Connection.BuildArguments(new[] { "a\\b c" }));
            Eq("trailing backslashes are doubled", "\"a b\\\\\"",
                Connection.BuildArguments(new[] { "a b\\" }));
        }

        // -- the harness ----------------------------------------------------

        // -- error codes: docs/decisions/0005 -----------------------------

        private static readonly string[] Codes = {"no_device", "device_not_ready", "toolchain_missing",
            "no_such_element", "ambiguous_locator", "element_not_reachable", "no_such_context",
            "no_such_alert", "unsupported", "not_confirmed", "timeout", "invalid_argument",
            "device_server", "internal"};

        private static void EveryErrorCodeHasItsException()
        {
            foreach (var code in Codes)
            {
                var e = MobiumException.From("x", "app_tap", new Dictionary<string, object> { ["code"] = code });
                Eq("code kept for " + code, code, e.Code);
                Yes(code + " has its own exception", e.GetType() != typeof(MobiumException));
            }
        }

        private static void AToolFailureKeepsCodeRemedyAndDetails()
        {
            var e = MobiumException.From("no element matches text=Go", "app_tap", new Dictionary<string, object>
            {
                ["code"] = "no_such_element", ["remedy"] = "run app_map again", ["retryable"] = false,
                ["details"] = new Dictionary<string, object> { ["locator"] = "text=Go" },
            });
            Yes("no_such_element is NoSuchElementException", e is NoSuchElementException);
            Eq("message names the tool", "app_tap: no element matches text=Go", e.Message);
            Eq("remedy kept", "run app_map again", e.Remedy);
            Eq("details kept", "text=Go", e.Details["locator"]);
            var t = MobiumException.From("timed out", "app_wait_for",
                new Dictionary<string, object> { ["code"] = "timeout", ["retryable"] = true });
            Yes("timeout is TimedOutException and retryable", t is TimedOutException && t.Retryable);
        }

        private static void AnUnknownOrMissingCodeIsTheBaseException()
        {
            var u = MobiumException.From("new kind", "app_tap", new Dictionary<string, object> { ["code"] = "something_new" });
            Yes("an unknown code is the base class", u.GetType() == typeof(MobiumException));
            Eq("an unknown code is kept", "something_new", u.Code);
            Eq("a daemon that sends no code gives error", "error", MobiumException.From("plain", "app_tap", null).Code);
        }

        private static void Eq(string what, object want, object got)
        {
            _checks++;
            if (Equals(want, got))
            {
                Console.WriteLine($"ok   {what}");
                return;
            }
            _failures++;
            Console.WriteLine($"FAIL {what}: want {Describe(want)}, got {Describe(got)}");
        }

        private static void Yes(string what, bool condition)
        {
            _checks++;
            if (condition)
            {
                Console.WriteLine($"ok   {what}");
                return;
            }
            _failures++;
            Console.WriteLine($"FAIL {what}");
        }

        private static string Describe(object o)
        {
            if (o == null) return "null";
            var s = o.ToString();
            if (o is string) s = "\"" + s.Replace("\n", "\\n").Replace("\t", "\\t") + "\"";
            return s.Length == 0 ? "\"\"" : s;
        }
    }
}
