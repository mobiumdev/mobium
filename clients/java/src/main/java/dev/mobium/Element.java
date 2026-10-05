package dev.mobium;

import java.util.Map;

/**
 * One actionable thing on screen.
 *
 * @param ref     the {@code @e1} handle, valid only for the screen it came from
 * @param label   what a person would call it
 * @param role    button, input, list and so on; empty if Mobium could not say
 * @param locator how the ref resolves on a later screen, as {@code kind=value}
 * @param bounds  where it is, in device pixels
 * @param context the WebView it came from, empty for native elements
 * @param checked a checkbox, radio or switch's state; null for anything with
 *                no such state, which is a different answer from unchecked
 * @param selected true for what the platform reports chosen: the current tab,
 *                 the chosen segment of a segmented control
 * @param value    what a slider reads, as the app states it ("80%", "1.2");
 *                 empty for anything else
 * @param disabled true for what the platform reports not enabled: an action
 *                 on it waits for it to be enabled, and is refused if it stays
 *                 disabled
 */
public record Element(String ref, String label, String role,
                      String locator, Bounds bounds, String context, Boolean checked,
                      boolean selected, String value, boolean disabled) {

    static Element from(Map<String, Object> m) {
        Map<String, Object> loc = Json.asObject(m.get("locator"));
        String locator = loc.isEmpty() ? "" : Json.str(loc, "kind") + "=" + Json.str(loc, "value");
        return new Element(
                Json.str(m, "ref"),
                Json.str(m, "label"),
                Json.str(m, "role"),
                locator,
                Bounds.from(Json.asObject(m.get("bounds"))),
                Json.str(m, "context"),
                m.get("checked") instanceof Boolean c ? c : null,
                Boolean.TRUE.equals(m.get("selected")),
                Json.str(m, "value"),
                Boolean.TRUE.equals(m.get("disabled")));
    }

    @Override public String toString() {
        return role.isEmpty() ? ref + " " + label : ref + " " + label + " (" + role + ")";
    }
}
