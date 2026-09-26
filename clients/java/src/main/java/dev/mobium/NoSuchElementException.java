package dev.mobium;

import java.util.Map;

/**
 * A locator or ref matched nothing on screen. Worth scrolling for.
 *
 * <p>Shares its simple name with {@code java.util.NoSuchElementException}, as
 * Selenium's does; import this one by name rather than through a wildcard.
 *
 * <p>Error code {@code no_such_element}. See {@link MobiumException}.
 */
public final class NoSuchElementException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "no_such_element";

    NoSuchElementException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
