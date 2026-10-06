"""The device API."""

from __future__ import annotations

import base64
from dataclasses import dataclass
from typing import Any

from ._rpc import Connection, MobiumError, find_binary, _data_of, _text_of


@dataclass(frozen=True)
class Bounds:
    """An on-screen rectangle in device pixels, on both platforms."""

    x1: int
    y1: int
    x2: int
    y2: int

    @property
    def center(self) -> tuple[int, int]:
        return (self.x1 + self.x2) // 2, (self.y1 + self.y2) // 2

    @property
    def width(self) -> int:
        return self.x2 - self.x1

    @property
    def height(self) -> int:
        return self.y2 - self.y1


@dataclass(frozen=True)
class Element:
    """One actionable element on screen."""

    ref: str
    label: str
    role: str = ""
    bounds: Bounds = Bounds(0, 0, 0, 0)
    locator: str = ""
    context: str = ""
    checked: bool | None = None
    """A checkbox, radio or switch's state; None for anything with no such state."""
    selected: bool = False
    """True for what the platform reports chosen: the current tab, a segment."""
    disabled: bool = False
    """True for what the platform reports not enabled; an action on it waits, then is refused."""
    value: str = ""
    """What a slider reads, as the app states it ("80%", "1.2"); empty for anything else."""

    def __str__(self) -> str:
        return f"{self.ref} {self.label} ({self.role})" if self.role else f"{self.ref} {self.label}"


@dataclass(frozen=True)
class DeviceInfo:
    """One attached device or simulator."""

    id: str
    platform: str
    state: str
    model: str = ""
    runtime: str = ""
    emulator: bool = False

    def __str__(self) -> str:
        bits = [self.id, self.state, self.platform]
        if self.model:
            bits.append(self.model)
        return "  ".join(bits)


def connect(
    device: str | None = None,
    driver: str | None = None,
    binary: str | None = None,
    call_timeout: float | None = None,
    session: str | None = None,
) -> "Device":
    """Start a mobium session.

    device: serial or UDID, when more than one is running.
    driver: "uiautomator2" (default), "uiautomator", or "wda".
    call_timeout: the longest, in seconds, any one call may take before the
        connection is given up, the handshake included. None, the default,
        waits as long as it takes: the first session on an iPhone builds
        WebDriverAgent, which takes minutes. A call that runs out ends the
        connection -- a late answer would be read as the next call's -- and
        every call after raises, saying so; connect again. Set it well above
        the longest wait_for timeout you use.
    session: a daemon of this connection's own, by name, as MOBIUM_SESSION
        sets one. One daemon serves one call at a time across every device,
        so parallel runs on different devices should each name one: sharing,
        an emulator's calls waited behind a simulator's, 22.3s against 0.3s.
        Keep it short; it is part of a socket path.
    """
    args: list[str] = []
    if device:
        args += ["--device", device]
    if driver:
        args += ["--driver", driver]
    return Device(Connection(find_binary(binary), args, call_timeout, session))


@dataclass(frozen=True)
class Session:
    """What start() opened: the device and how it is driven."""

    device: str
    platform: str
    driver: str
    reused: bool = False
    """A session was already open on the device, and was kept."""
    app: str = ""
    """The app start() launched, if one was asked for."""


def start(
    platform: str | None = None,
    device: str | None = None,
    app: str | None = None,
    driver: str | None = None,
    binary: str | None = None,
    call_timeout: float | None = None,
    session: str | None = None,
) -> "Device":
    """Connect and open the session on the device.

    platform: "android" or "ios"; "ios" picks wda, so the driver
        need not be named.
    device: serial or UDID, when more than one is running.
    app: a package name (Android) or bundle id (iOS) to launch once the
        session is up; start returns when it is in front.
    driver, binary, call_timeout, session: as for connect().

    Nothing requires it -- every call opens a session on first use -- but it
    puts the slow first start (installing UiAutomator2, building
    WebDriverAgent on an iPhone) where it was asked for. End it with quit(),
    or use it in a with block, which quits on the way out:

        with start(platform="android", app="com.android.settings") as device:
            device.map()
    """
    d = connect(device=device, driver=driver, binary=binary, call_timeout=call_timeout, session=session)
    args: dict[str, Any] = {"action": "start"}
    if platform:
        args["platform"] = platform
    if app:
        args["app"] = app
    try:
        got = d._data("app_session", args) or {}
    except BaseException:
        d.close()
        raise
    d.session = Session(
        device=got.get("device", ""), platform=got.get("platform", ""),
        driver=got.get("driver", ""), reused=bool(got.get("reused")), app=got.get("app", ""),
    )
    return d


