package dev.mobium;

import java.util.Map;

/**
 * The command reported success and reading the state back disagreed.
 *
 * <p>Error code {@code not_confirmed}. See {@link MobiumException}.
 */
public final class NotConfirmedException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "not_confirmed";

    NotConfirmedException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
