package dev.mobium;

import java.util.Map;

/**
 * Whoever started Mobium does not allow this call: a read-only Mobium asked to act, or an app outside its app list. Retrying cannot help.
 *
 * <p>Error code {@code not_permitted}. See {@link MobiumException}.
 */
public final class NotPermittedException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "not_permitted";

    NotPermittedException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
