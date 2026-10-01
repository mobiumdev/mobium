# Mobium for JavaScript

Drive native apps on Android emulators, Android phones, iOS simulators and
iPhones — map the screen, tap, type, wait and screenshot — through the same
tools as the [mobium](https://github.com/mobiumdev/mobium) command line and
MCP server. No dependencies; TypeScript declarations included.

```js
import { start } from 'mobium'

const device = await start({ platform: 'android', app: 'com.android.settings' })
try {
  const elements = await device.map()
  const row = elements.find((e) => e.label.startsWith('Network & internet'))
  await device.tap(row.ref)
  await device.waitFor('text=Internet')
  await device.screenshot('settings.png')
} finally {
  await device.quit()   // ends the session on the device
}
```

`start()` opens a session on the device and launches the app fresh;
`quit()` ends it. `platform: 'ios'` drives an iOS
simulator or iPhone the same way.

## Requirements

- Node.js 18 or later.
- **The `mobium` binary**, on your `PATH` or named by `MOBIUM_BIN_PATH`. This
  package is a client for it. Install it with Go 1.24+:

  ```sh
  go install github.com/mobiumdev/mobium/cmd/mobium@latest
  ```

- A device: an Android emulator or phone, or on macOS an iOS simulator or
  iPhone. `mobium doctor` checks the setup and names the fix for anything
  missing.

## Documentation

- [Quick start for JavaScript](https://github.com/mobiumdev/mobium/blob/main/docs/quickstart/javascript.md)
  — from nothing to a running script, on Android and iOS
- [Every tool](https://github.com/mobiumdev/mobium/blob/main/docs/API.md) and
  [setting up devices](https://github.com/mobiumdev/mobium/blob/main/docs/SETUP.md)

## Errors

Every failure rejects with `MobiumError` or a subclass named for its code —
`NoSuchElementError`, `AmbiguousLocatorError`, `TimedOutError` and the rest,
the same set in every Mobium client — with `code`, `remedy` and `retryable`.

## License

MIT.
