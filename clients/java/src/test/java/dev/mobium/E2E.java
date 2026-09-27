package dev.mobium;

import java.util.List;
import java.util.Map;

/**
 * The Java client end to end, against a booted Android device.
 *
 * <p>Not part of {@code make ci} — it needs a device. docs/checks/clients.sh
 * compiles the client and runs this with {@code MOBIUM_E2E_DEVICE} set, with
 * the four other clients' copies of the same flow.
 *
 * <p>Reading, acting, waiting, scrolling, lifecycle, permissions, the device
 * log and crash reports, and one failure that must arrive as its own
 * exception.
 */
public final class E2E {

    private static final String SETTINGS = "com.android.settings";

    private static void check(boolean ok, String what) {
        if (!ok) {
            System.err.println("FAIL: " + what);
            System.exit(1);
        }
        System.out.println("    ok   " + what);
    }

    public static void main(String[] args) {
        String serial = System.getenv("MOBIUM_E2E_DEVICE");
        if (serial == null || serial.isBlank()) {
            System.out.println("MOBIUM_E2E_DEVICE is not set; skipping");
            return;
        }
        Mobium.Builder b = Mobium.builder().device(serial);
        String bin = System.getenv("MOBIUM_BIN_PATH");
        if (bin != null && !bin.isBlank()) {
            b.binary(bin);
        }
        try (Mobium d = b.start()) {
            check(d.session() != null && serial.equals(d.session().device()) && "android".equals(d.session().platform()),
                    "start opens a session on the device");
            d.terminate(SETTINGS);
            d.launch(SETTINGS);
            check(SETTINGS.equals(d.current()), "launch brings Settings forward");

            Element found = d.waitFor("text=Network & internet");
            check(found != null && found.ref().startsWith("@e"), "waitFor returns a ref");
            d.tap(found.ref());
            // A tap returns when delivered, not when the next screen is up.
            check(d.waitFor("text=Internet") != null, "tapping the ref opens its screen");

            d.press("back");
            check(d.scrollTo("text=About") != null, "scrollTo reaches a row below the fold");

            d.grant("com.android.chrome", "camera");
            d.revoke("com.android.chrome", "camera");
            check(true, "grant and revoke, each read back by the tool");

            Map<String, Object> logs = d.deviceLogs(null, null, 5);
            check(!Json.asArray(logs.get("entries")).isEmpty(), "deviceLogs reads logcat");
            List<Map<String, Object>> crashes = d.crashes(null, 3);
            check(crashes != null, "crashes answers with a list");

            try {
                d.tap("text=Definitely Not Here");
                check(false, "a missing element throws");
            } catch (NoSuchElementException e) {
                check(true, "a missing element throws NoSuchElementException");
            }

            d.terminate(SETTINGS);
            check(!SETTINGS.equals(d.current()), "terminate takes Settings away");
        }
        // try-with-resources quit: the session is gone from the daemon.
        Mobium.Builder o = Mobium.builder();
        if (bin != null && !bin.isBlank()) o.binary(bin);
        try (Mobium other = o.connect()) {
            check(other.sessions().stream().noneMatch(s -> serial.equals(s.get("device"))), "try-with-resources ended the session");
        }
        System.out.println("java: passed");
    }
}
