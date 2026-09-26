package dev.mobium;

import java.util.Map;

/**
 * A bug in Mobium.
 *
 * <p>Error code {@code internal}. See {@link MobiumException}.
 */
public final class InternalException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "internal";

    InternalException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
