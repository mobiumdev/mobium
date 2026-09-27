# Writing a Mobium driver

Mobium drives Android and iOS out of the box. Anything else — a Roku, a Flutter
desktop app, a set-top box, a car head unit, a platform that does not exist yet
— is a **driver**: a program Mobium starts and talks to over a pipe.

You do not need to fork Mobium, build Mobium, or write Go. A driver is an
executable in any language that reads JSON-RPC on stdin and writes it on
stdout. The protocol is specified in
[../../docs/decisions/0003-drivers-are-processes-not-plugins.md](../../docs/decisions/0003-drivers-are-processes-not-plugins.md);
this page is the short version.

Everything above the driver — locators, `@ref`s, waiting, scrolling, the CLI,
MCP and all four client libraries — you get for free. The driver's whole job is
to answer *what is on screen* and *touch this point*.

## The smallest driver that works

Three methods are mandatory: `snapshot`, `screenshot` and `tap`. Plus
`initialize`, which says who you are.

```python
#!/usr/bin/env python3
import json, sys

def handle(method, params):
    if method == "initialize":
        return {"protocolVersion": "1", "name": "my-thing", "capabilities": []}
    if method == "snapshot":
        return {"class": "Screen", "bounds": [0, 0, 400, 800], "children": [
            {"class": "Button", "text": "Go", "bounds": [10, 10, 100, 60],
             "clickable": True},
        ]}
    if method == "screenshot":
        return {"png": "<base64 of a PNG>"}
    if method == "tap":
        print(f"tapped {params['x']},{params['y']}", file=sys.stderr)
        return {}
    if method == "shutdown":
        return {}
    raise Exception(f"not implemented: {method}")

for line in sys.stdin:
    req = json.loads(line)
    try:
        reply = {"jsonrpc": "2.0", "id": req["id"], "result": handle(req["method"], req.get("params") or {})}
    except Exception as err:
        reply = {"jsonrpc": "2.0", "id": req["id"], "error": {"code": -32000, "message": str(err)}}
    print(json.dumps(reply), flush=True)
```

Save it as `mobium-driver-mything`, `chmod +x`, put it on your `PATH`, and:

```
mobium map --driver mything
```

**On Windows** there is no `#!` line and no execute bit: a file is runnable by
its extension. Name the driver `mobium-driver-mything.exe`, or give a script a
wrapper named `mobium-driver-mything.cmd` containing
`@python "%~dp0mobium-driver-mything.py"` — anything listed in `PATHEXT`
is found the same way. That is the rule the lookup uses, and `mobium doctor`
reports a driver as `mything` either way. Written from how Go finds a program
on Windows and not yet run there; see [WINDOWS.md](../../docs/WINDOWS.md).

## The five rules

**1. stdout is the protocol. stderr is your log.** A stray `print()` on stdout
is the mistake everyone makes once. Mobium skips lines that are not the reply it
is waiting for, and quotes them back at you when something later fails — but do
not rely on that. Put your logging on stderr, where `mobium --verbose` shows it.

**2. Send what the platform told you, nothing derived.** A snapshot node
carries `bounds` and whatever of `text`, `label`, `testid`, `class`, `package`
and the boolean flags you have. Depth, index, path and the parent link are
Mobium's bookkeeping; it rebuilds them on arrival. There is no way for you to
get them wrong because there is no way for you to send them.

**Set `password` on any field that holds a secret.** Mobium then never prints
its contents: `map` labels it by its `testid` and gives it `role=password`, and
reads come back masked with the length kept. It is not cosmetic — Android puts
the typed value straight into a password field's `text`, and mobium printed one
in plaintext until it honored the flag ([CHALLENGES 43](../../docs/CHALLENGES.md)).
If your platform knows which fields are secret, say so; nothing downstream can
work it out for you.

The other flag worth knowing about is `displayed`. Leave it out and the node is
treated as visible, which is what you want if your platform does not tell you.
Send `false` and Mobium keeps the node in the hierarchy — so a locator still
resolves to it and the error explains itself — but leaves it out of `map`, so
an agent is never offered something it cannot see.

`enabled` works the same way: leave it out and the node is enabled. Send
`false` and an action on that node waits for it to become enabled and then
refuses, because a disabled control ignores a tap — so send it only when it is
true of the control.

**3. Advertise honestly.** A capability you do not list is one Mobium will never
ask you for — it tells the user to switch backends instead. That is much better
than being asked and failing. The reference driver here does not advertise
`text`, because `adb shell input text` mangles quotes and non-ASCII, and a
refusal beats silently entering the wrong string.

**4. Coordinates are device pixels, everywhere.** If your platform thinks in
points or in a normalized 0–1 space, convert. That conversion is knowledge only
you have.

**5. Never let an exit code stand in for evidence.** Read both output streams of
whatever you shell out to, and where the platform can report state back, read it
rather than believing the command. This is not general advice: every single tool
in the Android and iOS toolchains reports at least some failures while exiting
0, on inconsistent streams. See
[../../docs/CHALLENGES.md](../../docs/CHALLENGES.md).

## Capabilities

`snapshot`, `screenshot` and `tap` are mandatory. Everything else is opt-in:

| Capability | Methods | What you unlock |
|---|---|---|
| `gestures` | `swipe`, `longPress` | scrolling, `scroll-to`, long press |
| `text` | `setText`, `clear` | typing into a named element |
| `apps` | `launch`, `terminate`, `install`, `openUrl` | app lifecycle and deep links |
| `appearance` | `appearance`, `setAppearance` | light/dark |
| `inventory` | `listApps`, `uninstall` | what is installed |
| `permissions` | `setPermission`, `resetPermissions` | granting up front |
| `permissionState` | `permissionState` | reading what is granted |
| `health` | `health` | letting Mobium notice you have lost the device |

`gestures` is the one worth having: without it Mobium can see and tap, but not
scroll, and most screens are longer than the screen.

## Developing without installing

```
MOBIUM_DRIVER_MYTHING=./mobium-driver-mything mobium map --driver mything
```

`mobium doctor` lists every `mobium-driver-*` it can find on your `PATH`, so
"did it install" has an answer that is not "run it and see".

## The reference driver

[`mobium-driver-adb`](mobium-driver-adb) is a complete, working driver in ~350
lines of Python with no dependencies. It drives Android through plain `adb` —
deliberately the same ground Mobium's own `--driver uiautomator` covers, so its
output can be diffed against a known-good implementation on the same screen.
That diff is how you tell a driver that works from one that returns something
plausible, and it is exactly how this one was checked:

```
$ mobium map --driver adb        > external.txt   # the Python driver
$ mobium map --driver uiautomator > builtin.txt   # mobium's own
$ diff external.txt builtin.txt && echo IDENTICAL
IDENTICAL
```

Verified on a Pixel 7 AVD (Android 15), 2026-09-13: identical maps, plus
`launch`, `terminate`, `tap`, `scroll-to` (2 swipes to reach *About emulated
device*), `screenshot` (a real 1080x2400 PNG) and `apps`. Both capabilities it
does not advertise were refused with a message naming the backend, and its own
diagnosis of an unreadable screen reached the user verbatim:

```
$ mobium type 'text=Search settings' hello --driver adb
error: the adb backend cannot type into an element — use the uiautomator2 backend

$ mobium map --driver adb          # on Settings > About, which ticks
error: adb/uiautomator (reference driver): uiautomator dump gave up waiting for
the screen to stop changing (ERROR: could not get idle state.) …
```

It imports the Python standard library and nothing from Mobium. That is the
test that matters: if a reference driver needs anything from this repository,
the extension point is not really open.
