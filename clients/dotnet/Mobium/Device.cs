using System;
using System.Collections.Generic;
using System.IO;

namespace Mobium
{
    /// <summary>
    /// Drives native apps on Android emulators, Android phones, iOS
    /// simulators and iPhones.
    /// </summary>
    /// <remarks>
    /// <para>Speaks to the same tool layer the CLI and the MCP server use,
    /// over <c>mobium pipe</c>, so a .NET program and a command cannot drift
    /// apart. The <c>mobium</c> binary has to be on <c>PATH</c>, or named by
    /// <c>MOBIUM_BIN_PATH</c>.</para>
    /// <para>Refs like <c>@e1</c> are valid only for the screen they came
    /// from. Every action re-resolves its target immediately before acting,
    /// retries briefly while the screen settles, and scrolls to it if it is
    /// below the fold — so a tap can follow another tap without a sleep in
    /// between.</para>
    /// <para>Safe to share between threads, one call at a time: there is one
    /// pipe underneath, and one device, so calls from two threads are
    /// serialized rather than interleaved. <see cref="Dispose"/> from another
    /// thread ends a call that is waiting.</para>
    /// </remarks>
    /// <example>
    /// <code>
    /// using var device = Device.Connect();
    /// device.Launch("com.example.shop");
    /// var signIn = device.WaitFor("text=Sign in");
    /// device.Tap(signIn.Ref);
    /// </code>
    /// </example>
    public sealed class Device : IDisposable
    {
        private readonly Connection _conn;
        private bool _quit;

        internal Device(Connection conn) { _conn = conn; }

        /// <summary>
        /// What <see cref="DeviceBuilder.Start"/> opened — device, platform,
        /// driver — or null for a device from <see cref="Connect"/>.
        /// </summary>
        public Session? Session { get; internal set; }

        // -- connecting -----------------------------------------------------

        /// <summary>
        /// Connects to mobium, for whichever device is running. It does not
        /// touch the device: the session there opens on the first call that
        /// needs it, or with <see cref="DeviceBuilder.Start"/>.
        /// </summary>
        public static Device Connect() => Builder().Connect();

        /// <summary>Configures a connection or a session.</summary>
        public static DeviceBuilder Builder() => new DeviceBuilder();

        // -- reading --------------------------------------------------------

        /// <summary>Every attached device and simulator.</summary>
        public IList<DeviceInfo> Devices()
        {
            var list = new List<DeviceInfo>();
            foreach (var o in Field("app_devices", null, "devices"))
                list.Add(DeviceInfo.From(Json.AsObject(o)));
            return list;
        }

        /// <summary>
        /// The actionable elements on the current screen. Refs are valid only
        /// for this screen — call it again after anything that changes the
        /// screen, or just act, since actions re-resolve their target anyway.
        /// </summary>
        public IList<Element> Map() => Elements("app_map", null);

        /// <summary>The elements matching a locator, without acting on them.</summary>
        public IList<Element> Find(string locator) =>
            Elements("app_find", Args("locator", locator));

        /// <summary>
        /// The raw hierarchy — what Appium calls the page source — for when
        /// <see cref="Map"/> leaves out the thing you need to see; map is what
        /// to act on. <c>source</c> is the platform's XML, or in a WebView the
        /// page's markup; <c>units</c> is "px" on Android and "pt" on iOS, where
        /// map, taps and screenshots are in pixels, <c>scale</c> times as many.
        /// <c>redacted</c> counts the password fields hidden.
        /// </summary>
        public IDictionary<string, object?> Source() => Data("app_source", Args());

        /// <summary>
        /// Declares how to answer a dialog, so an action that meets it carries
        /// on: when a dialog whose text contains <paramref name="when"/> is in
        /// the way, press the button captioned <paramref name="press"/>. It names
        /// a button, not accept or dismiss, because which button those press
        /// differs by platform and by dialog. Captions match ignoring case.
        /// </summary>
        public void AddDialogRule(string when, string press) =>
            Act("app_dialogs", Args("when", when, "press", press));

        /// <summary>The declared rules, each with how often it has answered.</summary>
        public IList<IDictionary<string, object?>> DialogRules() =>
            Maps(Field("app_dialogs", Args(), "rules"));

        /// <summary>Removes every rule for this device.</summary>
        public void ClearDialogRules() => Act("app_dialogs", Args("clear", true));

        /// <summary>Everything readable on screen.</summary>
        public string Text() => Prose("app_text", Args());

        /// <summary>The text of one element, by ref or locator.</summary>
        public string Text(string target) => Prose("app_text", Args("target", target));

        /// <summary>
        /// The package name or bundle id of the foreground app. Costs no extra
        /// device call: it reads the hierarchy a snapshot fetches anyway.
        /// </summary>
        public string Current() => Json.Str(Data("app_current", null), "app");

        /// <summary>Captures the screen as PNG.</summary>
        public byte[] Screenshot()
        {
            var result = _conn.Call("app_screenshot", Args());
            result.TryGetValue("content", out var content);
            foreach (var block in Json.AsArray(content))
            {
                var c = Json.AsObject(block);
                if (Json.Str(c, "type") != "image") continue;
                try
                {
                    return Convert.FromBase64String(Json.Str(c, "data"));
                }
                catch (FormatException e)
                {
                    throw new MobiumException("mobium returned an image that is not base64", e);
                }
            }
            throw new MobiumException("mobium returned no image");
        }

        /// <summary>Captures the screen as PNG and writes it to a file.</summary>
        public byte[] Screenshot(string path)
        {
            Act("app_screenshot", Args("path", path));
            try
            {
                return File.ReadAllBytes(path);
            }
            catch (IOException e)
            {
                throw new MobiumException("could not read back " + path, e);
            }
        }

        // -- waiting and scrolling ------------------------------------------

        /// <summary>Waits for an element to be on screen, for up to ten seconds.</summary>
        public Element? WaitFor(string target) => WaitFor(target, Until.Visible());

        /// <summary>
        /// Blocks until the screen agrees, instead of sleeping. On success the
        /// screen is remapped, so the element returned already has a ref that
        /// can be tapped. Waiting for something to go away returns
        /// <c>null</c>: there is nothing left to point at.
        /// </summary>
        public Element? WaitFor(string target, Until condition) =>
            One("app_wait_for", condition.Args(target));

        /// <summary>Scrolls down until an element is on screen and returns it with a ref.</summary>
        public Element? ScrollTo(string target) => ScrollTo(target, "down");

