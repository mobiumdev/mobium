# The API surface

**Generated — do not edit.** Run `make api` to rebuild this and
[api/surface.json](api/surface.json) from the source.

Mobium's architecture rests on one claim: every front door reaches the same
tool layer, so a CLI command and an MCP tool cannot answer differently. This is
that claim, enumerated, and a drift check keeps it true —
`internal/apisurface` fails the build if any tool stops being reachable
from any surface without the gap being written down.

One level down, [FLAGS.md](FLAGS.md) asks whether the CLI and the schema agree
about what each tool *accepts*. Same procedure, same package, same kind of
exemption list — and a separate check, because the two questions fail
differently.

## The numbers

| | |
| --- | --- |
| Tools | **68** |
| CLI commands registered | 77 |
| …visible in `mobium --help` | 76 |
| …hidden | 1 (pipe) |
| Command constructors in source | 83 (includes `daemon start`, `stop`, `status`) |
| Client libraries | 5 |

Those three command counts differ on purpose, and the arithmetic is asserted
by a test: registered = visible + hidden, and the constructor count is higher
again because `daemon` has subcommands. Reported as one number, they have
been wrong twice.

## Every tool, and what reaches it

| # | Tool | CLI | Go | Python | JavaScript | Java |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | `app_accessibility` | `accessibility` | `Accessibility` | `accessibility` | `accessibility` | `accessibility` |
| 2 | `app_alert` | `alert` | `Alert` | `alert` | `alert` | `alert` |
| 3 | `app_appearance` | `appearance` | `Appearance` | `appearance` | `appearance` | `appearance` |
| 4 | `app_background` | `background` | `Background` | `background` | `background` | `background` |
| 5 | `app_batch` | `batch` | `Batch` | `batch` | `batch` | `batch` |
| 6 | `app_battery` | `battery` | `Battery` | `battery` | `battery` | `battery` |
| 7 | `app_biometric` | `biometric` | `Biometric` | `biometric` | `biometric` | `biometric` |
| 8 | `app_call` | `call` | `IncomingCall` | `incoming_call` | `incomingCall` | `incomingCall` |
| 9 | `app_check` | `check, uncheck` | `Check` | `check` | `check` | `check` |
| 10 | `app_clear_data` | `clear-data` | `ClearData` | `clear_data` | `clearData` | `clearData` |
| 11 | `app_clipboard` | `clipboard` | `Clipboard` | `clipboard` | `clipboard` | `clipboard` |
| 12 | `app_context` | `context` | `Context` | `context` | `context` | `context` |
| 13 | `app_contexts` | `contexts` | `Contexts` | `contexts` | `contexts` | `contexts` |
| 14 | `app_cookies` | `cookies` | `Cookies` | `cookies` | `cookies` | `cookies` |
| 15 | `app_crashes` | `crashes` | `Crashes` | `crashes` | `crashes` | `crashes` |
| 16 | `app_current` | `current` | `Current` | `current` | `current` | `current` |
| 17 | `app_devices` | `devices` | `Devices` | `devices` | `devices` | `devices` |
| 18 | `app_dialogs` | `dialogs` | `AddDialogRule` | `add_dialog_rule` | `addDialogRule` | `addDialogRule` |
| 19 | `app_doctor` | `doctor` | `Doctor` | `doctor` | `doctor` | `doctor` |
| 20 | `app_download` | `download` | `Download` | `download` | `download` | `download` |
| 21 | `app_drag` | `drag` | `Drag` | `drag` | `drag` | `drag` |
| 22 | `app_eval` | `eval` | `Eval` | `eval` | `eval` | `eval` |
| 23 | `app_fill` | `fill` | `Fill` | `fill` | `fill` | `fill` |
| 24 | `app_find` | `find` | `Find` | `find` | `find` | `find` |
| 25 | `app_grant` | `grant` | `Grant` | `grant` | `grant` | `grant` |
| 26 | `app_hit_test` | `hit-test` | `HitTest` | `hit_test` | `hitTest` | `hitTest` |
| 27 | `app_install` | `install` | `Install` | `install` | `install` | `install` |
| 28 | `app_keyboard` | `keyboard` | `Keyboard` | `keyboard` | `keyboard` | `keyboard` |
| 29 | `app_launch` | `launch` | `Launch` | `launch` | `launch` | `launch` |
| 30 | `app_list_apps` | `apps` | `Apps` | `apps` | `apps` | `apps` |
| 31 | `app_locale` | `locale` | `AppLocale` | `app_locale` | `appLocale` | `appLocale` |
| 32 | `app_location` | `location` | `Location` | `location` | `location` | `location` |
| 33 | `app_lock` | `lock` | `ScreenLocked` | `screen_locked` | `screenLocked` | `screenLocked` |
| 34 | `app_logs` | `logs` | `Logs` | `logs` | `logs` | `logs` |
| 35 | `app_long_press` | `long-press` | `LongPress` | `long_press` | `longPress` | `longPress` |
| 36 | `app_map` | `map` | `Map` | `map` | `map` | `map` |
| 37 | `app_network` | `network` | `ResetNetwork` | `network` | `network` | `network` |
| 38 | `app_notifications` | `notifications` | `Notifications` | `notifications` | `notifications` | `notifications` |
| 39 | `app_open_url` | `open` | `OpenURL` | `open_url` | `openUrl` | `openUrl` |
| 40 | `app_orientation` | `orientation` | `Orientation` | `orientation` | `orientation` | `orientation` |
| 41 | `app_press` | `press` | `Press` | `press` | `press` | `press` |
| 42 | `app_press_drag` | `press-drag` | `PressDrag` | `press_drag` | `pressDrag` | `pressDrag` |
| 43 | `app_press_tap` | `press-tap` | `PressTap` | `press_tap` | `pressTap` | `pressTap` |
| 44 | `app_record` | `record` | `Record` | `record` | `record` | `record` |
| 45 | `app_reset_permissions` | `reset-permissions` | `ResetPermissions` | `reset_permissions` | `resetPermissions` | `resetPermissions` |
| 46 | `app_revoke` | `revoke` | `Revoke` | `revoke` | `revoke` | `revoke` |
| 47 | `app_rotate` | `rotate` | `Rotate` | `rotate` | `rotate` | `rotate` |
| 48 | `app_screen` | `screen` | `Screen` | `screen` | `screen` | `screen` |
| 49 | `app_screenshot` | `screenshot` | `Screenshot` | `screenshot` | `screenshot` | `screenshot` |
| 50 | `app_scroll_to` | `scroll-to` | `ScrollTo` | `scroll_to` | `scrollTo` | `scrollTo` |
| 51 | `app_session` | `session` | `Start` | `start` | `start` | `start` |
| 52 | `app_shake` | `shake` | `Shake` | `shake` | `shake` | `shake` |
| 53 | `app_sms` | `sms` | `SendSMS` | `sms` | `sms` | `sms` |
| 54 | `app_source` | `source` | `Source` | `source` | `source` | `source` |
| 55 | `app_state` | `state` | `AppState` | `app_state` | `appState` | `appState` |
| 56 | `app_storage` | `storage` | `Storage` | `storage` | `storage` | `storage` |
| 57 | `app_swipe` | `swipe` | `Swipe` | `swipe` | `swipe` | `swipe` |
| 58 | `app_tap` | `tap, double-tap` | `Tap` | `tap` | `tap` | `tap` |
| 59 | `app_terminate` | `terminate` | `Terminate` | `terminate` | `terminate` | `terminate` |
| 60 | `app_text` | `text` | `Text` | `text` | `text` | `text` |
| 61 | `app_time` | `time` | `DeviceTime` | `device_time` | `deviceTime` | `deviceTime` |
| 62 | `app_timezone` | `timezone` | `Timezone` | `timezone` | `timezone` | `timezone` |
| 63 | `app_trace` | `trace` | `TraceStart` | `trace_start` | `traceStart` | `traceStart` |
| 64 | `app_type` | `type` | `Type` | `type` | `type` | `type` |
| 65 | `app_uninstall` | `uninstall` | `Uninstall` | `uninstall` | `uninstall` | `uninstall` |
| 66 | `app_upload` | `upload` | `Upload` | `upload` | `upload` | `upload` |
| 67 | `app_wait_for` | `wait` | `WaitFor` | `wait_for` | `waitFor` | `waitFor` |
| 68 | `app_zoom` | `zoom` | `Zoom` | `zoom` | `zoom` | `zoom` |

