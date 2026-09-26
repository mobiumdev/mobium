package dev.mobium;

import java.io.BufferedReader;
import java.io.File;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.Writer;
import java.io.OutputStreamWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;

/**
 * A live {@code mobium pipe} subprocess speaking JSON-RPC over stdio.
 *
 * <p>{@code pipe} rather than {@code mcp}, and this is not a detail: a
 * device-side server holds one session per device, so a client that started
 * its own would invalidate the CLI's and the CLI's would invalidate this one.
 * {@code pipe} forwards to the shared daemon instead.
 */
final class Connection implements AutoCloseable {

    private final Process process;
    private final Writer stdin;
    private final BufferedReader stdout;
    private int nextId;

    Connection(String binary, List<String> args) {
        List<String> command = new ArrayList<>();
        command.add(binary);
        command.add("pipe");
        command.addAll(args);

        ProcessBuilder pb = new ProcessBuilder(command);
        // Progress notes about downloading a device-side server go to stderr.
        // Passing them through keeps a slow first run explicable rather than
        // looking like a hang.
        pb.redirectError(ProcessBuilder.Redirect.INHERIT);
        try {
            this.process = pb.start();
        } catch (IOException e) {
            throw new MobiumException("could not start " + binary, e);
        }
        this.stdin = new OutputStreamWriter(process.getOutputStream(), StandardCharsets.UTF_8);
        this.stdout = new BufferedReader(
                new InputStreamReader(process.getInputStream(), StandardCharsets.UTF_8));
        initialize();
    }

    private void initialize() {
        Map<String, Object> params = new LinkedHashMap<>();
        params.put("protocolVersion", "2024-11-05");
        params.put("capabilities", Map.of());
        params.put("clientInfo", Map.of("name", "mobium-java", "version", "0.1.0"));
        request("initialize", params);
        notification();
    }

    private void notification() {
        Map<String, Object> payload = new LinkedHashMap<>();
        payload.put("jsonrpc", "2.0");
        payload.put("method", "notifications/initialized");
        write(payload);
    }

    private void write(Map<String, Object> payload) {
        if (!process.isAlive()) {
            throw new MobiumException("mobium exited with status " + process.exitValue());
        }
        try {
            stdin.write(Json.write(payload));
            stdin.write('\n');
            stdin.flush();
        } catch (IOException e) {
            throw new MobiumException("mobium closed the connection", e);
        }
    }

    private Object request(String method, Map<String, Object> params) {
        int id = ++nextId;
        Map<String, Object> payload = new LinkedHashMap<>();
        payload.put("jsonrpc", "2.0");
        payload.put("id", id);
        payload.put("method", method);
        if (params != null) payload.put("params", params);
        write(payload);

        while (true) {
            String line;
            try {
                line = stdout.readLine();
            } catch (IOException e) {
                throw new MobiumException("mobium closed the connection while answering " + method, e);
            }
            if (line == null) {
                throw new MobiumException("mobium closed the connection without answering " + method);
            }
            Map<String, Object> message;
            try {
                message = Json.asObject(Json.parse(line));
            } catch (MobiumException unreadable) {
                continue; // not a message we can read; keep looking
            }
            Object gotId = message.get("id");
            if (!(gotId instanceof Number) || ((Number) gotId).intValue() != id) {
                continue; // a notification, or a reply to something else
            }
            Object error = message.get("error");
            if (error != null) {
                Map<String, Object> e = Json.asObject(error);
                String detail = Json.str(e, "data");
                String msg = Json.str(e, "message");
                // A protocol error: the request itself was refused.
                throw new InvalidArgumentException(detail.isEmpty() ? msg : msg + ": " + detail,
                        "", "", false, Map.of());
            }
            return message.get("result");
        }
    }

    /** Runs a tool, turning a tool-level failure into an exception. */
    Map<String, Object> call(String tool, Map<String, Object> arguments) {
        Map<String, Object> params = new LinkedHashMap<>();
        params.put("name", tool);
        params.put("arguments", arguments == null ? Map.of() : arguments);
        Map<String, Object> result = Json.asObject(request("tools/call", params));
        // A failing tool answers with isError and its reason in the content
        // rather than a protocol error, so the reason has to be lifted out.
        if (Json.bool(result, "isError")) {
            throw MobiumException.from(textOf(result), tool, result.get("structuredContent"));
        }
        return result;
    }

    static String textOf(Map<String, Object> result) {
        StringBuilder b = new StringBuilder();
        for (Object block : Json.asArray(result.get("content"))) {
            Map<String, Object> c = Json.asObject(block);
            if ("text".equals(Json.str(c, "type"))) {
                if (b.length() > 0) b.append('\n');
                b.append(Json.str(c, "text"));
            }
        }
        return b.toString().strip();
    }

    static Map<String, Object> dataOf(Map<String, Object> result) {
        Object structured = result.get("structuredContent");
        return structured == null ? Map.of() : Json.asObject(structured);
    }

    @Override public void close() {
        try {
            stdin.close();
        } catch (IOException ignored) {
            // Closing stdin is how mobium is asked to exit; if that fails the
            // wait below will time out and it is killed instead.
        }
        try {
            if (!process.waitFor(10, TimeUnit.SECONDS)) process.destroyForcibly();
        } catch (InterruptedException e) {
            process.destroyForcibly();
            Thread.currentThread().interrupt();
        }
    }

    /**
     * Locates the mobium executable. MOBIUM_BIN_PATH wins, so a test run can
     * pin a specific build — the same escape hatch the other clients have.
     */
    static String findBinary(String explicit) {
        for (String candidate : new String[] { explicit, System.getenv("MOBIUM_BIN_PATH") }) {
            if (candidate == null || candidate.isBlank()) continue;
            if (executable(Paths.get(candidate))) return candidate;
            throw new MobiumException(candidate + " is not an executable mobium binary");
        }
        String path = System.getenv("PATH");
        if (path != null) {
            for (String dir : path.split(File.pathSeparator)) {
                Path p = Paths.get(dir, "mobium");
                if (executable(p)) return p.toString();
            }
        }
        for (String rel : new String[] { "./bin/mobium", "../bin/mobium", "../../bin/mobium" }) {
            Path p = Paths.get(rel);
            if (executable(p)) return p.toAbsolutePath().toString();
        }
        throw new MobiumException(
                "mobium not found — put it on PATH or set MOBIUM_BIN_PATH to the binary");
    }

    private static boolean executable(Path p) {
        return Files.isRegularFile(p) && Files.isExecutable(p);
    }
}
