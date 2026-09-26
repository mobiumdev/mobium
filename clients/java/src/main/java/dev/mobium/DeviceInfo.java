package dev.mobium;

import java.util.Map;

/** One attached device or simulator. */
public record DeviceInfo(String id, String platform, String state,
                         String model, String runtime, boolean emulator) {

    static DeviceInfo from(Map<String, Object> m) {
        return new DeviceInfo(Json.str(m, "id"), Json.str(m, "platform"),
                Json.str(m, "state"), Json.str(m, "model"),
                Json.str(m, "runtime"), Json.bool(m, "emulator"));
    }

    @Override public String toString() { return id + " (" + platform + ", " + state + ")"; }
}