## Client coverage

| Client | Source | Tools reached |
| --- | --- | --- |
| go | [clients/go/mobium.go](../clients/go/mobium.go) | 68 / 68 |
| python | [clients/python/mobium/_device.py](../clients/python/mobium/_device.py) | 68 / 68 |
| javascript | [clients/javascript/index.js](../clients/javascript/index.js) | 68 / 68 |
| java | [clients/java/src/main/java/dev/mobium/Mobium.java](../clients/java/src/main/java/dev/mobium/Mobium.java) | 68 / 68 |
| dotnet | [clients/dotnet/Mobium/Device.cs](../clients/dotnet/Mobium/Device.cs) | 68 / 68 |

## Commands that dispatch no tool

- `daemon`
- `daemon start`
- `daemon status`
- `daemon stop`
- `daemon up`
- `grid`
- `grid status`
- `grid ui`
- `inspect`
- `mcp`
- `pipe`
- `show-report`
- `test`


These run the process rather than the device — the daemon's own lifecycle, and
the two stdio servers. They are listed because a command that reaches no tool
is either one of these or an orphan, and from outside the two look identical; a
test names them so a new orphan fails rather than blending in.

## How drift is prevented

`internal/apisurface` sweeps the source — not a running binary, so it
cannot disagree with what would be built — and asserts:

1. **Every tool is reachable from the CLI.** Both dispatch paths count: most
   commands go through the daemon, while `doctor` calls the tool layer
   in-process so it still works with no daemon, no device and nothing on PATH.
2. **Every client reaches every tool.** Per client, so a failure names the
   client rather than a list of tools.
3. **Gaps may exist, but only declared ones.** An undeclared gap fails; so does
   an exemption for a gap that has since been closed, so the list cannot rot
   into a blanket excuse. It is currently empty.
4. **The sweep can see each surface.** A pattern that stopped matching would
   report a client as covering nothing; that is asserted against, because a
   drift check that fails open is worse than none.

The first thing this found, on the day it was written, was real: `doctor`
existed in the Java client and in none of the other three. Nothing had compared
them before.
