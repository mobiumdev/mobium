package dev.mobium;

import java.util.Map;

/**
 * Nothing to drive: no device matches, or none is connected.
 *
 * <p>Error code {@code no_device}. See {@link MobiumException}.
 */
public final class NoDeviceException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "no_device";

    NoDeviceException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
