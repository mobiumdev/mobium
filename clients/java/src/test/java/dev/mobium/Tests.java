package dev.mobium;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;

/**
 * The Java client's tests, with no test framework.
 *
 * <p>JUnit would be a test-scope dependency and therefore harmless to
 * consumers, but it would mean this client cannot be checked without Maven
 * and a network. `make ci` runs `javac` and `java` and nothing else, which is
 * worth more than the assertions JUnit would have provided.
 *
 * <p>Run with: {@code java -cp <classes> dev.mobium.Tests}
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
        boundsComputeTheirCentre();
        waitForBuildsItsArguments();
        findBinaryRejectsSomethingThatIsNotOne();
        everyErrorCodeHasItsException();
        aToolFailureKeepsCodeRemedyAndDetails();
        anUnknownOrMissingCodeIsTheBaseException();

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

    static void boundsComputeTheirCentre() {
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
