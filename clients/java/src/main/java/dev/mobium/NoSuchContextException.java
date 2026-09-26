package dev.mobium;

import java.util.Map;

/**
 * A WebView context that is not there.
 *
 * <p>Error code {@code no_such_context}. See {@link MobiumException}.
 */
public final class NoSuchContextException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "no_such_context";

    NoSuchContextException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
