package dev.mobium;

import java.util.Map;

/**
 * A locator matched more than one element. Narrow it; Mobium never guesses.
 *
 * <p>Error code {@code ambiguous_locator}. See {@link MobiumException}.
 */
public final class AmbiguousLocatorException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "ambiguous_locator";

    AmbiguousLocatorException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
