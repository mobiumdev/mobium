# Mobium for .NET

Drives native apps on Android emulators, Android phones and iOS simulators
from C#.

```csharp
using Mobium;

using var device = Device.Connect();

device.Launch("com.example.shop");
var signIn = device.WaitFor("text=Sign in");
device.Tap(signIn.Ref);
device.Type("role=input", "someone@example.com");
```

## What it needs

The `mobium` binary on `PATH`, or `MOBIUM_BIN_PATH` pointing at it. Nothing
else — **this package has no NuGet dependencies at all**, which is the point
of it: a library for driving a phone should not hand you a dependency graph.

Targets **netstandard2.0**, so it works from .NET Framework 4.6.1 and up,
.NET Core 2.0 and up, modern .NET, Xamarin and Unity.

## How it talks to mobium

It starts `mobium pipe` and speaks JSON-RPC over its stdin and stdout.

`pipe`, not `mcp`, and that is not a detail: the device-side server holds one
session per device, so a client that started its own would invalidate the
CLI's and the CLI's would invalidate this one. `pipe` forwards to the shared
daemon, which means a script and a terminal drive the same session and see the
same refs.

## Refs

`Map()` hands back elements whose `Ref` looks like `@e5`. A ref is valid only
for the screen it came from.

You rarely need to care. Every action re-resolves its target immediately
before acting, retries briefly while the screen settles, and scrolls to it if
it is below the fold — so a tap can follow another tap without a sleep in
between. Locators work anywhere a ref does:

```csharp
device.Tap("@e5");                 // from the last Map()
device.Tap("text=Sign in");        // resolved fresh
device.Tap(540, 1200);             // device pixels
```

## Waiting

```csharp
device.WaitFor("text=Welcome");                    // visible, ten seconds
device.WaitFor("role=progressbar", Until.Hidden());
device.WaitFor("@e4", Until.Text("Sent").Timeout(TimeSpan.FromSeconds(30)));
```

`Until` rather than `WaitFor` for the condition type, because C# will not let
a method and a type of the same name share a scope — and `Until.Hidden()`
reads better anyway.

Waiting for something to disappear returns `null`: there is nothing left to
point at.

## Errors

Everything throws `MobiumException` or a subclass of it. `Tool` names the tool
that failed, or is empty for a transport-level failure; `Code` is the error
code, `Remedy` what to do about it, `Retryable` whether a retry can help, and
`Details` any machine-readable facts.

Each code has its own subclass — `NoSuchElementException`,
`AmbiguousLocatorException`, `UnsupportedException`, `NotConfirmedException`,
`TimedOutException` (not `TimeoutException`, which is .NET's) and the rest,
the same set in every client (`docs/decisions/0005-errors.md`) — so a test
catches the kind it can handle:

```csharp
try
{
    device.Tap("text=Continue");
}
catch (NoSuchElementException)
{
    device.ScrollTo("text=Continue", "down");
}
```

Catching the base still catches everything:

```csharp
try
{
    device.Tap("text=Nope");
}
catch (MobiumException e)
{
    Console.WriteLine(e.Tool);    // app_tap
    Console.WriteLine(e.Message); // no element matches text=Nope on the current screen
}
```

## Choosing a device

```csharp
using var device = Device.Builder()
    .OnDevice("emulator-5554")
    .Backend("uiautomator2")
    .Connect();
```

Omit both when one device is running, which is the usual case.

## Anything not wrapped

`Call` reaches every tool by name, including ones this class does not have a
method for yet:

```csharp
var result = device.Call("app_map", new Dictionary<string, object>());
```

## Building and testing it

```sh
make dotnet          # from the repository root
```

That builds the library and runs the tests. The tests are a console program
with no test framework, for the same reason the Java client's are: a framework
would mean this client cannot be checked without a NuGet restore and a
network.

Warnings are errors, and XML documentation is generated and required on every
public member — a client nobody has a spare device for is held up by the
compiler or by nothing.

### Two things netstandard2.0 makes you do by hand

Both are in `Connection.cs`, both are tested, and both would be silent if they
were wrong:

- **There is no `ProcessStartInfo.StandardInputEncoding`**, so stdin is
  wrapped in a `UTF8Encoding(false)` writer directly. Otherwise it would go
  out in the console's codepage, and Android's own Settings has a
  non-breaking hyphen in "Wi‑Fi" — a locator carrying one has to arrive
  byte-for-byte or it matches nothing. No BOM either: mobium reads lines.
- **There is no `ProcessStartInfo.ArgumentList`**, so the command line is
  joined and quoted by hand under the CommandLineToArgvW rules. A device
  serial or a path with a space in it would otherwise arrive as two
  arguments.
