package dev.mobium;

import java.util.Map;

/**
 * A tool reported that it could not do what was asked, or the connection to
 * Mobium failed.
 *
 * <p>Every tool failure carries a stable {@link #code() code}, the same in
 * every client and on the wire (the error codes in the mobium
 * repository's docs/guides/cli.md), and each code has a subclass — {@link NoSuchElementException},
 * {@link UnsupportedException} and so on — so a test catches the kind it can
 * handle and lets the rest through. {@link #remedy()} says what to do about
 * it, {@link #retryable()} whether the same call can succeed if made again.
 * A code this client does not know, a daemon too old to send one, and a
 * failure of the connection itself are plain {@code MobiumException}s.
 *
 * <p>Unchecked on purpose. Every call in this client can fail — a device can
 * be unplugged mid-flow — and forcing a try/catch around each one would make
 * a readable test unreadable without making it safer.
 */
public class MobiumException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    /** The tool that failed, or empty for a transport-level failure. */
    private final String tool;
    /** The error code, or {@code "error"} when unclassified. */
    private final String code;
    /** What to do about it, or empty. */
    private final String remedy;
    /** Whether the same call, made again unchanged, can succeed. */
    private final boolean retryable;
    private final transient Map<String, Object> details;

    /**
     * A transport-level failure, with no tool to blame.
     *
     * @param message what went wrong
     */
    public MobiumException(String message) { this(message, ""); }

    /**
     * A named tool reported that it could not do what was asked.
     *
     * @param message what went wrong
     * @param tool    the tool that failed
     */
    public MobiumException(String message, String tool) {
        this(message, tool, "error", "", false, Map.of());
    }

    /**
     * A failure caused by something further down.
     *
     * @param message what went wrong
     * @param cause   the failure underneath
     */
    public MobiumException(String message, Throwable cause) {
        super(message, cause);
        this.tool = "";
        this.code = "error";
        this.remedy = "";
        this.retryable = false;
        this.details = Map.of();
    }

    MobiumException(String message, String tool, String code, String remedy,
                    boolean retryable, Map<String, Object> details) {
        super(tool.isEmpty() ? message : tool + ": " + message);
        this.tool = tool;
        this.code = code;
        this.remedy = remedy;
        this.retryable = retryable;
        this.details = details == null ? Map.of() : details;
    }

    /**
     * The tool that failed, or an empty string.
     *
     * @return the tool's name, or an empty string for a transport failure
     */
    public String tool() { return tool; }

    /**
     * The error code: {@code "no_such_element"}, {@code "timeout"}, ... — {@code "error"} when unclassified.
     *
     * @return the code, or {@code "error"} when unclassified
     */
    public String code() { return code; }

    /**
     * What to do about it, or an empty string.
     *
     * @return the remedy, or an empty string
     */
    public String remedy() { return remedy; }

    /**
     * Whether the same call, made again unchanged, can reasonably succeed.
     *
     * @return true when retrying unchanged can help
     */
    public boolean retryable() { return retryable; }

    /**
     * Machine-readable facts: the locator, the W3C code a device server sent.
     *
     * @return the facts, never null
     */
    public Map<String, Object> details() { return details; }

    /**
     * The exception for a failed tool call: the text as always, and the code,
     * remedy and details when the daemon sent them.
     */
    static MobiumException from(String text, String tool, Object structuredContent) {
        if (!(structuredContent instanceof Map)) return new MobiumException(text, tool);
        Map<String, Object> s = Json.asObject(structuredContent);
        String code = Json.str(s, "code");
        if (code.isEmpty()) return new MobiumException(text, tool);
        String remedy = Json.str(s, "remedy");
        boolean retryable = Json.bool(s, "retryable");
        Map<String, Object> details = Json.asObject(s.get("details"));
        switch (code) {
            case NoDeviceException.CODE: return new NoDeviceException(text, tool, remedy, retryable, details);
            case DeviceNotReadyException.CODE: return new DeviceNotReadyException(text, tool, remedy, retryable, details);
            case ToolchainMissingException.CODE: return new ToolchainMissingException(text, tool, remedy, retryable, details);
            case NoSuchElementException.CODE: return new NoSuchElementException(text, tool, remedy, retryable, details);
            case AmbiguousLocatorException.CODE: return new AmbiguousLocatorException(text, tool, remedy, retryable, details);
            case ElementNotReachableException.CODE: return new ElementNotReachableException(text, tool, remedy, retryable, details);
            case NoSuchContextException.CODE: return new NoSuchContextException(text, tool, remedy, retryable, details);
            case NoSuchAlertException.CODE: return new NoSuchAlertException(text, tool, remedy, retryable, details);
            case UnsupportedException.CODE: return new UnsupportedException(text, tool, remedy, retryable, details);
            case NotConfirmedException.CODE: return new NotConfirmedException(text, tool, remedy, retryable, details);
            case TimedOutException.CODE: return new TimedOutException(text, tool, remedy, retryable, details);
            case InvalidArgumentException.CODE: return new InvalidArgumentException(text, tool, remedy, retryable, details);
            case DeviceServerException.CODE: return new DeviceServerException(text, tool, remedy, retryable, details);
            case InternalException.CODE: return new InternalException(text, tool, remedy, retryable, details);
            default:
                return new MobiumException(text, tool, code, remedy, retryable, details);
        }
    }
}
