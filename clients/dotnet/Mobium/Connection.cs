using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Text;

namespace Mobium
{
    /// <summary>
    /// A live <c>mobium pipe</c> subprocess speaking JSON-RPC over stdio.
    /// </summary>
    /// <remarks>
    /// <c>pipe</c> rather than <c>mcp</c>, and this is not a detail: a
    /// device-side server holds one session per device, so a client that
    /// started its own would invalidate the CLI's and the CLI's would
    /// invalidate this one. <c>pipe</c> forwards to the shared daemon instead.
    /// </remarks>
    internal sealed class Connection : IDisposable
    {
        private readonly Process _process;
        private readonly StreamWriter _stdin;
        private readonly StreamReader _stdout;
        private int _nextId;

        internal Connection(string binary, IList<string> args)
        {
            var info = new ProcessStartInfo(binary)
            {
                UseShellExecute = false,
                RedirectStandardInput = true,
                RedirectStandardOutput = true,
                // Progress notes about downloading a device-side server go to
                // stderr. Letting them through keeps a slow first run
                // explicable rather than looking like a hang.
                RedirectStandardError = false,
            };
            // netstandard2.0 has no ProcessStartInfo.ArgumentList, so the
            // command line is built by hand. A device serial or a path with a
            // space in it would otherwise arrive as two arguments -- the same
            // failure that truncated a notification body to its first word
            // when it went through `adb shell` unquoted.
            var all = new List<string> { "pipe" };
            all.AddRange(args);
            info.Arguments = BuildArguments(all);

            try
            {
                _process = Process.Start(info);
            }
            catch (Exception e)
            {
                throw new MobiumException("could not start " + binary, e);
            }
            if (_process == null) throw new MobiumException("could not start " + binary);

            // Both streams are wrapped by hand rather than through
            // ProcessStartInfo's encoding properties: StandardInputEncoding
            // does not exist in netstandard2.0, so stdin would otherwise be
            // written in the console's codepage. Android's own Settings has a
            // non-breaking hyphen in "Wi‑Fi", and a locator carrying one has
            // to arrive byte-for-byte or it matches nothing. No BOM, for the
            // same reason — mobium reads lines, not files.
            var utf8 = new UTF8Encoding(false);
            _stdin = new StreamWriter(_process.StandardInput.BaseStream, utf8) { AutoFlush = false };
            _stdout = new StreamReader(_process.StandardOutput.BaseStream, utf8);

            Initialize();
        }

        private void Initialize()
        {
            Request("initialize", new Dictionary<string, object>(StringComparer.Ordinal)
            {
                ["protocolVersion"] = "2024-11-05",
                ["capabilities"] = new Dictionary<string, object>(StringComparer.Ordinal),
                ["clientInfo"] = new Dictionary<string, object>(StringComparer.Ordinal)
                {
                    ["name"] = "mobium-dotnet",
                    ["version"] = "0.1.0",
                },
            });
            Write(new Dictionary<string, object>(StringComparer.Ordinal)
            {
                ["jsonrpc"] = "2.0",
                ["method"] = "notifications/initialized",
            });
        }

        private void Write(IDictionary<string, object> payload)
        {
            if (_process.HasExited)
                throw new MobiumException("mobium exited with status " + _process.ExitCode);
            try
            {
                _stdin.Write(Json.Write(payload));
                _stdin.Write('\n');
                _stdin.Flush();
            }
            catch (IOException e)
            {
                throw new MobiumException("mobium closed the connection", e);
            }
        }

        private object Request(string method, IDictionary<string, object> parameters)
        {
            var id = ++_nextId;
            var payload = new Dictionary<string, object>(StringComparer.Ordinal)
            {
                ["jsonrpc"] = "2.0",
                ["id"] = id,
                ["method"] = method,
            };
            if (parameters != null) payload["params"] = parameters;
            Write(payload);

            while (true)
            {
                string line;
                try
                {
                    line = _stdout.ReadLine();
                }
                catch (IOException e)
                {
                    throw new MobiumException("mobium closed the connection while answering " + method, e);
                }
                if (line == null)
                    throw new MobiumException("mobium closed the connection without answering " + method);

                IDictionary<string, object> message;
                try
                {
                    message = Json.AsObject(Json.Parse(line));
                }
                catch (MobiumException)
                {
                    continue; // not a message we can read; keep looking
                }

                if (!message.TryGetValue("id", out var gotId) || !(gotId is long got) || got != id)
                    continue; // a notification, or a reply to something else

                if (message.TryGetValue("error", out var error) && error != null)
                {
                    var e = Json.AsObject(error);
                    var detail = Json.Str(e, "data");
                    var msg = Json.Str(e, "message");
                    // A protocol error: the request itself was refused.
                    throw new InvalidArgumentException(detail.Length == 0 ? msg : msg + ": " + detail,
                        "", "", false, null);
                }
                return message.TryGetValue("result", out var result) ? result : null;
            }
        }

