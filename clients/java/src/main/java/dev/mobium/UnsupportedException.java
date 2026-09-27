package dev.mobium;

import java.util.Map;

/**
 * This driver or platform cannot do it, and says why. Retrying cannot help.
 *
 * <p>Error code {@code unsupported}. See {@link MobiumException}.
 */
public final class UnsupportedException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "unsupported";

    UnsupportedException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
