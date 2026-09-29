using System;
using System.IO;
using System.Linq;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Threading;

namespace Mobium.Tests
{
    /// <summary>
    /// A stand-in for <c>mobium pipe</c>: this same test program, started with
    /// <c>pipe</c> as its first argument. It lets the connection's failure
    /// paths -- a daemon that hangs, one that exits mid-call, stdout carrying
    /// lines that are not the answer -- be exercised against a real
    /// subprocess, on every platform, with no shell script and no device.
    /// </summary>
    /// <remarks>
    /// <c>MOBIUM_FAKE</c> picks the behavior. The test binary may use
    /// System.Text.Json freely: it targets net10.0, where it is part of the
    /// framework, and nothing here ships.
    /// <list type="bullet">
    /// <item><c>ok</c> answers every call, first writing a notification, a
    /// line that is not JSON and a reply to another id, all of which must be
    /// skipped. Tool <c>slow</c> answers after 300ms.</item>
    /// <item><c>hang</c> answers the handshake and never a tool call.</item>
    /// <item><c>exit</c> answers the handshake and exits with status 3 on the
    /// first tool call.</item>
    /// <item><c>mute</c> never answers anything, the handshake included.</item>
    /// <item><c>argv</c> answers every call with the arguments this process
    /// was started with, so quoting can be checked by what arrived.</item>
    /// <item>In <c>ok</c>, <c>app_map</c> also answers with a map view, and
    /// with <c>{"diff": true}</c> a diff: <c>first</c> on the process's first
    /// map, what changed on every later one.</item>
    /// <item><c>noid</c> answers <c>app_map</c> as mobium answers a line it
    /// cannot parse -- an error with no id -- and every other call normally.</item>
    /// <item><c>refuse</c> answers the handshake with a protocol error, and
    /// then waits for stdin to close like a real daemon would.</item>
    /// </list>
    /// <c>MOBIUM_FAKE_NOTIFYLOG</c>, when set, has each notification's method
    /// appended.
    /// <c>MOBIUM_FAKE_PIDFILE</c>, when set, receives this process's id, so a
    /// test can tell whether it was left running.
    /// </remarks>
    internal static class FakeMobium
    {
        internal static int Run()
        {
            var mode = Environment.GetEnvironmentVariable("MOBIUM_FAKE") ?? "ok";
            var pidFile = Environment.GetEnvironmentVariable("MOBIUM_FAKE_PIDFILE");
            if (!string.IsNullOrEmpty(pidFile)) File.WriteAllText(pidFile, Environment.ProcessId.ToString());

            var notifyLog = Environment.GetEnvironmentVariable("MOBIUM_FAKE_NOTIFYLOG");
            if (!string.IsNullOrEmpty(notifyLog))
                File.AppendAllText(notifyLog, "session=" + (Environment.GetEnvironmentVariable("MOBIUM_SESSION") ?? "") + "\n");
            var stdin = new StreamReader(Console.OpenStandardInput(), new UTF8Encoding(false));
            var stdout = new StreamWriter(Console.OpenStandardOutput(), new UTF8Encoding(false)) { AutoFlush = true };

            var maps = 0;
            string? line;
            while ((line = stdin.ReadLine()) != null)
            {
                var msg = JsonNode.Parse(line)!.AsObject();
                if (!msg.TryGetPropertyValue("id", out var idNode) || idNode == null)
                {
                    // A notification.
                    var log = Environment.GetEnvironmentVariable("MOBIUM_FAKE_NOTIFYLOG");
                    if (!string.IsNullOrEmpty(log) && msg["method"] != null)
                        File.AppendAllText(log, msg["method"]!.GetValue<string>() + "\n");
                    continue;
                }
                var id = idNode.GetValue<long>();
                var method = msg["method"]!.GetValue<string>();

                if (mode == "mute") continue;
                if (method == "initialize" && mode == "refuse")
                {
                    stdout.WriteLine("{\"jsonrpc\":\"2.0\",\"id\":" + id + ",\"error\":{\"code\":-32602,\"message\":\"unsupported protocol version\"}}");
                    continue;
                }
                if (method == "initialize")
                {
                    Reply(stdout, id, new JsonObject());
                    continue;
                }
                if (mode == "hang") continue;
                if (mode == "exit") return 3;

                var name = msg["params"]!["name"]!.GetValue<string>();
                var args = msg["params"]!["arguments"]!.DeepClone();
                if (name == "slow") Thread.Sleep(300);
                if (name == "app_session")
                {
                    // Answers as the daemon does: start names the device it
                    // got, and echoes the platform and app it was asked for.
                    var action = (string?)args?["action"] ?? "";
                    var view = new JsonObject { ["action"] = action.Length == 0 ? "status" : action, ["sessions"] = new JsonArray() };
                    if (action == "start")
                    {
                        var platform = (string?)args?["platform"] ?? "android";
                        view["device"] = "fake-device";
                        view["platform"] = platform;
                        view["driver"] = platform == "ios" ? "wda" : "uiautomator2";
                        view["app"] = (string?)args?["app"] ?? "";
                    }
                    else if (action == "end")
                    {
                        view["device"] = (string?)args?["device"] ?? "";
                        view["ended"] = true;
                    }
                    Reply(stdout, id, new JsonObject
                    {
                        ["content"] = new JsonArray(new JsonObject { ["type"] = "text", ["text"] = "session" }),
                        ["structuredContent"] = view,
                    });
                    continue;
                }
                if (mode == "noid" && name == "app_map")
                {
                    stdout.WriteLine("{\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32700,\"message\":\"Parse error\",\"data\":\"invalid character 'N'\"}}");
                    continue;
                }

                stdout.WriteLine("{\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}");
                stdout.WriteLine("progress: this line is not JSON");
                Reply(stdout, id + 1000, new JsonObject { ["wrong"] = true });
                var structured = new JsonObject
                {
                    ["tool"] = name,
                    ["echo"] = args?.DeepClone(),
                    ["argv"] = new JsonArray(Environment.GetCommandLineArgs()[1..].Select(a => (JsonNode?)JsonValue.Create(a)).ToArray()),
                };
                if (name == "app_download") Download(structured, args);
                if (name == "app_map") MapDiff(structured, args, ref maps);
                Reply(stdout, id, new JsonObject
                {
                    ["content"] = new JsonArray(new JsonObject { ["type"] = "text", ["text"] = "ok " + name }),
                    ["structuredContent"] = structured,
                });
            }
            return 0;
        }