class Device:
    """A connected device or simulator."""

    def __init__(self, conn: Connection):
        self._conn = conn
        self.session: Session | None = None
        """What start() opened, or None for a Device from connect()."""
        self._quit = False

    # -- lifecycle ---------------------------------------------------------

    def close(self) -> None:
        """Close the connection. The device's session lives in the daemon and
        stays open, for the next connect() or the CLI; quit() ends it."""
        self._conn.close()

    def quit(self) -> None:
        """End the session on the device and close the connection.

        The teardown is the daemon's own: accessibility settings put back, a
        recording or route stopped, WebViews detached, the device-side server
        stopped, and the app start() launched, if any, stopped too. Quitting a
        session that is not open succeeds, and a second quit -- say, a with
        block ending after an explicit one -- does nothing. A script that exits
        without quit() or close() has the sessions it started ended for it:
        mobium sees the client go.
        """
        if self._quit:
            return
        self._quit = True
        args: dict[str, Any] = {"action": "end"}
        if self.session is not None:
            args["device"] = self.session.device
        try:
            self._call("app_session", args)
        finally:
            self.close()

    def sessions(self) -> list[dict]:
        """The sessions open on the daemon, each with device, platform and driver."""
        return list((self._data("app_session", {"action": "status"}) or {}).get("sessions", []))

    def __enter__(self) -> "Device":
        return self

    def __exit__(self, *_: Any) -> None:
        # A device from start() quits, as its session was opened for this
        # block; one from connect() only closes, leaving the session to
        # whoever opened it.
        if self.session is not None:
            self.quit()
        else:
            self.close()

    # -- reading -----------------------------------------------------------

    def boot(self, name: str, window: bool = False) -> dict:
        """Start an Android emulator by its AVD's name, or an iOS simulator by
        its name or UDID, and return once it has booted: ``device`` (its serial
        or UDID), ``name``, ``platform``, and ``already`` when it was running.
        An emulator cold-boots headless unless ``window``."""
        args: dict[str, Any] = {"name": name}
        if window:
            args["window"] = True
        return self._data("app_boot", args) or {}

    def shutdown(self, name: str) -> dict:
        """Shut down an emulator or a simulator — by serial, AVD name, UDID or
        simulator name — after ending the daemon's session on it, and return
        once it is gone. A real phone is refused."""
        return self._data("app_shutdown", {"name": name}) or {}

    def devices(self) -> list[DeviceInfo]:
        """Every attached device and simulator."""
        data = self._data("app_devices") or {}
        return [
            DeviceInfo(
                id=d.get("id", ""),
                platform=d.get("platform", ""),
                state=d.get("state", ""),
                model=d.get("model", ""),
                runtime=d.get("runtime", ""),
                emulator=bool(d.get("emulator")),
            )
            for d in data.get("devices", [])
        ]

    def map(self) -> list[Element]:
        """Actionable elements on the current screen.

        Refs are only valid for this screen; call map() again after anything
        that changes it.
        """
        return _elements(self._data("app_map"))

    def map_diff(self) -> dict[str, Any]:
        """What the screen did since the last map of this device: the elements
        that appeared, went away, or changed their label, checked or selected state,
        value or place. Map, act, then map_diff() says what the action just did.

        Returns a dict with ``added`` and ``removed`` as lists of the same
        Elements map() returns, ``changed`` as a list of dicts with ``before``
        and ``after`` Elements and ``what`` changed (``label``, ``checked``,
        ``selected``, ``disabled``, ``value``, ``moved`` or ``resized``), ``since`` as the time of the map compared with, and
        ``first`` true when there was no earlier map to compare with — then the
        whole screen is in ``added``. Refs are the new map's, so an added or
        changed element can be acted on at once; a removed element's ref is
        stale.
        """
        diff = (self._data("app_map", {"diff": True}) or {}).get("diff") or {}
        return {
            "first": bool(diff.get("first")),
            "since": diff.get("since", ""),
            "added": [_element(e) for e in diff.get("added") or []],
            "removed": [_element(e) for e in diff.get("removed") or []],
            "changed": [
                {"before": _element(c.get("before") or {}), "after": _element(c.get("after") or {}),
                 "what": list(c.get("what") or [])}
                for c in diff.get("changed") or []
            ],
        }

    def text(self, target: str | None = None) -> str:
        """All readable text, or the text of one element."""
        return self._call("app_text", {"target": target} if target else None)

    def add_dialog_rule(self, when: str, press: str) -> None:
        """Declare how to answer a dialog, so an action that meets it carries on:
        when a dialog whose text contains ``when`` is in the way, press the
        button captioned ``press``. It names a button, not accept or dismiss,
        because which button those press differs by platform and by dialog.
        Captions match ignoring case.
        """
        self._call("app_dialogs", {"when": when, "press": press})

    def dialog_rules(self) -> list:
        """The declared rules, each with how often it has answered."""
        return (self._data("app_dialogs") or {}).get("rules", [])

    def clear_dialog_rules(self) -> None:
        """Remove every rule for this device."""
        self._call("app_dialogs", {"clear": True})

    def source(self) -> dict:
        """The raw hierarchy — the page source — for when map leaves out the thing you need to see. map is what to act on.

        ``source`` is the platform's XML, or in a WebView the page's markup;
        ``format`` is "xml" or "html"; ``units`` is "px" on Android and "pt"
        on iOS, where map, taps and screenshots are in pixels, ``scale`` times
        as many. ``redacted`` counts the password fields hidden.
        """
        return self._data("app_source") or {}

    def find(self, locator: str) -> list[Element]:
        """Elements matching a locator, without acting on them."""
        return _elements(self._data("app_find", {"locator": locator}))

    def wait_for(
        self,
        target: str,
        condition: str = "visible",
        text: str | None = None,
        timeout_ms: int = 10000,
        negate: bool = False,
        exact: bool = False,
        count: int | None = None,
    ) -> Element | None:
        """Block until the screen agrees, instead of sleeping.

        condition is "visible" (default), "hidden", "text" — which needs the
        text to wait for — "value", a field's whole content ("" for empty;
        a password field is refused), "enabled", "disabled", "checked",
        "unchecked", "focused" or "count" (with count). negate waits for the
        opposite; exact makes "text" match the whole text. Raises MobiumError
        if it never happens, saying what was on screen instead.

        On success the screen is remapped, so the element returned already has
        a ref that can be tapped without calling map() first. Nothing is
        returned when waiting for something to go away.
        """
        args: dict[str, Any] = {
            "target": target,
            "condition": condition,
            "timeout_ms": timeout_ms,
        }
        if text is not None:
            args["text"] = text
        if negate:
            args["not"] = True
        if exact:
            args["exact"] = True
        if count is not None:
            args["count"] = count
        data = self._data("app_wait_for", args) or {}
        found = data.get("element")
        return _element(found) if found else None

    def scroll_to(self, target: str, direction: str = "down") -> Element | None:
        """Scroll until an element is on screen, and return it with a ref.

        map() only sees what is currently visible, and tap(), type() and
        long_press() already scroll to a target that is not — so call this to
        look without acting, or to scroll back up. direction is "down", "up",
        "left" or "right" -- nothing on screen says which way a container
        scrolls, so a horizontal pager needs "left" or "right". Swiping the
        wrong way is not a no-op, so if a swipe navigates instead of
        scrolling, it stops after one. Raises MobiumError if it is not found.
        """
        data = self._data("app_scroll_to", {"target": target, "direction": direction}) or {}
        found = data.get("element")
        return _element(found) if found else None

    def screenshot(self, path: str | None = None) -> bytes:
        """Capture the screen as PNG, optionally also writing it to path."""
        if path:
            self._call("app_screenshot", {"path": path})
            with open(path, "rb") as handle:
                return handle.read()

        result = self._conn.call_tool("app_screenshot", {})
        for block in result.get("content", []):
            if block.get("type") == "image":
                return base64.b64decode(block["data"])
        raise MobiumError("mobium returned no image")

    # -- acting ------------------------------------------------------------

    def tap(
        self,
        target: str | None = None,
        x: int | None = None,
        y: int | None = None,
        fingers: int | None = None,
    ) -> None:
        """Tap a ref, a locator, or a point in device pixels.

        fingers taps with several at once, side by side: 2 for a two-finger
        tap, 3 for three (up to 5). On iOS three fingers can reach the system
        instead of the app — three-finger gestures are undo, redo, copy and
        paste there.
        """
        extra = {} if fingers is None else {"fingers": fingers}
        if target is not None:
            self._call("app_tap", {"target": target, **extra})
        elif x is not None and y is not None:
            self._call("app_tap", {"x": x, "y": y, **extra})
        else:
            raise MobiumError("tap needs a target, or both x and y")

    def double_tap(
        self, target: str | None = None, x: int | None = None, y: int | None = None
    ) -> None:
        """Tap twice, as one gesture rather than as two taps.

        The same tool as tap with one argument set, so the target is resolved
        the same way and refused the same way when the screen has moved. The
        uiautomator dump driver refuses it: the window is 40-300ms and
        nothing there controls the interval between two adb calls.
        """
        if target is not None:
            self._call("app_tap", {"target": target, "double": True})
        elif x is not None and y is not None:
            self._call("app_tap", {"x": x, "y": y, "double": True})
        else:
            raise MobiumError("double_tap needs a target, or both x and y")

    def drag(self, source: str, target: str, hold_ms: int | None = None) -> dict:
        """Pick one element up, carry it onto another, and drop it.

        Not swipe with two targets: a swipe has no hold at either end, so
        pointed at a reorderable row it scrolls the list instead of moving the
        row. Both ends are resolved from one snapshot before anything is
        touched.

        hold_ms is how long the finger rests at each end, defaulting to 700 —
        above Android's 500ms long-press timeout, which is what a
        drag-to-reorder list arms on. Raise it first when a drag picks nothing
        up.

        Reports that the gesture was delivered. Whether the drop was accepted
        is the app's own state, so call map() again to see it.
        """
        args: dict = {"from": source, "to": target}
        if hold_ms is not None:
            args["hold_ms"] = hold_ms
        return self._data("app_drag", args) or {}

    def press_tap(self, hold: str, tap: str, lead_ms: int | None = None) -> dict:
        """Hold one element with a finger while a second finger taps another.

        The first finger lifts only after the second. Both are resolved before
        anything is touched. lead_ms is how long the first rests before the
        second taps, 300 by default — under Android's 500ms long-press timeout,
        so the held element does not open its own menu first.

        Reports that the gesture was delivered; what it meant is the app's own
        state, so call map() again to see it. Android 15 and earlier only: on
        iOS XCTest adds a zero-length touch at the second finger's target when
        the gesture starts, and on Android 16 and later UiAutomator2's down
        times are rejected, so both refuse with UnsupportedError.
        """
        args: dict = {"hold": hold, "tap": tap}
        if lead_ms is not None:
            args["lead_ms"] = lead_ms
        return self._data("app_press_tap", args) or {}

    def press_drag(self, hold: str, source: str, target: str, lead_ms: int | None = None) -> dict:
        """Hold one element with a finger while a second finger drags.

        The second finger lands on source, moves to target and lifts, then the
        first lifts. Not drag(), which is one finger carrying something: here
        one finger anchors and the other moves. Android 15 and earlier only,
        for press_tap's reasons.
        """
        args: dict = {"hold": hold, "from": source, "to": target}
        if lead_ms is not None:
            args["lead_ms"] = lead_ms
        return self._data("app_press_drag", args) or {}

    def type(self, target: str, text: str) -> None:
        """Type into an element, after what it holds. An empty string clears it."""
        self._call("app_type", {"target": target, "text": text})

    def fill(self, target: str, text: str) -> None:
        """Clear an element and type into it, replacing what it held."""
        self._call("app_fill", {"target": target, "text": text})

    def swipe(
        self,
        direction: str | None = None,
        coordinates: tuple[int, int, int, int] | None = None,
        duration_ms: int = 300,
        target: str | None = None,
    ) -> None:
        """Swipe by direction ("up", "down", "left", "right") or exact points.

        With a ``target`` and a direction, swipe across that element, after
        the checks a tap makes — part of the way, which reveals a list row's
        swipe actions without performing the first. Map again and tap the
        action you mean.
        """
        args: dict[str, Any] = {"duration_ms": duration_ms}
        if coordinates:
            args["x1"], args["y1"], args["x2"], args["y2"] = coordinates
        elif direction:
            args["direction"] = direction
            if target:
                args["target"] = target
        else:
            raise MobiumError("swipe needs a direction or four coordinates")
        self._call("app_swipe", args)

    def long_press(self, target: str | None = None, x: int | None = None,
                   y: int | None = None, duration_ms: int = 800) -> None:
        """Press and hold an element or a point."""
        args: dict[str, Any] = {"duration_ms": duration_ms}
        if target is not None:
            args["target"] = target
        elif x is not None and y is not None:
            args["x"], args["y"] = x, y
        else:
            raise MobiumError("long_press needs a target, or both x and y")
        self._call("app_long_press", args)

    # -- app lifecycle -----------------------------------------------------

    def launch(self, app: str, hit_test: bool = False, gray_box: bool = False) -> None:
        """Bring an app to the foreground by package name or bundle id.

        Every ref from the previous screen is discarded — call map(), or just
        act, since actions re-resolve their target anyway.

        ``hit_test``, on an iOS simulator, loads the hit probe into the app as
        it launches: every action on an element in it then asks UIKit where
        the touch goes first, and is refused when it would land elsewhere. A
        real iPhone and Android refuse it.

        ``gray_box``, on iOS, launches the app with Mobium's gray-box library
        turned on: the app says when it is busy, and every action waits for
        it to be idle before finding its target. It needs an app built with
        the library.
        """
        args: dict[str, Any] = {"app": app}
        if hit_test:
            args["hit_test"] = True
        if gray_box:
            args["gray_box"] = True
        self._call("app_launch", args)

    def terminate(self, app: str) -> None:
        """Stop a running app."""
        self._call("app_terminate", {"app": app})

    def install(self, path: str) -> str:
        """Install a local .apk (Android) or .app bundle (iOS).

        Returns the absolute path that was installed.
        """
        data = self._data("app_install", {"path": path}) or {}
        return data.get("path", "")

    def apps(self, system: bool = False) -> list[dict[str, Any]]:
        """Installed apps, each with id, name, version and whether it is a
        system app.

        By default only apps someone installed — a stock Android emulator
        ships about 240 system packages. Name is empty on Android.
        """
        data = self._data("app_list_apps", {"system": system}) or {}
        return data.get("apps", [])

    def uninstall(self, app: str) -> None:
        """Remove an app, verified by listing afterwards.

        `adb uninstall` reports success when it has only removed the updates
        to a system app, so this raises rather than reporting a lie.
        """
        self._call("app_uninstall", {"app": app})

    def clear_data(self, app: str, bundle: str | None = None) -> dict:
        """Delete an app's data and leave it installed — a fresh install's
        state, without reinstalling.

        Returns what was read back empty (``emptied``), what was kept
        (``kept``) and, on Android, the runtime permissions still granted
        (``still_granted``): `pm clear` revokes what the user granted. An iOS
        simulator keeps its privacy grants and keychain.

        A real iPhone cannot clear in place: pass the app's own ``bundle``, a
        .app or .ipa, and it is uninstalled and installed again. Its data
        container is read back empty, and its permissions, which nothing
        outside the app can read, are listed in ``not_read_back``. Without a
        bundle a phone refuses, and with one any other device does.
        """
        args: dict[str, Any] = {"app": app}
        if bundle:
            args["path"] = bundle
        return self._data("app_clear_data", args) or {}

    def upload(self, path: str, name: str | None = None, app: str | None = None) -> dict:
        """Put a local file where the device keeps downloads, so an app's
        file picker finds it.

        On Android that is the shared Download folder, one for every app, and
        the file is indexed in MediaStore and read back there, because the
        picker reads MediaStore rather than the folder. On an iOS simulator it
        is an app's own Documents, which the Files app shows under On My
        iPhone: the app named, or the one in front. On a real iPhone it is
        the same folder, and the upload is confirmed by reading its bytes
        back.

        name is what to call it on the device — a name, not a path — and
        defaults to the file's own. Returns the transfer: ``name``,
        ``where``, ``bytes`` and ``checked``, which says how it was confirmed.
        """
        args: dict[str, Any] = {"path": path}
        if name is not None:
            args["name"] = name
        if app is not None:
            args["app"] = app
        return self._data("app_upload", args) or {}

    def download(self, name: str, path: str | None = None, app: str | None = None) -> bytes | dict:
        """Bring back a file from where the device keeps downloads, to check
        what an app saved.

        The folder is Android's shared Download folder, or on an iOS simulator
        an app's own Documents — the app named, or the one in front — and on
        a real iPhone the same. The copy's size is read back against the
        device's.

        With path, saves it there and returns the transfer (``name``,
        ``where``, ``bytes``, ``checked``, ``path``); without one, returns the
        file's bytes.
        """
        args: dict[str, Any] = {"name": name}
        if path is not None:
            args["path"] = path
        if app is not None:
            args["app"] = app
        data = self._data("app_download", args) or {}
        if path is not None:
            return data
        # An empty file has no data on the wire (it is omitted when empty),
        # so only a missing one for a file with bytes is a failure.
        if "data" not in data and data.get("bytes"):
            raise MobiumError("mobium returned no file")
        return base64.b64decode(data.get("data", ""))

    def push_path(self, local: str, device_path: str, app: str | None = None) -> dict:
        """Send a local file or folder to a path on the device, and read every
        file's size back there.

        On Android device_path is absolute (``/sdcard/...``,
        ``/data/local/tmp/...``), or with app a path in that app's private
        data, which needs a debuggable build. On iOS it is a path in an app's
        data container — app, or the one in front.
        """
        args: dict[str, Any] = {"path": local, "device_path": device_path}
        if app is not None:
            args["app"] = app
        return self._data("app_upload", args) or {}

    def pull_path(self, device_path: str, path: str | None = None, app: str | None = None) -> bytes | dict:
        """Bring back the file or folder at a path on the device — the paths
        push_path takes.

        With path, saves it there (a folder needs a path that does not exist
        yet) and returns the transfer, with ``files`` and ``folder``; without
        one, returns a file's bytes.
        """
        args: dict[str, Any] = {"device_path": device_path}
        if path is not None:
            args["path"] = path
        if app is not None:
            args["app"] = app
        data = self._data("app_download", args) or {}
        if path is not None:
            return data
        if "data" not in data and data.get("bytes"):
            raise MobiumError("mobium returned no file")
        return base64.b64decode(data.get("data", ""))

    def downloads(self, app: str | None = None) -> list[dict]:
        """What the downloads folder holds, each file with name, bytes and
        modified.

        Android's shared Download folder, whatever app put it there; on an
        iOS simulator an app's own Documents — the app named, or the one in
        front — and on a real iPhone the same.
        """
        data = self._data("app_download", {} if app is None else {"app": app}) or {}
        return data.get("files", [])

    def batch(self, steps: list[Any]) -> list[dict]:
        """Run several tools in order, on this device, in one call.

        Each step is ``(tool, arguments)`` or ``{"name": tool, "arguments":
        {...}}``, with the arguments that tool takes called on its own::

            device.batch([
                ("app_tap", {"target": "text=Sign in"}),
                ("app_fill", {"target": "testid=user", "text": "mobium"}),
                ("app_wait_for", {"target": "text=Welcome"}),
            ])

        Every step is checked before the first runs, and the batch stops at
        the first failure, raising that step's own exception; its
        ``details`` hold ``step`` and what ``completed`` before it. Returns
        each step's ``name``, ``text`` and ``data``, in order.
        """
        wire = []
        for s in steps:
            if isinstance(s, tuple):
                name, args = s
                wire.append({"name": name, "arguments": args or {}})
            else:
                wire.append(s)
        data = self._data("app_batch", {"steps": wire}) or {}
        return list(data.get("steps") or [])

    def network(self) -> dict:
        """The network: ``airplane``, ``online``, and the shaping —
        ``latency_ms``, ``download_kbps``, ``upload_kbps``, zero for none.
        Android only."""
        return self._data("app_network") or {}

    def set_offline(self, offline: bool = True) -> dict:
        """Turn airplane mode on (or off) and wait for the network to go (or
        come back) — on an emulator or a real Android phone."""
        return self._data("app_network", {"offline": offline}) or {}

    def shape_network(self, latency_ms: int = 0, download_kbps: int = 0, upload_kbps: int = 0) -> dict:
        """Add latency to each round trip and limit download and upload, in
        kbit/s, replacing any shaping set before; zero is none. Needs root,
        so an emulator."""
        return self._data("app_network", {"latency_ms": latency_ms, "download_kbps": download_kbps,
                                          "upload_kbps": upload_kbps}) or {}

    def reset_network(self) -> dict:
        """Remove the shaping and turn airplane mode off."""
        return self._data("app_network", {"reset": True}) or {}

    def battery(self) -> dict:
        """The battery: ``level`` in percent, ``state`` (charging,
        discharging, not_charging, full or unknown) and on Android
        ``plugged``. An iOS simulator has none: ``present`` is False.
        """
        return self._data("app_battery") or {}

    def device_time(self) -> dict:
        """What time the device thinks it is: ``time`` (RFC 3339, in its
        own offset), ``zone``, and ``clock`` — "device", or "mac" on an iOS
        simulator, which has no clock of its own.
        """
        return self._data("app_time") or {}

    def shake(self) -> None:
        """Shake an emulator or simulator — what shake-to-undo and
        shake-to-report listen for. Whether the app reacts is up to its own
        detector, so check the screen after. A real phone refuses.
        """
        self._call("app_shake")

    def biometric(self, action: str = "status") -> dict:
        """Biometrics on an emulator or simulator. ``action`` is ``status``,
        ``enroll`` or ``unenroll``, or ``match`` / ``nomatch`` to present a
        matching or a stranger's face or finger to the prompt that is up.
        Returns ``kind`` (face or fingerprint), ``enrolled``, and for a match
        or non-match the ``outcome`` read back: accepted, not recognized,
        failed (the prompt gave up) or locked out. With no prompt up it
        raises rather than sending to nothing. A real phone refuses.
        """
        return self._data("app_biometric", {"action": action}) or {}

    def hit_test(self, target: str) -> dict:
        """Ask UIKit's own hit test, below accessibility, whether a tap on
        ``target`` would reach it. Raises when the touch would go elsewhere,
        naming what would take it and whether accessibility can see it.
        iOS only, and opt-in: it attaches lldb to the app, about two seconds
        on a simulator and nine on an iPhone, where the app must be a
        development build.
        """
        return self._data("app_hit_test", {"target": target}) or {}

    def audit(self) -> dict:
        """Run the platform's own accessibility audit on the screen in front:
        on iOS, Apple's, on a simulator or an iPhone (iOS 17 and later).
        ``findings`` lists each with its ``type``, ``summary``, ``detail``,
        ``element``, a ``locator`` when it has one, and ``bounds`` in device
        pixels when the audit names an element. Android raises: its audits
        run inside the app.
        """
        return self._data("app_audit", {}) or {}

    def app_state(self, app: str) -> dict:
        """One app's state: ``not_installed``, ``not_running``, ``background``
        or ``foreground``, for any app.

        An app under its own permission prompt is still in front, and
        ``covered_by`` names the prompt's process. On iOS a background app
        also has ``suspended``.
        """
        return self._data("app_state", {"app": app}) or {}

    def background(self, seconds: float, app: str | None = None) -> dict:
        """Send the app in front away for ``seconds`` and bring it back,
        resumed rather than relaunched, confirmed in front again — how a
        resume path is tested. At most 180 seconds.
        """
        args: dict[str, Any] = {"seconds": seconds}
        if app:
            args["app"] = app
        return self._data("app_background", args) or {}

    def open_url(self, url: str) -> None:
        """Open a URL or deep link — the quickest way to a specific screen."""
        self._call("app_open_url", {"url": url})

    def current(self) -> str:
        """The package name or bundle id of the foreground app.

        Costs no extra device call: it reads the hierarchy a snapshot fetches
        anyway. Use it to confirm a tap went where you expected.
        """
        data = self._data("app_current") or {}
        return data.get("app", "")

    # -- permissions -------------------------------------------------------

    def grant(self, app: str, *permissions: str) -> None:
        """Grant permissions up front, so no dialog blocks the flow.

        Names are cross-platform ("camera", "location", "contacts", ...);
        "all" grants everything the app declares, and a platform name like
        "android.permission.NFC" also works. On Android the result is verified
        by reading the state back, because `pm grant` reports success for
        permissions the app never declared.
        """
        self._call("app_grant", {"app": app, "permissions": list(permissions)})

    def revoke(self, app: str, *permissions: str) -> None:
        """Deny permissions, to test how the app behaves without them."""
        self._call("app_revoke", {"app": app, "permissions": list(permissions)})

    def reset_permissions(self, app: str | None = None) -> None:
        """Put permissions back to their defaults, so the app prompts again.

        Naming an app resets only that app's, on both platforms; on Android
        that stops the app if it had a permission granted. Omit app to reset
        every app on the device.
        """
        self._call("app_reset_permissions", {"app": app} if app else None)

    # -- device state ------------------------------------------------------

    def appearance(self, mode: str | None = None) -> str:
        """Read the light/dark setting, or change it and return the new one.

        mode is "light", "dark", or "auto" (Android only; iOS raises). Dark
        mode is a different rendering of every screen, so a flow is worth
        running in both. Changing it discards the refs from the last map.
        """
        data = self._data("app_appearance", {"appearance": mode} if mode else None) or {}
        return data.get("appearance", "")

    def accessibility(self, setting: str | None = None, value: str | None = None):
        """Read the accessibility settings, or change one for the session.

        With no argument, returns a dict of every setting the device has —
        reduce_motion, bold_text, increase_contrast and the rest. With a
        setting, returns its value; with a setting and a value, changes it,
        confirmed by reading it back, and returns the new value. A switch takes
        "on" or "off"; text_size a category such as "accessibility-large"
        (iOS); text_scale a number such as "1.3" (Android). Every change is
        put back when the session ends. A real iPhone raises. Changing one
        discards the refs from the last map.
        """
        args = {}
        if setting:
            args["setting"] = setting
        if value is not None:
            args["value"] = value
        data = self._data("app_accessibility", args or None) or {}
        if not setting:
            return data.get("settings", {})
        return data.get("value", "")

    def orientation(self) -> tuple[str, bool]:
        """Which way the screen is turned, and whether that is pinned.

        A screen that merely happens to be portrait can rotate under you, so
        the two are separate answers.
        """
        data = self._data("app_orientation") or {}
        return data.get("orientation", ""), bool(data.get("locked"))

    def set_orientation(self, mode: str) -> str:
        """Turn the screen and pin it. "auto" hands it back to the sensor.

        mode is "portrait", "landscape", "portrait-reverse",
        "landscape-reverse" or "auto" — not "left"/"right", which the two
        platforms name differently. A rotation re-lays out every screen, so
        the refs from the last map are discarded. An activity that locks its
        own orientation cannot be turned from outside and raises.
        """
        data = self._data("app_orientation", {"orientation": mode}) or {}
        return data.get("orientation", "")

    def screen(self, profile: str = "", inspect: bool = False) -> dict:
        """Read the screen, or make an Android device pretend to be another.

        A flow that works on the screen you happen to have is a flow tested
        once. Pass a profile name to apply it, or "reset" to put the device
        back — an override outlives this session, so reset when you are done.
        Applying one discards the refs from the last map, because nothing is
        where it was.

        On iOS the screen is fixed when the simulator is created, so this
        reads only and names the simulator to boot instead.

        With inspect=True the answer also carries "findings": elements past
        the edge, touch targets below the platform minimum, text the platform
        truncated, and tappable elements with nothing to announce. Treat a
        touch-target finding as worth a look rather than a defect — Android
        can enlarge a tap area without changing an element's bounds.
        """
        args: dict = {}
        if profile:
            args["profile"] = profile
        if inspect:
            args["inspect"] = True
        return self._data("app_screen", args) or {}

    def app_locale(self, app: str) -> list[str]:
        """Language tags pinned for an app; empty means it follows the device."""
        data = self._data("app_locale", {"app": app}) or {}
        return list(data.get("locales") or [])

    def set_app_locale(self, app: str, *tags: str) -> list[str]:
        """Run one app in a chosen language; no tags follows the device again.

        Android 13 and later. What this confirms is that the device stored the
        tag, not that the app has a translation for it — Android reports no
        difference — so check the screen. Relaunch the app to re-render it.
        """
        data = self._data("app_locale", {"app": app, "locale": ",".join(tags)}) or {}
        return list(data.get("locales") or [])

    def rotate(self, degrees: float = 90, target: str | None = None) -> dict:
        """Turn two fingers about an element or the screen, positive clockwise.

        Like :meth:`zoom` it reports that the gesture was delivered and nothing
        more, and this one is harder still to confirm: nothing in either
        hierarchy reports a rotation, and there is no WebView property to ask.
        """
        args: dict = {"degrees": degrees}
        if target is not None:
            args["target"] = target
        return self._data("app_rotate", args) or {}

    def zoom(self, direction: str = "in", target: str | None = None) -> dict:
        """Pinch apart or together, about an element or the screen.

        Reports that the gesture was delivered and nothing more: neither
        platform exposes a zoom level in the accessibility hierarchy, so
        confirming a zoom means asking whatever was zoomed — a WebView can
        answer with ``visualViewport.scale`` through :meth:`eval`.
        """
        args: dict = {"direction": direction}
        if target is not None:
            args["target"] = target
        return self._data("app_zoom", args) or {}

    def check(self, target: str, checked: bool = True) -> dict:
        """Put a checkbox or switch into a state, rather than toggling it.

        Idempotent: asking for a state it is already in does nothing, which
        is what makes it safe to call without reading first. Anything with no
        checked state is refused rather than tapped, and a radio cannot be
        unchecked — a group is cleared by choosing a different member.
        """
        return self._data("app_check", {"target": target, "checked": checked}) or {}

    def alert(self) -> str:
        """What a system dialog says, or "" when none is up.

        A permission prompt is another process's window, not the app's, and
        reading it needs no knowledge of what the buttons say.
        """
        data = self._data("app_alert", {}) or {}
        return str(data.get("text") or "")

    def answer_alert(self, accept: bool = True, text: str | None = None) -> dict:
        """Accept or dismiss a system dialog.

        These answer a dialog; they do not choose an outcome. On a permission
        prompt they do not mean grant and deny, and on iOS they are the other
        way round — accept leaves it denied and dismiss leaves it granted,
        because W3C accept presses the affirmative button and Apple puts
        "Don't Allow" last. Tap the button by ref for a particular answer.

        text is typed into a prompt's field first, so one call fills and
        answers it. A plain alert has no field, and the platform refuses it.
        """
        args = {"action": "accept" if accept else "dismiss"}
        if text is not None:
            args["text"] = text
        return self._data("app_alert", args) or {}

    def clipboard(self) -> str:
        """What the device clipboard holds.

        iOS only. On Android 10 and later only an app with focus may read the
        clipboard and the UiAutomator2 server has no activity, so it would
        answer "empty" for a clipboard that is full — this raises there
        instead.
        """
        data = self._data("app_clipboard", {}) or {}
        return str(data.get("text") or "")

    def set_clipboard(self, text: str) -> dict:
        """Write the device clipboard.

        On iOS the write is confirmed by reading it back; on Android it is
        reported as sent, nothing there being able to read it.
        """
        return self._data("app_clipboard", {"text": text}) or {}

    def location(self) -> dict:
        """Where the device believes it is.

        Returns latitude, longitude, ``mock`` (this fix was injected),
        ``mocking`` (a test provider is installed now) and ``known``. The two
        mock flags differ after :meth:`clear_location`, because Android keeps
        the last known position after the provider supplying it is gone.

        Android only. ``simctl location`` has no ``get``, so on iOS this
        raises rather than returning a position it never read.
        """
        return self._data("app_location", {}) or {}

    def set_location(self, latitude: float, longitude: float) -> dict:
        """Place the device at a coordinate.

        On Android this goes through a test provider, is read back, and works
        on real hardware. On iOS it goes through simctl and cannot be
        confirmed — success means the request was accepted, not that an app
        will read it.
        """
        return self._data(
            "app_location", {"latitude": latitude, "longitude": longitude}
        ) or {}

    def clear_location(self) -> dict:
        """Remove the injected position.

        Does not clear the device's last known location, which Android caches.
        """
        return self._data("app_location", {"clear": True}) or {}

    def follow_route(self, waypoints, speed: float | None = None) -> dict:
        """Move along two or more (latitude, longitude) pairs over time.

        On iOS the simulator interpolates the route itself; on Android the
        daemon steps a test provider once a second, the platform having no
        route command. Either way this returns as the route starts.
        """
        args: dict = {"waypoints": [list(w) for w in waypoints]}
        if speed is not None:
            args["speed"] = speed
        return self._data("app_location", args) or {}

    def follow_gpx(self, path: str, speed: float | None = None) -> dict:
        """Follow a GPX file, read on the machine running the daemon."""
        args: dict = {"gpx": path}
        if speed is not None:
            args["speed"] = speed
        return self._data("app_location", args) or {}

    def press(self, button: str) -> None:
        """Press a hardware button.

        "back", "home", "recents", "volume-up" or "volume-down"; a remote's
        D-pad, "dpad-up", "dpad-down", "dpad-left", "dpad-right" and
        "select"; or a media key, "play-pause", "stop", "next", "previous",
        "rewind" or "fast-forward". A D-pad press reports where focus went.
        On Android
        back is primary navigation. iOS has no back button by design and
        raises with what to do instead, rather than sending an edge swipe —
        a different event an app can tell apart. Any press can move the
        screen, so the refs from the last map are discarded.
        """
        self._data("app_press", {"button": button})

    def back(self, gesture: bool = False) -> dict:
        """Go back, and say where it went.

        Returns ``foreground`` (the app in front afterwards), ``left`` (the
        app back closed, absent when it stayed in front), ``title`` (an iOS
        navigation bar's, afterwards) and ``confirmed``. With
        ``gesture=True`` it is a swipe in from the left edge rather than the
        key: the only back iOS has, and refused on an Android device that
        navigates with buttons.
        """
        args: dict = {"button": "back"}
        if gesture:
            args["gesture"] = True
        return self._data("app_press", args) or {}

    def screen_locked(self) -> bool:
        """Whether the screen is locked."""
        data = self._data("app_lock") or {}
        return bool(data.get("locked"))

    def set_screen_locked(self, locked: bool) -> bool:
        """Lock or unlock the screen, confirmed against the device.

        A state rather than a power-button press: power is a toggle, so asking
        twice leaves the device where it started. A device with a PIN, pattern
        or password cannot be unlocked from outside and raises.
        """
        data = self._data("app_lock", {"state": "lock" if locked else "unlock"}) or {}
        return bool(data.get("locked"))

    def incoming_call(self, action: str = "ring", number: str | None = None) -> None:
        """Simulate an incoming call: "ring", "accept" or "hang".

        Emulator only — a real phone cannot be made to ring from outside.
        """
        args: dict = {"action": action}
        if number:
            args["number"] = number
        self._data("app_call", args)

    def sms(self, text: str, sender: str | None = None) -> None:
        """Deliver a simulated text message. Emulator only."""
        args: dict = {"text": text}
        if sender:
            args["from"] = sender
        self._data("app_sms", args)

    def doctor(self) -> dict:
        """Check the environment and return the report.

        Needs no device: its whole job is to be runnable when nothing works
        yet, so it is the first thing to call when something fails for a
        reason that makes no sense.
        """
        return self._data("app_doctor") or {}

    def logs(self, level: str | None = None) -> list[dict]:
        """Console output from the current WebView since the last call.

        Includes uncaught errors and unhandled promise rejections. Each read
        drains what it returns, so it reports what happened since the last
        call — which is what makes "nothing was logged during this step"
        assertable. Capture starts when the context is entered, so a page's
        initial load is already over by then.
        """
        # Named, because with no source the tool follows the context and
        # would read the device log on the native shell.
        args = {"source": "webview"}
        if level:
            args["level"] = level
        data = self._data("app_logs", args) or {}
        return list(data.get("entries") or [])

    def device_logs(
        self, app: str | None = None, level: str | None = None, lines: int | None = None
    ) -> dict:
        """The device's own log since the last read: logcat on Android, the
        unified log on an iOS simulator, what a real iPhone's session has captured.

        Returns a dict with ``entries`` (each has time, level, tag, pid and
        message) and ``skipped`` — lines newer than the last read that the
        limit dropped, which will not come back. The first read returns the
        most recent lines. A read narrowed by app or level leaves the rest
        unread.
        """
        args: dict = {"source": "device"}
        if app:
            args["app"] = app
        if level:
            args["level"] = level
        if lines:
            args["lines"] = lines
        data = self._data("app_logs", args) or {}
        return {"entries": list(data.get("entries") or []), "skipped": data.get("skipped", 0)}

    def record(self, action: str | None = None, path: str | None = None) -> dict:
        """Record the screen: ``action`` "start", or "stop" with ``path`` to save
        the video; neither asks whether one is running.

        Stop returns the saved file's ``frames`` and ``duration`` read from its
        own header. A still screen is one frame on Android, which is not a
        failure. Raises when a recording could not be finished.
        """
        args: dict = {}
        if action:
            args["action"] = action
        if path:
            args["path"] = path
        return self._data("app_record", args or None) or {}

    def trace_start(
        self, name: str | None = None, screenshots: bool | None = None, maps: bool | None = None
    ) -> dict:
        """Start recording the session as a trace. Until trace_stop, every
        call on the device is a step, with the screen after it and the map's
        elements drawn over it.

        ``name`` is the trace's title. ``screenshots`` and ``maps`` default
        to true. Text typed into a field is not recorded, only its length. On
        a real phone the screenshots are its owner's screen;
        ``screenshots=False`` keeps none. One trace per device; ending the
        session discards it.
        """
        args: dict = {"action": "start"}
        if name:
            args["name"] = name
        if screenshots is not None:
            args["screenshots"] = screenshots
        if maps is not None:
            args["maps"] = maps
        return self._data("app_trace", args) or {}

    def trace_stop(self, path: str) -> dict:
        """Stop the trace and save it to ``path``: a zip in Vibium's
        record format, which player.vibium.dev opens.
        ``mobium pipe`` makes a relative path absolute. Returns ``calls``,
        ``path`` and ``bytes``.
        """
        return self._data("app_trace", {"action": "stop", "path": path}) or {}

    def trace(self) -> dict:
        """Whether a trace is running: ``tracing``, and ``calls`` and
        ``elapsed`` when one is."""
        return self._data("app_trace") or {}

    def keyboard(self, text: str | None = None, key: str | None = None, hide: bool = False) -> dict:
        """The soft keyboard: read it, type at the focused field, press a key, or hide it.

        With no arguments, returns whether it is ``shown`` and the ``focused``
        field (id, kind, value — a password's value is never shown). ``text``
        is added to the end of the focused field and confirmed by reading it
        back; ``key`` is "enter", "delete" or "space", pressed after any text;
        ``hide`` hides the keyboard, confirmed, and cannot be combined with the
        others. Raises NoSuchElementError when nothing has focus.
        """
        args: dict = {}
        if text is not None:
            args["text"] = text
        if key:
            args["key"] = key
        if hide:
            args["hide"] = True
        return self._data("app_keyboard", args or None) or {}

    def crashes(self, app: str | None = None, limit: int | None = None) -> list[dict]:
        """The crashes the device recorded, newest first.

        Each has id, time, kind (crash, native_crash or anr), app and summary.
        Not drained: a crash is a record, so asking twice shows it twice.
        """
        args: dict = {}
        if app:
            args["app"] = app
        if limit:
            args["limit"] = limit
        data = self._data("app_crashes", args or None) or {}
        return list(data.get("crashes") or [])

    def crash(self, id: str) -> dict:
        """One crash report in full, by an id from crashes(); the text is in ``text``."""
        data = self._data("app_crashes", {"id": id}) or {}
        found = data.get("crashes") or []
        return found[0] if found else {}

    def eval(self, expression: str) -> str:
        """Run a JavaScript expression in the current WebView.

        Objects come back as JSON.
        """
        data = self._data("app_eval", {"expression": expression}) or {}
        return data.get("value", "")

    def cookies(self) -> list[dict]:
        """The current WebView's cookies: the ones its page's URL is sent,
        HttpOnly ones included. Needs a web context -- ``context()`` first.

        Each is a dict with Vibium's keys: ``name``,
        ``value``, ``domain``, ``path``, ``expires`` (seconds since the epoch,
        absent for a session cookie), ``httpOnly``, ``secure``, ``sameSite``.
        """
        data = self._data("app_cookies", {"action": "get"}) or {}
        return data.get("cookies") or []

    def set_cookies(self, cookies: list[dict]) -> None:
        """Set each cookie on the current page and read the store back, so
        one the browser accepted and stored expired raises NotConfirmedError.
        Each needs ``name`` and ``value``; the rest default to the page's
        host, ``/`` and a session cookie."""
        self._data("app_cookies", {"action": "set", "cookies": list(cookies)})

    def clear_cookies(self, name: str | None = None) -> None:
        """Delete the current page's cookies, or only those called ``name``."""
        args: dict[str, Any] = {"action": "clear"}
        if name:
            args["name"] = name
        self._data("app_cookies", args)

    def storage(self) -> dict:
        """The current page's storage state, in the shape Vibium saves: ``{"cookies": [...], "origins": [{"origin",
        "localStorage", "sessionStorage"}]}``."""
        data = self._data("app_storage", {"action": "get"}) or {}
        return data.get("state") or {"cookies": [], "origins": []}

    def set_storage(self, state: dict) -> None:
        """Restore a saved state: its cookies, and each origin's storage into
        the page only if the page is on that origin."""
        self._data("app_storage", {"action": "restore", "state": state})

    def clear_storage(self) -> None:
        """Empty the current page's cookies, localStorage and sessionStorage."""
        self._data("app_storage", {"action": "clear"})

    def notifications(self) -> list[dict]:
        """What is in the notification shade.

        How a test asserts an app posted what it should: each entry has
        package, title and text.
        """
        data = self._data("app_notifications") or {}
        return list(data.get("notifications") or [])

    def post_notification(self, text: str, title: str | None = None) -> list[dict]:
        """Put a notification in the shade as an interruption.

        Confirmed by reading the shade back — the underlying command prints
        what it thinks it built and says nothing about whether the system
        accepted it.
        """
        args: dict = {"text": text}
        if title:
            args["title"] = title
        data = self._data("app_notifications", args) or {}
        return list(data.get("notifications") or [])

    def shade(self, open: bool) -> list[dict]:
        """Open or close the notification panel.

        A notification cannot be tapped until the shade is open: until then it
        is not on screen and map cannot see it. Discards the refs from the
        last map.
        """
        data = self._data("app_notifications", {"shade": "open" if open else "close"}) or {}
        return list(data.get("notifications") or [])

    def timezone(self, tz: str | None = None) -> str:
        """Read the device timezone, or change it and return the new one.

        Takes an IANA name such as "Asia/Tokyo". Confirmed by reading it
        back — an unknown zone is accepted by the device and ignored. Works
        on real hardware, unlike call and sms.
        """
        data = self._data("app_timezone", {"timezone": tz} if tz else None) or {}
        return data.get("timezone", "")

    # -- contexts ----------------------------------------------------------

    def contexts(self) -> list[str]:
        """Automatable contexts: the native shell plus any WebViews."""
        data = self._data("app_contexts") or {}
        return [c.get("id", "") for c in data.get("contexts", [])]

    def context(self, name: str | None = None) -> str:
        """Switch context, or read the current one."""
        return self._call("app_context", {"context": name} if name else None)

    # -- internals ---------------------------------------------------------

    def _call(self, tool: str, args: dict[str, Any] | None = None) -> str:
        return _text_of(self._conn.call_tool(tool, args))

    def _data(self, tool: str, args: dict[str, Any] | None = None) -> Any:
        """A tool's structured answer.

        Tools return their result as data as well as prose, so nothing here
        parses the human-readable rendering.
        """
        return _data_of(self._conn.call_tool(tool, args))


def _element(e: dict[str, Any]) -> Element:
    b = e.get("bounds") or {}
    loc = e.get("locator") or {}
    return Element(
        ref=e.get("ref", ""),
        label=e.get("label", ""),
        role=e.get("role", ""),
        bounds=Bounds(b.get("x1", 0), b.get("y1", 0), b.get("x2", 0), b.get("y2", 0)),
        locator=f"{loc['kind']}={loc['value']}" if loc.get("kind") else "",
        context=e.get("context", ""),
        checked=e.get("checked"),
        selected=bool(e.get("selected")),
        disabled=bool(e.get("disabled")),
        value=e.get("value", ""),
    )


def _elements(data: Any) -> list[Element]:
    if not data:
        return []
    return [_element(e) for e in data.get("elements", [])]