        /// <summary>Runs a tool, turning a tool-level failure into an exception.</summary>
        internal IDictionary<string, object> Call(string tool, IDictionary<string, object> arguments)
        {
            var result = Json.AsObject(Request("tools/call", new Dictionary<string, object>(StringComparer.Ordinal)
            {
                ["name"] = tool,
                ["arguments"] = arguments ?? new Dictionary<string, object>(StringComparer.Ordinal),
            }));
            // A failing tool answers with isError and its reason in the
            // content rather than a protocol error, so the reason has to be
            // lifted out.
            if (Json.Bool(result, "isError"))
            {
                result.TryGetValue("structuredContent", out var structured);
                throw MobiumException.From(TextOf(result), tool, structured);
            }
            return result;
        }

        internal static string TextOf(IDictionary<string, object> result)
        {
            var b = new StringBuilder();
            result.TryGetValue("content", out var content);
            foreach (var block in Json.AsArray(content))
            {
                var c = Json.AsObject(block);
                if (Json.Str(c, "type") == "text")
                {
                    if (b.Length > 0) b.Append('\n');
                    b.Append(Json.Str(c, "text"));
                }
            }
            return b.ToString().Trim();
        }

        internal static IDictionary<string, object> DataOf(IDictionary<string, object> result) =>
            result.TryGetValue("structuredContent", out var s) && s != null
                ? Json.AsObject(s)
                : new Dictionary<string, object>(StringComparer.Ordinal);

        public void Dispose()
        {
            try
            {
                // Closing stdin is how mobium is asked to exit.
                _stdin.Dispose();
            }
            catch (IOException)
            {
                // If that fails the wait below times out and it is killed.
            }
            try
            {
                if (!_process.WaitForExit(10000)) _process.Kill();
            }
            catch (InvalidOperationException)
            {
                // Already gone.
            }
            _process.Dispose();
        }

        /// <summary>
        /// Locates the mobium executable. MOBIUM_BIN_PATH wins, so a test run
        /// can pin a specific build — the same escape hatch the other clients
        /// have.
        /// </summary>
        internal static string FindBinary(string explicitPath)
        {
            var name = IsWindows ? "mobium.exe" : "mobium";
            foreach (var candidate in new[] { explicitPath, Environment.GetEnvironmentVariable("MOBIUM_BIN_PATH") })
            {
                if (string.IsNullOrWhiteSpace(candidate)) continue;
                if (File.Exists(candidate)) return candidate;
                throw new MobiumException(candidate + " is not an executable mobium binary");
            }

            var path = Environment.GetEnvironmentVariable("PATH");
            if (path != null)
            {
                foreach (var dir in path.Split(Path.PathSeparator))
                {
                    if (dir.Length == 0) continue;
                    var p = Path.Combine(dir, name);
                    if (File.Exists(p)) return p;
                }
            }

            foreach (var rel in new[] { "./bin/" + name, "../bin/" + name, "../../bin/" + name })
            {
                if (File.Exists(rel)) return Path.GetFullPath(rel);
            }

            throw new MobiumException(
                "mobium not found — put it on PATH or set MOBIUM_BIN_PATH to the binary");
        }

        private static bool IsWindows =>
            Environment.OSVersion.Platform == PlatformID.Win32NT;

        /// <summary>
        /// Joins arguments into one command line, quoted so the runtime parses
        /// back exactly what went in.
        /// </summary>
        /// <remarks>
        /// Follows the CommandLineToArgvW rules, which is what .NET's own
        /// parser applies when it is handed a single string. The backslash
        /// case is the subtle one: a backslash only escapes when it runs into
        /// a quote, so the run before one has to be doubled and a run
        /// elsewhere must not be.
        /// </remarks>
        internal static string BuildArguments(IList<string> args)
        {
            var b = new StringBuilder();
            foreach (var arg in args)
            {
                if (b.Length > 0) b.Append(' ');
                if (arg.Length > 0 && arg.IndexOfAny(new[] { ' ', '\t', '\n', '\v', '"' }) < 0)
                {
                    b.Append(arg);
                    continue;
                }
                b.Append('"');
                for (var i = 0; i < arg.Length; i++)
                {
                    var slashes = 0;
                    while (i < arg.Length && arg[i] == '\\') { slashes++; i++; }
                    if (i == arg.Length)
                    {
                        // Trailing backslashes run into the closing quote.
                        b.Append('\\', slashes * 2);
                        break;
                    }
                    if (arg[i] == '"')
                    {
                        b.Append('\\', slashes * 2 + 1).Append('"');
                    }
                    else
                    {
                        b.Append('\\', slashes).Append(arg[i]);
                    }
                }
                b.Append('"');
            }
            return b.ToString();
        }
    }
}
