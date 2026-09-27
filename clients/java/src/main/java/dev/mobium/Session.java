package dev.mobium;

import java.util.Map;

/**
 * What {@link Mobium.Builder#start()} opened: the device and how it is driven.
 *
 * @param device   the serial (Android) or UDID (iOS) the session is on
 * @param platform {@code "android"}, {@code "ios"}, or a third-party driver's name
 * @param driver   the driver driving it, such as {@code "uiautomator2"} or {@code "wda"}
 * @param reused   true when a session was already open on the device, and was kept
 * @param app      the app start launched, or empty
 */
public record Session(String device, String platform, String driver, boolean reused, String app) {

    static Session from(Map<String, Object> m) {
        return new Session(Json.str(m, "device"), Json.str(m, "platform"), Json.str(m, "driver"),
                Json.bool(m, "reused"), Json.str(m, "app"));
    }
}
