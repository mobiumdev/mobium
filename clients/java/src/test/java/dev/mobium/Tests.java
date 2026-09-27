package dev.mobium;

import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;

/**
 * The Java client's tests, with no test framework.
 *
 * <p>JUnit would be a test-scope dependency and therefore harmless to
 * consumers, but the build's enforcer allows no dependency at all, and a main
 * method needs none. The Maven build runs this in its {@code test} phase.
 *
 * <p>Run with: {@code ./mvnw test} from clients/java, or {@code make java}.
 */
public final class Tests {

    private static int checks;
    private static int failures;

    public static void main(String[] args) {
        jsonRoundTripsStrings();
        jsonEscapesControlCharacters();
        jsonKeepsNonAsciiIntact();
        jsonReadsIntegersAsIntegers();
        jsonRejectsMalformedInput();
        jsonParsesTheShapesTheWireUses();
        elementReadsAToolResult();
        boundsComputeTheirCenter();
        waitForBuildsItsArguments();
        findBinaryRejectsSomethingThatIsNotOne();
        pathSearchSkipsRelativeDirectories();
        everyErrorCodeHasItsException();
        aToolFailureKeepsCodeRemedyAndDetails();
        anUnknownOrMissingCodeIsTheBaseException();
        jsonRefusesNestingThatWouldOverflowTheStack();
        jsonNumbersFollowTheGrammar();
        jsonEscapesAreExact();
        jsonIntegerRefusesWhatDoesNotFit();
        jsonWritesNoNumberJsonCannotCarry();
        callTimeoutMustBePositive();

        // The connection, against FakeMobium standing in for mobium.
        itSkipsWhatIsNotTheAnswer();
        concurrentCallsAreSerialized();
        aNullArgumentIsRefusedBeforeItIsSent();
        aTimedOutCallEndsTheConnection();
        anExitMidCallIsReportedWithItsStatus();
        aFailedHandshakeLeavesNoProcess();
        closeIsIdempotentAndEndsAWaitingCall();
        anAnswerWithNoIdFailsTheCallInFlight();

        System.out.printf("%n%d checks, %d failed%n", checks, failures);
        if (failures > 0) System.exit(1);
    }

    // -- the JSON layer, which is the only hand-written parsing here -------

    static void jsonRoundTripsStrings() {
        for (String s : List.of("", "plain", "with \"quotes\"", "back\\slash",
                                "new\nline", "tab\there", "emoji 🐛")) {
            Object back = Json.parse(Json.write(Map.of("k", s))) ;
            eq("round trip " + describe(s), s, Json.str(Json.asObject(back), "k"));
        }
    }

    static void jsonEscapesControlCharacters() {
        // A raw control character in a JSON string is invalid, and mobium's
        // own error messages can carry one.
        String written = Json.write(Map.of("k", "a\u0001b"));
        yes("control character escaped", written.contains("\\u0001"));
        eq("control character survives", "a\u0001b",
                Json.str(Json.asObject(Json.parse(written)), "k"));
    }

    static void jsonKeepsNonAsciiIntact() {
        // Android's own Settings has a non-breaking hyphen in "Wi‑Fi", and a
        // locator containing one must survive unchanged or it matches nothing.
        String wifi = "text=Wi\u2011Fi";
        eq("non-breaking hyphen", wifi,
                Json.str(Json.asObject(Json.parse(Json.write(Map.of("k", wifi)))), "k"));
        // And it must arrive as \u2011 or literal UTF-8, either of which parses.
        eq("escaped form parses", wifi,
                Json.str(Json.asObject(Json.parse("{\"k\":\"text=Wi\\u2011Fi\"}")), "k"));
    }

    static void jsonReadsIntegersAsIntegers() {
        // A coordinate that arrives as 540.0 prints wrongly and compares
        // wrongly. Whole numbers must stay whole.
        Map<String, Object> m = Json.asObject(Json.parse("{\"x\":540,\"y\":-12,\"s\":1.5}"));
        eq("integer stays integer", 540, Json.integer(m, "x"));
        eq("negative integer", -12, Json.integer(m, "y"));
        yes("integer is not floating point", m.get("x") instanceof Long);
        yes("real number is floating point", m.get("s") instanceof Double);
    }