        /// <summary>
        /// Scrolls <c>"down"</c>, <c>"up"</c>, <c>"left"</c> or <c>"right"</c>
        /// until an element is on screen. Nothing on screen says which way a
        /// container scrolls, so a horizontal pager needs <c>"left"</c> or
        /// <c>"right"</c>. Swiping the wrong way is not a no-op, so if a swipe
        /// navigates instead of scrolling, it stops after one.
        /// </summary>
        public Element? ScrollTo(string target, string direction) =>
            One("app_scroll_to", Args("target", target, "direction", direction));

        // -- acting ----------------------------------------------------------

        /// <summary>Taps a ref (<c>"@e5"</c>) or a locator (<c>"text=Sign In"</c>).</summary>
        public void Tap(string target) => Act("app_tap", Args("target", target));

        /// <summary>Taps a point in device pixels.</summary>
        public void Tap(int x, int y) => Act("app_tap", Args("x", x, "y", y));

        /// <summary>
        /// Taps twice, as one gesture rather than as two taps. The same tool
        /// as <see cref="Tap(string)"/> with one argument set, so the target
        /// is resolved the same way and refused the same way when the screen
        /// has moved. The uiautomator dump driver refuses it: the window is
        /// 40-300ms and nothing there controls the interval between two adb
        /// calls.
        /// </summary>
        public void DoubleTap(string target) =>
            Act("app_tap", Args("target", target, "double", true));

        /// <summary>Double-taps a point in device pixels.</summary>
        public void DoubleTap(int x, int y) =>
            Act("app_tap", Args("x", x, "y", y, "double", true));

        /// <summary>
        /// Picks one element up, carries it onto another, and drops it. Not a
        /// swipe with two targets: a swipe has no hold at either end, so
        /// pointed at a reorderable row it scrolls the list instead of moving
        /// the row. Both ends are resolved from one snapshot before anything
        /// is touched. Reports that the gesture was delivered; whether the
        /// drop was accepted is the app's own state, so call Map again.
        /// </summary>
        public void Drag(string from, string to) =>
            Act("app_drag", Args("from", from, "to", to));

        /// <summary>
        /// Drags with the hold at each end spelled out, in milliseconds.
        /// Raise it first when a drag picks nothing up: the default is 700,
        /// above Android's 500ms long-press timeout, and some lists arm
        /// slower than that.
        /// </summary>
        public void Drag(string from, string to, int holdMillis) =>
            Act("app_drag", Args("from", from, "to", to, "hold_ms", holdMillis));

        /// <summary>
        /// Taps an element with several fingers at once, side by side — a
        /// two-finger tap with 2, three with 3 (up to 5). On iOS three fingers
        /// can reach the system instead of the app: three-finger gestures are
        /// undo, redo, copy and paste there.
        /// </summary>
        public void TapFingers(string target, int fingers) =>
            Act("app_tap", Args("target", target, "fingers", fingers));

        /// <summary>
        /// Holds one element with a finger while a second finger taps another;
        /// the first lifts only after the second. Both are resolved before
        /// anything is touched. Reports that the gesture was delivered; call
        /// Map again to see what it did. Android 15 and earlier only: on iOS
        /// XCTest adds a zero-length touch at the second finger's target when
        /// the gesture starts, and on Android 16 and later UiAutomator2's down
        /// times are rejected, so both refuse with UnsupportedException.
        /// </summary>
        public void PressTap(string hold, string tap) =>
            Act("app_press_tap", Args("hold", hold, "tap", tap));

        /// <summary>
        /// Holds one element with a finger while a second finger drags from
        /// one element to another. Not <see cref="Drag(string, string)"/>,
        /// which is one finger carrying something: here one finger anchors and
        /// the other moves. Android 15 and earlier only, for PressTap's reasons.
        /// </summary>
        public void PressDrag(string hold, string from, string to) =>
            Act("app_press_drag", Args("hold", hold, "from", from, "to", to));

        /// <summary>Types into an element, after what it holds. Pass <c>""</c> to clear it.</summary>
        public void Type(string target, string text) =>
            Act("app_type", Args("target", target, "text", text));

        /// <summary>Clears an element and types into it, replacing what it held.</summary>
        public void Fill(string target, string text) =>
            Act("app_fill", Args("target", target, "text", text));

        /// <summary>
        /// Drags across the middle of the screen: <c>"up"</c>, <c>"down"</c>,
        /// <c>"left"</c> or <c>"right"</c>. The finger moves that way, so
        /// <c>"up"</c> scrolls a page down.
        /// </summary>
        public void Swipe(string direction) => Act("app_swipe", Args("direction", direction));

        /// <summary>Drags between two points in device pixels.</summary>
        public void Swipe(int x1, int y1, int x2, int y2) =>
            Act("app_swipe", Args("x1", x1, "y1", y1, "x2", x2, "y2", y2));

        /// <summary>Presses and holds an element.</summary>
        public void LongPress(string target) => Act("app_long_press", Args("target", target));

        /// <summary>Presses and holds an element for a given time.</summary>
        public void LongPress(string target, TimeSpan duration) =>
            Act("app_long_press", Args("target", target, "duration_ms", (long)duration.TotalMilliseconds));

        // -- app lifecycle ---------------------------------------------------

        /// <summary>
        /// Brings an app to the foreground by package name or bundle id, and
        /// waits for it to actually be in front. Discards every ref from the
        /// previous screen.
        /// </summary>
        public void Launch(string app) => Act("app_launch", Args("app", app));

        /// <summary>Stops a running app.</summary>
        public void Terminate(string app) => Act("app_terminate", Args("app", app));

        /// <summary>Installs a local .apk or .app, returning the absolute path installed.</summary>
        public string Install(string path) =>
            Json.Str(Data("app_install", Args("path", path)), "path");

        /// <summary>
        /// Removes an app, verified by listing afterwards — <c>adb uninstall</c>
        /// reports success when it has only removed the updates to a system app.
        /// </summary>
        public void Uninstall(string app) => Act("app_uninstall", Args("app", app));

        /// <summary>
        /// Deletes an app's data and leaves it installed — a fresh install's
        /// state, without reinstalling. The answer holds <c>emptied</c>,
        /// <c>kept</c> and, on Android, <c>still_granted</c>: <c>pm clear</c>
        /// revokes the runtime permissions the user granted. An iOS simulator
        /// keeps its privacy grants and keychain; a real iPhone refuses.
        /// </summary>
        public IDictionary<string, object?> ClearData(string app) =>
            Data("app_clear_data", Args("app", app));

        // -- files -----------------------------------------------------------

