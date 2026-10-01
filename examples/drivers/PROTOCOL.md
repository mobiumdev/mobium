# The driver protocol

What a third-party driver speaks: a process Mobium starts, talking JSON-RPC
over its stdin and stdout, which gives Mobium a platform it does not know —
a Roku, a Flutter engine, a TV. Everything above the driver — refs, waiting,
scrolling, `map` — comes from Mobium unchanged. [README.md](README.md) is the
guide to writing one; this is the protocol it follows, and
[`mobium-driver-adb`](mobium-driver-adb) is a working one in Python.

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
([CHALLENGES 43](../../docs/CHALLENGES.md)). Nothing downstream can infer it.

`displayed` and `enabled` are the two fields that default to **true** when
absent, so a driver that does not track them does not accidentally report an
empty screen or a screen of dead controls. `enabled` is acted on since
2026-09-26: an action waits for its target to be enabled, as Vibium's do, and
refuses one that stays disabled — so a driver that sends
`enabled:false` for a control the user can tap will find every tap on it
refused.
Sending `false` explicitly is believed, and it is the only field a driver can
use to say "this exists but the user cannot see it": a node marked
`displayed:false` is kept in the hierarchy, so a locator still resolves to it
and the error explains why it cannot be touched, but it is left out of `map`.

That last part was not always true — `Actionable` once did not consult the
field at all, so a driver could say `false` and be mapped anyway.
Fixed, with a test that fails if it regresses; recorded as
[CHALLENGES 39](../../docs/CHALLENGES.md). A protocol that documents a field
which changes nothing is worse than one that omits it.

### Errors

A driver reports a failure as a JSON-RPC error object with a message written
for a human — it is shown to the user verbatim, prefixed with the driver name.
Protocol errors (malformed request, unknown method) use the standard codes.
This is the same rule the tool layer follows upward: an error is an
explanation, not a code to look up.

## Discovery

`--driver roku` looks for a built-in backend of that name first, then for an
executable named **`mobium-driver-roku`** on `PATH`. Git and kubectl both do
this, it needs no registry, no config file and no install step beyond putting a
file somewhere, and it makes the failure legible: "no built-in backend named
roku, and no `mobium-driver-roku` on your PATH".

`MOBIUM_DRIVER_<NAME>` overrides the path for one backend, which is how you
develop a driver without installing it.

`mobium doctor` lists the drivers it can find, so "is it installed" has an
answer that is not "run it and see".
