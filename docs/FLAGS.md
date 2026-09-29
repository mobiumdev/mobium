# The flag surface

**Generated — do not edit.** Run `make flags` to rebuild this and
[api/flags.json](api/flags.json) from the source.

[API.md](API.md) answers whether every front door reaches every tool. This
answers the question underneath it: **whether the CLI and the schema agree
about what each tool accepts.**

That is worth enforcing separately because the two ends fail in opposite
directions and neither one shouts. Every tool's schema is
`additionalProperties: false`, so an MCP client passing an undeclared key
is refused outright; the CLI hands a plain map to the same handler, and a
handler reads the keys it knows by name. A command writing
`call["speeed"]` is therefore rejected loudly on one surface and
silently ignored on the other — the one people use.

`--device` and `--driver` are persistent flags that
`daemonCall` attaches to every call, so they are marked global and no
command mentions them.

## Arguments, by tool

| Tool | Argument | Type | Set by |
| --- | --- | --- | --- |
| `app_accessibility` | `device` | string | _global_ --device |
| `app_accessibility` | `driver` | string | _global_ --driver |
| `app_accessibility` | `setting` | string | accessibility |
| `app_accessibility` | `value` | string | accessibility |
| `app_alert` | `action` | string | alert |
| `app_alert` | `device` | string | _global_ --device |
| `app_alert` | `driver` | string | _global_ --driver |
| `app_alert` | `text` | string | alert |
| `app_appearance` | `appearance` | string | appearance |
| `app_appearance` | `device` | string | _global_ --device |
| `app_appearance` | `driver` | string | _global_ --driver |
| `app_background` | `app` | string | background |
| `app_background` | `device` | string | _global_ --device |
| `app_background` | `driver` | string | _global_ --driver |
| `app_background` | `seconds` | number | background |
| `app_batch` | `device` | string | _global_ --device |
| `app_batch` | `driver` | string | _global_ --driver |
| `app_batch` | `steps` | array | batch |
| `app_battery` | `device` | string | _global_ --device |
| `app_battery` | `driver` | string | _global_ --driver |
| `app_call` | `action` | string | call |
| `app_call` | `device` | string | _global_ --device |
| `app_call` | `driver` | string | _global_ --driver |
| `app_call` | `number` | string | call |
| `app_check` | `checked` | boolean | check, uncheck |
| `app_check` | `device` | string | _global_ --device |
| `app_check` | `driver` | string | _global_ --driver |
| `app_check` | `target` | string | check, uncheck |
| `app_clear_data` | `app` | string | clear-data |
| `app_clear_data` | `device` | string | _global_ --device |
| `app_clear_data` | `driver` | string | _global_ --driver |
| `app_clipboard` | `device` | string | _global_ --device |
| `app_clipboard` | `driver` | string | _global_ --driver |
| `app_clipboard` | `text` | string | clipboard |
| `app_context` | `context` | string | context |
| `app_context` | `device` | string | _global_ --device |
| `app_context` | `driver` | string | _global_ --driver |
| `app_contexts` | `device` | string | _global_ --device |
| `app_contexts` | `driver` | string | _global_ --driver |
| `app_cookies` | `action` | string | cookies |
| `app_cookies` | `cookies` | array | cookies |
| `app_cookies` | `device` | string | _global_ --device |
| `app_cookies` | `driver` | string | _global_ --driver |
| `app_cookies` | `name` | string | cookies |
| `app_crashes` | `app` | string | crashes |
| `app_crashes` | `device` | string | _global_ --device |
| `app_crashes` | `driver` | string | _global_ --driver |
| `app_crashes` | `id` | string | crashes |
| `app_crashes` | `limit` | integer | crashes |
| `app_current` | `device` | string | _global_ --device |
| `app_current` | `driver` | string | _global_ --driver |
| `app_dialogs` | `clear` | boolean | dialogs |
| `app_dialogs` | `device` | string | _global_ --device |
| `app_dialogs` | `driver` | string | _global_ --driver |
| `app_dialogs` | `press` | string | dialogs |
| `app_dialogs` | `when` | string | dialogs |
| `app_drag` | `device` | string | _global_ --device |
| `app_drag` | `driver` | string | _global_ --driver |
| `app_drag` | `duration_ms` | integer | drag |
| `app_drag` | `from` | string | drag |
| `app_drag` | `hold_ms` | integer | drag |
| `app_drag` | `to` | string | drag |
| `app_drag` | `x1` | integer | drag |
| `app_drag` | `x2` | integer | drag |
| `app_drag` | `y1` | integer | drag |
| `app_drag` | `y2` | integer | drag |
| `app_eval` | `device` | string | _global_ --device |
| `app_eval` | `driver` | string | _global_ --driver |
| `app_eval` | `expression` | string | eval |
| `app_fill` | `device` | string | _global_ --device |
| `app_fill` | `driver` | string | _global_ --driver |
| `app_fill` | `target` | string | fill |
| `app_fill` | `text` | string | fill |
| `app_find` | `device` | string | _global_ --device |
| `app_find` | `driver` | string | _global_ --driver |
| `app_find` | `locator` | string | find |
| `app_grant` | `app` | string | grant |
| `app_grant` | `device` | string | _global_ --device |
| `app_grant` | `driver` | string | _global_ --driver |
| `app_grant` | `permissions` | array | grant |
| `app_install` | `content` | string | — |
| `app_install` | `device` | string | _global_ --device |
| `app_install` | `driver` | string | _global_ --driver |
| `app_install` | `name` | string | — |
| `app_install` | `path` | string | install |
| `app_keyboard` | `device` | string | _global_ --device |
| `app_keyboard` | `driver` | string | _global_ --driver |
| `app_keyboard` | `hide` | boolean | keyboard |
| `app_keyboard` | `key` | string | keyboard |
| `app_keyboard` | `text` | string | keyboard |
| `app_launch` | `app` | string | launch |
| `app_launch` | `device` | string | _global_ --device |
| `app_launch` | `driver` | string | _global_ --driver |
| `app_list_apps` | `device` | string | _global_ --device |
| `app_list_apps` | `driver` | string | _global_ --driver |
| `app_list_apps` | `system` | boolean | apps |
| `app_locale` | `app` | string | locale |
| `app_locale` | `device` | string | _global_ --device |
| `app_locale` | `driver` | string | _global_ --driver |
| `app_locale` | `locale` | string | locale |
| `app_location` | `clear` | boolean | location |
| `app_location` | `device` | string | _global_ --device |
| `app_location` | `driver` | string | _global_ --driver |
| `app_location` | `gpx` | string | location |
| `app_location` | `gpx_data` | string | — |
| `app_location` | `latitude` | number | location |
| `app_location` | `longitude` | number | location |
| `app_location` | `speed` | number | location |
| `app_location` | `waypoints` | array | location |
| `app_lock` | `device` | string | _global_ --device |
| `app_lock` | `driver` | string | _global_ --driver |
| `app_lock` | `state` | string | lock |
| `app_logs` | `app` | string | logs |
| `app_logs` | `device` | string | _global_ --device |
| `app_logs` | `driver` | string | _global_ --driver |
| `app_logs` | `level` | string | logs |
| `app_logs` | `lines` | integer | logs |
| `app_logs` | `source` | string | logs |
| `app_long_press` | `device` | string | _global_ --device |
| `app_long_press` | `driver` | string | _global_ --driver |
| `app_long_press` | `duration_ms` | integer | long-press |
| `app_long_press` | `target` | string | long-press |
| `app_long_press` | `x` | integer | long-press |
| `app_long_press` | `y` | integer | long-press |
| `app_map` | `device` | string | _global_ --device |
| `app_map` | `driver` | string | _global_ --driver |
| `app_network` | `device` | string | _global_ --device |
| `app_network` | `download_kbps` | integer | network |
| `app_network` | `driver` | string | _global_ --driver |
| `app_network` | `latency_ms` | integer | network |
| `app_network` | `offline` | boolean | network |
| `app_network` | `reset` | boolean | network |
| `app_network` | `upload_kbps` | integer | network |
| `app_notifications` | `device` | string | _global_ --device |
| `app_notifications` | `driver` | string | _global_ --driver |
| `app_notifications` | `shade` | string | notifications |
| `app_notifications` | `tag` | string | notifications |
| `app_notifications` | `text` | string | notifications |
| `app_notifications` | `title` | string | notifications |
| `app_open_url` | `device` | string | _global_ --device |
| `app_open_url` | `driver` | string | _global_ --driver |
| `app_open_url` | `url` | string | open |
| `app_orientation` | `device` | string | _global_ --device |
| `app_orientation` | `driver` | string | _global_ --driver |
| `app_orientation` | `orientation` | string | orientation |
| `app_press` | `button` | string | press |
| `app_press` | `device` | string | _global_ --device |
| `app_press` | `driver` | string | _global_ --driver |
| `app_press_drag` | `device` | string | _global_ --device |
| `app_press_drag` | `driver` | string | _global_ --driver |
| `app_press_drag` | `duration_ms` | integer | press-drag |
| `app_press_drag` | `from` | string | press-drag |
| `app_press_drag` | `hold` | string | press-drag |
| `app_press_drag` | `lead_ms` | integer | press-drag |
| `app_press_drag` | `to` | string | press-drag |
| `app_press_drag` | `x1` | integer | press-drag |
| `app_press_drag` | `x2` | integer | press-drag |
| `app_press_drag` | `x3` | integer | press-drag |
| `app_press_drag` | `y1` | integer | press-drag |
| `app_press_drag` | `y2` | integer | press-drag |
| `app_press_drag` | `y3` | integer | press-drag |
| `app_press_tap` | `device` | string | _global_ --device |
| `app_press_tap` | `driver` | string | _global_ --driver |
| `app_press_tap` | `hold` | string | press-tap |
| `app_press_tap` | `lead_ms` | integer | press-tap |
| `app_press_tap` | `tap` | string | press-tap |
| `app_press_tap` | `x1` | integer | press-tap |
| `app_press_tap` | `x2` | integer | press-tap |
| `app_press_tap` | `y1` | integer | press-tap |
| `app_press_tap` | `y2` | integer | press-tap |
| `app_record` | `action` | string | record |
| `app_record` | `device` | string | _global_ --device |
| `app_record` | `driver` | string | _global_ --driver |
| `app_record` | `path` | string | record |
| `app_record` | `return_data` | boolean | — |
| `app_reset_permissions` | `app` | string | reset-permissions |
| `app_reset_permissions` | `device` | string | _global_ --device |
| `app_reset_permissions` | `driver` | string | _global_ --driver |
| `app_revoke` | `app` | string | revoke |
| `app_revoke` | `device` | string | _global_ --device |
| `app_revoke` | `driver` | string | _global_ --driver |
| `app_revoke` | `permissions` | array | revoke |
| `app_rotate` | `degrees` | number | rotate |
| `app_rotate` | `device` | string | _global_ --device |
| `app_rotate` | `driver` | string | _global_ --driver |
| `app_rotate` | `radius` | integer | rotate |
| `app_rotate` | `target` | string | rotate |
| `app_screen` | `device` | string | _global_ --device |
| `app_screen` | `driver` | string | _global_ --driver |
| `app_screen` | `inspect` | boolean | screen |
| `app_screen` | `profile` | string | screen |
| `app_screenshot` | `device` | string | _global_ --device |
| `app_screenshot` | `driver` | string | _global_ --driver |
| `app_screenshot` | `path` | string | screenshot |
| `app_scroll_to` | `device` | string | _global_ --device |
| `app_scroll_to` | `direction` | string | scroll-to |
| `app_scroll_to` | `driver` | string | _global_ --driver |
| `app_scroll_to` | `target` | string | scroll-to |
| `app_session` | `action` | string | session |
| `app_session` | `app` | string | session |
| `app_session` | `device` | string | _global_ --device |
| `app_session` | `driver` | string | _global_ --driver |
| `app_session` | `platform` | string | session |
| `app_shake` | `device` | string | _global_ --device |
| `app_shake` | `driver` | string | _global_ --driver |
| `app_sms` | `device` | string | _global_ --device |
| `app_sms` | `driver` | string | _global_ --driver |
| `app_sms` | `from` | string | sms |
| `app_sms` | `text` | string | sms |
| `app_source` | `device` | string | _global_ --device |
| `app_source` | `driver` | string | _global_ --driver |
| `app_state` | `app` | string | state |
| `app_state` | `device` | string | _global_ --device |
| `app_state` | `driver` | string | _global_ --driver |
| `app_storage` | `action` | string | storage |
| `app_storage` | `device` | string | _global_ --device |
| `app_storage` | `driver` | string | _global_ --driver |
| `app_storage` | `state` | object | storage |
| `app_swipe` | `device` | string | _global_ --device |
| `app_swipe` | `direction` | string | swipe |
| `app_swipe` | `driver` | string | _global_ --driver |
| `app_swipe` | `duration_ms` | integer | swipe |
| `app_swipe` | `x1` | integer | swipe |
| `app_swipe` | `x2` | integer | swipe |
| `app_swipe` | `y1` | integer | swipe |
| `app_swipe` | `y2` | integer | swipe |
| `app_tap` | `device` | string | _global_ --device |
| `app_tap` | `double` | boolean | double-tap |
| `app_tap` | `driver` | string | _global_ --driver |
| `app_tap` | `fingers` | integer | tap |
| `app_tap` | `target` | string | double-tap, tap |
| `app_tap` | `x` | integer | double-tap, tap |
| `app_tap` | `y` | integer | double-tap, tap |
| `app_terminate` | `app` | string | terminate |
| `app_terminate` | `device` | string | _global_ --device |
| `app_terminate` | `driver` | string | _global_ --driver |
| `app_text` | `device` | string | _global_ --device |
| `app_text` | `driver` | string | _global_ --driver |
| `app_text` | `target` | string | text |
| `app_time` | `device` | string | _global_ --device |
| `app_time` | `driver` | string | _global_ --driver |
| `app_timezone` | `device` | string | _global_ --device |
| `app_timezone` | `driver` | string | _global_ --driver |
| `app_timezone` | `timezone` | string | timezone |
| `app_type` | `device` | string | _global_ --device |
| `app_type` | `driver` | string | _global_ --driver |
| `app_type` | `target` | string | type |
| `app_type` | `text` | string | type |
| `app_uninstall` | `app` | string | uninstall |
| `app_uninstall` | `device` | string | _global_ --device |
| `app_uninstall` | `driver` | string | _global_ --driver |
| `app_wait_for` | `condition` | string | wait |
| `app_wait_for` | `device` | string | _global_ --device |
| `app_wait_for` | `driver` | string | _global_ --driver |
| `app_wait_for` | `target` | string | wait |
| `app_wait_for` | `text` | string | wait |
| `app_wait_for` | `timeout_ms` | integer | wait |
| `app_zoom` | `device` | string | _global_ --device |
| `app_zoom` | `direction` | string | zoom |
| `app_zoom` | `driver` | string | _global_ --driver |
| `app_zoom` | `from` | integer | zoom |
| `app_zoom` | `target` | string | zoom |
| `app_zoom` | `to` | integer | zoom |