        /// <summary>
        /// Puts a file from this machine where the device keeps downloads, so
        /// an app's file picker finds it, under the file's own name. On
        /// Android that is the shared Download folder, one for every app; the
        /// file is indexed in MediaStore, which is what the picker reads, and
        /// read back there. On an iOS simulator it is the Documents folder of
        /// the app in front, which the Files app shows under On My iPhone; on a
        /// real iPhone the same, through CoreDevice, confirmed by its bytes.
        /// </summary>
        /// <returns>
        /// The transfer: <c>device</c>, <c>app</c> (iOS), <c>name</c>,
        /// <c>where</c>, <c>bytes</c>, <c>checked</c> — how it was confirmed
        /// at both ends — and <c>path</c>.
        /// </returns>
        public IDictionary<string, object?> Upload(string path) =>
            Data("app_upload", Args("path", path));

        /// <summary>
        /// As <see cref="Upload(string)"/>, giving the file
        /// <paramref name="name"/> on the device — a name, not a path.
        /// </summary>
        public IDictionary<string, object?> Upload(string path, string name) =>
            Data("app_upload", Args("path", path, "name", name));

        /// <summary>
        /// As <see cref="Upload(string)"/>, into the Documents of
        /// <paramref name="app"/>, a bundle id, rather than the app in front.
        /// Android has one Download folder for every app and ignores it, so a
        /// test written for both platforms can name it on both. A
        /// <c>null</c> <paramref name="name"/> keeps the file's own.
        /// </summary>
        public IDictionary<string, object?> Upload(string path, string? name, string app)
        {
            var args = Args("path", path, "app", app);
            if (!string.IsNullOrWhiteSpace(name)) args["name"] = name;
            return Data("app_upload", args);
        }

        /// <summary>
        /// Brings back a file from where the device keeps downloads and saves
        /// it at <paramref name="path"/> on this machine, to check what an app
        /// saved: Android's shared Download folder, or on an iOS simulator the
        /// Documents of the app in front. The copy's size is read back against
        /// the device's, on a simulator or a real iPhone alike.
        /// <see cref="Downloads()"/> lists what there is to fetch.
        /// </summary>
        /// <returns>
        /// The transfer: <c>device</c>, <c>app</c> (iOS), <c>name</c>,
        /// <c>where</c>, <c>bytes</c>, <c>checked</c> and <c>path</c>, the
        /// absolute path it was saved at.
        /// </returns>
        public IDictionary<string, object?> Download(string name, string path) =>
            Data("app_download", Args("name", name, "path", path));

        /// <summary>
        /// As <see cref="Download(string, string)"/>, from the Documents of
        /// <paramref name="app"/>, a bundle id, rather than the app in front.
        /// Android ignores it.
        /// </summary>
        public IDictionary<string, object?> Download(string name, string path, string app) =>
            Data("app_download", Args("name", name, "path", path, "app", app));

        /// <summary>
        /// Brings back a file from where the device keeps downloads, as
        /// <see cref="Download(string, string)"/> does, and returns its bytes
        /// rather than saving it — for a caller whose disk is not the daemon's.
        /// </summary>
        public byte[] Download(string name)
        {
            var d = Data("app_download", Args("name", name));
            // An empty file comes back with no data at all: the field is
            // omitted when empty, so its absence is only an answer at zero bytes.
            if (!d.TryGetValue("data", out var data) || data == null)
            {
                if (d.ContainsKey("bytes") && Json.Integer(d, "bytes") == 0) return new byte[0];
                throw new MobiumException("mobium returned no contents for " + name);
            }
            try
            {
                return Convert.FromBase64String(Json.Str(d, "data"));
            }
            catch (FormatException e)
            {
                throw new MobiumException("mobium returned contents for " + name + " that are not base64", e);
            }
        }

        /// <summary>
        /// What the downloads folder holds, each file with <c>name</c>,
        /// <c>bytes</c> and <c>modified</c>: Android's shared Download folder,
        /// or on iOS, a simulator or a real iPhone, the Documents of the app in
        /// front.
        /// </summary>
        public IList<IDictionary<string, object?>> Downloads() =>
            Maps(Field("app_download", Args(), "files"));

        /// <summary>
        /// As <see cref="Downloads()"/>, for the Documents of
        /// <paramref name="app"/>, a bundle id, rather than the app in front.
        /// Android ignores it.
        /// </summary>
        public IList<IDictionary<string, object?>> Downloads(string app) =>
            Maps(Field("app_download", Args("app", app), "files"));

        /// <summary>
        /// One step for <see cref="Batch"/>: a tool and the arguments it
        /// takes on its own.
        /// </summary>
        public static IDictionary<string, object?> Step(string tool, IDictionary<string, object?>? arguments = null)
        {
            if (string.IsNullOrWhiteSpace(tool))
                throw new InvalidArgumentException("a step needs a tool name", "", "", false, null);
            return new Dictionary<string, object?>(StringComparer.Ordinal)
            {
                ["name"] = tool,
                ["arguments"] = arguments ?? new Dictionary<string, object?>(StringComparer.Ordinal),
            };
        }

        /// <summary>
        /// Runs several tools in order, on this device, in one call:
        /// <code>
        /// device.Batch(
        ///     Device.Step("app_tap", new Dictionary&lt;string, object?&gt; { ["target"] = "text=Sign in" }),
        ///     Device.Step("app_wait_for", new Dictionary&lt;string, object?&gt; { ["target"] = "text=Welcome" }));
        /// </code>
        /// Every step is checked before the first runs, and the batch stops at
        /// the first failure, throwing that step's own exception; its
        /// <c>Details</c> hold <c>step</c> and what <c>completed</c> before it.
        /// Returns each step's <c>name</c>, <c>text</c> and <c>data</c>, in order.
        /// </summary>
        public IList<IDictionary<string, object?>> Batch(params IDictionary<string, object?>[] steps)
        {
            if (steps == null || steps.Length == 0)
                throw new InvalidArgumentException("a batch needs at least one step", "", "", false, null);
            return Maps(Field("app_batch", Args("steps", new List<object?>(steps)), "steps"));
        }

        /// <summary>
        /// The network: <c>airplane</c>, <c>online</c>, and the shaping —
        /// <c>latency_ms</c>, <c>download_kbps</c>, <c>upload_kbps</c>, zero
        /// for none. Android only.
        /// </summary>
        public IDictionary<string, object?> Network() => Data("app_network", null);

        /// <summary>
        /// Turns airplane mode on or off and waits for the network to follow —
        /// on an emulator or a real Android phone.
        /// </summary>
        public IDictionary<string, object?> SetOffline(bool offline = true) =>
            Data("app_network", Args("offline", offline));

        /// <summary>
        /// Adds latency to each round trip and limits download and upload, in
        /// kbit/s, replacing any shaping set before; zero is none. Needs root,
        /// so an emulator.
        /// </summary>
        public IDictionary<string, object?> ShapeNetwork(int latencyMs = 0, int downloadKbps = 0, int uploadKbps = 0) =>
            Data("app_network", Args("latency_ms", latencyMs, "download_kbps", downloadKbps, "upload_kbps", uploadKbps));

