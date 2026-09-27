package dev.mobium;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Drives native apps on Android emulators, Android phones, iOS simulators
 * and iPhones.
 *
 * <p>Speaks to the same tool layer the CLI and the MCP server use, over
 * {@code mobium pipe}, so a Java program and a command cannot drift apart.
 * The {@code mobium} binary has to be on {@code PATH}, or named by
 * {@code MOBIUM_BIN_PATH}.
 *
 * <pre>{@code
 * try (Mobium device = Mobium.connect()) {
 *     device.launch("com.example.shop");
 *     Element signIn = device.waitFor("text=Sign in");
 *     device.tap(signIn.ref());
 * }
 * }</pre>
 *
 * <p>Refs like {@code @e1} are valid only for the screen they came from.
 * Every action re-resolves its target immediately before acting, retries
 * briefly while the screen settles, and scrolls to it if it is below the
 * fold — so a tap can follow another tap without a sleep in between.
 *
 * <p>Safe to share between threads, one call at a time: there is one pipe
 * underneath, and one device, so calls from two threads are serialized
 * rather than interleaved. {@link #close()} from another thread ends a call
 * that is waiting.
 */
public final class Mobium implements AutoCloseable {

    private final Connection conn;
    // What start() opened, so quit() ends that device's session even when
    // several are running. Null after connect().
    private Session session;
    private boolean quit;

    private Mobium(Connection conn) { this.conn = conn; }

    // -- connecting --------------------------------------------------------

    /**
     * Connects to mobium, for whichever device is running. It does not touch
     * the device: the session there opens on the first call that needs it, or
     * with {@link Builder#start()}.
     *
     * @return a connection; close it when done
     */
    public static Mobium connect() { return builder().connect(); }

    /**
     * Configures a connection or a session.
     *
     * @return a builder, which connects or starts when asked
     */
    public static Builder builder() { return new Builder(); }

    /** Options for {@link Mobium#connect()} and {@link Builder#start()}. */
    public static final class Builder {
        private Builder() { }

        private String binary = "";
        private String device = "";
        private String driver = "";
        private String platform = "";
        private String app = "";
        private Duration callTimeout;

        /**
         * The platform for {@link #start()}: {@code "android"} or {@code "ios"}.
         * {@code "ios"} picks the wda driver, so none need be named.
         *
         * @param name {@code "android"} or {@code "ios"}
         * @return this builder
         */
        public Builder platform(String name) { this.platform = name; return this; }

        /**
         * An app for {@link #start()} to launch once the session is up.
         *
         * @param id a package name (Android) or bundle id (iOS)
         * @return this builder
         */
        public Builder app(String id) { this.app = id; return this; }

        /**
         * Pins the mobium executable, ahead of MOBIUM_BIN_PATH and PATH.
         *
         * @param path the mobium executable
         * @return this builder
         */
        public Builder binary(String path) { this.binary = path; return this; }

        /**
         * Targets one device by serial or UDID. Omit when only one is running.
         *
         * @param serial a serial (Android) or UDID (iOS)
         * @return this builder
         */
        public Builder device(String serial) { this.device = serial; return this; }

        /**
         * Chooses the driver: {@code uiautomator2} (default on Android),
         * {@code uiautomator} (installs nothing, slower, cannot type) or
         * {@code wda} (iOS simulators and iPhones).
         *
         * @param name {@code "uiautomator2"}, {@code "uiautomator"} or {@code "wda"}
         * @return this builder
         */
        public Builder driver(String name) { this.driver = name; return this; }

        /**
         * The longest any one call may take before the connection is given
         * up, the handshake included. Unlimited by default, because the first
         * session on an iPhone builds WebDriverAgent and that takes minutes.
         *
         * <p>A call that runs out <b>ends the connection</b>: there is one
         * pipe, replies are told apart only by id, and the late answer to an
         * abandoned call would be read as the answer to the next one. Every
         * call after throws, saying so; connect again. The device's session
         * lives in the daemon, so reconnecting is cheap. Set it well above the
         * longest {@link WaitFor#timeout} you use.
         *
         * @param timeout how long, positive
         * @return this builder
         */
        public Builder callTimeout(Duration timeout) {
            if (timeout == null || timeout.isZero() || timeout.isNegative()) {
                throw new IllegalArgumentException("a call timeout must be positive");
            }
            this.callTimeout = timeout;
            return this;
        }

        /**
         * Connects and opens the session on the device, as Appium's new
         * session does: the device-side server is started now and, given an
         * {@link #app}, it is launched and in front when this returns.
         *
         * <p>Nothing requires it -- every call opens a session on first use --
         * but it puts the slow first start (installing UiAutomator2, building
         * WebDriverAgent on an iPhone) where it was asked for. End it with
         * {@link Mobium#quit()}, or in try-with-resources, which quits:
         *
         * <pre>{@code
         * try (Mobium device = Mobium.builder().platform("android").app("com.android.settings").start()) {
         *     device.map();
         * }
         * }</pre>
         *
         * @return a device with its session open
         */
        public Mobium start() {
            Mobium m = connect();
            Map<String, Object> args = new LinkedHashMap<>();
            args.put("action", "start");
            if (!platform.isBlank()) args.put("platform", platform);
            if (!app.isBlank()) args.put("app", app);
            try {
                m.session = Session.from(Connection.dataOf(m.conn.call("app_session", args)));
            } catch (RuntimeException e) {
                m.conn.close();
                throw e;
            }
            return m;
        }

        /**
         * Connects to mobium without touching the device.
         *
         * @return a connection; close it when done
         */
        public Mobium connect() {
            List<String> args = new ArrayList<>();
            if (!device.isBlank())  { args.add("--device");  args.add(device); }
            if (!driver.isBlank()) { args.add("--driver"); args.add(driver); }
            return new Mobium(new Connection(Connection.findBinary(binary), args, callTimeout));
        }
    }

    // -- reading -----------------------------------------------------------

    /**
     * Every attached device and simulator.
     *
     * @return each attached device and simulator
     */
    public List<DeviceInfo> devices() {
        List<DeviceInfo> out = new ArrayList<>();
        for (Object o : Json.asArray(data("app_devices", null).get("devices"))) {
            out.add(DeviceInfo.from(Json.asObject(o)));
        }
        return out;
    }

    /**
     * The actionable elements on the current screen.
     *
     * <p>Refs are valid only for this screen. Call it again after anything
     * that changes the screen — or just act, since actions re-resolve their
     * target anyway.
     *
     * @return the elements, each with a ref
     */
    public List<Element> map() { return elements("app_map", null); }

    /**
     * The elements matching a locator, without acting on them.
     *
     * @param locator a locator, such as {@code "text=Sign in"} or {@code "testid=email"}
     * @return the matching elements, each with a ref
     */
    public List<Element> find(String locator) {
        return elements("app_find", args("locator", locator));
    }

    /**
     * The raw hierarchy — what Appium calls the page source — for when
     * {@link #map} leaves out the thing you need to see; map is what to act
     * on. {@code source} is the platform's XML, or in a WebView the page's
     * markup; {@code units} is "px" on Android and "pt" on iOS, where map, taps
     * and screenshots are in pixels, {@code scale} times as many.
     * {@code redacted} counts the password fields hidden.
     *
     * @return the hierarchy under {@code source}, with {@code units}, {@code scale} and {@code redacted}
     */
    public Map<String, Object> source() { return data("app_source", args()); }

    /**
     * Declares how to answer a dialog, so an action that meets it carries on:
     * when a dialog whose text contains {@code when} is in the way, press the
     * button captioned {@code press}. It names a button, not accept or
     * dismiss, because which button those press differs by platform and by
     * dialog. Captions match ignoring case.
     *
     * @param when text the dialog contains, ignoring case
     * @param press the caption of the button to press, ignoring case
     */
    public void addDialogRule(String when, String press) {
        act("app_dialogs", args("when", when, "press", press));
    }

    /**
     * The declared rules, each with how often it has answered.
     *
     * @return the rules, each with how often it has answered
     */
    public List<Map<String, Object>> dialogRules() {
        List<Map<String, Object>> out = new ArrayList<>();
        for (Object o : Json.asArray(data("app_dialogs", args()).get("rules"))) out.add(Json.asObject(o));
        return out;
    }

    /** Removes every rule for this device. */
    public void clearDialogRules() { act("app_dialogs", args("clear", true)); }

    /**
     * Everything readable on screen.
     *
     * @return everything readable on screen
     */
    public String text() { return prose("app_text", args()); }

    /**
     * The text of one element, by ref or locator.
     *
     * @param target the element: a ref or a locator
     * @return the element's text
     */
    public String text(String target) { return prose("app_text", args("target", target)); }

    /**
     * The package name or bundle id of the foreground app.
     *
     * <p>Costs no extra device call: it reads the hierarchy a snapshot
     * fetches anyway. Use it to confirm a tap went where you expected.
     *
     * @return the foreground app's package name or bundle id
     */
    public String current() { return Json.str(data("app_current", null), "app"); }

    /**
     * Captures the screen as PNG.
     *
     * @return the PNG's bytes
     */
    public byte[] screenshot() {
        Map<String, Object> result = conn.call("app_screenshot", args());
        for (Object block : Json.asArray(result.get("content"))) {
            Map<String, Object> c = Json.asObject(block);
            if ("image".equals(Json.str(c, "type"))) {
                try {
                    return Base64.getDecoder().decode(Json.str(c, "data"));
                } catch (IllegalArgumentException e) {
                    throw new MobiumException("mobium returned an image that is not base64", e);
                }
            }
        }
        throw new MobiumException("mobium returned no image");
    }

    /**
     * Captures the screen as PNG and writes it to a file.
     *
     * @param path where to write the PNG
     * @return the PNG's bytes
     */
    public byte[] screenshot(Path path) {
        act("app_screenshot", args("path", path.toString()));
        try {
            return Files.readAllBytes(path);
        } catch (IOException e) {
            throw new MobiumException("could not read back " + path, e);
        }
    }

    // -- waiting and scrolling ---------------------------------------------

    /**
     * Waits for an element to be on screen, for up to ten seconds.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @return the element, with a fresh ref, or null when the wait was for it to go away
     */
    public Element waitFor(String target) { return waitFor(target, WaitFor.visible()); }

    /**
     * Blocks until the screen agrees, instead of sleeping.
     *
     * <p>On success the screen is remapped, so the element returned already
     * has a ref that can be tapped without calling {@link #map()} first.
     * Waiting for something to go away returns {@code null}: there is nothing
     * left to point at. If the condition never holds, the exception says what
     * was on screen instead, which is usually the answer.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param condition what to wait for, and how long
     * @return the element, with a fresh ref, or null when the wait was for it to go away
     */
    public Element waitFor(String target, WaitFor condition) {
        return element("app_wait_for", condition.args(target));
    }

    /**
     * Scrolls until an element is on screen and returns it with a ref.
     *
     * <p>{@link #map()} only sees what is currently visible. Tap, type and
     * long-press already scroll to a target that is not, so reach for this to
     * look without acting, or to scroll back up. This one scrolls down;
     * nothing in the hierarchy says which way a container scrolls, so for any
     * other way say so with {@link #scrollTo(String, String)}.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @return the element, with a fresh ref
     */
    public Element scrollTo(String target) { return scrollTo(target, "down"); }

    /**
     * Scrolls the given way until an element is on screen. Swiping the wrong
     * way is not a no-op, so if a swipe navigates instead of scrolling, it
     * stops after one.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param direction {@code "down"}, {@code "up"}, {@code "left"} or {@code "right"}
     * @return the element, with a fresh ref
     */
    public Element scrollTo(String target, String direction) {
        return element("app_scroll_to", args("target", target, "direction", direction));
    }

    // -- acting ------------------------------------------------------------

    /**
     * Taps a ref ({@code "@e5"}) or a locator ({@code "text=Sign In"}).
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     */
    public void tap(String target) { act("app_tap", args("target", target)); }

    /**
     * Taps a point in device pixels.
     *
     * @param x the horizontal coordinate, in device pixels
     * @param y the vertical coordinate, in device pixels
     */
    public void tap(int x, int y) { act("app_tap", args("x", x, "y", y)); }

    /**
     * Taps twice, as one gesture rather than as two taps.
     *
     * <p>The same tool as {@link #tap(String)} with one argument set, so the
     * target is resolved the same way and refused the same way when the screen
     * has moved. The uiautomator dump driver refuses it: the window is
     * 40-300ms and nothing there controls the interval between two adb calls.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     */
    public void doubleTap(String target) {
        act("app_tap", args("target", target, "double", true));
    }

    /**
     * Double-taps a point in device pixels.
     *
     * @param x the horizontal coordinate, in device pixels
     * @param y the vertical coordinate, in device pixels
     */
    public void doubleTap(int x, int y) {
        act("app_tap", args("x", x, "y", y, "double", true));
    }

    /**
     * Picks one element up, carries it onto another, and drops it.
     *
     * <p>Not a swipe with two targets: a swipe has no hold at either end, so
     * pointed at a reorderable row it scrolls the list instead of moving the
     * row. Both ends are resolved from one snapshot before anything is
     * touched.
     *
     * <p>Reports that the gesture was delivered. Whether the drop was accepted
     * is the app's own state, so call {@code map} again to see it.
     *
     * @param from what to pick up: a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param to where to drop it: a ref or a locator
     */
    public void drag(String from, String to) {
        act("app_drag", args("from", from, "to", to));
    }

    /**
     * Drags with the hold at each end spelled out, in milliseconds. Raise it
     * first when a drag picks nothing up: the default is 700, above Android's
     * 500ms long-press timeout, and some lists arm slower than that.
     *
     * @param from what to pick up: a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param to where to drop it: a ref or a locator
     * @param holdMillis how long to hold at each end, in milliseconds
     */
    public void drag(String from, String to, int holdMillis) {
        act("app_drag", args("from", from, "to", to, "hold_ms", holdMillis));
    }

    /**
     * Taps an element with several fingers at once, side by side — a
     * two-finger tap with 2, three with 3 (up to 5). On iOS three fingers can
     * reach the system instead of the app: three-finger gestures are undo,
     * redo, copy and paste there.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param fingers how many fingers, from 2 to 5
     */
    public void tapFingers(String target, int fingers) {
        act("app_tap", args("target", target, "fingers", fingers));
    }

    /**
     * Holds one element with a finger while a second finger taps another; the
     * first lifts only after the second. Both are resolved before anything is
     * touched. Reports that the gesture was delivered; call {@code map} again
     * to see what it did. Android 15 and earlier only: on iOS XCTest adds a
     * zero-length touch at the second finger's target when the gesture starts,
     * and on Android 16 and later UiAutomator2's down times are rejected, so
     * both refuse with {@link UnsupportedException}.
     *
     * @param hold what the first finger holds: a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param tap what the second finger taps: a ref or a locator
     */
    public void pressTap(String hold, String tap) {
        act("app_press_tap", args("hold", hold, "tap", tap));
    }

    /**
     * Holds one element with a finger while a second finger drags from one
     * element to another. Not {@link #drag(String, String)}, which is one
     * finger carrying something: here one finger anchors and the other moves.
     * Android 15 and earlier only, for {@link #pressTap}'s reasons.
     *
     * @param hold what the first finger holds: a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param from where the second finger starts: a ref or a locator
     * @param to where the second finger ends: a ref or a locator
     */
    public void pressDrag(String hold, String from, String to) {
        act("app_press_drag", args("hold", hold, "from", from, "to", to));
    }

    /**
     * Types into an element. Pass {@code ""} to clear it.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param text the text to type; {@code ""} clears the field
     */
    public void type(String target, String text) {
        act("app_type", args("target", target, "text", text));
    }

    /**
     * Clears an element and types into it.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param text the text to type once the field is cleared
     */
    public void replace(String target, String text) {
        act("app_type", args("target", target, "text", text, "clear", true));
    }

    /**
     * Drags across the middle of the screen: {@code "up"}, {@code "down"},
     * {@code "left"} or {@code "right"}. The finger moves that way, so
     * {@code "up"} scrolls a page down.
     *
     * @param direction {@code "up"}, {@code "down"}, {@code "left"} or {@code "right"}: the way the finger moves
     */
    public void swipe(String direction) { act("app_swipe", args("direction", direction)); }

    /**
     * Drags between two points in device pixels.
     *
     * @param x1 where the finger lands, horizontally, in device pixels
     * @param y1 where the finger lands, vertically, in device pixels
     * @param x2 where the finger lifts, horizontally, in device pixels
     * @param y2 where the finger lifts, vertically, in device pixels
     */
    public void swipe(int x1, int y1, int x2, int y2) {
        act("app_swipe", args("x1", x1, "y1", y1, "x2", x2, "y2", y2));
    }

    /**
     * Presses and holds an element.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     */
    public void longPress(String target) { act("app_long_press", args("target", target)); }

    /**
     * Presses and holds an element for a given time.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param duration how long to hold
     */
    public void longPress(String target, Duration duration) {
        act("app_long_press", args("target", target, "duration_ms", duration.toMillis()));
    }

    // -- app lifecycle -----------------------------------------------------

    /**
     * Brings an app to the foreground by package name or bundle id, and waits
     * for it to actually be in front. Discards every ref from the previous
     * screen.
     *
     * @param app the package name (Android) or bundle id (iOS)
     */
    public void launch(String app) { act("app_launch", args("app", app)); }

    /**
     * Stops a running app.
     *
     * @param app the package name (Android) or bundle id (iOS)
     */
    public void terminate(String app) { act("app_terminate", args("app", app)); }

    /**
     * Installs a local .apk or .app, returning the absolute path installed.
     *
     * @param path the .apk or .app to install
     * @return the absolute path installed
     */
    public String install(Path path) {
        return Json.str(data("app_install", args("path", path.toString())), "path");
    }

    /**
     * Removes an app, verified by listing afterwards — {@code adb uninstall}
     * reports success when it has only removed the updates to a system app.
     *
     * @param app the package name (Android) or bundle id (iOS)
     */
    public void uninstall(String app) { act("app_uninstall", args("app", app)); }

    /**
     * Deletes an app's data and leaves it installed — a fresh install's state,
     * without reinstalling. The answer holds {@code emptied}, {@code kept} and,
     * on Android, {@code still_granted}: {@code pm clear} revokes the runtime
     * permissions the user granted. An iOS simulator keeps its privacy grants
     * and keychain; a real iPhone refuses.
     *
     * @param app the package name (Android) or bundle id (iOS)
     * @return what was emptied and kept, and on Android what is still granted
     */
    public Map<String, Object> clearData(String app) {
        return data("app_clear_data", args("app", app));
    }

    /**
     * Opens a URL or deep link — the quickest way to a specific screen — and
     * returns the app that ended up in the foreground.
     *
     * @param url the URL or deep link
     * @return the app that ended up in the foreground
     */
    public String openUrl(String url) {
        return Json.str(data("app_open_url", args("url", url)), "app");
    }

    /**
     * Apps someone installed. A stock emulator ships about 240 system ones.
     *
     * @return the installed apps
     */
    public List<App> apps() { return apps(false); }

    /**
     * Installed apps, optionally including the platform's own.
     *
     * @param includeSystem whether to list the apps the platform ships with too
     * @return the installed apps
     */
    public List<App> apps(boolean includeSystem) {
        List<App> out = new ArrayList<>();
        Map<String, Object> d = data("app_list_apps", args("system", includeSystem));
        for (Object o : Json.asArray(d.get("apps"))) out.add(App.from(Json.asObject(o)));
        return out;
    }

    // -- device state ------------------------------------------------------

    /**
     * Grants permissions up front, so no dialog blocks the flow.
     *
     * <p>Names are cross-platform ({@code "camera"}, {@code "location"},
     * {@code "contacts"}, …); {@code "all"} grants everything the app
     * declares, and a platform name such as {@code "android.permission.NFC"}
     * also works. On Android the result is verified by reading the state
     * back, because {@code pm grant} reports success for permissions the app
     * never declared.
     *
     * @param app the package name (Android) or bundle id (iOS)
     * @param permissions cross-platform names such as {@code "camera"}, {@code "all"}, or platform names
     */
    public void grant(String app, String... permissions) {
        act("app_grant", args("app", app, "permissions", permissions));
    }

    /**
     * Denies permissions, to test how the app behaves without them.
     *
     * @param app the package name (Android) or bundle id (iOS)
     * @param permissions cross-platform names such as {@code "camera"}, {@code "all"}, or platform names
     */
    public void revoke(String app, String... permissions) {
        act("app_revoke", args("app", app, "permissions", permissions));
    }

    /**
     * Puts permissions back to their defaults. iOS can reset one app; Android
     * cannot — {@code pm reset-permissions} is device-wide — so pass
     * {@code null} there rather than an app.
     *
     * @param app the app on iOS; null on Android, where the reset is device-wide
     */
    public void resetPermissions(String app) {
        act("app_reset_permissions", app == null ? args() : args("app", app));
    }

    /**
     * The device's light/dark setting.
     *
     * @return {@code "light"} or {@code "dark"}
     */
    public String appearance() { return Json.str(data("app_appearance", null), "appearance"); }

    /**
     * Changes the light/dark setting and returns the new one. {@code "light"},
     * {@code "dark"}, or {@code "auto"} on Android only. Discards the refs
     * from the last map.
     *
     * @param mode {@code "light"}, {@code "dark"}, or {@code "auto"} on Android
     * @return the setting now in force
     */
    public String appearance(String mode) {
        return Json.str(data("app_appearance", args("appearance", mode)), "appearance");
    }

    /**
     * Every accessibility setting the device has, by name — reduce_motion,
     * bold_text, increase_contrast and the rest. A setting the platform lacks
     * is absent.
     *
     * @return each setting's value, by name
     */
    public Map<String, String> accessibility() {
        Map<String, Object> settings = Json.asObject(data("app_accessibility", null).get("settings"));
        Map<String, String> out = new java.util.LinkedHashMap<>();
        if (settings != null) {
            for (Map.Entry<String, Object> e : settings.entrySet()) {
                out.put(e.getKey(), String.valueOf(e.getValue()));
            }
        }
        return out;
    }

    /**
     * One accessibility setting.
     *
     * @param setting the setting, such as {@code "reduce_motion"}
     * @return the setting's value
     */
    public String accessibility(String setting) {
        return Json.str(data("app_accessibility", args("setting", setting)), "value");
    }

    /**
     * Changes one accessibility setting for the rest of the session and
     * returns its new value, confirmed by reading it back; the device is put
     * back as it was when the session ends. A switch takes {@code "on"} or
     * {@code "off"}; text_size a category (iOS); text_scale a number such as
     * {@code "1.3"} (Android). A real iPhone refuses. Discards the refs from
     * the last map.
     *
     * @param setting the setting, such as {@code "reduce_motion"}
     * @param value {@code "on"} or {@code "off"} for a switch, a category for text_size, a number for text_scale
     * @return the new value, as read back
     */
    public String setAccessibility(String setting, String value) {
        return Json.str(data("app_accessibility", args("setting", setting, "value", value)), "value");
    }

    /**
     * Which way the screen is turned. A screen that merely happens to be
     * portrait can rotate under you, so {@link #orientationLocked()} is a
     * separate question.
     *
     * @return {@code "portrait"}, {@code "landscape"}, {@code "portrait-reverse"} or {@code "landscape-reverse"}
     */
    public String orientation() { return Json.str(data("app_orientation", null), "orientation"); }

    /**
     * Whether the orientation is pinned rather than following the sensor.
     *
     * @return true when the orientation is pinned
     */
    public boolean orientationLocked() {
        return Json.bool(data("app_orientation", null), "locked");
    }

    /**
     * Turns the screen and pins it there; {@code "auto"} hands it back to the
     * sensor. {@code "portrait"}, {@code "landscape"},
     * {@code "portrait-reverse"} or {@code "landscape-reverse"} — not
     * left/right, which the two platforms name differently. Discards the refs
     * from the last map, since bounds do not survive a rotation.
     *
     * @param mode {@code "portrait"}, {@code "landscape"}, {@code "portrait-reverse"}, {@code "landscape-reverse"} or {@code "auto"}
     * @return the orientation now in force
     */
    public String orientation(String mode) {
        return Json.str(data("app_orientation", args("orientation", mode)), "orientation");
    }

    /**
     * Reads the screen, or makes an Android device pretend to be another.
     *
     * <p>A flow that works on the screen you happen to have is a flow tested
     * once. Pass a profile name to apply it, or {@code "reset"} to put the
     * device back — an override outlives this session. Applying one discards
     * the refs from the last map, because nothing is where it was.
     *
     * <p>On iOS the screen is fixed when the simulator is created, so this
     * reads only and names the simulator to boot instead.
     *
     * @return the screen's size, density and profile, and with {@code inspect} its findings
     */
    public Map<String, Object> screen() { return screen("", false); }

    /**
     * Reads or applies a screen profile, optionally reporting layout findings.
     *
     * <p>With {@code inspect}, the answer carries {@code findings}: elements
     * past the edge, touch targets below the platform minimum, text the
     * platform truncated, and tappable elements with nothing to announce.
     * Treat a touch-target finding as worth a look rather than a defect —
     * Android can enlarge a tap area without changing an element's bounds.
     *
     * @param profile a screen profile's name, {@code "reset"}, or empty to only read
     * @param inspect whether to report layout findings
     * @return the screen's size, density and profile, and with {@code inspect} its findings
     */
    public Map<String, Object> screen(String profile, boolean inspect) {
        Map<String, Object> args = new LinkedHashMap<>();
        if (profile != null && !profile.isBlank()) args.put("profile", profile);
        if (inspect) args.put("inspect", true);
        return data("app_screen", args);
    }

    /**
     * Turns two fingers about an element or the screen, positive clockwise.
     * Pass an empty target for the middle of the screen.
     *
     * <p>Reports that the gesture was delivered and nothing more, and this one
     * is harder still to confirm than a zoom: nothing in either hierarchy
     * reports a rotation, and there is no WebView property to ask either.
     *
     * @param degrees how far to turn, in degrees, positive clockwise
     * @param target what to turn about: a ref or a locator, or empty for the middle of the screen
     */
    public void rotate(double degrees, String target) {
        Map<String, Object> args = new LinkedHashMap<>();
        args.put("degrees", degrees);
        if (target != null && !target.isEmpty()) { args.put("target", target); }
        data("app_rotate", args);
    }

    /**
     * Pinches apart or together, about an element or the screen. Pass an
     * empty target for the middle of the screen.
     *
     * <p>Reports that the gesture was delivered and nothing more: neither
     * platform exposes a zoom level in the accessibility hierarchy, so
     * confirming a zoom means asking whatever was zoomed — a WebView can
     * answer with {@code visualViewport.scale} through {@link #eval}.
     *
     * @param direction {@code "in"} to pinch apart, {@code "out"} to pinch together
     * @param target what to pinch about: a ref or a locator, or empty for the middle of the screen
     */
    public void zoom(String direction, String target) {
        Map<String, Object> args = new LinkedHashMap<>();
        args.put("direction", direction);
        if (target != null && !target.isEmpty()) { args.put("target", target); }
        data("app_zoom", args);
    }

    /**
     * Puts a checkbox or switch into a state, rather than toggling it.
     *
     * <p>Idempotent: asking for a state it is already in does nothing, which
     * is what makes it safe to call without reading first. Anything with no
     * checked state is refused rather than tapped, and a radio cannot be
     * unchecked — a group is cleared by choosing a different member.
     *
     * @param target a ref from {@link #map()} such as {@code "@e5"}, or a locator such as {@code "text=Sign in"}
     * @param checked true to check it, false to uncheck it
     */
    public void check(String target, boolean checked) {
        data("app_check", args("target", target, "checked", checked));
    }

    /**
     * What a system dialog says, or an empty string when none is up.
     *
     * <p>A permission prompt is another process's window, not the app's, and
     * reading it needs no knowledge of what the buttons say.
     *
     * @return the dialog's text, or an empty string when none is up
     */
    public String alert() {
        return Json.str(data("app_alert", null), "text");
    }

    /**
     * Accepts or dismisses a system dialog.
     *
     * <p>These answer a dialog; they do not choose an outcome. On a permission
     * prompt they do not mean grant and deny, and on iOS they are the other
     * way round — accept leaves it denied and dismiss leaves it granted,
     * because W3C accept presses the affirmative button and Apple puts
     * "Don't Allow" last. Tap the button by ref for a particular answer.
     *
     * @param accept true to accept, false to dismiss
     */
    public void answerAlert(boolean accept) {
        data("app_alert", args("action", accept ? "accept" : "dismiss"));
    }

    /**
     * What the device clipboard holds.
     *
     * <p>iOS only. On Android 10 and later only an app with focus may read the
     * clipboard and the UiAutomator2 server has no activity, so it would
     * answer "empty" for a clipboard that is full — this throws there instead.
     *
     * @return the clipboard's text
     */
    public String clipboard() {
        return Json.str(data("app_clipboard", null), "text");
    }

    /**
     * Writes the device clipboard. On iOS the write is confirmed by reading it
     * back; on Android it is reported as sent, nothing there being able to
     * read it.
     *
     * @param text what to put on the clipboard
     */
    public void setClipboard(String text) {
        data("app_clipboard", args("text", text));
    }

    /**
     * Where the device believes it is.
     *
     * <p>{@code mock} says this fix was injected; {@code mocking} says a test
     * provider is installed now. They differ after {@link #clearLocation()},
     * because Android keeps the last known position after the provider that
     * supplied it is gone.
     *
     * <p>Android only. {@code simctl location} has no {@code get}, so on iOS
     * this throws rather than returning a position it never read.
     *
     * @return the position, and whether it was injected
     */
    public Location location() {
        Map<String, Object> d = data("app_location", null);
        return new Location(Json.dbl(d, "latitude"), Json.dbl(d, "longitude"),
                Json.bool(d, "mock"), Json.bool(d, "mocking"), Json.bool(d, "known"));
    }

    /**
     * Places the device at a coordinate.
     *
     * <p>On Android this goes through a test provider, is read back, and works
     * on real hardware. On iOS it goes through simctl and cannot be confirmed:
     * returning means the request was accepted, not that an app will read it.
     *
     * @param latitude in degrees, north positive
     * @param longitude in degrees, east positive
     */
    public void setLocation(double latitude, double longitude) {
        data("app_location", args("latitude", latitude, "longitude", longitude));
    }

    /**
     * Removes the injected position. Does not clear the device's last known
     * location, which Android caches.
     */
    public void clearLocation() {
        data("app_location", args("clear", true));
    }

    /**
     * Moves along two or more waypoints over time, each a
     * {@code {latitude, longitude}} pair, at {@code speedMPS} meters per
     * second — pass 0 for the default.
     *
     * <p>On iOS the simulator interpolates the route itself; on Android the
     * daemon steps a test provider once a second, the platform having no
     * route command. Either way this returns as the route starts.
     *
     * @param waypoints two or more {@code {latitude, longitude}} pairs, in degrees
     * @param speedMPS meters per second, or 0 for the default
     */
    public void followRoute(List<double[]> waypoints, double speedMPS) {
        if (waypoints == null) throw invalid("waypoints must not be null");
        List<Object> wp = new ArrayList<>();
        for (double[] p : waypoints) {
            if (p == null || p.length != 2) throw invalid("each waypoint is {latitude, longitude}");
            wp.add(List.of(p[0], p[1]));
        }
        Map<String, Object> args = new LinkedHashMap<>();
        args.put("waypoints", wp);
        if (speedMPS > 0) { args.put("speed", speedMPS); }
        data("app_location", args);
    }

    /**
     * Follows a GPX file, read on the machine running the daemon.
     *
     * @param path the GPX file, on the machine running the daemon
     * @param speedMPS meters per second, or 0 for the default
     */
    public void followGpx(String path, double speedMPS) {
        Map<String, Object> args = new LinkedHashMap<>();
        args.put("gpx", path);
        if (speedMPS > 0) { args.put("speed", speedMPS); }
        data("app_location", args);
    }

    /**
     * Language tags pinned for an app; empty means it follows the device.
     *
     * @param app the package name (Android) or bundle id (iOS)
     * @return the tags now pinned; empty when the app follows the device
     */
    public List<String> appLocale(String app) {
        List<String> out = new ArrayList<>();
        for (Object o : Json.asArray(data("app_locale", args("app", app)).get("locales"))) {
            out.add(String.valueOf(o));
        }
        return out;
    }

    /**
     * Runs one app in a chosen language; an empty tag follows the device
     * again. Android 13 and later.
     *
     * <p>What this confirms is that the device stored the tag, not that the
     * app has a translation for it — Android reports no difference — so check
     * the screen. Relaunch the app for it to re-render.
     *
     * @param app the package name (Android) or bundle id (iOS)
     * @param tags language tags such as {@code "ja-JP"}, or empty to follow the device
     * @return the tags now pinned; empty when the app follows the device
     */
    public List<String> appLocale(String app, String tags) {
        List<String> out = new ArrayList<>();
        Object raw = data("app_locale", args("app", app, "locale", tags)).get("locales");
        for (Object o : Json.asArray(raw)) {
            out.add(String.valueOf(o));
        }
        return out;
    }

    /**
     * Presses a hardware button: {@code "back"}, {@code "home"},
     * {@code "recents"}, {@code "volume-up"} or {@code "volume-down"}.
     *
     * <p>On Android back is primary navigation. iOS has no back button by
     * design and throws with what to do instead, rather than sending an edge
     * swipe — a different event an app can tell apart. Any press can move the
     * screen, so the refs from the last map are discarded.
     *
     * @param button {@code "back"}, {@code "home"}, {@code "recents"}, {@code "volume-up"} or {@code "volume-down"}
     */
    public void press(String button) { data("app_press", args("button", button)); }

    /**
     * Whether the screen is locked.
     *
     * @return true when the screen is locked
     */
    public boolean screenLocked() { return Json.bool(data("app_lock", null), "locked"); }

    /**
     * Locks or unlocks the screen, confirmed against the device. A state
     * rather than a power-button press: power is a toggle, so asking twice
     * leaves the device where it started. A device with a PIN, pattern or
     * password cannot be unlocked from outside and throws.
     *
     * @param locked true to lock, false to unlock
     * @return whether the screen is locked now, as read back
     */
    public boolean screenLocked(boolean locked) {
        return Json.bool(data("app_lock", args("state", locked ? "lock" : "unlock")), "locked");
    }

    /**
     * Simulates an incoming call: {@code "ring"}, {@code "accept"} or
     * {@code "hang"}. Emulator only — a real phone cannot be made to ring
     * from outside.
     *
     * <p>Not named {@code call}: that is the raw tool-call escape hatch.
     *
     * @param action {@code "ring"}, {@code "accept"} or {@code "hang"}
     */
    public void incomingCall(String action) { data("app_call", args("action", action)); }

    /**
     * Delivers a simulated text message. Emulator only.
     *
     * @param text the message body
     */
    public void sms(String text) { data("app_sms", args("text", text)); }

    /**
     * Console output from the current WebView since the last call, including
     * uncaught errors and unhandled promise rejections. Each read drains what
     * it returns, so it reports what happened since the last call. Capture
     * starts when the context is entered, so a page's initial load is already
     * over by then.
     *
     * @return each console entry since the last call
     */
    public List<Map<String, Object>> logs() {
        List<Map<String, Object>> out = new ArrayList<>();
        // Named, because with no source the tool follows the context and
        // would read the device log on the native shell.
        for (Object o : Json.asArray(data("app_logs", args("source", "webview")).get("entries"))) {
            out.add(Json.asObject(o));
        }
        return out;
    }

    /**
     * The device's own log since the last read: logcat on Android, the
     * unified log on an iOS simulator, what a real iPhone's session has captured. The map has {@code entries} — each with
     * time, level, tag, pid and message — and {@code skipped}, lines newer than
     * the last read that the limit dropped, which will not come back. The
     * first read returns the most recent lines. Pass null or zero to leave a
     * filter unset.
     *
     * @param app only this app's lines, or null for every app
     * @param level the lowest level to include, or null for every level
     * @param lines the most lines to return, or 0 for the default
     * @return the lines under {@code entries}, and how many were {@code skipped}
     */
    public Map<String, Object> deviceLogs(String app, String level, int lines) {
        Map<String, Object> args = new LinkedHashMap<>();
        args.put("source", "device");
        if (app != null && !app.isBlank()) args.put("app", app);
        if (level != null && !level.isBlank()) args.put("level", level);
        if (lines > 0) args.put("lines", lines);
        return data("app_logs", args);
    }

    /**
     * Records the screen: action "start", or "stop" with a path to save the
     * video; null asks whether one is running. Stop returns the file's frames
     * and duration, read from its own header; a still screen is one frame on
     * Android, which is not a failure. A relative path is this process's.
     *
     * @param action {@code "start"}, {@code "stop"}, or null to ask whether one is running
     * @param path where to save the video on stop, or null
     * @return whether a recording is running, or on stop its frames and duration
     */
    public Map<String, Object> record(String action, String path) {
        Map<String, Object> args = new LinkedHashMap<>();
        if (action != null && !action.isBlank()) args.put("action", action);
        if (path != null && !path.isBlank()) args.put("path", path);
        return data("app_record", args);
    }

    /**
     * The soft keyboard: read it, type at the focused field, press a key, or
     * hide it. With nulls and false, returns {@code shown} and the
     * {@code focused} field — a password's value is never shown. Text is added
     * to the end of the focused field and confirmed; key is "enter", "delete"
     * or "space", after any text; hide hides the keyboard, confirmed, alone.
     * Throws {@link NoSuchElementException} when nothing has focus.
     *
     * @param text text to add at the end of the focused field, or null
     * @param key {@code "enter"}, {@code "delete"} or {@code "space"}, or null
     * @param hide whether to hide the keyboard
     * @return whether the keyboard is {@code shown}, and the {@code focused} field
     */
    public Map<String, Object> keyboard(String text, String key, boolean hide) {
        Map<String, Object> args = new LinkedHashMap<>();
        if (text != null) args.put("text", text);
        if (key != null && !key.isBlank()) args.put("key", key);
        if (hide) args.put("hide", true);
        return data("app_keyboard", args);
    }

    /**
     * The crashes the device recorded, newest first: each map has id, time,
     * kind (crash, native_crash or anr), app and summary. Not drained — asking
     * twice shows a crash twice. Pass null for every app and zero for the
     * default limit.
     *
     * @param app only this app's crashes, or null for every app
     * @param limit the most to return, or 0 for the default
     * @return the crashes, newest first
     */
    public List<Map<String, Object>> crashes(String app, int limit) {
        Map<String, Object> args = new LinkedHashMap<>();
        if (app != null && !app.isBlank()) args.put("app", app);
        if (limit > 0) args.put("limit", limit);
        List<Map<String, Object>> out = new ArrayList<>();
        for (Object o : Json.asArray(data("app_crashes", args).get("crashes"))) {
            out.add(Json.asObject(o));
        }
        return out;
    }

    /**
     * One crash report in full, by an id from {@link #crashes}; the text is under {@code text}.
     *
     * @param id an id from {@link #crashes}
     * @return the report, or an empty map when there is no such crash
     */
    public Map<String, Object> crash(String id) {
        List<Object> found = Json.asArray(data("app_crashes", args("id", id)).get("crashes"));
        return found.isEmpty() ? Map.of() : Json.asObject(found.get(0));
    }

    /**
     * Runs a JavaScript expression in the current WebView; objects come back as JSON.
     *
     * @param expression a JavaScript expression
     * @return the expression's value; an object comes back as JSON
     */
    public String eval(String expression) {
        return Json.str(data("app_eval", args("expression", expression)), "value");
    }

    /**
     * What is in the notification shade — how a test asserts an app posted
     * what it should. Each map has package, title and text.
     *
     * @return each notification, with its package, title and text
     */
    public List<Map<String, Object>> notifications() {
        List<Map<String, Object>> out = new ArrayList<>();
        for (Object o : Json.asArray(data("app_notifications", null).get("notifications"))) {
            out.add(Json.asObject(o));
        }
        return out;
    }

    /**
     * Puts a notification in the shade as an interruption, confirmed by
     * reading the shade back.
     *
     * @param title the notification's title
     * @param text the notification's body
     */
    public void postNotification(String title, String text) {
        data("app_notifications", args("title", title, "text", text));
    }

    /**
     * Opens or closes the notification panel. A notification cannot be tapped
     * until the shade is open: until then it is not on screen and
     * {@link #map()} cannot see it. Discards the refs from the last map.
     *
     * @param open true to open the shade, false to close it
     */
    public void shade(boolean open) {
        data("app_notifications", args("shade", open ? "open" : "close"));
    }

    /**
     * The device timezone.
     *
     * @return the zone, as an IANA name
     */
    public String timezone() { return Json.str(data("app_timezone", null), "timezone"); }

    /**
     * Changes the device timezone and returns the new one. Takes an IANA name
     * such as {@code "Asia/Tokyo"}; confirmed by reading it back, since an
     * unknown zone is accepted by the device and ignored. Works on real
     * hardware, unlike {@link #incomingCall} and {@link #sms}.
     *
     * @param tz an IANA zone name, such as {@code "Asia/Tokyo"}
     * @return the zone now in force, as read back
     */
    public String timezone(String tz) {
        return Json.str(data("app_timezone", args("timezone", tz)), "timezone");
    }

    // -- contexts ----------------------------------------------------------

    /**
     * The automatable contexts: the native shell plus any WebViews.
     *
     * @return the context ids
     */
    public List<String> contexts() {
        List<String> out = new ArrayList<>();
        for (Object o : Json.asArray(data("app_contexts", null).get("contexts"))) {
            out.add(Json.str(Json.asObject(o), "id"));
        }
        return out;
    }

    /**
     * Switches context, or reads the current one when {@code name} is null.
     *
     * @param name a context from {@link #contexts()}, or null to read the current one
     * @return the context now in force
     */
    public String context(String name) {
        return prose("app_context", name == null ? args() : args("context", name));
    }

    // -- diagnostics -------------------------------------------------------

    /**
     * Checks the environment and returns the report. Needs no device — that
     * is the point of it.
     *
     * @return the report
     */
    public String doctor() { return prose("app_doctor", args()); }

    // -- plumbing ----------------------------------------------------------

    /**
     * Runs any tool by name, for anything this class does not wrap yet, and
     * returns its structured answer.
     *
     * @param tool the tool's name, such as {@code "app_map"}
     * @param arguments the tool's arguments, as its schema names them
     * @return the tool's structured answer
     */
    public Map<String, Object> call(String tool, Map<String, Object> arguments) {
        if (tool == null || tool.isBlank()) throw invalid("a tool name is required");
        return Connection.dataOf(conn.call(tool, arguments));
    }

    /**
     * What {@link Builder#start()} opened: the device, platform and driver.
     *
     * @return the session, or null for an instance from {@link #connect()}
     */
    public Session session() { return session; }

    /**
     * Ends the session on the device, as Appium's quit does, and closes the
     * connection. The teardown is the daemon's own: accessibility settings put
     * back, a recording or route stopped, WebViews detached, the device-side
     * server stopped. Quitting a session that is not open succeeds, and a
     * second quit -- say, try-with-resources closing after an explicit one --
     * does nothing.
     */
    public void quit() {
        if (quit) return;
        quit = true;
        Map<String, Object> args = new LinkedHashMap<>();
        args.put("action", "end");
        if (session != null) args.put("device", session.device());
        try {
            conn.call("app_session", args);
        } finally {
            conn.close();
        }
    }

    /**
     * The sessions open on the daemon.
     *
     * @return each with device, platform and driver
     */
    public List<Map<String, Object>> sessions() {
        List<Map<String, Object>> out = new ArrayList<>();
        for (Object o : Json.asArray(data("app_session", args("action", "status")).get("sessions"))) {
            out.add(Json.asObject(o));
        }
        return out;
    }

    /**
     * Quits an instance from {@link Builder#start()}, whose session was opened
     * for this block; closes one from {@link #connect()}, leaving the session
     * to whoever opened it.
     */
    @Override public void close() {
        if (session != null) quit();
        else if (!quit) conn.close();
    }

    private void act(String tool, Map<String, Object> args) { conn.call(tool, args); }

    /**
     * A tool's arguments, as key-value pairs. Every value passed here is one
     * the tool needs -- optional ones are added only when set -- so a null is
     * the caller's mistake, and it is refused here, naming the argument, as
     * the .NET client does, rather than as Map.of's bare
     * NullPointerException. An array becomes a list, and may not hold a null
     * either.
     */
    private static Map<String, Object> args(Object... pairs) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i + 1 < pairs.length; i += 2) {
            String key = (String) pairs[i];
            Object value = pairs[i + 1];
            if (value == null) throw invalid(key + " must not be null");
            if (value instanceof Object[]) {
                List<Object> items = new ArrayList<>();
                for (Object o : (Object[]) value) {
                    if (o == null) throw invalid(key + " must not contain null");
                    items.add(o);
                }
                value = items;
            }
            m.put(key, value);
        }
        return m;
    }

    private static InvalidArgumentException invalid(String why) {
        return new InvalidArgumentException(why, "", "", false, Map.of());
    }

    private String prose(String tool, Map<String, Object> args) {
        return Connection.textOf(conn.call(tool, args));
    }

    private Map<String, Object> data(String tool, Map<String, Object> args) {
        return Connection.dataOf(conn.call(tool, args == null ? Map.of() : args));
    }

    private List<Element> elements(String tool, Map<String, Object> args) {
        List<Element> out = new ArrayList<>();
        for (Object o : Json.asArray(data(tool, args).get("elements"))) {
            out.add(Element.from(Json.asObject(o)));
        }
        return out;
    }

    /** Reads a tool that answers with a single element, which may be absent. */
    private Element element(String tool, Map<String, Object> args) {
        Object el = data(tool, new LinkedHashMap<>(args)).get("element");
        return el == null ? null : Element.from(Json.asObject(el));
    }
}