    static void jsonRejectsMalformedInput() {
        for (String bad : List.of("{", "{\"a\"}", "[1,]", "tru", "{\"a\":1}x", "", "\"unterminated")) {
            boolean threw = false;
            try { Json.parse(bad); } catch (MobiumException e) { threw = true; }
            yes("rejects " + describe(bad), threw);
        }
    }

    static void jsonParsesTheShapesTheWireUses() {
        String wire = "{\"elements\":[{\"ref\":\"@e1\",\"label\":\"Sign in\",\"role\":\"button\","
                + "\"locator\":{\"kind\":\"text\",\"value\":\"Sign in\",\"exact\":true},"
                + "\"bounds\":{\"x1\":100,\"y1\":200,\"x2\":300,\"y2\":280}}],"
                + "\"context\":\"NATIVE_APP\",\"device\":\"emulator-5554\"}";
        Map<String, Object> m = Json.asObject(Json.parse(wire));
        List<Object> els = Json.asArray(m.get("elements"));
        eq("one element", 1, els.size());
        Element e = Element.from(Json.asObject(els.get(0)));
        eq("ref", "@e1", e.ref());
        eq("locator", "text=Sign in", e.locator());
        eq("bounds", "[100,200][300,280]", e.bounds().toString());
    }

    // -- the shapes ---------------------------------------------------------

    static void elementReadsAToolResult() {
        Element e = Element.from(Json.asObject(Json.parse(
                "{\"ref\":\"@e3\",\"label\":\"Go\",\"bounds\":{\"x1\":0,\"y1\":0,\"x2\":10,\"y2\":10}}")));
        eq("no role is empty, not null", "", e.role());
        eq("no locator is empty, not null", "", e.locator());
        eq("no context is empty, not null", "", e.context());
        eq("toString without a role", "@e3 Go", e.toString());
    }

    static void boundsComputeTheirCenter() {
        Bounds b = new Bounds(100, 200, 300, 280);
        eq("center x", 200, b.centerX());
        eq("center y", 240, b.centerY());
        eq("width", 200, b.width());
        eq("height", 80, b.height());
    }

    static void waitForBuildsItsArguments() {
        Map<String, Object> a = WaitFor.visible().args("@e1");
        eq("default condition", "visible", a.get("condition"));
        yes("no timeout unless asked", !a.containsKey("timeout_ms"));
        yes("no text unless asked", !a.containsKey("text"));

        Map<String, Object> b = WaitFor.text("Sent")
                .timeout(java.time.Duration.ofSeconds(30)).args("@e4");
        eq("text condition", "text", b.get("condition"));
        eq("expected text", "Sent", b.get("text"));
        eq("timeout in milliseconds", 30000L, b.get("timeout_ms"));
    }

    static void findBinaryRejectsSomethingThatIsNotOne() {
        boolean threw = false;
        try {
            Path dir = Files.createTempDirectory("mobium-test");
            Connection.findBinary(dir.resolve("not-here").toString());
        } catch (MobiumException e) {
            threw = true;
        } catch (Exception e) {
            throw new AssertionError(e);
        }
        yes("a path with no binary at it is refused", threw);
    }

    static void pathSearchSkipsRelativeDirectories() {
        // A relative PATH entry is the current directory by another name,
        // and anything could have been planted there. Java cannot change its
        // own working directory, so the entry is made relative to it instead.
        try {
            Path dir = Files.createTempDirectory("mobium-test");
            Path planted = dir.resolve("mobium");
            Files.writeString(planted, "#!/bin/sh\nexit 99\n");
            planted.toFile().setExecutable(true);
            String relative = java.nio.file.Paths.get("").toAbsolutePath().relativize(dir).toString();
            eq("a relative PATH entry is not searched", null, Connection.searchPath(relative, "mobium"));
            eq("nor is an empty one", null, Connection.searchPath("", "mobium"));
            // The positive control: the same file, by its absolute directory.
            eq("an absolute PATH entry is", planted.toString(), Connection.searchPath(dir.toString(), "mobium"));
        } catch (java.io.IOException e) {
            throw new AssertionError(e);
        }
    }

    // -- error codes: docs/decisions/0005 -----------------------------------