        /// <summary>Removes the shaping and turns airplane mode off.</summary>
        public IDictionary<string, object?> ResetNetwork() => Data("app_network", Args("reset", true));

        /// <summary>
        /// The battery: <c>level</c> in percent, <c>state</c> (charging,
        /// discharging, not_charging, full or unknown) and on Android
        /// <c>plugged</c>. An iOS simulator has none: <c>present</c> is false.
        /// </summary>
        public IDictionary<string, object?> Battery() => Data("app_battery", null);

        /// <summary>
        /// What time the device thinks it is: <c>time</c> (RFC 3339, in its
        /// own offset), <c>zone</c>, and <c>clock</c> — "device", or "mac" on
        /// an iOS simulator, which has no clock of its own.
        /// </summary>
        public IDictionary<string, object?> DeviceTime() => Data("app_time", null);

        /// <summary>
        /// Shakes an emulator or simulator — what shake-to-undo and
        /// shake-to-report listen for. Whether the app reacts is up to its own
        /// detector, so check the screen after. A real phone refuses.
        /// </summary>
        public void Shake() => Act("app_shake", Args());

        /// <summary>
        /// One app's state, for any app: <c>state</c> is not_installed,
        /// not_running, background or foreground. An app under its own
        /// permission prompt is still in front, and <c>covered_by</c> names
        /// the prompt's process. On iOS a background app also has
        /// <c>suspended</c>.
        /// </summary>
        public IDictionary<string, object?> AppState(string app) =>
            Data("app_state", Args("app", app));

        /// <summary>
        /// Sends the app in front away for <paramref name="seconds"/> and
        /// brings it back, resumed rather than relaunched, confirmed in front
        /// again. At most 180 seconds; <paramref name="app"/> defaults to the
        /// one in front.
        /// </summary>
        public void Background(double seconds, string? app = null)
        {
            var args = Args("seconds", seconds);
            if (!string.IsNullOrWhiteSpace(app)) args["app"] = app;
            Act("app_background", args);
        }

        /// <summary>
        /// Opens a URL or deep link — the quickest way to a specific screen —
        /// and returns the app that ended up in the foreground.
        /// </summary>
        public string OpenUrl(string url) =>
            Json.Str(Data("app_open_url", Args("url", url)), "app");

        /// <summary>Apps someone installed. A stock emulator ships about 240 system ones.</summary>
        public IList<App> Apps() => Apps(false);

        /// <summary>Installed apps, optionally including the platform's own.</summary>
        public IList<App> Apps(bool includeSystem)
        {
            var list = new List<App>();
            foreach (var o in Field("app_list_apps", Args("system", includeSystem), "apps"))
                list.Add(App.From(Json.AsObject(o)));
            return list;
        }

        // -- device state ----------------------------------------------------

        /// <summary>
        /// Grants permissions up front, so no dialog blocks the flow. Names are
        /// cross-platform (<c>"camera"</c>, <c>"location"</c>, …);
        /// <c>"all"</c> grants everything the app declares. On Android the
        /// result is verified by reading the state back, because
        /// <c>pm grant</c> reports success for permissions the app never
        /// declared.
        /// </summary>
        public void Grant(string app, params string[] permissions) =>
            Act("app_grant", Args("app", app, "permissions", permissions));

        /// <summary>Denies permissions, to test how the app behaves without them.</summary>
        public void Revoke(string app, params string[] permissions) =>
            Act("app_revoke", Args("app", app, "permissions", permissions));

        /// <summary>
        /// Puts permissions back to asking. Naming an app resets only that
        /// app's, on both platforms; on Android that stops the app if it had a
        /// permission granted. Pass <c>null</c> to reset every app on the device.
        /// </summary>
        public void ResetPermissions(string app) =>
            Act("app_reset_permissions", app == null ? Args() : Args("app", app));

        /// <summary>The device's light/dark setting.</summary>
        public string Appearance() => Json.Str(Data("app_appearance", null), "appearance");

        /// <summary>
        /// Changes the light/dark setting and returns the new one.
        /// <c>"light"</c>, <c>"dark"</c>, or <c>"auto"</c> on Android only.
        /// Discards the refs from the last map.
        /// </summary>
        public string Appearance(string mode) =>
            Json.Str(Data("app_appearance", Args("appearance", mode)), "appearance");

        /// <summary>
        /// Every accessibility setting the device has, by name — reduce_motion,
        /// bold_text, increase_contrast and the rest. A setting the platform
        /// lacks is absent.
        /// </summary>
        public IDictionary<string, string> Accessibility()
        {
            var data = Data("app_accessibility", null);
            var result = new Dictionary<string, string>();
            if (data != null && data.TryGetValue("settings", out var raw) && raw != null)
            {
                foreach (var e in Json.AsObject(raw))
                {
                    result[e.Key] = Convert.ToString(e.Value, System.Globalization.CultureInfo.InvariantCulture);
                }
            }
            return result;
        }

        /// <summary>One accessibility setting.</summary>
        public string Accessibility(string setting) =>
            Json.Str(Data("app_accessibility", Args("setting", setting)), "value");

        /// <summary>
        /// Changes one accessibility setting for the rest of the session and
        /// returns its new value, confirmed by reading it back; the device is
        /// put back as it was when the session ends. A switch takes
        /// <c>"on"</c> or <c>"off"</c>; text_size a category (iOS);
        /// text_scale a number such as <c>"1.3"</c> (Android). A real iPhone
        /// refuses. Discards the refs from the last map.
        /// </summary>
        public string SetAccessibility(string setting, string value) =>
            Json.Str(Data("app_accessibility", Args("setting", setting, "value", value)), "value");

        /// <summary>
        /// Which way the screen is turned. A screen that merely happens to be
        /// portrait can rotate under you, so <see cref="OrientationLocked"/>
        /// is a separate question.
        /// </summary>
        public string Orientation() => Json.Str(Data("app_orientation", null), "orientation");

        /// <summary>Whether the orientation is pinned rather than following the sensor.</summary>
        public bool OrientationLocked() => Json.Bool(Data("app_orientation", null), "locked");

        /// <summary>
        /// Turns the screen and pins it there; <c>"auto"</c> hands it back to
        /// the sensor. <c>"portrait"</c>, <c>"landscape"</c>,
        /// <c>"portrait-reverse"</c> or <c>"landscape-reverse"</c> — not
        /// left/right, which the two platforms name differently. Discards the
        /// refs from the last map, since bounds do not survive a rotation.
        /// </summary>
        public string Orientation(string mode) =>
            Json.Str(Data("app_orientation", Args("orientation", mode)), "orientation");

