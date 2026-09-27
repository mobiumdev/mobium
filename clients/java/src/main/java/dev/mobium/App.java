package dev.mobium;

import java.util.Map;

/**
 * One installed app.
 *
 * @param id      the package name (Android) or bundle id (iOS)
 * @param name    empty on Android, where reading a package's label costs a
 *                {@code dumpsys} per app and is not worth it for a listing
 * @param version the version the app declares, as a string
 * @param system  true for an app the platform ships with
 */
public record App(String id, String name, String version, boolean system) {

    static App from(Map<String, Object> m) {
        return new App(Json.str(m, "id"), Json.str(m, "name"),
                Json.str(m, "version"), Json.bool(m, "system"));
    }

    @Override public String toString() { return name.isEmpty() ? id : id + " (" + name + ")"; }
}
