package dev.mobium;

import java.time.Duration;

/**
 * What to wait for, and how long.
 *
 * <pre>{@code
 * device.waitFor("text=Welcome");                                  // visible, 10s
 * device.waitFor("role=progressbar", WaitFor.hidden());            // until it goes
 * device.waitFor("@e4", WaitFor.text("Sent").timeout(ofSeconds(30)));
 * device.waitFor("@e4", WaitFor.text("Sending").not());         // until it changes
 * device.waitFor("testid=row", WaitFor.count(3));                // three rows
 * }</pre>
 */
public final class WaitFor {

    private final String condition;
    private final String text;
    private final Duration timeout;
    private final boolean negate;
    private final boolean exact;
    private final Integer count;

    private WaitFor(String condition, String text, Duration timeout) {
        this(condition, text, timeout, false, false, null);
    }

    private WaitFor(String condition, String text, Duration timeout, boolean negate, boolean exact, Integer count) {
        this.condition = condition;
        this.text = text;
        this.timeout = timeout;
        this.negate = negate;
        this.exact = exact;
        this.count = count;
    }

    /**
     * Wait until its text is exactly this, where {@link #text} waits for a part.
     *
     * @param expected the whole text
     * @return the condition
     */
    public static WaitFor exactText(String expected) { return new WaitFor("text", expected, null, false, true, null); }

    /**
     * Wait until the locator matches exactly this many elements on screen.
     *
     * @param n how many, 0 or more
     * @return the condition
     */
    public static WaitFor count(int n) { return new WaitFor("count", null, null, false, false, n); }

    /**
     * The opposite of this condition: {@code text("Sending").not()} waits for the
     * text to change.
     *
     * @return a copy of this condition, negated
     */
    public WaitFor not() { return new WaitFor(condition, text, timeout, !negate, exact, count); }

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
     * Wait until a field holds exactly this value; {@code ""} waits for it to
     * be empty. A password field is refused, since its value is never read.
     *
     * @param expected the whole value the field must hold
     * @return the condition
     */
    public static WaitFor value(String expected) { return new WaitFor("value", expected, null); }

    /**
     * Wait for a control to be enabled — a Submit the app enables once a form is valid.
     *
     * @return the condition
     */
    public static WaitFor enabled() { return new WaitFor("enabled", null, null); }

    /**
     * Wait for a control to be disabled.
     *
     * @return the condition
     */
    public static WaitFor disabled() { return new WaitFor("disabled", null, null); }

    /**
     * Wait for a checkbox, radio or switch to be checked.
     *
     * @return the condition
     */
    public static WaitFor checked() { return new WaitFor("checked", null, null); }

    /**
     * Wait for a checkbox, radio or switch to be unchecked.
     *
     * @return the condition
     */
    public static WaitFor unchecked() { return new WaitFor("unchecked", null, null); }

    /**
     * Wait for a field to have keyboard focus.
     *
     * @return the condition
     */
    public static WaitFor focused() { return new WaitFor("focused", null, null); }

    /**
     * How long before giving up. Ten seconds by default, two minutes at most.
     *
     * @param d how long, at most two minutes
     * @return a copy of this condition with the timeout set
     */
    public WaitFor timeout(Duration d) { return new WaitFor(condition, text, d, negate, exact, count); }

    java.util.Map<String, Object> args(String target) {
        java.util.Map<String, Object> m = new java.util.LinkedHashMap<>();
        m.put("target", target);
        m.put("condition", condition);
        if (text != null) m.put("text", text);
        if (timeout != null) m.put("timeout_ms", timeout.toMillis());
        if (negate) m.put("not", true);
        if (exact) m.put("exact", true);
        if (count != null) m.put("count", count);
        return m;
    }
}