        /// <summary>
        /// Reads the screen, or makes an Android device pretend to be another.
        /// </summary>
        /// <remarks>
        /// <para>A flow that works on the screen you happen to have is a flow
        /// tested once. Pass a profile name to apply it, or <c>"reset"</c> to
        /// put the device back — an override outlives this session. Applying
        /// one discards the refs from the last map, because nothing is where
        /// it was.</para>
        /// <para>On iOS the screen is fixed when the simulator is created, so
        /// this reads only and names the simulator to boot instead.</para>
        /// <para>With <paramref name="inspect"/> the answer carries
        /// <c>findings</c>: elements past the edge, touch targets below the
        /// platform minimum, text the platform truncated, and tappable
        /// elements with nothing to announce. Treat a touch-target finding as
        /// worth a look rather than a defect — Android can enlarge a tap area
        /// without changing an element's bounds.</para>
        /// </remarks>
        public IDictionary<string, object?> Screen(string profile = "", bool inspect = false)
        {
            var args = Args();
            if (!string.IsNullOrWhiteSpace(profile)) args["profile"] = profile;
            if (inspect) args["inspect"] = true;
            return Data("app_screen", args);
        }

        /// <summary>
        /// Turns two fingers about an element or the screen, positive
        /// clockwise. Reports that the gesture was delivered and nothing more,
        /// and this one is harder still to confirm than a zoom: nothing in
        /// either hierarchy reports a rotation, and there is no WebView
        /// property to ask either.
        /// </summary>
        public void Rotate(double degrees = 90, string? target = null)
        {
            var args = Args("degrees", degrees);
            if (!string.IsNullOrEmpty(target)) args["target"] = target;
            Act("app_rotate", args);
        }

        /// <summary>
        /// Pinches apart or together, about an element or the screen. Reports
        /// that the gesture was delivered and nothing more: neither platform
        /// exposes a zoom level in the accessibility hierarchy, so confirming a
        /// zoom means asking whatever was zoomed — a WebView can answer with
        /// <c>visualViewport.scale</c> through <see cref="Eval"/>.
        /// </summary>
        public void Zoom(string direction = "in", string? target = null)
        {
            var args = Args("direction", direction);
            if (!string.IsNullOrEmpty(target)) args["target"] = target;
            Act("app_zoom", args);
        }

        /// <summary>
        /// Puts a checkbox or switch into a state, rather than toggling it.
        /// Idempotent: asking for a state it is already in does nothing, which
        /// is what makes it safe to call without reading first. Anything with
        /// no checked state is refused rather than tapped, and a radio cannot
        /// be unchecked — a group is cleared by choosing a different member.
        /// </summary>
        public void Check(string target, bool checkedState = true) =>
            Act("app_check", Args("target", target, "checked", checkedState));

        /// <summary>
        /// What a system dialog says, or an empty string when none is up. A
        /// permission prompt is another process's window, not the app's, and
        /// reading it needs no knowledge of what the buttons say.
        /// </summary>
        public string Alert() => Json.Str(Data("app_alert", Args()), "text");

        /// <summary>
        /// Accepts or dismisses a system dialog. These answer a dialog; they
        /// do not choose an outcome. On a permission prompt they do not mean
        /// grant and deny, and on iOS they are the other way round — accept
        /// leaves it denied and dismiss leaves it granted, because W3C accept
        /// presses the affirmative button and Apple puts "Don't Allow" last.
        /// Tap the button by ref for a particular answer.
        /// </summary>
        public void AnswerAlert(bool accept) =>
            Act("app_alert", Args("action", accept ? "accept" : "dismiss"));

        /// <summary>
        /// Types into a dialog's field, then accepts or dismisses it, in one
        /// call. A plain alert has no field, and the platform refuses the
        /// text. What accept and dismiss press is as for
        /// <see cref="AnswerAlert(bool)"/>.
        /// </summary>
        public void AnswerAlert(bool accept, string text) =>
            Act("app_alert", Args("action", accept ? "accept" : "dismiss", "text", text));

        /// <summary>
        /// What the device clipboard holds. iOS only: on Android 10 and later
        /// only an app with focus may read the clipboard and the UiAutomator2
        /// server has no activity, so it would answer "empty" for a clipboard
        /// that is full — this throws there instead.
        /// </summary>
        public string Clipboard() => Json.Str(Data("app_clipboard", Args()), "text");

        /// <summary>
        /// Writes the device clipboard. On iOS the write is confirmed by
        /// reading it back; on Android it is reported as sent.
        /// </summary>
        public void SetClipboard(string text) => Act("app_clipboard", Args("text", text));

        /// <summary>
        /// Where the device believes it is. <c>Mock</c> says this fix was
        /// injected; <c>Mocking</c> says a test provider is installed now.
        /// They differ after <see cref="ClearLocation"/>, because Android keeps
        /// the last known position after the provider supplying it is gone.
        /// Android only: <c>simctl location</c> has no <c>get</c>, so on iOS
        /// this throws rather than returning a position it never read.
        /// </summary>
        public Location Location()
        {
            var d = Data("app_location", Args());
            return new Location(
                Json.Number(d, "latitude"), Json.Number(d, "longitude"),
                Json.Bool(d, "mock"), Json.Bool(d, "mocking"), Json.Bool(d, "known"));
        }

        /// <summary>
        /// Places the device at a coordinate. On Android this goes through a
        /// test provider, is read back, and works on real hardware. On iOS it
        /// goes through simctl and cannot be confirmed: returning means the
        /// request was accepted, not that an app will read it.
        /// </summary>
        public void SetLocation(double latitude, double longitude) =>
            Act("app_location", Args("latitude", latitude, "longitude", longitude));

        /// <summary>
        /// Removes the injected position. Does not clear the device's last
        /// known location, which Android caches.
        /// </summary>
        public void ClearLocation() => Act("app_location", Args("clear", true));

        /// <summary>
        /// Moves along two or more waypoints over time, each a
        /// (latitude, longitude) pair, at <paramref name="speedMPS"/> meters
        /// per second — pass 0 for the default. On iOS the simulator
        /// interpolates the route itself; on Android the daemon steps a test
        /// provider once a second, the platform having no route command.
        /// </summary>
        public void FollowRoute(IEnumerable<double[]> waypoints, double speedMPS)
        {
            if (waypoints == null)
                throw new InvalidArgumentException("waypoints must not be null", "", "", false, null);
            var wp = new List<object?>();
            foreach (var p in waypoints)
            {
                if (p == null || p.Length != 2)
                    throw new InvalidArgumentException("each waypoint is {latitude, longitude}", "", "", false, null);
                wp.Add(new List<object?> { p[0], p[1] });
            }
            var args = Args("waypoints", wp);
            if (speedMPS > 0) args["speed"] = speedMPS;
            Act("app_location", args);
        }