    static final String[] CODES = {"no_device", "device_not_ready", "toolchain_missing",
        "no_such_element", "ambiguous_locator", "element_not_reachable", "no_such_context",
        "no_such_alert", "unsupported", "not_confirmed", "timeout", "invalid_argument",
        "device_server", "internal"};

    static void everyErrorCodeHasItsException() {
        for (String code : CODES) {
            MobiumException e = MobiumException.from("x", "app_tap", Map.of("code", code));
            eq("code kept for " + code, code, e.code());
            yes(code + " has its own exception", e.getClass() != MobiumException.class);
        }
    }

    static void aToolFailureKeepsCodeRemedyAndDetails() {
        MobiumException e = MobiumException.from("no element matches text=Go", "app_tap",
                Map.of("code", "no_such_element", "remedy", "run app_map again",
                        "retryable", false, "details", Map.of("locator", "text=Go")));
        yes("no_such_element is NoSuchElementException", e instanceof NoSuchElementException);
        eq("message names the tool", "app_tap: no element matches text=Go", e.getMessage());
        eq("remedy kept", "run app_map again", e.remedy());
        eq("details kept", "text=Go", e.details().get("locator"));
        MobiumException t = MobiumException.from("timed out", "app_wait_for",
                Map.of("code", "timeout", "retryable", true));
        yes("timeout is TimedOutException and retryable", t instanceof TimedOutException && t.retryable());
    }

    static void anUnknownOrMissingCodeIsTheBaseException() {
        MobiumException u = MobiumException.from("new kind", "app_tap", Map.of("code", "something_new"));
        yes("an unknown code is the base class", u.getClass() == MobiumException.class);
        eq("an unknown code is kept", "something_new", u.code());
        MobiumException o = MobiumException.from("plain", "app_tap", null);
        eq("a daemon that sends no code gives error", "error", o.code());
    }

    // -- the smallest assertion library that will do -----------------------

    // -- hardening: the JSON reader ------------------------------------------

    static void jsonRefusesNestingThatWouldOverflowTheStack() {
        String deep = "[".repeat(200000) + "]".repeat(200000);
        boolean refused = false;
        try { Json.parse(deep); } catch (MobiumException e) { refused = true; }
        yes("200000 levels of nesting is a MobiumException, not a StackOverflowError", refused);
        String ok = "[".repeat(Json.MAX_DEPTH) + "]".repeat(Json.MAX_DEPTH);
        yes("nesting at the limit parses", Json.parse(ok) instanceof List);
    }

    static void jsonNumbersFollowTheGrammar() {
        for (String bad : new String[] { "+1", "01", "1.", ".5", "-", "1e", "1e+", "--1", "1-2", "0x10" }) {
            yes("number " + bad + " is refused as a MobiumException", refusedByJson(bad));
        }
        eq("zero", 0L, Json.parse("0"));
        eq("negative zero is a long", 0L, Json.parse("-0"));
        eq("exponent with a sign", -500d, Json.parse("-0.5e+3"));
        yes("too big for a long becomes a double", Json.parse("99999999999999999999") instanceof Double);
    }

    static void jsonEscapesAreExact() {
        eq("\\u with four digits", "A", Json.parse("\"\\u0041\""));
        for (String bad : new String[] { "\"\\u+041\"", "\"\\u 41a\"", "\"\\uzzzz\"", "\"\\u004\"",
                                         "\"\\u\uff11\uff11\uff11\uff11\"", "\"a\u0001b\"" }) {
            yes("string " + describe(bad) + " is refused as a MobiumException", refusedByJson(bad));
        }
        eq("surrogate pair", "\uD83D\uDC1B", Json.parse("\"\\ud83d\\udc1b\""));
    }

    static void jsonIntegerRefusesWhatDoesNotFit() {
        Map<String, Object> m = Json.asObject(Json.parse("{\"x\":2147483648,\"y\":2147483647}"));
        eq("Integer.MAX_VALUE fits", Integer.MAX_VALUE, Json.integer(m, "y"));
        boolean refused = false;
        try { Json.integer(m, "x"); } catch (MobiumException e) { refused = true; }
        yes("2^31 is refused rather than wrapped negative", refused);
    }

    static void jsonWritesNoNumberJsonCannotCarry() {
        for (double d : new double[] { Double.NaN, Double.POSITIVE_INFINITY }) {
            boolean refused = false;
            try { Json.write(Map.of("latitude", d)); } catch (InvalidArgumentException e) { refused = true; }
            yes(d + " is refused rather than written bare", refused);
        }
    }

