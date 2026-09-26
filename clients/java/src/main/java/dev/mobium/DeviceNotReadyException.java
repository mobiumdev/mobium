package dev.mobium;

import java.util.Map;

/**
 * The device is there and cannot be driven yet: locked, not trusted, Developer Mode off.
 *
 * <p>Error code {@code device_not_ready}. See {@link MobiumException}.
 */
public final class DeviceNotReadyException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "device_not_ready";

    DeviceNotReadyException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