        /// <summary>Follows a GPX file, read on the machine running the daemon.</summary>
        public void FollowGpx(string path, double speedMPS)
        {
            var args = Args("gpx", path);
            if (speedMPS > 0) args["speed"] = speedMPS;
            Act("app_location", args);
        }

        /// <summary>Language tags pinned for an app; empty means it follows the device.</summary>
        public IList<string> AppLocale(string app) => Strings(Field("app_locale", Args("app", app), "locales"));

        /// <summary>
        /// Runs one app in a chosen language; an empty tag follows the device
        /// again. Android 13 and later. What this confirms is that the device
        /// stored the tag, not that the app has a translation for it, so check
        /// the screen. Relaunch the app for it to re-render.
        /// </summary>
        public IList<string> AppLocale(string app, string tags) =>
            Strings(Field("app_locale", Args("app", app, "locale", tags), "locales"));

        /// <summary>
        /// Presses a hardware button: <c>"back"</c>, <c>"home"</c>,
        /// <c>"recents"</c>, <c>"volume-up"</c> or <c>"volume-down"</c>. On
        /// Android back is primary navigation. iOS has no back button by
        /// design and throws with what to do instead, rather than sending an
        /// edge swipe — a different event an app can tell apart.
        /// </summary>
        public void Press(string button) => Act("app_press", Args("button", button));

        /// <summary>Whether the screen is locked.</summary>
        public bool ScreenLocked() => Json.Bool(Data("app_lock", null), "locked");

        /// <summary>
        /// Locks or unlocks the screen, confirmed against the device. A state
        /// rather than a power-button press: power is a toggle, so asking twice
        /// leaves the device where it started. A device with a PIN, pattern or
        /// password cannot be unlocked from outside and throws.
        /// </summary>
        public bool ScreenLocked(bool locked) =>
            Json.Bool(Data("app_lock", Args("state", locked ? "lock" : "unlock")), "locked");

        /// <summary>
        /// Simulates an incoming call: <c>"ring"</c>, <c>"accept"</c> or
        /// <c>"hang"</c>. Emulator only — a real phone cannot be made to ring
        /// from outside. Not named <c>Call</c>: that is the raw tool-call
        /// escape hatch.
        /// </summary>
        public void IncomingCall(string action) => Act("app_call", Args("action", action));

        /// <summary>Simulates an incoming call from a given number. Emulator only.</summary>
        public void IncomingCall(string action, string number) =>
            Act("app_call", Args("action", action, "number", number));

        /// <summary>Delivers a simulated text message. Emulator only.</summary>
        public void Sms(string text) => Act("app_sms", Args("text", text));

        /// <summary>Delivers a simulated text message from a given number. Emulator only.</summary>
        public void Sms(string text, string from) => Act("app_sms", Args("text", text, "from", from));

        /// <summary>
        /// Console output from the current WebView since the last call,
        /// including uncaught errors and unhandled promise rejections. Each
        /// read drains what it returns.
        /// </summary>
        /// <remarks>
        /// The source is named because with none the tool follows the context
        /// and would read the device log on the native shell.
        /// </remarks>
        public IList<IDictionary<string, object?>> Logs() =>
            Maps(Field("app_logs", Args("source", "webview"), "entries"));

        /// <summary>
        /// The device's own log since the last read: logcat on Android, the
        /// unified log on an iOS simulator, what a real iPhone's session has captured. The map has <c>entries</c> — each
        /// with time, level, tag, pid and message — and <c>skipped</c>, lines
        /// newer than the last read that the limit dropped, which will not
        /// come back. The first read returns the most recent lines. Pass null
        /// or zero to leave a filter unset.
        /// </summary>
        public IDictionary<string, object?> DeviceLogs(string? app = null, string? level = null, int lines = 0)
        {
            var args = Args("source", "device");
            if (!string.IsNullOrWhiteSpace(app)) args["app"] = app;
            if (!string.IsNullOrWhiteSpace(level)) args["level"] = level;
            if (lines > 0) args["lines"] = lines;
            return Data("app_logs", args);
        }

        /// <summary>
        /// Records the screen: action "start", or "stop" with a path to save
        /// the video; neither asks whether one is running. Stop returns the
        /// file's frames and duration, read from its own header; a still
        /// screen is one frame on Android, which is not a failure. A relative
        /// path is this process's.
        /// </summary>
        public IDictionary<string, object?> Record(string? action = null, string? path = null)
        {
            var args = Args();
            if (!string.IsNullOrWhiteSpace(action)) args["action"] = action;
            if (!string.IsNullOrWhiteSpace(path)) args["path"] = path;
            return Data("app_record", args);
        }

        /// <summary>
        /// The soft keyboard: read it, type at the focused field, press a key,
        /// or hide it. With no arguments, returns <c>shown</c> and the
        /// <c>focused</c> field — a password's value is never shown. Text is
        /// added to the end of the focused field and confirmed; key is
        /// "enter", "delete" or "space", after any text; hide hides the
        /// keyboard, confirmed, alone. Throws
        /// <see cref="NoSuchElementException"/> when nothing has focus.
        /// </summary>
        public IDictionary<string, object?> Keyboard(string? text = null, string? key = null, bool hide = false)
        {
            var args = Args();
            if (text != null) args["text"] = text;
            if (!string.IsNullOrWhiteSpace(key)) args["key"] = key;
            if (hide) args["hide"] = true;
            return Data("app_keyboard", args);
        }

        /// <summary>
        /// The crashes the device recorded, newest first: each map has id,
        /// time, kind (crash, native_crash or anr), app and summary. Not
        /// drained — asking twice shows a crash twice.
        /// </summary>
        public IList<IDictionary<string, object?>> Crashes(string? app = null, int limit = 0)
        {
            var args = Args();
            if (!string.IsNullOrWhiteSpace(app)) args["app"] = app;
            if (limit > 0) args["limit"] = limit;
            return Maps(Field("app_crashes", args, "crashes"));
        }

        /// <summary>One crash report in full, by an id from <see cref="Crashes"/>; the text is under <c>text</c>.</summary>
        public IDictionary<string, object?> Crash(string id)
        {
            var found = Maps(Field("app_crashes", Args("id", id), "crashes"));
            return found.Count > 0 ? found[0] : new Dictionary<string, object?>();
        }

        /// <summary>Runs a JavaScript expression in the current WebView; objects come back as JSON.</summary>
        public string Eval(string expression) =>
            Json.Str(Data("app_eval", Args("expression", expression)), "value");

