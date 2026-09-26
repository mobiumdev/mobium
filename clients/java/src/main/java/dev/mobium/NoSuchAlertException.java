package dev.mobium;

import java.util.Map;

/**
 * A dialog was expected and none is on screen.
 *
 * <p>Error code {@code no_such_alert}. See {@link MobiumException}.
 */
public final class NoSuchAlertException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "no_such_alert";

    NoSuchAlertException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
