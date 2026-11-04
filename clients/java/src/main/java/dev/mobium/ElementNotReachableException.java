package dev.mobium;

import java.util.Map;

/**
 * The element cannot be touched where it is: off screen, or something is over it — a dialog, the
 * keyboard, another screen, a control. A cover has {@code details.get("check")} {@code "receivesEvents"}
 * and names itself in details: dialog, keyboard, screen, cover, siri, assistant or clipboard_preview.
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