        /// <summary>
        /// The current WebView's cookies: the ones its page's URL is sent,
        /// HttpOnly ones included. Needs a web context — <c>Context</c> first.
        /// Each has Playwright's and Vibium's keys: name, value, domain, path,
        /// expires (seconds since the epoch, absent for a session cookie),
        /// httpOnly, secure and sameSite.
        /// </summary>
        public IList<IDictionary<string, object?>> Cookies() =>
            Maps(Field("app_cookies", Args("action", "get"), "cookies"));

        /// <summary>
        /// Sets each cookie on the current page and reads the store back, so
        /// one the browser accepted and stored expired throws
        /// <see cref="NotConfirmedException"/>.
        /// </summary>
        public void SetCookies(IEnumerable<IDictionary<string, object?>> cookies)
        {
            if (cookies == null) throw new ArgumentNullException(nameof(cookies));
            var list = new List<object?>();
            foreach (var c in cookies) list.Add(c);
            Data("app_cookies", Args("action", "set", "cookies", list));
        }

        /// <summary>Deletes the current page's cookies, or only those called <paramref name="name"/>.</summary>
        public void ClearCookies(string? name = null) =>
            Data("app_cookies", string.IsNullOrEmpty(name) ? Args("action", "clear") : Args("action", "clear", "name", name));

        /// <summary>
        /// The current page's storage state, in the shape Playwright and
        /// Vibium save: cookies, and origins with origin, localStorage and
        /// sessionStorage.
        /// </summary>
        public IDictionary<string, object?> Storage()
        {
            var data = Data("app_storage", Args("action", "get"));
            return data.TryGetValue("state", out var state) ? Json.AsObject(state) : new Dictionary<string, object?>();
        }

        /// <summary>
        /// Restores a saved state: its cookies, and each origin's storage into
        /// the page only if the page is on that origin.
        /// </summary>
        public void SetStorage(IDictionary<string, object?> state) =>
            Data("app_storage", Args("action", "restore", "state", state));

        /// <summary>Empties the current page's cookies, localStorage and sessionStorage.</summary>
        public void ClearStorage() => Data("app_storage", Args("action", "clear"));

        /// <summary>
        /// What is in the notification shade — how a test asserts an app posted
        /// what it should. Each map has package, title and text.
        /// </summary>
        public IList<IDictionary<string, object?>> Notifications() =>
            Maps(Field("app_notifications", null, "notifications"));

        /// <summary>
        /// Puts a notification in the shade as an interruption, confirmed by
        /// reading the shade back.
        /// </summary>
        public void PostNotification(string title, string text) =>
            Act("app_notifications", Args("title", title, "text", text));

        /// <summary>Puts a notification in the shade under the default title, "Mobium".</summary>
        public void PostNotification(string text) =>
            Act("app_notifications", Args("text", text));

        /// <summary>
        /// Opens or closes the notification panel. A notification cannot be
        /// tapped until the shade is open: until then it is not on screen and
        /// <see cref="Map"/> cannot see it.
        /// </summary>
        public void Shade(bool open) =>
            Act("app_notifications", Args("shade", open ? "open" : "close"));

        /// <summary>The device timezone.</summary>
        public string Timezone() => Json.Str(Data("app_timezone", null), "timezone");

        /// <summary>
        /// Changes the device timezone and returns the new one. Takes an IANA
        /// name such as <c>"Asia/Tokyo"</c>; confirmed by reading it back,
        /// since an unknown zone is accepted by the device and ignored. Works
        /// on real hardware, unlike <see cref="IncomingCall(string)"/> and
        /// <see cref="Sms(string)"/>.
        /// </summary>
        public string Timezone(string tz) =>
            Json.Str(Data("app_timezone", Args("timezone", tz)), "timezone");

        // -- contexts ---------------------------------------------------------

        /// <summary>The automatable contexts: the native shell plus any WebViews.</summary>
        public IList<string> Contexts()
        {
            var list = new List<string>();
            foreach (var o in Field("app_contexts", null, "contexts"))
                list.Add(Json.Str(Json.AsObject(o), "id"));
            return list;
        }

        /// <summary>Switches context, or reads the current one when <paramref name="name"/> is null.</summary>
        public string Context(string name) =>
            Prose("app_context", name == null ? Args() : Args("context", name));

        // -- diagnostics -------------------------------------------------------

        /// <summary>
        /// Checks the environment and returns the report. Needs no device —
        /// that is the point of it.
        /// </summary>
        public string Doctor() => Prose("app_doctor", Args());

        // -- plumbing ----------------------------------------------------------

        /// <summary>
        /// Runs any tool by name, for anything this class does not wrap yet,
        /// and returns its structured answer.
        /// </summary>
        public IDictionary<string, object?> Call(string tool, IDictionary<string, object?>? arguments)
        {
            if (string.IsNullOrWhiteSpace(tool))
                throw new InvalidArgumentException("a tool name is required", "", "", false, null);
            return Connection.DataOf(_conn.Call(tool, arguments));
        }

        /// <summary>
        /// Quits a device from <see cref="DeviceBuilder.Start"/>, whose session
        /// was opened for this <c>using</c> block. For one from
        /// <see cref="Connect"/>, closes the pipe and waits for mobium to exit,
        /// leaving the session in the daemon to whoever opened it.
        /// </summary>
        public void Dispose()
        {
            if (Session != null) Quit();
            else if (!_quit) _conn.Dispose();
        }

        /// <summary>
        /// Ends the session on the device, as Appium's quit does, and closes
        /// the connection. The teardown is the daemon's own: accessibility
        /// settings put back, a recording or route stopped, WebViews detached,
        /// the device-side server stopped, and the app Start launched, if any,
        /// stopped too. Quitting a session that is not open succeeds, and a
        /// second quit — a <c>using</c> block ending after an explicit one —
        /// does nothing. A program that exits without quitting or disposing
        /// has the sessions it started ended for it: mobium sees the client go.
        /// </summary>
        public void Quit()
        {
            if (_quit) return;
            _quit = true;
            var args = Args("action", "end");
            if (Session != null) args["device"] = Session.Device;
            try
            {
                _conn.Call("app_session", args);
            }
            finally
            {
                _conn.Dispose();
            }
        }

        /// <summary>The sessions open on the daemon, each with device, platform and driver.</summary>
        public IList<IDictionary<string, object?>> Sessions() =>
            Maps(Field("app_session", Args("action", "status"), "sessions"));

        internal void OpenSession(string platform, string app)
        {
            var args = Args("action", "start");
            if (!string.IsNullOrWhiteSpace(platform)) args["platform"] = platform;
            if (!string.IsNullOrWhiteSpace(app)) args["app"] = app;
            try
            {
                Session = Session.From(Data("app_session", args));
            }
            catch
            {
                _conn.Dispose();
                throw;
            }
        }

        // -- internals ---------------------------------------------------------

