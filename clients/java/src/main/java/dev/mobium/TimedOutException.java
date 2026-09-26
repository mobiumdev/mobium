package dev.mobium;

import java.util.Map;

/**
 * A wait ran out. Named TimedOut so it does not collide with
 * {@code java.util.concurrent.TimeoutException}.
 *
 * <p>Error code {@code timeout}. See {@link MobiumException}.
 */
public final class TimedOutException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "timeout";

    TimedOutException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
