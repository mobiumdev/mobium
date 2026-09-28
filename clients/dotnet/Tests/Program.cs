using System;
using System.Collections.Generic;
using System.IO;
using System.Diagnostics;
using System.Linq;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
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
            if (args.Length > 0 && args[0] == "pipe") return FakeMobium.Run();
            JsonRoundTripsStrings();
            JsonEscapesControlCharacters();
            JsonKeepsNonAsciiIntact();
            JsonReadsIntegersAsIntegers();
            JsonRejectsMalformedInput();
            JsonParsesTheShapesTheWireUses();
            JsonWritesNumbersWithoutTheCulturesOpinion();
            StdioIsUtf8WhateverTheConsoleThinks();
            ElementReadsAToolResult();
            BoundsComputeTheirCenter();
            UntilBuildsItsArguments();
            FindBinaryRejectsSomethingThatIsNotOne();
            FindBinaryNeverSearchesTheCurrentDirectory();
            ArgumentsSurviveSpacesAndQuotes();
            EveryErrorCodeHasItsException();
            AToolFailureKeepsCodeRemedyAndDetails();
            AnUnknownOrMissingCodeIsTheBaseException();
            JsonRefusesNestingThatWouldOverflowTheStack();
            JsonNumbersFollowTheGrammar();
            JsonEscapesAreExact();
            JsonIntegerRefusesWhatDoesNotFit();
            JsonWritesNoNumberJsonCannotCarry();
            TheClientReportsItsAssemblyVersion();
            CallTimeoutMustBePositive();

            // The connection, against this program standing in for mobium.
            ItSkipsWhatIsNotTheAnswer();
            DisposingDetachesBeforeItGoes();
            ArgumentsArriveExactlyThroughARealProcess();
            ConcurrentCallsAreSerialized();
            ANullArgumentIsRefusedBeforeItIsSent();
            ATimedOutCallEndsTheConnection();
            AnExitMidCallIsReportedWithItsStatus();
            AFailedHandshakeLeavesNoProcess();
            DisposeIsIdempotentAndEndsAWaitingCall();
            AnAnswerWithNoIdFailsTheCallInFlight();
            StartOpensTheSessionAndQuitEndsIt();

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
            Yes("no checked state is null, not false", e.Checked == null);
            var box = Element.From(Json.AsObject(Json.Parse("{\"ref\":\"@e4\",\"role\":\"checkbox\",\"checked\":true}")));
            Eq("a checkbox carries its state", true, box.Checked);
            var off = Element.From(Json.AsObject(Json.Parse("{\"ref\":\"@e5\",\"role\":\"switch\",\"checked\":false}")));
            Eq("an unchecked switch is false, not null", false, off.Checked);
        }

        private static void BoundsComputeTheirCenter()
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

        private static void FindBinaryNeverSearchesTheCurrentDirectory()
        {
            // Not in ./bin, and not through a relative PATH entry, which is
            // the current directory by another name: anything could have been
            // planted there.
            var name = OperatingSystem.IsWindows() ? "mobium.exe" : "mobium";
            var tmp = Path.Combine(Path.GetTempPath(), "mobium-" + Guid.NewGuid());
            foreach (var where in new[] { tmp, Path.Combine(tmp, "bin") })
            {
                Directory.CreateDirectory(where);
                File.WriteAllText(Path.Combine(where, name), "#!/bin/sh\nexit 99\n");
            }
            var cwd = Directory.GetCurrentDirectory();
            var path = Environment.GetEnvironmentVariable("PATH");
            var bin = Environment.GetEnvironmentVariable("MOBIUM_BIN_PATH");
            try
            {
                Environment.SetEnvironmentVariable("MOBIUM_BIN_PATH", null);
                Directory.SetCurrentDirectory(tmp);
                foreach (var entry in new[] { ".", "bin", "./bin", "", "C:bin", "\\bin" })
                {
                    Environment.SetEnvironmentVariable("PATH", entry);
                    var found = Throws<MobiumException>(() => Connection.FindBinary(null)) != null;
                    Yes("PATH=" + Describe(entry) + " does not reach the current directory", found);
                }
                // The positive control: the same file, by its absolute directory.
                Environment.SetEnvironmentVariable("PATH", Path.Combine(tmp, "bin"));
                Eq("an absolute PATH entry is searched", Path.Combine(tmp, "bin", name), Connection.FindBinary(null));
            }
            finally
            {
                Directory.SetCurrentDirectory(cwd);
                Environment.SetEnvironmentVariable("PATH", path);
                Environment.SetEnvironmentVariable("MOBIUM_BIN_PATH", bin);
                Directory.Delete(tmp, true);
            }
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

        // -- hardening: the JSON reader --------------------------------------

        private static void JsonRefusesNestingThatWouldOverflowTheStack()
        {
            // A stack overflow in .NET ends the process; it cannot be caught.
            // So nesting past the limit must be an ordinary exception.
            var deep = new string('[', 200000) + new string(']', 200000);
            var threw = false;
            try { Json.Parse(deep); } catch (MobiumException) { threw = true; }
            Yes("200000 levels of nesting is refused, not a crash", threw);
            var ok = new string('[', Json.MaxDepth) + new string(']', Json.MaxDepth);
            Yes("nesting at the limit parses", Json.Parse(ok) is IList<object?>);
        }

        private static void JsonNumbersFollowTheGrammar()
        {
            foreach (var bad in new[] { "+1", "01", "1.", ".5", "-", "1e", "1e+", "--1", "1-2", "0x10" })
            {
                var threw = false;
                try { Json.Parse(bad); } catch (MobiumException) { threw = true; }
                Yes("number " + bad + " is refused", threw);
            }
            Eq("zero", 0L, Json.Parse("0"));
            Eq("negative zero is a long", 0L, Json.Parse("-0"));
            Eq("exponent with a sign", -500d, Json.Parse("-0.5e+3"));
            Yes("too big for a long becomes a double", Json.Parse("99999999999999999999") is double);
        }

        private static void JsonEscapesAreExact()
        {
            Eq("\\u with four digits", "A", Json.Parse("\"\\u0041\""));
            foreach (var bad in new[] { "\"\\u 41a\"", "\"\\u+041\"", "\"\\u00g1\"", "\"\\u004\"", "\"a\u0001b\"" })
            {
                var threw = false;
                try { Json.Parse(bad); } catch (MobiumException) { threw = true; }
                Yes("string " + Describe(bad) + " is refused", threw);
            }
            // A surrogate pair arrives as two escapes and must come out whole.
            Eq("surrogate pair", "\U0001F41B", Json.Parse("\"\\ud83d\\udc1b\""));
        }

        private static void JsonIntegerRefusesWhatDoesNotFit()
        {
            var m = Json.AsObject(Json.Parse("{\"x\":2147483648,\"y\":2147483647}"));
            Eq("int.MaxValue fits", int.MaxValue, Json.Integer(m, "y"));
            var threw = false;
            try { Json.Integer(m, "x"); } catch (MobiumException) { threw = true; }
            Yes("2^31 is refused rather than wrapped negative", threw);
        }

        private static void JsonWritesNoNumberJsonCannotCarry()
        {
            foreach (var d in new[] { double.NaN, double.PositiveInfinity })
            {
                var refused = Throws<InvalidArgumentException>(() => Json.Write(new Dictionary<string, object?> { ["latitude"] = d })) != null;
                Yes(d + " is refused rather than written bare", refused);
            }
        }

        private static void TheClientReportsItsAssemblyVersion()
        {
            // One version, in Mobium.csproj; the handshake reads it from there.
            Eq("clientInfo version", "0.1.0", Connection.ClientVersion);
        }

        private static void CallTimeoutMustBePositive()
        {
            foreach (var bad in new[] { TimeSpan.Zero, TimeSpan.FromSeconds(-1) })
            {
                var threw = false;
                try { Device.Builder().CallTimeout(bad); } catch (ArgumentOutOfRangeException) { threw = true; }
                Yes("CallTimeout(" + bad + ") is refused", threw);
            }
            Device.Builder().CallTimeout(Timeout.InfiniteTimeSpan);
            Yes("CallTimeout(Infinite) is accepted", true);
        }

        // -- hardening: the connection, against a fake mobium ---------------

        private static string FakeBinary() =>
            Path.Combine(AppContext.BaseDirectory, OperatingSystem.IsWindows() ? "Mobium.Tests.exe" : "Mobium.Tests");

        private static Device Fake(string mode, TimeSpan? timeout = null, string? pidFile = null)
        {
            Environment.SetEnvironmentVariable("MOBIUM_FAKE", mode);
            Environment.SetEnvironmentVariable("MOBIUM_FAKE_PIDFILE", pidFile);
            var b = Device.Builder().Binary(FakeBinary());
            if (timeout.HasValue) b.CallTimeout(timeout.Value);
            return b.Connect();
        }

        private static T? Throws<T>(Action a) where T : Exception
        {
            try { a(); } catch (T e) { return e; }
            return null;
        }

        // mobium ends the sessions a client started when the client goes away,
        // and a crash closes stdin just as Dispose does; without the detach,
        // Dispose would quit.
        private static void DisposingDetachesBeforeItGoes()
        {
            var log = Path.Combine(Path.GetTempPath(), "mobium-notify-" + Guid.NewGuid().ToString("N") + ".log");
            Environment.SetEnvironmentVariable("MOBIUM_FAKE_NOTIFYLOG", log);
            try
            {
                Fake("ok").Dispose();
            }
            finally
            {
                Environment.SetEnvironmentVariable("MOBIUM_FAKE_NOTIFYLOG", null);
            }
            var sent = File.Exists(log) ? string.Join(",", File.ReadAllLines(log)) : "";
            Eq("Dispose sends the handshake's notification and then mobium/detach",
                "session=,notifications/initialized,mobium/detach", sent);
            if (File.Exists(log)) File.Delete(log);

            // Session gives the connection a daemon of its own.
            Environment.SetEnvironmentVariable("MOBIUM_FAKE_NOTIFYLOG", log);
            Environment.SetEnvironmentVariable("MOBIUM_FAKE", "ok");
            try
            {
                Device.Builder().Binary(FakeBinary()).Session("run7").Connect().Dispose();
            }
            finally
            {
                Environment.SetEnvironmentVariable("MOBIUM_FAKE_NOTIFYLOG", null);
            }
            var first = File.Exists(log) ? File.ReadAllLines(log)[0] : "";
            Eq("Session reaches the pipe as MOBIUM_SESSION", "session=run7", first);
            if (File.Exists(log)) File.Delete(log);
        }

        private static void ItSkipsWhatIsNotTheAnswer()
        {
            using var d = Fake("ok");
            var r = d.Call("app_echo", new Dictionary<string, object?> { ["k"] = "v" });
            Eq("the answer, past a notification, a non-JSON line and another id's reply", "app_echo", Json.Str(r, "tool"));
            Eq("arguments arrive", "v", Json.Str(Json.AsObject(r["echo"]), "k"));
        }

        private static void ArgumentsArriveExactlyThroughARealProcess()
        {
            // BuildArguments is checked above against the strings it builds;
            // this checks what a started process actually receives, which on
            // Windows is CommandLineToArgvW's reading of it and elsewhere
            // .NET's own. Backslashes before a quote and at the end are the
            // cases that differ from a naive join.
            const string serial = "my \"odd\" dev\\ice\\";
            Environment.SetEnvironmentVariable("MOBIUM_FAKE", "ok");
            using var d = Device.Builder().Binary(FakeBinary()).OnDevice(serial).Driver("").Connect();
            var argv = Json.AsArray(d.Call("app_x", null)["argv"]);
            Eq("argv[0] is pipe", "pipe", argv.Count > 0 ? argv[0] : null);
            Eq("--device then the serial, byte for byte", serial, argv.Count > 2 ? argv[2] : null);
            Eq("nothing else", 3, argv.Count);
        }

        private static void ConcurrentCallsAreSerialized()
        {
            // Two calls in flight on one pipe would read each other's answer.
            using var d = Fake("ok");
            var answers = new string[8];
            var threads = Enumerable.Range(0, answers.Length).Select(i => new Thread(() =>
            {
                // An exception on a worker thread would end the run rather
                // than fail a check, so it is kept as the answer instead.
                try
                {
                    var r = d.Call("slow", new Dictionary<string, object?> { ["n"] = (long)i });
                    answers[i] = Json.Str(Json.AsObject(r["echo"]), "n");
                }
                catch (Exception e)
                {
                    answers[i] = e.GetType().Name;
                }
            })).ToList();
            var clock = Stopwatch.StartNew();
            threads.ForEach(t => t.Start());
            threads.ForEach(t => t.Join());
            Yes("each of 8 threads got its own answer",
                Enumerable.Range(0, answers.Length).All(i => answers[i] == i.ToString()));
            Yes("they ran one at a time (8 x 300ms)", clock.ElapsedMilliseconds >= 8 * 300 - 50);
        }

        private static void ANullArgumentIsRefusedBeforeItIsSent()
        {
            using var d = Fake("ok");
            var e = Throws<InvalidArgumentException>(() => d.Tap(null!));
            Yes("a null target is InvalidArgumentException", e != null);
            Yes("naming the argument", e != null && e.Message.Contains("target"));
            Yes("a null inside a params array is refused",
                Throws<InvalidArgumentException>(() => d.Grant("com.example", "camera", null!)) != null);
            Yes("a null waypoint is refused",
                Throws<InvalidArgumentException>(() => d.FollowRoute(new[] { new[] { 1d, 2d }, null! }, 0)) != null);
            Yes("an empty tool name is refused",
                Throws<InvalidArgumentException>(() => d.Call(" ", null)) != null);
            Eq("and the connection is still fine", "app_current", Json.Str(d.Call("app_current", null), "tool"));
        }

        private static void ATimedOutCallEndsTheConnection()
        {
            using var d = Fake("hang", TimeSpan.FromMilliseconds(500));
            var clock = Stopwatch.StartNew();
            var first = Throws<MobiumException>(() => d.Call("app_map", null));
            Yes("a call with no answer times out", first != null && clock.ElapsedMilliseconds < 5000);
            Yes("saying why the connection is closed", first != null && first.Message.Contains("CallTimeout"));
            var second = Throws<MobiumException>(() => d.Call("app_map", null));
            Yes("the next call is refused, not read out of step",
                second != null && second.Message.Contains("no longer usable"));
        }

        private static void AnExitMidCallIsReportedWithItsStatus()
        {
            using var d = Fake("exit");
            var e = Throws<MobiumException>(() => d.Call("app_map", null));
            Yes("an exit mid-call names the status", e != null && e.Message.Contains("status 3"));
            var again = Throws<MobiumException>(() => d.Call("app_map", null));
            Yes("and the connection stays closed", again != null && again.Message.Contains("no longer usable"));
        }

        private static void AFailedHandshakeLeavesNoProcess()
        {
            var pidFile = Path.Combine(Path.GetTempPath(), "mobium-fake-" + Guid.NewGuid() + ".pid");
            try
            {
                // Refused rather than silent: a timeout kills the process on
                // its own, so only a handshake that fails some other way shows
                // whether Connect cleans up after itself.
                var clock = Stopwatch.StartNew();
                var silent = Throws<MobiumException>(() => Fake("mute", TimeSpan.FromMilliseconds(500)).Dispose());
                Yes("a silent handshake fails Connect within CallTimeout", silent != null && clock.ElapsedMilliseconds < 5000);
                var e = Throws<MobiumException>(() => Fake("refuse", null, pidFile).Dispose());
                Yes("a refused handshake fails Connect", e != null);
                var pid = int.Parse(File.ReadAllText(pidFile));
                var gone = Throws<ArgumentException>(() => Process.GetProcessById(pid)) != null
                           || Process.GetProcessById(pid).WaitForExit(5000);
                Yes("and the process it started is not left running", gone);
            }
            finally
            {
                File.Delete(pidFile);
            }
        }

        private static void DisposeIsIdempotentAndEndsAWaitingCall()
        {
            var d = Fake("hang");
            var waiting = Task.Run(() => Throws<MobiumException>(() => d.Call("app_map", null)));
            Thread.Sleep(300);
            d.Dispose();
            Yes("Dispose from another thread ends a waiting call", waiting.Wait(15000) && waiting.Result != null);
            d.Dispose();
            Yes("a second Dispose is harmless", true);
            var after = Throws<MobiumException>(() => d.Call("app_map", null));
            Yes("a call after Dispose is a MobiumException saying so",
                after != null && after.Message.Contains("disposed"));
        }

        private static void AnAnswerWithNoIdFailsTheCallInFlight()
        {
            // mobium answers a request it cannot parse with an error that has
            // no id. Skipped as a notification would be, it left the call
            // waiting forever; the timeout only turns a regression into a
            // failure rather than a hang.
            using var d = Fake("noid", TimeSpan.FromSeconds(5));
            var e = Throws<InvalidArgumentException>(() => d.Call("app_map", null));
            Yes("an answer with no id fails the call as InvalidArgumentException", e != null);
            Yes("saying the request was unreadable", e != null && e.Message.Contains("could not read the request"));
            Eq("and the connection is still in step", "app_current", Json.Str(d.Call("app_current", null), "tool"));
        }

        private static void StartOpensTheSessionAndQuitEndsIt()
        {
            Environment.SetEnvironmentVariable("MOBIUM_FAKE", "ok");
            var d = Device.Builder().Binary(FakeBinary()).Platform("ios").App("com.apple.Preferences").Start();
            var s = d.Session;
            Yes("start reports the device, platform, driver and app",
                s != null && s.Device == "fake-device" && s.Platform == "ios" && s.Driver == "wda" && s.App == "com.apple.Preferences");
            d.Quit();
            Yes("a second quit does nothing", Throws<Exception>(() => d.Quit()) == null);
            Yes("a call after quit fails", Throws<MobiumException>(() => d.Current()) != null);
            using (var c = Device.Builder().Binary(FakeBinary()).Connect())
                Yes("connect reports no session it did not start", c.Session == null);
            Device kept;
            using (var b = Device.Builder().Binary(FakeBinary()).Platform("android").Start())
            {
                kept = b;
                Eq("start in a using block opens a session", "uiautomator2", b.Session?.Driver);
            }
            Yes("a using block quits on the way out", Throws<MobiumException>(() => kept.Current()) != null);
            using (var b = Device.Builder().Binary(FakeBinary()).Platform("android").Start())
                b.Quit();
            Yes("disposing after an explicit quit does nothing", true);
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

        private static void Eq(string what, object? want, object? got)
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

        private static string Describe(object? o)
        {
            if (o == null) return "null";
            var s = o.ToString() ?? "";
            if (o is string) s = "\"" + s.Replace("\n", "\\n").Replace("\t", "\\t") + "\"";
            return s.Length == 0 ? "\"\"" : s;
        }
    }
}
