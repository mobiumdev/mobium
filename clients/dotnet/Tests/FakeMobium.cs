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

            var stdin = new StreamReader(Console.OpenStandardInput(), new UTF8Encoding(false));
            var stdout = new StreamWriter(Console.OpenStandardOutput(), new UTF8Encoding(false)) { AutoFlush = true };

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
                Reply(stdout, id, new JsonObject
                {
                    ["content"] = new JsonArray(new JsonObject { ["type"] = "text", ["text"] = "ok " + name }),
                    ["structuredContent"] = new JsonObject
                    {
                        ["tool"] = name,
                        ["echo"] = args,
                        ["argv"] = new JsonArray(Environment.GetCommandLineArgs()[1..].Select(a => (JsonNode?)JsonValue.Create(a)).ToArray()),
                    },
                });
            }
            return 0;
        }

        private static void Reply(StreamWriter w, long id, JsonNode result)
        {
            var reply = new JsonObject { ["jsonrpc"] = "2.0", ["id"] = id, ["result"] = result };
            w.WriteLine(reply.ToJsonString(new JsonSerializerOptions { WriteIndented = false }));
        }
    }
}
