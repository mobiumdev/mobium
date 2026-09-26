# 0005 — Every failure has a code

**2026-09-23.** Mobium's failures carry a stable code, on the wire, in the CLI's
exit status, and as an exception in every client. The codes follow W3C
WebDriver's vocabulary where it has a word for the thing.

## What it was

A failure was a sentence. 470 `fmt.Errorf` calls and 13 `errors.New`, two error
types in the whole tool layer, and one place where all of it was flattened to
text: `CallTool`, which returned `isError: true` and a string. Each client
raised one exception type holding that string. The CLI exited 1 for everything,
a mistyped flag included.

The sentences were good — [CONTRIBUTING.md](../../CONTRIBUTING.md) requires each to name a
cause and a remedy that works — but nothing could act on one without reading
it. Three places in Mobium's own code did exactly that, deciding by message
text: a stale session ("invalid session id"), a missing dialog ("no such
alert"), a server that could not type into one ("unknown command"). Meanwhile
WebDriverAgent and UiAutomator2 were sending proper W3C error codes, which
`errorFrom` flattened into the message.

A caller had the same problem with no way around it. "No element matches" and
"no device" and "the phone cannot do that" all arrived as the same exception,
and a test that wanted to scroll on the first, fail fast on the second and skip
on the third had to parse prose to tell them apart.

## What it is

**One type, `internal/mobiumerr.Error`**: a code, the message exactly as it
always read, a remedy, whether a retry can help, machine-readable details, and
the cause it wraps. `mobiumerr.New` formats the way `fmt.Errorf` does, `%w`
included, so converting a site changed its constructor and never its wording.
The outermost code wins: a timeout that wraps a device-server error is a
timeout.

**Fifteen codes.** Adding one is safe; renaming or removing one breaks every
client that catches it, so they are named once and checked.

| Code | Means | Exit |
| --- | --- | --- |
| `no_device` | nothing to drive | 3 |
| `device_not_ready` | there, and not drivable yet: locked, not trusted, Developer Mode off | 3 |
| `toolchain_missing` | missing on this machine: adb, Xcode, a signing certificate, a device agent that would not download or build | 3 |
| `no_such_element` | a locator or ref matched nothing on screen; worth scrolling for | 4 |
| `ambiguous_locator` | matched more than one; narrow it, Mobium never guesses | 4 |
| `element_not_reachable` | found, and not touchable where it is | 4 |
| `no_such_context` | a WebView context that is not there | 4 |
| `no_such_alert` | a dialog was expected and none is up | 4 |
| `unsupported` | this backend or platform cannot, and says why | 5 |
| `timeout` | a wait ran out | 6 |
| `not_confirmed` | the command said it worked and reading the state back disagreed | 7 |
| `invalid_argument` | the request itself is wrong, or cobra refused the command line | 2 |
| `device_server` | the device side failed — a device server, or adb, simctl, devicectl, lockdown — in a way no narrower code names; a server's own W3C code is in `details.w3c` | 1 |
| `internal` | a bug in Mobium | 1 |
| `error` | unclassified: a daemon too old to send a code, or a code a client does not know | 1 |

`not_confirmed` has no W3C counterpart. It is the failure this project exists
to report — `pm grant` succeeding on a permission the app never declared, iOS
dropping a keystroke while the call returned — and it gets a code of its own
rather than being folded into something vaguer.

`device_server` was first proposed as WebDriverAgent and UiAutomator2 only. It
was widened while classifying, because 95 remaining errors were the same kind
of thing from the device's other tools, and a separate code for "adb said no"
would not change what any caller does.

**On the wire**, a failed tool call keeps its text in `content`, so an MCP agent
reading it sees nothing new, and adds `structuredContent`:
`{code, message, remedy, retryable, details}`. `mobium pipe` forwards it
unchanged, which is how the clients get it — **since 2026-09-25**. Until then
this sentence was false: `pipe` built its own failure result without the
structured half, so every client received every failure as its base
exception, and none of the per-code exceptions below had ever been raised
against a real daemon. Both front doors now report a failure through one
function, `agent.ErrorResult` (CHALLENGES 91).

**In the CLI**, the exit status is the group in the table. `--json` prints the
payload beside the `error` key it has always printed.

**In the clients**, one exception per code, named the same in all five:
`NoSuchElementError` in Python and JavaScript, `NoSuchElementException` in Java
and .NET, a sentinel `ErrNoSuchElement` for `errors.Is` in Go. Each extends the
base type that already existed, so code catching the base keeps working. One
name departs from its code: `timeout` is `TimedOut`, because `TimeoutError` is
a Python builtin and `TimeoutException` a Java one, and shadowing either makes
an `except` clause catch the wrong thing without a word.

## How it stays true

- `internal/apisurface` checks that every code has its exception in every
  client, mapped to the right wire string, and that no client carries a code
  the server does not send. A control test removes, renames and remaps one
  exception and watches all three reported.
- It also parses every file under `internal/` and fails on any `fmt.Errorf` or
  `errors.New` that creates an error without a code — one that wraps with `%w`
  passes, since its cause's code shows through. Its control is a file with two
  bare errors and one wrapping one.
- Each client tests its own mapping, with no framework: Go and Java and .NET in
  their existing suites, Python and JavaScript in one small script each that
  `make clients` runs.
- The three decisions made by matching text now read the code.
  `staleSession` keeps its text match as a fallback, for replies that arrive
  with only an HTTP status line.

## Measured

Every code a caller is likely to meet was produced by a real failure, through
the CLI, on the iPhone 15 Plus (iOS 26.6.2) and an iPhone 17 Pro simulator:
`no_such_element` (exit 4), `ambiguous_locator` (4, with `matches: 24` in its
details), `unsupported` (5, appearance on a phone), `timeout` (6),
`no_such_context` (4), `invalid_argument` (2, a bad lock state, an app that is
not installed, and a command line cobra refused), `no_device` (3, a serial that
does not exist) and `device_not_ready` (3) — the last by accident: the phone
was still locked from an iPhone Mirroring test, and every command said so at
once instead of waiting two minutes (defect 80's fix, reached through its code).

**Through the clients**, on 2026-09-25 and not before: `no_such_element`, raised
by tapping a locator that matches nothing, arrives as `NoSuchElementError` in
Python and JavaScript, `ErrNoSuchElement` in Go and `NoSuchElementException` in
Java and .NET — `docs/checks/clients.sh`, against a Pixel 7 AVD. Every code
above was measured through the CLI, which is why the missing half of the pipe
went unseen: the CLI reads the daemon's error directly and never goes through
`pipe`.

Two results needed a second look, and are recorded because the first look was
wrong both times. A tap meant to fail on an off-screen row succeeded, because
Mobium scrolled to it by itself. A wait meant to time out on "Never" succeeded,
because text matching is a substring match and the screen said "whenever".
Both were re-run with targets that could not exist.

Not produced live: `not_confirmed`, `element_not_reachable`,
`no_such_alert`, `toolchain_missing`, `device_server` and `internal` — each
comes from a failure that is hard to cause on purpose on a healthy device.
Where they are tested instead: `not_confirmed` at its site (a keystroke iOS
drops, `wda_test.go`); `element_not_reachable` in the locator tests
(`errors_test.go`); `no_such_alert`, `device_server` and `unsupported` in the
W3C mapping (`TestServerErrorsKeepTheirW3CCode`). `toolchain_missing` and
`internal` are assigned by the classification but asserted nowhere yet; the
client suites prove only that each would arrive as its own exception.
