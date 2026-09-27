import dev.mobium.Element;
import dev.mobium.Mobium;
import dev.mobium.Session;

import java.nio.file.Path;
import java.util.List;
import java.util.Map;

/**
 * Mobium quick start: start a session, drive Settings, quit.
 *
 * <pre>MOBIUM_PLATFORM=android java Quickstart.java    # or ios</pre>
 */
public class Quickstart {

    // Settings is on every emulator, simulator and phone, with nothing to install.
    record Target(String app, String row, String next) {}

    static final Map<String, Target> PLATFORMS = Map.of(
            "android", new Target("com.android.settings", "Network & internet", "text=Internet"),
            "ios", new Target("com.apple.Preferences", "General", "label=About,role=button"));

    public static void main(String[] args) {
        String platform = System.getenv().getOrDefault("MOBIUM_PLATFORM", "android");
        Target p = PLATFORMS.get(platform);

        Mobium.Builder b = Mobium.builder().platform(platform).app(p.app());
        String serial = System.getenv("MOBIUM_DEVICE");
        if (serial != null) b.device(serial);

        // 1. Start the session: the driver is started on the device and
        //    Settings is launched. try-with-resources quits when it ends.
        try (Mobium device = b.start()) {
            Session s = device.session();
            System.out.printf("session on %s (%s, %s)%n", s.device(), s.platform(), s.driver());

            // 2. Map the screen: every element you can act on, each with a @ref.
            List<Element> elements = device.map();
            elements.stream().limit(5).forEach(e -> System.out.println("  " + e));

            // 3. Tap a row by its ref, then wait for the screen it opens. A row's
            //    label can carry its summary too ("Network & internet Mobile,
            //    Wi-Fi, ..."), so match its start.
            Element row = elements.stream().filter(e -> e.label().startsWith(p.row())).findFirst().orElseThrow();
            device.tap(row.ref());
            device.waitFor(p.next());
            System.out.println("opened " + p.row());

            // 4. Take a screenshot.
            device.screenshot(Path.of("quickstart-" + platform + ".png"));
            System.out.println("saved quickstart-" + platform + ".png");
        }
        // 5. try-with-resources has quit: the device's session is closed.
        System.out.println("session ended");
    }
}
