"""Mobium quick start: start a session, drive Settings, quit.

    MOBIUM_PLATFORM=android python3 quickstart.py    # or ios
"""
import os

from mobium import start

# Settings is on every emulator, simulator and phone, with nothing to install.
PLATFORMS = {
    "android": {"app": "com.android.settings", "row": "Network & internet", "next": "text=Internet"},
    "ios": {"app": "com.apple.Preferences", "row": "General", "next": "label=About,role=button"},
}
platform = os.environ.get("MOBIUM_PLATFORM", "android")
p = PLATFORMS[platform]

# 1. Start the session: the driver is started on the device and Settings is
#    launched. The with block quits the session when it ends, even on an error.
with start(platform=platform, app=p["app"], device=os.environ.get("MOBIUM_DEVICE")) as device:
    s = device.session
    print(f"session on {s.device} ({s.platform}, {s.driver})")

    # 2. Map the screen: every element you can act on, each with a @ref.
    elements = device.map()
    for e in elements[:5]:
        print(" ", e)

    # 3. Tap a row by its ref, then wait for the screen it opens. A row's label
    #    can carry its summary too ("Network & internet Mobile, Wi-Fi, ..."),
    #    so match its start.
    row = next(e for e in elements if e.label.startswith(p["row"]))
    device.tap(row.ref)
    device.wait_for(p["next"])
    print(f"opened {p['row']}")

    # 4. Take a screenshot.
    device.screenshot(f"quickstart-{platform}.png")
    print(f"saved quickstart-{platform}.png")

# 5. The with block has quit: the device's session is closed.
print("session ended")
