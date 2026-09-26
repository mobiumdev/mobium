package dev.mobium;

import java.util.Map;

/**
 * The element was found and cannot be touched where it is, usually off screen.
 *
 * <p>Error code {@code element_not_reachable}. See {@link MobiumException}.
 */
public final class ElementNotReachableException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "element_not_reachable";

    ElementNotReachableException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
