package dev.mobium;

import java.util.Map;

/**
 * The device side failed (a device server, or adb, simctl, devicectl, lockdown); a server's W3C code is {@code details().get("w3c")}.
 *
 * <p>Error code {@code device_server}. See {@link MobiumException}.
 */
public final class DeviceServerException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "device_server";

    DeviceServerException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
