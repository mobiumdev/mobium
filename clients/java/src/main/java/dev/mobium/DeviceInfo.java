package dev.mobium;

import java.util.Map;

/**
 * One attached device or simulator.
 *
 * @param id       the serial (Android) or UDID (iOS) to pass to {@link Mobium.Builder#device}
 * @param platform {@code "android"} or {@code "ios"}
 * @param state    whether it can be driven now, as the platform reports it
 * @param model    the device's model
 * @param runtime  the OS version
 * @param emulator true for an emulator or simulator, false for real hardware
 */
public record DeviceInfo(String id, String platform, String state,
                         String model, String runtime, boolean emulator) {

    static DeviceInfo from(Map<String, Object> m) {
        return new DeviceInfo(Json.str(m, "id"), Json.str(m, "platform"),
                Json.str(m, "state"), Json.str(m, "model"),
                Json.str(m, "runtime"), Json.bool(m, "emulator"));
    }

    @Override public String toString() { return id + " (" + platform + ", " + state + ")"; }
}