        // Every value passed here is one the tool needs; optional ones are
        // added only when set. So a null is the caller's mistake, and it is
        // refused here, naming the argument, rather than sent to mobium to
        // come back as a schema complaint about a key the caller never typed.
        private static IDictionary<string, object?> Args(params object?[] pairs)
        {
            var m = new Dictionary<string, object?>(StringComparer.Ordinal);
            for (var i = 0; i + 1 < pairs.Length; i += 2)
            {
                var key = (string)pairs[i]!;
                var value = pairs[i + 1];
                if (value == null)
                    throw new InvalidArgumentException(key + " must not be null", "", "", false, null);
                if (value is object?[] items && Array.IndexOf(items, null) >= 0)
                    throw new InvalidArgumentException(key + " must not contain null", "", "", false, null);
                m[key] = value;
            }
            return m;
        }

        private void Act(string tool, IDictionary<string, object?>? args) => _conn.Call(tool, args);

        private string Prose(string tool, IDictionary<string, object?>? args) =>
            Connection.TextOf(_conn.Call(tool, args));

        private IDictionary<string, object?> Data(string tool, IDictionary<string, object?>? args) =>
            Connection.DataOf(_conn.Call(tool, args ?? Args()));

        private IList<object?> Field(string tool, IDictionary<string, object?>? args, string key)
        {
            var d = Data(tool, args);
            return Json.AsArray(d.TryGetValue(key, out var v) ? v : null);
        }

        private IList<Element> Elements(string tool, IDictionary<string, object?>? args)
        {
            var list = new List<Element>();
            foreach (var o in Field(tool, args, "elements")) list.Add(Element.From(Json.AsObject(o)));
            return list;
        }

        /// <summary>Reads a tool that answers with a single element, which may be absent.</summary>
        private Element? One(string tool, IDictionary<string, object?>? args)
        {
            var d = Data(tool, args);
            return d.TryGetValue("element", out var el) && el != null
                ? Element.From(Json.AsObject(el))
                : null;
        }

        private static IList<string> Strings(IList<object?> raw)
        {
            var list = new List<string>();
            foreach (var o in raw) list.Add(Convert.ToString(o, System.Globalization.CultureInfo.InvariantCulture));
            return list;
        }

        private static IList<IDictionary<string, object?>> Maps(IList<object?> raw)
        {
            var list = new List<IDictionary<string, object?>>();
            foreach (var o in raw) list.Add(Json.AsObject(o));
            return list;
        }
    }

    /// <summary>Options for <see cref="Device.Connect()"/>.</summary>
    public sealed class DeviceBuilder
    {
        private string _binary = "";
        private string _device = "";
        private string _driver = "";
        private string _platform = "";
        private string _app = "";
        private TimeSpan _timeout = System.Threading.Timeout.InfiniteTimeSpan;
        private string _session = "";

        /// <summary>Pins the mobium executable, ahead of MOBIUM_BIN_PATH and PATH.</summary>
        public DeviceBuilder Binary(string path) { _binary = path ?? ""; return this; }

        /// <summary>Targets one device by serial or UDID. Omit when only one is running.</summary>
        public DeviceBuilder OnDevice(string serial) { _device = serial ?? ""; return this; }

        /// <summary>
        /// Chooses the driver: <c>uiautomator2</c> (default on Android),
        /// <c>uiautomator</c> (installs nothing, slower, cannot type) or
        /// <c>wda</c> (iOS simulators and iPhones).
        /// </summary>
        public DeviceBuilder Driver(string name) { _driver = name ?? ""; return this; }

        /// <summary>
        /// The longest any one call may take before the connection is given
        /// up, including the handshake. Unlimited by default, because the first
        /// session on an iPhone builds WebDriverAgent and that takes minutes.
        /// </summary>
        /// <remarks>
        /// A call that runs out <b>ends the connection</b>: there is one pipe,
        /// replies are told apart only by id, and the late answer to an
        /// abandoned call would be read as the answer to the next one. Every
        /// call after fails saying so; connect again. The device's session
        /// lives in the daemon, so reconnecting is cheap. Set it well above the
        /// longest <see cref="Until.Timeout"/> you use.
        /// </remarks>
        public DeviceBuilder CallTimeout(TimeSpan timeout)
        {
            if (timeout <= TimeSpan.Zero && timeout != System.Threading.Timeout.InfiniteTimeSpan)
                throw new ArgumentOutOfRangeException(nameof(timeout), "a call timeout must be positive, or Timeout.InfiniteTimeSpan");
            _timeout = timeout;
            return this;
        }

        /// <summary>
        /// A daemon of this connection's own, by name, as <c>MOBIUM_SESSION</c>
        /// sets one. One daemon serves one call at a time across every device,
        /// so parallel runs on different devices should each name one: sharing,
        /// an emulator's calls waited behind a simulator's, 22.3s against 0.3s.
        /// Keep it short; it is part of a socket path.
        /// </summary>
        public DeviceBuilder Session(string name) { _session = name ?? ""; return this; }

        /// <summary>
        /// The platform for <see cref="Start"/>: <c>android</c> or <c>ios</c>.
        /// <c>ios</c> picks the wda driver, so none need be named.
        /// </summary>
        public DeviceBuilder Platform(string name) { _platform = name ?? ""; return this; }

        /// <summary>An app for <see cref="Start"/> to launch once the session is up: a package name (Android) or bundle id (iOS).</summary>
        public DeviceBuilder App(string id) { _app = id ?? ""; return this; }

        /// <summary>
        /// Connects and opens the session on the device, as Appium's new
        /// session does: the device-side server is started now and, given an
        /// <see cref="App"/>, it is launched and in front when this returns.
        /// </summary>
        /// <remarks>
        /// Nothing requires it — every call opens a session on first use — but
        /// it puts the slow first start (installing UiAutomator2, building
        /// WebDriverAgent on an iPhone) where it was asked for. End it with
        /// <see cref="Device.Quit"/>, or a <c>using</c> block, which quits:
        /// <code>
        /// using var device = Device.Builder().Platform("android").App("com.android.settings").Start();
        /// device.Map();
        /// </code>
        /// </remarks>
        public Device Start()
        {
            var device = Connect();
            device.OpenSession(_platform, _app);
            return device;
        }

        /// <summary>Connects to mobium without touching the device.</summary>
        public Device Connect()
        {
            var args = new List<string>();
            if (!string.IsNullOrWhiteSpace(_device)) { args.Add("--device"); args.Add(_device); }
            if (!string.IsNullOrWhiteSpace(_driver)) { args.Add("--driver"); args.Add(_driver); }
            return new Device(new Connection(Connection.FindBinary(_binary), args, _timeout, _session));
        }
    }

}