    static boolean refusedByJson(String text) {
        try {
            Json.parse(text);
            return false;
        } catch (MobiumException e) {
            return true;
        } catch (RuntimeException e) {
            return false; // a NumberFormatException or the like: exactly what must not escape
        }
    }

    static void callTimeoutMustBePositive() {
        for (Duration bad : new Duration[] { Duration.ZERO, Duration.ofSeconds(-1), null }) {
            boolean refused = false;
            try { Mobium.builder().callTimeout(bad); } catch (IllegalArgumentException e) { refused = true; }
            yes("callTimeout(" + bad + ") is refused", refused);
        }
    }

    // -- hardening: the connection, against FakeMobium ------------------------

    static <T extends Throwable> T throwsA(Class<T> kind, Runnable r) {
        try {
            r.run();
        } catch (Throwable t) {
            if (kind.isInstance(t)) return kind.cast(t);
            throw new AssertionError("expected " + kind.getSimpleName() + ", got " + t, t);
        }
        return null;
    }

    static void itSkipsWhatIsNotTheAnswer() {
        try (Connection c = FakeProcess.connect("ok", null, null)) {
            Map<String, Object> r = Connection.dataOf(c.call("app_echo", Map.of("k", "v")));
            eq("the answer, past a notification, two unreadable lines and another id's reply",
                    "app_echo", Json.str(r, "tool"));
            eq("arguments arrive", "v", Json.str(Json.asObject(r.get("echo")), "k"));
        }
    }

    static void concurrentCallsAreSerialized() {
        // Measured before the lock: two requests arrived on one line.
        try (Connection c = FakeProcess.connect("ok", null, null)) {
            String[] answers = new String[8];
            List<Thread> threads = new ArrayList<>();
            for (int i = 0; i < answers.length; i++) {
                final int n = i;
                threads.add(new Thread(() -> {
                    try {
                        Map<String, Object> r = Connection.dataOf(c.call("slow", Map.of("n", n)));
                        answers[n] = Json.str(Json.asObject(r.get("echo")), "n");
                    } catch (RuntimeException e) {
                        answers[n] = e.getClass().getSimpleName();
                    }
                }));
            }
            long start = System.nanoTime();
            threads.forEach(Thread::start);
            for (Thread t : threads) {
                try { t.join(20000); } catch (InterruptedException e) { throw new AssertionError(e); }
            }
            long ms = (System.nanoTime() - start) / 1_000_000;
            boolean all = true;
            for (int i = 0; i < answers.length; i++) all &= String.valueOf(i).equals(answers[i]);
            yes("each of 8 threads got its own answer", all);
            yes("they ran one at a time (8 x 300ms)", ms >= 8 * 300 - 50);
        }
    }

    static void aNullArgumentIsRefusedBeforeItIsSent() {
        try (Mobium d = FakeProcess.mobium("ok")) {
            InvalidArgumentException e = throwsA(InvalidArgumentException.class, () -> d.tap((String) null));
            yes("a null target is InvalidArgumentException", e != null);
            yes("naming the argument", e != null && e.getMessage().contains("target"));
            yes("a null inside a varargs array is refused",
                    throwsA(InvalidArgumentException.class, () -> d.grant("com.example", "camera", null)) != null);
            yes("a malformed waypoint is refused",
                    throwsA(InvalidArgumentException.class, () -> d.followRoute(List.of(new double[] { 1, 2 }, new double[] { 1 }), 0)) != null);
            yes("an empty tool name is refused",
                    throwsA(InvalidArgumentException.class, () -> d.call(" ", null)) != null);
            yes("NaN is refused, not written as bare NaN",
                    throwsA(InvalidArgumentException.class, () -> d.setLocation(Double.NaN, 0)) != null);
            eq("and the connection is still fine", "app_current", Json.str(d.call("app_current", null), "tool"));
        }
    }

