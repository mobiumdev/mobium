using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Reflection;
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
        private readonly TimeSpan _timeout;
        private int _nextId;

        // _gate serializes whole request/response pairs, as the Go client's
        // mutex does. The transport is one pipe with replies told apart only
        // by id, so two calls in flight would race to read each other's
        // answer.
        private readonly object _gate = new object();

        // _dead says why the connection can no longer be used, once something
        // has made that true: it was disposed, mobium exited, or a call timed
        // out half-way. A call abandoned half-way cannot be resynchronized --
        // its answer would be read as the answer to the next one -- so the
        // connection ends rather than being left subtly wrong, and every call
        // after says so plainly.
        private string? _dead;

        internal Connection(string binary, IList<string> args, TimeSpan timeout)
        {
            _timeout = timeout;
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

            Process? started;
            try
            {
                started = Process.Start(info);
            }
            catch (Exception e) when (e is System.ComponentModel.Win32Exception || e is InvalidOperationException
                                      || e is IOException || e is UnauthorizedAccessException)
            {
                throw new MobiumException("could not start " + binary + ": " + e.Message, e);
            }
            _process = started ?? throw new MobiumException("could not start " + binary);

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

            // A handshake that fails leaves a process nobody will ever close,
            // so it is ended here rather than leaked.
            try
            {
                Initialize();
            }
            catch
            {
                Dispose();
                throw;
            }
        }

        /// <summary>The version this client reports to mobium: the assembly's own.</summary>
        internal static string ClientVersion
        {
            get
            {
                var v = typeof(Connection).GetTypeInfo().Assembly.GetName().Version;
                return v == null ? "0.0.0" : v.Major + "." + v.Minor + "." + v.Build;
            }
        }

        private void Initialize()
        {
            Request("initialize", new Dictionary<string, object?>(StringComparer.Ordinal)
            {
                ["protocolVersion"] = "2024-11-05",
                ["capabilities"] = new Dictionary<string, object?>(StringComparer.Ordinal),
                ["clientInfo"] = new Dictionary<string, object?>(StringComparer.Ordinal)
                {
                    ["name"] = "mobium-dotnet",
                    ["version"] = ClientVersion,
                },
            });
            Write(new Dictionary<string, object?>(StringComparer.Ordinal)
            {
                ["jsonrpc"] = "2.0",
                ["method"] = "notifications/initialized",
            });
        }

        private void Write(IDictionary<string, object?> payload)
        {
            if (_process.HasExited)
                throw Die("mobium exited with status " + _process.ExitCode);
            try
            {
                _stdin.Write(Json.Write(payload));
                _stdin.Write('\n');
                _stdin.Flush();
            }
            catch (Exception e) when (e is IOException || e is ObjectDisposedException)
            {
                throw Die("mobium closed the connection", e);
            }
        }

        /// <summary>Marks the connection unusable and returns the exception that says why.</summary>
        private MobiumException Die(string why, Exception? cause = null)
        {
            if (_dead == null) _dead = why;
            try
            {
                if (!_process.HasExited) _process.Kill();
            }
            catch (Exception e) when (e is InvalidOperationException || e is System.ComponentModel.Win32Exception)
            {
                // Already gone, or going.
            }
            return cause == null ? new MobiumException(why) : new MobiumException(why, cause);
        }

        /// <summary>Reads one line, within the call timeout when there is one.</summary>
        private string? ReadLine(string method)
        {
            if (_timeout == System.Threading.Timeout.InfiniteTimeSpan)
            {
                try
                {
                    return _stdout.ReadLine();
                }
                catch (Exception e) when (e is IOException || e is ObjectDisposedException)
                {
                    throw Die("mobium closed the connection while answering " + method, e);
                }
            }

            // A read abandoned here is left running, and nothing reads stdout
            // after it: the connection is dead by then, and killing mobium
            // ends the read.
            var read = _stdout.ReadLineAsync();
            bool done;
            try
            {
                done = read.Wait(_timeout);
            }
            catch (AggregateException e)
            {
                throw Die("mobium closed the connection while answering " + method, e.InnerException ?? e);
            }
            if (done) return read.Result;
            throw Die(method + " got no answer within " + _timeout.TotalSeconds.ToString(System.Globalization.CultureInfo.InvariantCulture)
                + "s, so the connection was closed: a reply arriving later would be read as the answer to the next call. "
                + "Connect again, with a longer CallTimeout if the device is slow");
        }

        private object? Request(string method, IDictionary<string, object?>? parameters)
        {
            lock (_gate)
            {
                if (_dead != null)
                    throw new MobiumException("this mobium connection is no longer usable: " + _dead);
                return RequestLocked(method, parameters);
            }
        }

        private object? RequestLocked(string method, IDictionary<string, object?>? parameters)
        {
            var id = ++_nextId;
            var payload = new Dictionary<string, object?>(StringComparer.Ordinal)
            {
                ["jsonrpc"] = "2.0",
                ["id"] = id,
                ["method"] = method,
            };
            if (parameters != null) payload["params"] = parameters;
            Write(payload);

            while (true)
            {
                var line = ReadLine(method);
                if (line == null)
                {
                    var status = "";
                    try
                    {
                        if (_process.WaitForExit(2000)) status = " (it exited with status " + _process.ExitCode + ")";
                    }
                    catch (InvalidOperationException)
                    {
                        // No status to report.
                    }
                    throw Die("mobium closed the connection without answering " + method + status);
                }

                IDictionary<string, object?> message;
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
        internal IDictionary<string, object?> Call(string tool, IDictionary<string, object?>? arguments)
        {
            var result = Json.AsObject(Request("tools/call", new Dictionary<string, object?>(StringComparer.Ordinal)
            {
                ["name"] = tool,
                ["arguments"] = arguments ?? new Dictionary<string, object?>(StringComparer.Ordinal),
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

        internal static string TextOf(IDictionary<string, object?> result)
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

        internal static IDictionary<string, object?> DataOf(IDictionary<string, object?> result) =>
            result.TryGetValue("structuredContent", out var s) && s != null
                ? Json.AsObject(s)
                : new Dictionary<string, object?>(StringComparer.Ordinal);

        /// <summary>
        /// Asks mobium to exit and waits for it, killing it after ten seconds.
        /// Safe to call more than once, and from another thread while a call is
        /// waiting: that call then fails, saying the connection was closed.
        /// </summary>
        public void Dispose()
        {
            if (_disposed) return;
            _disposed = true;
            if (_dead == null) _dead = "it was disposed";
            try
            {
                // Closing stdin is how mobium is asked to exit.
                _stdin.Dispose();
            }
            catch (Exception e) when (e is IOException || e is ObjectDisposedException)
            {
                // If that fails the wait below times out and it is killed.
            }
            try
            {
                if (!_process.WaitForExit(10000)) _process.Kill();
            }
            catch (Exception e) when (e is InvalidOperationException || e is System.ComponentModel.Win32Exception)
            {
                // Already gone.
            }
            _process.Dispose();
        }

        private volatile bool _disposed;

        /// <summary>
        /// Locates the mobium executable: the explicit path, then
        /// MOBIUM_BIN_PATH, then PATH — and nothing else. MOBIUM_BIN_PATH wins,
        /// so a test run can pin a specific build — the same escape hatch the
        /// other clients have.
        /// </summary>
        /// <remarks>
        /// The current directory is never searched, not in ./bin and not
        /// through a relative PATH entry such as <c>.</c>: a library that runs
        /// whatever mobium sits where a test was started runs a binary anyone
        /// could have planted there. The result is always an absolute path,
        /// so Process.Start does not search again.
        /// </remarks>
        internal static string FindBinary(string? explicitPath)
        {
            var name = IsWindows ? "mobium.exe" : "mobium";
            foreach (var candidate in new[] { explicitPath, Environment.GetEnvironmentVariable("MOBIUM_BIN_PATH") })
            {
                if (string.IsNullOrWhiteSpace(candidate)) continue;
                if (File.Exists(candidate)) return candidate!;
                throw new MobiumException(candidate + " is not an executable mobium binary");
            }

            var path = Environment.GetEnvironmentVariable("PATH");
            if (path != null)
            {
                foreach (var dir in path.Split(Path.PathSeparator))
                {
                    if (dir.Length == 0 || !IsAbsolute(dir)) continue;
                    var p = Path.Combine(dir, name);
                    if (File.Exists(p)) return p;
                }
            }

            throw new MobiumException(
                "mobium not found — put it on PATH or set MOBIUM_BIN_PATH to the binary");
        }

        // Path.IsPathRooted accepts "\\bin" and "C:bin" on Windows, both of
        // which resolve against the current drive or directory; netstandard2.0
        // has no Path.IsPathFullyQualified, so it is spelled out.
        internal static bool IsAbsolute(string dir)
        {
            if (!IsWindows) return dir.StartsWith("/", StringComparison.Ordinal);
            if (dir.StartsWith(@"\\", StringComparison.Ordinal)) return true; // UNC
            return dir.Length >= 3 && char.IsLetter(dir[0]) && dir[1] == ':' && (dir[2] == '\\' || dir[2] == '/');
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
