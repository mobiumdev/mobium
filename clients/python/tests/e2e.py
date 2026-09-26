"""The Python client end to end, against a booted Android device.

Not part of `make ci` — it needs a device. docs/checks/clients.sh runs it,
with the four other clients' copies of the same flow, so a release can say
each language was driven rather than only compiled:

    MOBIUM_E2E_DEVICE=emulator-5554 python3 clients/python/tests/e2e.py

Reading, acting, waiting, scrolling, lifecycle, permissions, the device log
and crash reports, and one failure that must arrive as its own exception.
Every step checks what the device did, not only that the call returned.
"""

import os
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent))

import mobium  # noqa: E402

SETTINGS = "com.android.settings"


def check(ok: bool, what: str) -> None:
    if not ok:
        print(f"FAIL: {what}", file=sys.stderr)
        sys.exit(1)
    print(f"    ok   {what}")


def main() -> None:
    serial = os.environ.get("MOBIUM_E2E_DEVICE")
    if not serial:
        print("MOBIUM_E2E_DEVICE is not set; skipping")
        return
    binary = os.environ.get("MOBIUM_BIN_PATH")
    with mobium.connect(device=serial, binary=binary) as d:
        d.terminate(SETTINGS)
        d.launch(SETTINGS)
        check(d.current() == SETTINGS, "launch brings Settings forward")

        found = d.wait_for("text=Network & internet")
        check(found is not None and found.ref.startswith("@e"), "wait_for returns a ref")
        d.tap(found.ref)
        # A tap returns when it is delivered, not when the next screen is up,
        # so wait for the destination rather than reading at once — on a
        # just-booted emulator the read beat the transition.
        check(d.wait_for("text=Internet") is not None, "tapping the ref opens its screen")

        d.press("back")
        row = d.scroll_to("text=About")
        check(row is not None, "scroll_to reaches a row below the fold")

        d.grant("com.android.chrome", "camera")
        d.revoke("com.android.chrome", "camera")
        check(True, "grant and revoke, each read back by the tool")

        logs = d.device_logs(lines=5)
        check(isinstance(logs["entries"], list) and len(logs["entries"]) > 0, "device_logs reads logcat")
        check(isinstance(d.crashes(limit=3), list), "crashes answers with a list")

        try:
            d.tap("text=Definitely Not Here")
            check(False, "a missing element raises")
        except mobium.NoSuchElementError:
            check(True, "a missing element raises NoSuchElementError")

        d.terminate(SETTINGS)
        check(d.current() != SETTINGS, "terminate takes Settings away")
    print("python: passed")


if __name__ == "__main__":
    main()
