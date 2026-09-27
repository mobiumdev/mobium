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
import java.time.Duration;
import java.util.Map;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

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
    private final Duration timeout;
    private long nextId;

    // Every line mobium writes, in order, put here by one reader thread. A call
    // takes from it with or without a deadline, so the timed and untimed paths
    // read the same way. END is put last, when stdout closes or fails.
    private final BlockingQueue<String> lines = new LinkedBlockingQueue<>();
    private static final String END = new String("end of stdout");
    private volatile IOException readFailure;

    // gate serializes whole request/response pairs, as the Go client's mutex
    // does. The transport is one pipe with replies told apart only by id, so
    // two calls in flight would race to read each other's answer -- and
    // before that, their requests' bytes interleave on the one stdin, which
    // was measured: two requests arrived on one line and mobium could parse
    // neither.
    private final Object gate = new Object();

    // dead says why the connection can no longer be used, once something has
    // made that true: it was closed, mobium exited, or a call timed out
    // half-way. A call abandoned half-way cannot be resynchronized -- its
    // answer would be read as the answer to the next one -- so the connection
    // ends rather than being left subtly wrong, and every call after says so.
    private volatile String dead;
    private final AtomicBoolean closed = new AtomicBoolean();

    Connection(String binary, List<String> args) { this(binary, args, null); }

    /** A null or zero timeout waits as long as each call takes. */
    Connection(String binary, List<String> args, Duration timeout) {
        this.timeout = timeout == null || timeout.isZero() ? null : timeout;
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
        BufferedReader stdout = new BufferedReader(
                new InputStreamReader(process.getInputStream(), StandardCharsets.UTF_8));
        Thread reader = new Thread(() -> {
            try {
                String line;
                while ((line = stdout.readLine()) != null) lines.add(line);
            } catch (IOException e) {
                readFailure = e;
            } finally {
                lines.add(END);
            }
        }, "mobium-pipe-reader");
        // A daemon thread, so a connection nobody closed cannot keep the JVM
        // from exiting.
        reader.setDaemon(true);
        reader.start();

        // A handshake that fails leaves a process nobody will ever close, so it
        // is ended here rather than leaked -- measured: a refused handshake
        // left mobium running.
        try {
            initialize();
        } catch (RuntimeException e) {
            close();
            throw e;
        }
    }

    private void initialize() {
        Map<String, Object> params = new LinkedHashMap<>();
        params.put("protocolVersion", "2024-11-05");
        params.put("capabilities", Map.of());
        params.put("clientInfo", Map.of("name", "mobium-java", "version", clientVersion()));
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
            throw die("mobium exited with status " + process.exitValue(), null);
        }
        try {
            // One write of the whole line: a payload and its newline written
            // separately are two chances for another writer to get between.
            stdin.write(Json.write(payload) + "\n");
            stdin.flush();
        } catch (IOException e) {
            throw die("mobium closed the connection", e);
        }
    }

    /** Marks the connection unusable, ends mobium, and returns the exception that says why. */
    private MobiumException die(String why, Throwable cause) {
        if (dead == null) dead = why;
        process.destroy();
        return cause == null ? new MobiumException(why) : new MobiumException(why, cause);
    }

    /** The next line mobium wrote, within the call timeout when there is one; null at the end. */
    private String readLine(String method) {
        String line;
        try {
            line = timeout == null ? lines.take() : lines.poll(timeout.toMillis(), TimeUnit.MILLISECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw die("interrupted while waiting for mobium to answer " + method
                    + ", so the connection was closed: a reply arriving later would be read as the answer to the next call", e);
        }
        if (line == null) {
            throw die(method + " got no answer within " + timeout.toMillis() / 1000.0
                    + "s, so the connection was closed: a reply arriving later would be read as the answer to the next call. "
                    + "Connect again, with a longer callTimeout if the device is slow", null);
        }
        if (line == END) {
            lines.add(END); // every later read sees the end too
            if (readFailure != null) throw die("mobium closed the connection while answering " + method, readFailure);
            return null;
        }
        return line;
    }

    private Object request(String method, Map<String, Object> params) {
        synchronized (gate) {
            if (dead != null) throw new MobiumException("this mobium connection is no longer usable: " + dead);
            return requestLocked(method, params);
        }
    }

    private Object requestLocked(String method, Map<String, Object> params) {
        long id = ++nextId;
        Map<String, Object> payload = new LinkedHashMap<>();
        payload.put("jsonrpc", "2.0");
        payload.put("id", id);
        payload.put("method", method);
        if (params != null) payload.put("params", params);
        write(payload);

        while (true) {
            String line = readLine(method);
            if (line == null) {
                String status = "";
                try {
                    if (process.waitFor(2, TimeUnit.SECONDS)) status = " (it exited with status " + process.exitValue() + ")";
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
                throw die("mobium closed the connection without answering " + method + status, null);
            }
            Map<String, Object> message;
            try {
                message = Json.asObject(Json.parse(line));
            } catch (MobiumException unreadable) {
                continue; // not a message we can read; keep looking
            }
            Object gotId = message.get("id");
            if (!(gotId instanceof Long) || (Long) gotId != id) {
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

    /**
     * Asks mobium to exit and waits for it, killing it after ten seconds. Safe
     * to call more than once, and from another thread while a call is
     * waiting: that call then fails, saying the connection was closed.
     */
    @Override public void close() {
        if (!closed.compareAndSet(false, true)) return;
        if (dead == null) dead = "it was closed";
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
     * Locates the mobium executable: the explicit path, then MOBIUM_BIN_PATH,
     * then PATH — and nothing else. MOBIUM_BIN_PATH wins, so a test run can
     * pin a specific build — the same escape hatch the other clients have.
     *
     * <p>The current directory is never searched, not in ./bin and not
     * through a relative PATH entry such as {@code .}: a library that runs
     * whatever mobium sits where a test was started runs a binary anyone
     * could have planted there.
     */
    static String findBinary(String explicit) {
        for (String candidate : new String[] { explicit, System.getenv("MOBIUM_BIN_PATH") }) {
            if (candidate == null || candidate.isBlank()) continue;
            if (executable(Paths.get(candidate))) return candidate;
            throw new MobiumException(candidate + " is not an executable mobium binary");
        }
        String name = System.getProperty("os.name", "").startsWith("Windows") ? "mobium.exe" : "mobium";
        String found = searchPath(System.getenv("PATH"), name);
        if (found != null) return found;
        throw new MobiumException(
                "mobium not found — put it on PATH or set MOBIUM_BIN_PATH to the binary");
    }

    /**
     * The version this client reports to mobium: the jar's
     * Implementation-Version, which Maven writes from pom.xml, so it is
     * stated once. Run from a classes directory there is no manifest, and it
     * says so rather than inventing a number.
     */
    static String clientVersion() {
        String v = Connection.class.getPackage().getImplementationVersion();
        return v == null ? "unpackaged" : v;
    }

    /** The first executable {@code name} in an absolute directory of {@code path}, or null. */
    static String searchPath(String path, String name) {
        if (path == null) return null;
        for (String dir : path.split(File.pathSeparator, -1)) {
            if (dir.isEmpty() || !Paths.get(dir).isAbsolute()) continue;
            Path p = Paths.get(dir, name);
            if (executable(p)) return p.toString();
        }
        return null;
    }

    private static boolean executable(Path p) {
        return Files.isRegularFile(p) && Files.isExecutable(p);
    }
}
