# Mobium for Python

Drive mobile apps on Android emulators, Android phones, iOS simulators and
iPhones — map the screen, tap, type, wait and screenshot — through the same
tools as the [mobium](https://github.com/mobiumdev/mobium) command line and
MCP server. No dependencies.

```python
from mobium import start

with start(platform="android", app="com.android.settings") as device:
    for element in device.map()[:5]:
        print(element)                      # @e5 Network & internet ... (button)
    row = next(e for e in device.map() if e.label.startswith("Network & internet"))
    device.tap(row.ref)
    device.wait_for("text=Internet")
    device.screenshot("settings.png")
# the with block has quit: the session on the device is over
```

`start()` opens a session on the device and launches the app fresh;
`quit()` — or leaving the `with` block — ends it.
`platform="ios"` drives an iOS simulator or iPhone the same way.

## Requirements

- Python 3.9 or later.
- **The `mobium` binary**, on your `PATH` or named by `MOBIUM_BIN_PATH`. This
  package is a client for it. Install it with Go 1.24+:

  ```sh
  go install github.com/mobiumdev/mobium/cmd/mobium@latest
  ```

- A device: an Android emulator or phone, or on macOS an iOS simulator or
  iPhone. `mobium doctor` checks the setup and names the fix for anything
  missing.

## Documentation

- [Quick start for Python](https://github.com/mobiumdev/mobium/blob/main/docs/quickstart/python.md)
  — from nothing to a running script, on Android and iOS
- [Every tool](https://github.com/mobiumdev/mobium/blob/main/docs/API.md) and
  [setting up devices](https://github.com/mobiumdev/mobium/blob/main/docs/SETUP.md)

## Errors

Every failure raises `MobiumError` or a subclass named for its code —
`NoSuchElementError`, `AmbiguousLocatorError`, `TimedOutError` (not
`TimeoutError`, which is Python's) and the rest, the same set in every Mobium
client — with `.code`, `.remedy` and `.retryable`.

## License

Apache License 2.0; see LICENSE and NOTICE.
