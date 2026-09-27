package dev.mobium;

import java.time.Duration;

/**
 * What to wait for, and how long.
 *
 * <pre>{@code
 * device.waitFor("text=Welcome");                                  // visible, 10s
 * device.waitFor("role=progressbar", WaitFor.hidden());            // until it goes
 * device.waitFor("@e4", WaitFor.text("Sent").timeout(ofSeconds(30)));
 * }</pre>
 */
public final class WaitFor {

    private final String condition;
    private final String text;
    private final Duration timeout;

    private WaitFor(String condition, String text, Duration timeout) {
        this.condition = condition;
        this.text = text;
        this.timeout = timeout;
    }

    /**
     * Wait for the element to be on screen. The default.
     *
     * @return the condition
     */
    public static WaitFor visible() { return new WaitFor("visible", null, null); }

    /**
     * Wait for it to go away — a spinner, say.
     *
     * @return the condition
     */
    public static WaitFor hidden() { return new WaitFor("hidden", null, null); }

    /**
     * Wait until it contains this text.
     *
     * @param expected the text the element must contain
     * @return the condition
     */
    public static WaitFor text(String expected) { return new WaitFor("text", expected, null); }

    /**
     * How long before giving up. Ten seconds by default, two minutes at most.
     *
     * @param d how long, at most two minutes
     * @return a copy of this condition with the timeout set
     */
    public WaitFor timeout(Duration d) { return new WaitFor(condition, text, d); }

    java.util.Map<String, Object> args(String target) {
        java.util.Map<String, Object> m = new java.util.LinkedHashMap<>();
        m.put("target", target);
        m.put("condition", condition);
        if (text != null) m.put("text", text);
        if (timeout != null) m.put("timeout_ms", timeout.toMillis());
        return m;
    }
}
