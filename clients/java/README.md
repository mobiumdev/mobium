# Mobium Java client

Drives mobile apps on Android emulators, Android phones, iOS simulators and
iPhones, over the same tool layer the CLI and the MCP server use.

```java
import dev.mobium.*;

try (Mobium device = Mobium.builder().platform("android").app("com.example.shop").start()) {
    Element signIn = device.waitFor("text=Sign in");
    device.tap(signIn.ref());

    device.type("testid=email", "someone@example.com");
    device.tap("text=Continue");

    assert device.current().equals("com.example.shop");
}   // try-with-resources quits: the session on the device ends here
```

`start()` opens the session on the device and launches the app fresh;
`quit()` — or the end of try-with-resources — ends
it. `Mobium.connect()` opens a connection without touching the device, and
its `close()` leaves the session open for whoever started it. The
[quick start](../../docs/quickstart/java.md) walks through it on Android and
iOS.

Requires Java 17+ and the `mobium` binary on `PATH`, or `MOBIUM_BIN_PATH`
pointing at it. The current directory is never searched for it.

## Installing

Maven:

```xml
<dependency>
  <groupId>dev.mobium</groupId>
  <artifactId>mobium</artifactId>
  <version>0.1.0</version>
  <scope>test</scope>
</dependency>
```

Gradle:

```kotlin
testImplementation("dev.mobium:mobium:0.1.0")
```

The jar declares `Automatic-Module-Name: dev.mobium` for the module path, and
ships with sources and javadoc.

## No dependencies, on purpose

Not even a JSON library. A test harness that drags Jackson or Gson onto the
classpath will sooner or later collide with the version the application under
test already uses, and the person who has to untangle that is you. There is a
small JSON reader in `Json.java` instead, and it is tested rather than
trusted.

The tests have no dependencies either: they are a main method, which the
build runs in its `test` phase, so not even a test-scope framework is needed.
The build's enforcer fails if the client ever acquires a dependency, in any
scope, so this is checked rather than promised.

## Building

```sh
./mvnw verify        # from clients/java; or `make java` from the repository root
```

`./mvnw` is the Maven wrapper. On first use it downloads a pinned Maven,
checked against its SHA-256 (in `.mvn/wrapper/maven-wrapper.properties`),
into `~/.m2/wrapper`, so nothing needs installing but a JDK. `verify`:

- compiles with `-Xlint:all -Werror`,
- runs the tests,
- builds the jar, a sources jar and a javadoc jar, with javadoc's doclint on
  and its warnings fatal, so every public member is documented,
- and fails on any dependency, and on any plugin whose version is not pinned.

The build is reproducible: two clean builds of the same source produce
byte-identical jars (`project.build.outputTimestamp` in `pom.xml`).

The end-to-end flow needs a device: `MOBIUM_E2E_DEVICE=<serial>
MOBIUM_BIN_PATH=../../bin/mobium ./mvnw test-compile exec:exec@e2e`, which is
what `docs/checks/clients.sh` runs.

## Releasing

To Maven Central through the Central Portal, as `dev.mobium`. Nothing is
published by an ordinary build: signing and upload live in the `release`
profile, and the upload waits in the Portal to be published by hand.

Once:

1. Sign in at [central.sonatype.com](https://central.sonatype.com), add the
   namespace `dev.mobium`, and put the TXT record it gives you on
   `mobium.dev`'s DNS. The namespace is verified by that record.
2. Generate a Portal user token and add it to `~/.m2/settings.xml`:

   ```xml
   <settings>
     <servers>
       <server>
         <id>central</id>
         <username>TOKEN-USERNAME</username>
         <password>TOKEN-PASSWORD</password>
       </server>
     </servers>
   </settings>
   ```

3. Have a GPG key, and publish its public half to a keyserver
   (`gpg --keyserver keys.openpgp.org --send-keys <id>`); Central checks
   signatures against it.

Each release:

1. Set `<version>` in `pom.xml` to the release (drop `-SNAPSHOT`), and
   `project.build.outputTimestamp` to the date.
2. `./mvnw -P release deploy` — builds, tests, signs, and uploads the bundle.
3. Check it in the Portal's Deployments page, then Publish. Only that step is
   irreversible: a version published to Central can never be changed or
   removed.
4. Move `<version>` on to the next `-SNAPSHOT`.


## What it covers

The whole tool surface, and `call()` for anything not wrapped yet.

| | |
| --- | --- |
| Reading | `map`, `mapDiff`, `find`, `text`, `current`, `screenshot`, `devices` |
| Waiting | `waitFor`, with `WaitFor.visible()`, `.hidden()`, `.text(…)` |
| Scrolling | `scrollTo` |
| Acting | `tap`, `type`, `replace`, `swipe`, `longPress` |
| Apps | `launch`, `terminate`, `install`, `uninstall`, `openUrl`, `apps` |
| Files | `upload`, `download`, `downloads` |
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
with `java.util`'s. The codes are the same in every client;
see [the codes](../../docs/guides/cli.md#6-when-a-command-fails).

### Threads, timeouts and closing

One `Mobium` is one pipe to one device. It is safe to share between threads,
but calls are **serialized**, one at a time, never interleaved: replies on the
pipe are told apart only by id, and before that, two requests written at once
land on one line and mobium can read neither.

A call waits as long as the tool takes, which is unlimited by default because
the first session on an iPhone builds WebDriverAgent and that takes minutes.
To bound it:

```java
try (Mobium device = Mobium.builder().callTimeout(Duration.ofMinutes(2)).connect()) {
    …
}
```

A call that runs out **ends the connection**. A late answer to an abandoned
call would otherwise be read as the answer to the next one, so every later
call throws, saying the connection is no longer usable, and you connect again.
That is cheap, because the device session lives in the daemon, not in this
object. The same goes for mobium exiting mid-call. Set the timeout well above
the longest `WaitFor.timeout` you use.

`close()` closes the pipe and waits up to ten seconds for mobium to exit. It
is safe to call twice, and calling it from another thread ends a call that is
still waiting.

A `null` passed where a value is required throws `InvalidArgumentException`
naming the argument, before anything is sent.

The Maven wrapper scripts (`mvnw`, `mvnw.cmd`) are Apache Maven's, under the
Apache License 2.0, as their headers say; the client itself is MIT.

Waiting for something to *disappear* returns `null`, because there is nothing
left to point at.
