# Mobium Java client

Drives native apps on Android emulators, Android phones and iOS simulators,
over the same tool layer the CLI and the MCP server use.

```java
import dev.mobium.*;

try (Mobium device = Mobium.connect()) {
    device.launch("com.example.shop");

    Element signIn = device.waitFor("text=Sign in");
    device.tap(signIn.ref());

    device.type("testid=email", "someone@example.com");
    device.tap("text=Continue");

    assert device.current().equals("com.example.shop");
}
```

Requires Java 17+ and the `mobium` binary on `PATH`, or `MOBIUM_BIN_PATH`
pointing at it.

## No dependencies, on purpose

Not even a JSON library. A test harness that drags Jackson or Gson onto the
classpath will sooner or later collide with the version the application under
test already uses, and the person who has to untangle that is you. There is a
small JSON reader in `Json.java` instead, and it is tested rather than
trusted.

The tests have no dependencies either, so the client can be built and checked
with `javac` and `java` alone — no Maven, no network. `make java` from the
repository root does exactly that.

## What it covers

The whole tool surface, and `call()` for anything not wrapped yet.

| | |
| --- | --- |
| Reading | `map`, `find`, `text`, `current`, `screenshot`, `devices` |
| Waiting | `waitFor`, with `WaitFor.visible()`, `.hidden()`, `.text(…)` |
| Scrolling | `scrollTo` |
| Acting | `tap`, `type`, `replace`, `swipe`, `longPress` |
| Apps | `launch`, `terminate`, `install`, `uninstall`, `openUrl`, `apps` |
| Device state | `grant`, `revoke`, `resetPermissions`, `appearance`, `orientation`, `appLocale`, `press`, `screenLocked`, `incomingCall`, `sms`, `timezone`, `notifications`, `shade` |
| Contexts | `contexts`, `context`, `logs`, `eval` |
| Diagnostics | `doctor` |

Refs like `@e1` are valid only for the screen they came from. Every action
re-resolves its target immediately before acting, retries briefly while the
screen settles, and scrolls to it if it is below the fold — so a tap can
follow another tap without a sleep.

## Notes

`Mobium` is `AutoCloseable`; use try-with-resources or the subprocess outlives
your test.

`MobiumException` is unchecked. Every call here can fail — a device can be
unplugged mid-flow — and a checked exception on each one would make a readable
test unreadable without making it safer. It carries the tool that failed in
`tool()`, the error code in `code()`, what to do about it in `remedy()`, and
whether a retry can help in `retryable()`.

Each code has its own subclass, so a test catches the kind it can handle:

```java
try {
    device.tap("text=Continue");
} catch (NoSuchElementException e) {      // dev.mobium, not java.util
    device.scrollTo("text=Continue", "down");
}
```

`NoDeviceException`, `DeviceNotReadyException`, `ToolchainMissingException`,
`NoSuchElementException`, `AmbiguousLocatorException`,
`ElementNotReachableException`, `NoSuchContextException`,
`NoSuchAlertException`, `UnsupportedException`, `NotConfirmedException`,
`TimedOutException` (not `TimeoutException`, which is Java's),
`InvalidArgumentException`, `DeviceServerException`, `InternalException`.
Import `dev.mobium.NoSuchElementException` by name: it shares its simple name
with `java.util`'s, as Selenium's does. The codes are the same in every client;
see `docs/decisions/0005-errors.md`.

Not thread-safe: there is one pipe underneath, and one device.

Waiting for something to *disappear* returns `null`, because there is nothing
left to point at.
