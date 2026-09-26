package dev.mobium;

import java.util.Map;

/**
 * Something on this machine is missing: adb, Xcode, a signing certificate.
 *
 * <p>Error code {@code toolchain_missing}. See {@link MobiumException}.
 */
public final class ToolchainMissingException extends MobiumException {
    private static final long serialVersionUID = 1L;

    /** The wire code this exception stands for. */
    public static final String CODE = "toolchain_missing";

    ToolchainMissingException(String message, String tool, String remedy, boolean retryable,
                    Map<String, Object> details) {
        super(message, tool, CODE, remedy, retryable, details);
    }
}
