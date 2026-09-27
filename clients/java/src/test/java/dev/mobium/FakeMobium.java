package dev.mobium;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A stand-in for {@code mobium pipe}, so the connection's failure paths -- a
 * daemon that hangs, one that exits mid-call, one that refuses the handshake,
 * stdout carrying lines that are not the answer -- are exercised against a
 * real subprocess with no device. It speaks through the client's own
 * {@link Json}, which the tests above check separately.
 *
 * <p>{@code MOBIUM_FAKE} picks the behavior:
 * <ul>
 * <li>{@code ok} answers every call, first writing a notification, a line that
 * is not JSON, a line whose {@code \\u} escape is not hex, and a reply to
 * another id, all of which must be skipped. Tool {@code slow} answers after
 * 300ms.
 * <li>{@code hang} answers the handshake and never a tool call.
 * <li>{@code exit} answers the handshake and exits with status 3 on the first
 * tool call.
 * <li>{@code mute} never answers anything, the handshake included.
 * <li>{@code noid} answers {@code app_map} as mobium answers a line it cannot
 * parse -- an error with no id -- and every other call normally.
 * <li>{@code refuse} answers the handshake with a protocol error, then waits
 * for stdin to close as a real daemon would.
 * </ul>
 * {@code MOBIUM_FAKE_PIDFILE}, when set, receives this process's id, so a test
 * can tell whether it was left running.
 */
final class FakeMobium {

    private FakeMobium() {}

    public static void main(String[] args) throws Exception {
        String mode = System.getenv().getOrDefault("MOBIUM_FAKE", "ok");
        String pidFile = System.getenv("MOBIUM_FAKE_PIDFILE");
        if (pidFile != null && !pidFile.isEmpty()) {
            Files.writeString(Path.of(pidFile), Long.toString(ProcessHandle.current().pid()));
        }
        BufferedReader in = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8));
        PrintStream out = new PrintStream(System.out, true, StandardCharsets.UTF_8);

        String line;
        while ((line = in.readLine()) != null) {
            Map<String, Object> msg = Json.asObject(Json.parse(line));
            Object id = msg.get("id");
            if (id == null) continue; // a notification
            String method = Json.str(msg, "method");

            if (mode.equals("mute")) continue;
            if (method.equals("initialize")) {
                if (mode.equals("refuse")) {
                    out.println("{\"jsonrpc\":\"2.0\",\"id\":" + id
                            + ",\"error\":{\"code\":-32602,\"message\":\"unsupported protocol version\"}}");
                } else {
                    reply(out, id, Map.of());
                }
                continue;
            }
            if (mode.equals("hang")) continue;
            if (mode.equals("exit")) System.exit(3);

            Map<String, Object> params = Json.asObject(msg.get("params"));
            String name = Json.str(params, "name");
            Object arguments = params.get("arguments");
            if (name.equals("slow")) Thread.sleep(300);
            if (name.equals("app_session")) {
                // Answers as the daemon does: start names the device it got,
                // and echoes the platform and app it was asked for.
                Map<String, Object> a = Json.asObject(arguments);
                String action = Json.str(a, "action");
                Map<String, Object> view = new LinkedHashMap<>();
                view.put("action", action.isEmpty() ? "status" : action);
                if (action.equals("start")) {
                    String platform = Json.str(a, "platform").isEmpty() ? "android" : Json.str(a, "platform");
                    view.put("device", "fake-device");
                    view.put("platform", platform);
                    view.put("driver", platform.equals("ios") ? "wda" : "uiautomator2");
                    view.put("app", Json.str(a, "app"));
                } else if (action.equals("end")) {
                    view.put("device", Json.str(a, "device"));
                    view.put("ended", true);
                }
                view.put("sessions", List.of());
                reply(out, id, Map.of("content", List.of(Map.of("type", "text", "text", "session")), "structuredContent", view));
                continue;
            }
            if (mode.equals("noid") && name.equals("app_map")) {
                out.println("{\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32700,\"message\":\"Parse error\","
                        + "\"data\":\"invalid character 'N' looking for beginning of value\"}}");
                continue;
            }

            out.println("{\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}");
            out.println("progress: this line is not JSON");
            out.println("{\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"text\":\"\\uzzzz\"}}");
            reply(out, ((Long) id) + 1000, Map.of("wrong", true));
            Map<String, Object> structured = new LinkedHashMap<>();
            structured.put("tool", name);
            structured.put("echo", arguments);
            reply(out, id, Map.of(
                    "content", List.of(Map.of("type", "text", "text", "ok " + name)),
                    "structuredContent", structured));
        }
    }

    private static void reply(PrintStream out, Object id, Map<String, Object> result) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("jsonrpc", "2.0");
        m.put("id", id);
        m.put("result", result);
        out.println(Json.write(m));
    }
}
