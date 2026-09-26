package dev.mobium;

import java.util.Map;

/**
 * The request itself is wrong.
 *
 * <p>Error code {@code invalid_argument}. See {@link MobiumException}.
 */
public final class InvalidArgumentException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "invalid_argument";

    InvalidArgumentException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