## Commands, and what they send

A command with no flags of its own is not unusual: most take positional
arguments and the two global flags.

| Command | Tools | Flags | Sends |
| --- | --- | --- | --- |
| `accessibility` | app_accessibility | — | setting, value |
| `alert` | app_alert | --text | action, text |
| `appearance` | app_appearance | — | appearance |
| `apps` | app_list_apps | --system | system |
| `background` | app_background | --app | app, seconds |
| `batch` | app_batch | — | steps |
| `battery` | app_battery | — | — |
| `call` | app_call | --number | action, number |
| `check` | app_check | — | checked, target |
| `clear-data` | app_clear_data | — | app |
| `clipboard` | app_clipboard | — | text |
| `context` | app_context | — | context |
| `contexts` | app_contexts | — | — |
| `cookies` | app_cookies, app_cookies, app_cookies | --domain --expires --http-only --path --same-site --secure | action, cookies, name |
| `crashes` | app_crashes | --app --limit | app, id, limit |
| `current` | app_current | — | — |
| `daemon` | — | — | — |
| `devices` | app_devices | — | — |
| `dialogs` | app_dialogs | --clear --press --when | clear, press, when |
| `doctor` | app_doctor | — | — |
| `double-tap` | app_tap, app_tap | — | double, target, x, y |
| `drag` | app_drag | --duration-ms --hold-ms | duration_ms, from, hold_ms, to, x1, x2, y1, y2 |
| `eval` | app_eval | — | expression |
| `fill` | app_fill | — | target, text |
| `find` | app_find | — | locator |
| `grant` | app_grant | — | app, permissions |
| `grid` | — | --holder --want | — |
| `install` | app_install | — | path |
| `keyboard` | app_keyboard | --hide --key --text | hide, key, text |
| `launch` | app_launch | — | app |
| `locale` | app_locale | — | app, locale |
| `location` | app_location | --clear --gpx --lat --lon --speed | clear, gpx, latitude, longitude, speed, waypoints |
| `lock` | app_lock | — | state |
| `logs` | app_logs | --app --level --lines --source | app, level, lines, source |
| `long-press` | app_long_press | --duration | duration_ms, target, x, y |
| `map` | app_map | — | — |
| `mcp` | — | — | — |
| `network` | app_network | --download --latency --offline --online --reset --upload | download_kbps, latency_ms, offline, reset, upload_kbps |
| `notifications` | app_notifications | --post --shade --tag --title | shade, tag, text, title |
| `open` | app_open_url | — | url |
| `orientation` | app_orientation | — | orientation |
| `pipe` | — | — | — |
| `press` | app_press | — | button |
| `press-drag` | app_press_drag | --duration-ms --lead-ms | duration_ms, from, hold, lead_ms, to, x1, x2, x3, y1, y2, y3 |
| `press-tap` | app_press_tap | --lead-ms | hold, lead_ms, tap, x1, x2, y1, y2 |
| `record` | app_record | --output | action, path |
| `reset-permissions` | app_reset_permissions | — | app |
| `revoke` | app_revoke | — | app, permissions |
| `rotate` | app_rotate | --degrees --radius --target | degrees, radius, target |
| `screen` | app_screen | --inspect | inspect, profile |
| `screenshot` | app_screenshot | --output | path |
| `scroll-to` | app_scroll_to | --direction | direction, target |
| `session` | app_session | --app --platform | action, app, platform |
| `shake` | app_shake | — | — |
| `sms` | app_sms | --from | from, text |
| `source` | app_source | — | — |
| `start` | — | --idle-timeout | — |
| `state` | app_state | — | app |
| `status` | — | — | running |
| `status` | — | — | — |
| `stop` | — | — | status |
| `storage` | app_storage, app_storage, app_storage | --output | action, state |
| `swipe` | app_swipe | --duration | direction, duration_ms, x1, x2, y1, y2 |
| `tap` | app_tap, app_tap | --fingers | fingers, target, x, y |
| `terminate` | app_terminate | — | app |
| `text` | app_text | — | target |
| `time` | app_time | — | — |
| `timezone` | app_timezone | — | timezone |
| `type` | app_type | — | target, text |
| `ui` | — | --port | — |
| `uncheck` | app_check | — | checked, target |
| `uninstall` | app_uninstall | — | app |
| `up` | — | — | — |
| `wait` | app_wait_for | --for --text --timeout | condition, target, text, timeout_ms |
| `zoom` | app_zoom | --from --target --to | direction, from, target, to |
