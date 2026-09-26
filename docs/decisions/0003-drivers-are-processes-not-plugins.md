# 0003 — A third-party driver is a process, not a Go plugin

**2026-09-13.** Answers a structural criticism of the design: the extension
point is private. `mobiumdriver.Driver` lives under `internal/`, so nobody outside
this repository can add a Roku, Flutter, tvOS or Tizen backend without forking.
Appium's ecosystem exists precisely because strangers can, and that is the
single largest advantage it has over Mobium.

## The three ways to open it

**Export the Go interface.** Move `internal/mobiumdriver` to `mobiumdriver/`, and a
third party imports it. Cheapest to do and the worst of the three. It makes
every type reachable from `Driver` public API — `uitree.Node`, `uitree.Tree`,
`device.InstalledApp`, `Rect` — and each of them is still moving. The eleven
capability interfaces would freeze on the day they were exported, which is
about ten interfaces too early. Worse, a driver written this way is a Go
library: the author has to import Mobium and build their own binary, so
"install a driver" means "build a different Mobium". That is a fork with extra
steps.

**`plugin.Open`.** Rejected outright. Go plugins require the exact same
compiler version, the exact same versions of every shared dependency, and the
exact same build flags; they do not work on Windows at all, and they are
effectively unsupported on macOS with CGO disabled — which is how Mobium is
built. A distribution mechanism that fails on two of three platforms is not
one.

**A subprocess speaking a protocol.** Chosen. The driver is an executable that
reads JSON-RPC 2.0 on stdin and writes it on stdout, exactly the framing
`mobium pipe` already speaks to its clients. Mobium spawns it, negotiates, and
talks to it for the life of the session.

## Why the subprocess wins

- **The single-binary property survives.** Mobium still ships as one static
  executable with no runtime dependencies. A driver is a separate file the user
  chooses to install; nothing is required to run Mobium without one.
- **No language lock-in.** Appium's ecosystem is Node-only because Appium is
  Node. A Mobium driver can be a shell script, a Python file, a Go binary or a
  Rust one. For a device whose SDK is Python — which is most of the odd ones —
  that is the difference between possible and not.
- **Nothing internal is frozen.** The contract is the wire format, which is
  small and deliberately conservative, rather than every Go type reachable from
  an interface. `uitree.Node` can keep changing; `WireNode` cannot, and it is
  the only thing a driver author sees.
- **A crashing driver is not a crashing Mobium.** In-process plugins share the
  address space. A third-party driver segfaulting takes the daemon with it, and
  the daemon holds the device session.
- **It matches how this already works.** Mobium speaks JSON-RPC over a pipe to
  five clients today. Speaking it downward as well as upward adds a direction,
  not a mechanism.

The cost is a process boundary per call. Measured against the dump backend's
1.96s snapshot, and even against UiAutomator2's 0.04s, the pipe is not where
the time goes: the round trip through a spawned process is tens of
microseconds, and every driver worth writing is talking to a device over USB or
TCP behind it.

## The protocol

Transport: JSON-RPC 2.0, one object per line (newline-delimited, not
Content-Length framed), UTF-8, over the child's stdin and stdout. **stderr is
the driver's log** and Mobium forwards it under `--verbose`; anything a driver
prints there is free to be unstructured, which means a stray `print()` cannot
corrupt the channel. This is the one framing decision worth stating loudly,
because it is the mistake every author of a stdio protocol makes once.

### Handshake

Mobium calls `initialize` first and sends nothing else until it answers:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize",
 "params":{"protocolVersion":"1","device":"<the --device string, verbatim>"}}
```

The driver replies with its name and what it can do:

```json
{"jsonrpc":"2.0","id":1,"result":{
  "protocolVersion":"1",
  "name":"roku/ecp",
  "capabilities":["gestures","text","apps","appearance","inventory",
                  "permissions","permissionState","health"]}}
```

`snapshot`, `screenshot` and `tap` are not capabilities: they are the whole of
`Driver` and every driver must implement them. Everything else is optional and
must be advertised. **Mobium wires up only the capabilities named**, so a
driver that does not list `text` is a driver Mobium knows cannot type — and the
user is told to use a different backend, rather than the driver being asked and
failing. This mirrors the Go interfaces exactly: `Gesturer`, `TextEntry`,
`AppControl`, `Appearance`, `AppInventory`, `Permissions`, `PermissionReader`,
`Health`. A capability Mobium does not recognize is ignored, so a driver may
advertise ahead of a Mobium release.

**Not every capability Mobium knows about is one this protocol carries.**
`mobiumdriver.KnownCapabilities` also lists `pinch`, `doubleTap`, `drag` and
`multiTouch`, among others, and the table below has no method for any of them:
they exist to
gate the built-in backends, where the answer is whether the Go type has the
method. An external driver that advertises `drag` is therefore recognized and
still cannot drag — the type assertion fails before the capability is
consulted, and the tool layer refuses as it would for any backend without it.
That is the safe direction to be wrong in, and it is written down here because
the reverse would not be: a driver author reading only the capability list
would have no way to learn that the wire has no `drag`. Adding one is a
protocol change, and the version field is how it would be made.

### Methods

| Method | Params | Result | Capability |
|---|---|---|---|
| `initialize` | `protocolVersion`, `device` | `protocolVersion`, `name`, `capabilities` | — |
| `snapshot` | — | a wire node (see below) | — |
| `screenshot` | — | `{"png":"<base64>"}` | — |
| `tap` | `x`, `y` | `{}` | — |
| `swipe` | `x1`,`y1`,`x2`,`y2`,`durationMs` | `{}` | `gestures` |
| `longPress` | `x`,`y`,`durationMs` | `{}` | `gestures` |
| `setText` | `path`, `text` | `{}` | `text` |
| `clear` | `path` | `{}` | `text` |
| `launch` / `terminate` | `appId` | `{}` | `apps` |
| `install` | `path` | `{}` | `apps` |
| `openUrl` | `url` | `{}` | `apps` |
| `appearance` | — | `{"mode":"light"\|"dark"\|…}` | `appearance` |
| `setAppearance` | `mode` | `{}` | `appearance` |
| `listApps` | `includeSystem` | `{"apps":[{"id","name","version","system"}]}` | `inventory` |
| `uninstall` | `appId` | `{}` | `inventory` |
| `setPermission` | `appId`,`permission`,`grant` | `{}` | `permissions` |
| `resetPermissions` | `appId` | `{}` | `permissions` |
| `permissionState` | `appId` | `{"permissions":{"name":true}}` | `permissionState` |
| `health` | — | `{"healthy":true}` | `health` |
| `shutdown` | — | `{}` | — |

`setText` and `clear` take a **path**, not a node: the driver already sent the
hierarchy and can find its own element by the same path Mobium derived from it.
Sending the node back would mean putting Mobium's bookkeeping fields on the
wire, which is exactly what the wire format avoids.

Coordinates are **device pixels on every platform**, matching Android. A driver
for a platform that thinks in points converts, because that conversion is the
driver's knowledge and nobody else's. This is the rule iOS geometry already
follows internally.

### The hierarchy

`snapshot` returns one node, recursively, in the shape of `uitree.WireNode`
([../../internal/uitree/wire.go](../../internal/uitree/wire.go)):

```json
{"class":"Button","text":"7","testid":"com.example:id/digit_7",
 "bounds":[0,0,100,50],"clickable":true,
 "children":[]}