    static void aTimedOutCallEndsTheConnection() {
        try (Connection c = FakeProcess.connect("hang", Duration.ofMillis(500), null)) {
            long start = System.nanoTime();
            MobiumException first = throwsA(MobiumException.class, () -> c.call("app_map", null));
            long ms = (System.nanoTime() - start) / 1_000_000;
            yes("a call with no answer times out", first != null && ms < 5000);
            yes("saying why the connection is closed", first != null && first.getMessage().contains("callTimeout"));
            MobiumException second = throwsA(MobiumException.class, () -> c.call("app_map", null));
            yes("the next call is refused, not read out of step",
                    second != null && second.getMessage().contains("no longer usable"));
        }
    }

    static void anExitMidCallIsReportedWithItsStatus() {
        try (Connection c = FakeProcess.connect("exit", null, null)) {
            MobiumException e = throwsA(MobiumException.class, () -> c.call("app_map", null));
            yes("an exit mid-call names the status", e != null && e.getMessage().contains("status 3"));
            MobiumException again = throwsA(MobiumException.class, () -> c.call("app_map", null));
            yes("and the connection stays closed", again != null && again.getMessage().contains("no longer usable"));
        }
    }

    static void aFailedHandshakeLeavesNoProcess() {
        long start = System.nanoTime();
        MobiumException silent = throwsA(MobiumException.class,
                () -> FakeProcess.connect("mute", Duration.ofMillis(500), null).close());
        yes("a silent handshake fails within callTimeout",
                silent != null && (System.nanoTime() - start) / 1_000_000 < 5000);
        try {
            // Refused rather than silent: a timeout ends the process on its
            // own, so only a handshake that fails some other way shows whether
            // the constructor cleans up after itself. Measured before the fix:
            // it did not.
            Path pid = Files.createTempFile("mobium-fake", ".pid");
            MobiumException e = throwsA(MobiumException.class, () -> FakeProcess.connect("refuse", null, pid).close());
            yes("a refused handshake fails the connection", e != null);
            long p = Long.parseLong(Files.readString(pid).trim());
            boolean gone = ProcessHandle.of(p).map(h -> {
                try { return h.onExit().get(5, TimeUnit.SECONDS) != null; } catch (Exception x) { return false; }
            }).orElse(true);
            yes("and the process it started is not left running", gone);
            Files.deleteIfExists(pid);
        } catch (java.io.IOException e) {
            throw new AssertionError(e);
        }
    }

    static void closeIsIdempotentAndEndsAWaitingCall() {
        Connection c = FakeProcess.connect("hang", null, null);
        MobiumException[] seen = new MobiumException[1];
        Thread waiting = new Thread(() -> seen[0] = throwsA(MobiumException.class, () -> c.call("app_map", null)));
        waiting.start();
        try {
            Thread.sleep(300);
            c.close();
            waiting.join(15000);
        } catch (InterruptedException e) {
            throw new AssertionError(e);
        }
        yes("close from another thread ends a waiting call", !waiting.isAlive() && seen[0] != null);
        c.close();
        yes("a second close is harmless", true);
        MobiumException after = throwsA(MobiumException.class, () -> c.call("app_map", null));
        yes("a call after close says the connection was closed",
                after != null && after.getMessage().contains("closed"));
    }

    static void anAnswerWithNoIdFailsTheCallInFlight() {
        // mobium answers a request it cannot parse with an error that has no
        // id. Skipped as a notification would be, it left the call waiting
        // forever; the timeout is only there so a regression fails rather
        // than hangs.
        try (Connection c = FakeProcess.connect("noid", Duration.ofSeconds(5), null)) {
            InvalidArgumentException e = throwsA(InvalidArgumentException.class, () -> c.call("app_map", null));
            yes("an answer with no id fails the call as InvalidArgumentException", e != null);
            yes("saying the request was unreadable", e != null && e.getMessage().contains("could not read the request"));
            eq("and the connection is still in step", "app_current",
                    Json.str(Connection.dataOf(c.call("app_current", null)), "tool"));
        }
    }

    private static void eq(String what, Object want, Object got) {
        checks++;
        if (want == null ? got == null : want.equals(got)) return;
        failures++;
        System.out.printf("FAIL  %s%n        want %s%n        got  %s%n", what, want, got);
    }

    private static void yes(String what, boolean condition) {
        checks++;
        if (condition) return;
        failures++;
        System.out.printf("FAIL  %s%n", what);
    }

    private static String describe(String s) {
        return "\"" + s.replace("\n", "\\n").replace("\t", "\\t") + "\"";
    }
}
