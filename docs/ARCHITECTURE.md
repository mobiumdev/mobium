# Architecture

Mobium is one Go binary. Every way in — the CLI, an MCP client, the five
language clients — reaches the same tool layer, and the tool layer reaches
every device through one driver interface. Only the bottom layer knows which
platform it is on.

## The whole system

```mermaid
flowchart TB
    subgraph doors["Front doors"]
        cli["CLI<br/>mobium map, mobium tap @e5"]
        clients["Language clients<br/>Python · JavaScript · Go · Java · .NET"]
        mcp["MCP client<br/>an agent"]
    end

    pipe["mobium pipe<br/>JSON-RPC on stdio"]
    mcpcmd["mobium mcp<br/>MCP on stdio, in-process"]
    daemon["Daemon<br/>Unix socket · named pipe on Windows<br/>holds sessions and the @ref table"]

    cli --> daemon
    clients --> pipe --> daemon
    mcp --> mcpcmd

    subgraph core["internal/agent — the tool layer, written once"]
        tools["Tools and schemas<br/>map · tap · type · wait · scroll-to · alert · …"]
        refs["@refs, locators, auto-wait,<br/>dialog and keyboard refusals"]
    end

    daemon --> tools
    mcpcmd --> tools
    tools --- refs

    tree["internal/uitree<br/>platform-neutral node tree"]
    web["internal/webview<br/>CDP · Remote Web Inspector"]
    refs --> tree
    tools --> web

    subgraph drivers["internal/mobiumdriver — the only platform-specific layer"]
        uia2["uiautomator2<br/>Android default"]
        dump["uiautomator<br/>Android, installs nothing"]
        wda["wda<br/>iOS simulator and iPhone"]
        ext["external driver<br/>mobium-driver-NAME"]
    end

    tools --> uia2 & dump & wda & ext

    subgraph devicepkg["internal/device — finding and readying devices"]
        adb["adb"]
        simctl["simctl"]
        devicectl["devicectl · CoreDevice"]
        lockdown["usbmuxd · lockdown"]
    end

    uia2 -- "HTTP over adb forward" --> uiaserver["UiAutomator2 server<br/>on the device"]
    uia2 --> adb
    dump -- "uiautomator dump" --> adb
    wda -- "HTTP" --> wdarunner["WebDriverAgent runner<br/>on the simulator or phone"]
    wda --> simctl & devicectl
    ext -- "JSON-RPC on stdio" --> other["any platform:<br/>a TV, a desktop app, …"]
    web -- "Android: devtools socket" --> adb
    web -- "iPhone: com.apple.webinspector" --> lockdown
```

- **The CLI does not implement behavior.** It parses flags and calls a tool by
  name, so the CLI, MCP and every client get identical behavior.
- **Clients go through the daemon**, via `mobium pipe`, never through
  `mobium mcp`. A device-side server holds one session at a time, so a client
  with its own session would invalidate the CLI's.
- **`mobium mcp` runs the tool layer in-process**, because an MCP client already
  holds a long-lived connection of its own.
- **Reads in a WebView go over its debugging protocol; taps stay native.** The
  page's coordinates are converted to device pixels and the tap is delivered by
  the ordinary driver.
- **A third-party backend is a process**, not a Go plugin: any executable named
  `mobium-driver-<name>` on `PATH`, speaking JSON-RPC
  ([decisions/0003](decisions/0003-drivers-are-processes-not-plugins.md)).

## Packages

Arrows point from a package to the packages it imports. This is the import
graph as it stands, not an aspiration: `internal/apisurface` checks the surface
on every build, and nothing imports `cmd/`.

```mermaid
flowchart LR
    cmd["cmd/mobium<br/>CLI, pipe, mcp"]
    daemon["internal/daemon<br/>socket, PID, idle exit"]
    agent["internal/agent<br/>tools, schemas, @refs"]
    apisurface["internal/apisurface<br/>drift checks"]
    driver["internal/mobiumdriver<br/>backends"]
    webview["internal/webview<br/>CDP, RWI"]
    formflux["internal/formflux<br/>screen profiles"]
    uitree["internal/uitree<br/>nodes, locators, map"]
    device["internal/device<br/>adb, simctl, devicectl, lockdown"]
    plist["internal/plist<br/>property lists"]
    paths["internal/paths"]
    err["internal/mobiumerr<br/>error codes"]

    cmd --> agent & daemon & paths
    daemon --> agent & paths
    apisurface --> agent
    agent --> driver & webview & formflux & uitree & device & paths
    driver --> device & uitree
    webview --> device & uitree & plist
    formflux --> uitree
    device --> plist & paths
```

Every package also imports `internal/mobiumerr`, left out above for
legibility: every failure carries one of its codes, which are public API
([decisions/0005](decisions/0005-errors.md)).

| Package | Responsibility |
| --- | --- |
| `cmd/mobium` | the CLI: flags in, a tool call out. Also `pipe` and `mcp` |
| `internal/daemon` | the long-lived process that holds device sessions and refs |
| `internal/agent` | every tool: its schema, its handler, waiting, scrolling, refusals |
| `internal/uitree` | the platform-neutral hierarchy, locators, `map` and password redaction |
| `internal/mobiumdriver` | the `Driver` interface and its backends, each advertising what it can do |
| `internal/webview` | finding WebViews and speaking CDP or Remote Web Inspector to them |
| `internal/device` | adb, simctl, devicectl, usbmuxd and lockdown; installing the pinned device agents |
| `internal/formflux` | one device impersonating many screens ([FORMFLUX.md](FORMFLUX.md)) |
| `internal/plist` | the property-list codec iOS's inspector and lockdown speak |
| `internal/apisurface` | build-time checks that every tool and flag is reachable from every front door |
| `internal/paths` | socket, PID and session paths, and the OS length limit on them |
| `internal/mobiumerr` | the error codes |

## One call, end to end

`mobium tap @e5`, on Android with the default backend:

```mermaid
sequenceDiagram
    participant CLI as mobium tap @e5
    participant D as daemon
    participant A as internal/agent
    participant U as uiautomator2 driver
    participant S as UiAutomator2 server

    CLI->>D: tools/call app_tap {target: "@e5"}
    D->>A: dispatch
    A->>A: look up @e5's locator from the last map
    loop until settled, or the wait runs out
        A->>U: snapshot
        U->>S: GET /source
        S-->>U: hierarchy
        U-->>A: nodes
        A->>A: resolve the locator to exactly one node,<br/>check it is in view, still, enabled,<br/>and not under a dialog or the keyboard
    end
    A->>U: tap at the node's center
    U->>S: W3C actions
    S-->>U: ok
    A-->>D: result, with any dialog it answered
    D-->>CLI: "tapped @e5 at (540, 930)"
```

The ref is **re-resolved against a fresh snapshot** before the tap. The
coordinates from the earlier `map` are never replayed, because the screen moves
between commands. A locator that now matches nothing, or more than one thing,
is refused with a code rather than tapped.

## Where the units change

Pixels are what `map` bounds, screenshots and taps use on every platform.
WebDriverAgent reports points; the iOS driver converts them to pixels at the
driver boundary, so nothing above `internal/mobiumdriver` sees a point.
WebView coordinates are CSS pixels, converted using the WebView's on-screen
frame and the page's visual viewport.