```

Every field is optional except `bounds`, which is `[x1,y1,x2,y2]`. The driver
sends **only what the platform told it**. `depth`, `index`, `path` and the
parent link are Mobium's own bookkeeping and are recomputed on arrival by
`uitree.Link`, so a driver author cannot get them subtly wrong. A test walks
four real captures and asserts `Link` produces byte-identical derived fields to
the ones the XML parsers build inline; if that ever drifts, a `@ref` would mean
different things depending on the backend, and nothing else in the codebase
would notice.

`password` is acted on and is worth setting: a field marked with it is never
printed by `map` — it is labeled by its `testid`, given `role=password`, and
read back masked with only its length kept. Android puts a password field's
typed value straight into `text`, and mobium leaked one until it honored this
([CHALLENGES 43](../CHALLENGES.md)). Nothing downstream can infer it.

`displayed` and `enabled` are the two fields that default to **true** when
absent, so a driver that does not track them does not accidentally report an
empty screen or a screen of dead controls. `enabled` is acted on since
2026-09-26: an action waits for its target to be enabled, as Playwright's and
Vibium's do, and refuses one that stays disabled — so a driver that sends
`enabled:false` for a control the user can tap will find every tap on it
refused.
Sending `false` explicitly is believed, and it is the only field a driver can
use to say "this exists but the user cannot see it": a node marked
`displayed:false` is kept in the hierarchy, so a locator still resolves to it
and the error explains why it cannot be touched, but it is left out of `map`.

That last part was not true when this was first written — `Actionable` did not
consult the field at all, so a driver could say `false` and be mapped anyway.
Fixed, with a test that fails if it regresses; recorded as
[CHALLENGES 39](../CHALLENGES.md). A protocol that documents a field which
changes nothing is worse than one that omits it.

### Errors

A driver reports a failure as a JSON-RPC error object with a message written
for a human — it is shown to the user verbatim, prefixed with the driver name.
Protocol errors (malformed request, unknown method) use the standard codes.
This is the same rule the tool layer follows upward: an error is an
explanation, not a code to look up.

## Discovery

`--backend roku` looks for a built-in backend of that name first, then for an
executable named **`mobium-driver-roku`** on `PATH`. Git and kubectl both do
this, it needs no registry, no config file and no install step beyond putting a
file somewhere, and it makes the failure legible: "no built-in backend named
roku, and no `mobium-driver-roku` on your PATH".

`MOBIUM_DRIVER_<NAME>` overrides the path for one backend, which is how you
develop a driver without installing it.

`mobium doctor` lists the drivers it can find, so "is it installed" has an
answer that is not "run it and see".

## What this does not do

It does not make Mobium's *tool* layer extensible — there is no way to add an
`app_foo` tool from outside, and there should not be one. The value of a single
shared tool layer is that every client gets every tool with the same semantics;
a third-party tool would break that for whoever did not install it. The
platform seam is the right place to open, and the only one.

## How to re-check this

The claims here are about a protocol somebody outside this repository has to
implement, so they are worth re-running rather than trusting:

```sh
# The reference driver still needs nothing from this module. Every line should
# name a Python standard-library module and nothing else — anything from
# mobium here means the extension point is not as open as this document says.
grep -n "^import\|^from" examples/drivers/mobium-driver-adb

# Every capability has an As* helper, and nothing above internal/mobiumdriver
# asserts a capability interface directly.
grep -rn 's\.driver\.(mobiumdriver\.' internal/

# The whole thing, against a device.
docs/checks/external-driver.sh <serial>
```

**Re-run 2026-09-14**: the two that need no device both hold — the reference
driver imports `base64`, `json`, `os`, `subprocess`, `sys` and `xml` and
nothing more, and no capability interface is asserted directly anywhere above
`internal/mobiumdriver`.

It also does not promise version-1 stability yet. `protocolVersion` is `"1"`
and Mobium refuses anything else, so the negotiation is in place from the
start; whether `"1"` is frozen is a decision for the public release, not for
today.