        // app_download answers in the daemon's three shapes: no name lists the
        // folder; a name with no path carries the file, base64, whose content
        // here is the name itself -- and, as the daemon's omitempty does, no
        // data field at all for an empty file; a name with a path is saved.
        private static void Download(JsonObject view, JsonNode? args)
        {
            var file = (string?)args?["name"] ?? "";
            if (file.Length == 0)
            {
                view["folder"] = "the Download folder";
                view["files"] = new JsonArray(
                    new JsonObject { ["name"] = "report.pdf", ["bytes"] = 3, ["modified"] = "2026-09-29T10:00:00Z" },
                    new JsonObject { ["name"] = "notes.txt", ["bytes"] = 0, ["modified"] = "2026-09-29T10:01:00Z" });
                return;
            }
            var content = file == "empty.txt" ? "" : file;
            view["name"] = file;
            view["bytes"] = Encoding.UTF8.GetByteCount(content);
            var path = (string?)args?["path"];
            if (path != null) view["path"] = path;
            else if (file == "garbled.bin") view["data"] = "not base64!";
            else if (content.Length > 0) view["data"] = Convert.ToBase64String(Encoding.UTF8.GetBytes(content));
        }

        // app_map with diff answers as the daemon does: the first map of the
        // device has nothing to compare with, so it says first and lists
        // nothing; each later one names what appeared, went away and changed.
        // It answers a diff only for exactly {"diff": true}, so a diff coming
        // back is evidence of what was sent.
        private static void MapDiff(JsonObject view, JsonNode? args, ref int maps)
        {
            view["elements"] = new JsonArray(Element("@e1", "Sign in", "button"));
            view["context"] = "NATIVE_APP";
            view["device"] = "fake-device";
            var sent = args as JsonObject;
            var wanted = sent != null && sent.Count == 1 && sent["diff"] is JsonValue v
                && v.TryGetValue<bool>(out var b) && b;
            maps++;
            if (!wanted) return;
            if (maps == 1)
            {
                view["diff"] = new JsonObject
                {
                    ["first"] = true,
                    ["added"] = new JsonArray(), ["removed"] = new JsonArray(), ["changed"] = new JsonArray(),
                };
                return;
            }
            view["diff"] = new JsonObject
            {
                ["since"] = "2026-09-29T10:00:00Z",
                ["added"] = new JsonArray(Element("@e1", "Sign in", "button")),
                ["removed"] = new JsonArray(Element("@e2", "Loading", "")),
                ["changed"] = new JsonArray(new JsonObject
                {
                    ["before"] = Element("@e3", "Remember me", "checkbox"),
                    ["after"] = Element("@e2", "Remember me", "checkbox"),
                    ["what"] = new JsonArray("checked", "moved"),
                }),
            };
        }

        private static JsonObject Element(string @ref, string label, string role)
        {
            var e = new JsonObject { ["ref"] = @ref, ["label"] = label };
            if (role.Length > 0) e["role"] = role;
            return e;
        }

        private static void Reply(StreamWriter w, long id, JsonNode result)
        {
            var reply = new JsonObject { ["jsonrpc"] = "2.0", ["id"] = id, ["result"] = result };
            w.WriteLine(reply.ToJsonString(new JsonSerializerOptions { WriteIndented = false }));
        }
    }
}
