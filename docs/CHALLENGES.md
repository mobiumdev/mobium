# Challenges

Platform behaviors found while building Mobium, what each one broke, how it
was caught and how it is handled now. Code comments cite entries here by
number — "CHALLENGES 105" — so each entry is the long form of a decision in
the source.

The pattern across them is the reason the document exists: **almost nothing
here was found by reading code or by a test written from imagination.** Of
206 defects, 170 were found only by running against a real device. The other
thirty-six — 4, 5, 6, 14, 23, 24, 32, 35, 36, 39, 44, 50, 53, 54, 57, 66, 89,
99, 100, 121, 123, 129, 130, 133, 134, 139, 140, 141, 142, 144, 150, 158, 162,
164, 165 and 172 — came from reading code, the compiler, a test, a linter,
cross-checking a computed number against a screenshot, using the tooling on
itself, typing a negative number at a command line, and driving the clients
against a stand-in daemon, CI on Windows, following the quick start from
a fresh clone, and type-checking the documentation site's examples.

Read it before writing a test that asserts platform behavior.

---

## The recurring pattern

Three times a carefully built fixture produced confident, wrong behavior that
a real screen corrected within minutes:

| Fixture said | The device said |
| --- | --- |
| clickable `TextView` is a link | every app icon on the launcher is one — they are buttons |
| one element per rectangle | widgets stack two actionable nodes on identical bounds |
| layout viewport == visual viewport | 1648 vs 412 CSS px on a wide page |
| iOS actionability is a set of element types | the home screen is 23 `XCUIElementTypeIcon`, which was not on the list — **zero elements mapped** |
| child bounds overflowing the parent reveal which way a container scrolls | Android clips child bounds to the parent; every scrollable on the launcher and in Settings reported **zero overflow in both axes** |
| if the screen reads the same, the list did not move | the About page shows an uptime counter, so it always read differently — 15 swipes at a list pinned to the bottom |
| `pm grant` reporting success means the app has the permission | it exits 0, prints nothing and changes nothing when the app never declared it |
| Apple's tooling reports failure properly | `simctl privacy` prints the error on stderr and exits 0, and grants to an app that is not installed |
| the captured hierarchies can say whether Android ever marks a node hidden | not one of the four carries a single `displayed="false"`, so the question was unanswerable from them; ten live screen states answered it — 39 |

The fixtures were not sloppy. The first was modeled on a real login screen and
included deliberate edge cases. It still encoded the author's assumptions,
because that is what a hand-written fixture is.

**Working rule:** a fixture may prove logic self-consistent. It may not
establish platform semantics. Anything of the form "elements of type X behave
like Y" needs a device.

**And its corollary, learned later:** a fixture cannot establish that something
*never* happens either. The absence of a value in four captures is not evidence
the platform never emits it — it is evidence that nobody captured a screen
where it does. Defect 39 needed ten deliberately-chosen live screens to turn
"we have not seen it" into "the server does not emit it".

---

## If you are seeing something odd, start here

Symptom first, because that is what you have. Every row points at the entry
with the evidence.

| What you are seeing | What it usually is |
| --- | --- |
| A command "succeeded" and nothing changed | Nothing here reports failure by exit code. `pm grant`, `simctl privacy`, `am start`, `adb uninstall` and `uiautomator dump` each fail differently — 25, 26, 27, 30 |
| An element is on screen but a locator says it is not | It matched more than once and the message said "no element matches" — 29; or it resolved but sits outside the container being judged — 33 |
| A tap lands slightly wrong | The element was still moving; actions now wait for stable bounds — 109 and 112, and 17 for iOS points against pixels |
| `map` shows nothing useful, or every icon is a "link" | Role inference — 1, 4, 15, 16 |
| Scrolling swipes forever, or refuses to scroll | A ticking clock defeating the progress check — 22; or a container the element is not inside — 33 |
| The dump backend cannot read a screen at all | It waits for idle, and some screens never are — 25 |
| A phone is plugged in and `adb devices` is empty | USB debugging, not the cable. `mobium doctor` says so — [SETUP.md](SETUP.md) |
| The first command after a rebuild fails | A daemon serving the old binary — [SHUTDOWN.md](SHUTDOWN.md) |
| Coordinates are out by 3x, or a quarter | iOS points against pixels — 17; or the layout viewport against the visual one — 6 |
| Something works on the emulator and not on a phone | Real hardware is ~10x slower, and apps ship at different versions — the Clock entry below |
| A read on a real iPhone takes a minute and shows the app you just left | The switch stall — 71. Mobium avoids it for `launch` and Home; after anything else that changes app, the first read can still stall |
| `scroll-to` refuses to swipe for a row you know is further down | A row off screen reported with no bounds — 72, fixed; if it is back, the refusal is being raised before the scroll |
| A check passes that should not | Read it again before believing it. 35 and 44 are both ours doing exactly that |
| A password, token or PIN appears in output | It should not. 43 — the platform marks these fields and Mobium now honors it; if you see one, the marking is missing or a new read path skipped `uitree.Redact` |
| `map` offers something that is not on screen | Visibility. Android never reports it, iOS does, and a driver may — 39 |
| A measurement comes back "zero" or "no difference" | Show it can report non-zero before believing it. A probe reported 0 for four apps and had never left the launcher — 39 |
| An app "launched" and the screen did not change | `monkey` is a fuzzer, not a launcher: chatter on stdout, exit 0 for a package that does not exist — 37. Resolve the activity and `am start -n` it |
| A third-party driver's answer is ignored | Only advertised capabilities are wired up; `mobium doctor` lists drivers found, and the refusal names the backend — [the driver protocol](../examples/drivers/PROTOCOL.md) |

---

## Defects found

### 1. `role=link` mislabelled the entire launcher
**Step 1. Found by:** first `map` against a real emulator.

A clickable `TextView` was classified as a link, on web instinct. On Android
every app icon is a clickable `TextView`, so the whole home screen came back as
links. Native Android has no link widget. Clickable TextViews are buttons;
`role=link` is retained only for WebView content.

### 2. Stacked nodes on one rectangle
**Step 1. Found by:** reading real `map` output and noticing two rows with the
same bounds.

The launcher's "At a glance" widget is a long-clickable `ViewPager` wrapping a
clickable `ViewGroup` on identical bounds. Both mapped, and their labels
disagreed — the outer knew the widget's name, the inner had borrowed the date
shown inside it. Actionable nodes sharing a rectangle now collapse to one
entry: innermost clickable node as the tap target, most specific self-carried
label as the name. This subsumed an earlier hand-written wrapper special case,
which had missed exactly this shape.

### 3. Scroll containers labeled by their contents
**Step 1.** A `RecyclerView` was labeled `Continue Continue`, having borrowed
descendant text. A container's contents are not its name.

### 4. Android/iOS role table collision
**Step 4. Found by:** the compiler, while writing the iOS role map.

`TextView` is a read-only label on Android and a multi-line text **input** on
iOS. One suffix-matching table would have classified half of every Android
screen as an input. Roles now resolve against a separate table per platform.

### 5. `parse_ios.go` was silently excluded from every build
**Step 4. Found by:** the compiler refusing to see a function that was plainly
there.

Go reads a trailing `_ios` in a filename as a `GOOS` build constraint. The file
compiled for nobody. Renamed to `parse_xcui.go`. `_android`, `_windows`,
`_linux` and friends are the same trap — this is the kind of thing that ships
silently if the file happens to have no callers yet.

### 6. The visual viewport
**Step 5. Found by:** computing where Mobium thought a link was, taking a
screenshot, and noticing the numbers disagreed.

WebView coordinates must scale by the page's **visual** viewport, not its
layout viewport. On an ordinary page they are identical — example.com reported
412 for both, so the first probe looked perfect. Android's own third-party
licenses page reports a layout viewport of **1648** CSS px against a visual
viewport of **412**: scaling by the wrong one put every tap at roughly a
quarter of the correct offset.

No test caught this and no test could have, because the test would have used
the same wrong assumption. It took an independent measurement — a screenshot —
to see it.

### 7. Client sessions collided with the CLI's
**Step 6. Found by:** running the first Python client against a live device.

The client spawned `mobium mcp`, which runs handlers in-process. That started a
second UiAutomator2 instrumentation on the same device, took over the
device-side server, and left the daemon holding a session id the server no
longer knew. Both UiAutomator2 and WebDriverAgent hold exactly **one session
per device**.

Fixed by adding `mobium pipe`, which forwards to the shared daemon. This is why
Vibium has a `pipe` command and a rule against clients shelling out to anything
else — a rule that had been copied without being understood.

### 8. Health checks passed while the session was dead
**Step 6, exposed by 7.**

`Healthy()` pinged `/status`, which reports ready when the *server* is up. The
*session* being gone is invisible to it. The shared W3C client now recovers: a
call failing with "invalid session id" reopens the session, rewrites the dead
id out of the request path, and retries once. Without it, anything else
attaching to the device — a second Mobium, or another tool — breaks every subsequent
command until the daemon restarts.

### 9. Stale sessions across emulator restarts
**Step 3. Found by:** quitting the emulator mid-session.

Emulator serials are deterministic (`emulator-5554`), so restarting an emulator
hands back the same serial with a different device behind it. The daemon's
cached session pointed at a server that died with the old emulator. Cached
driver sessions are now health-checked before reuse.

### 10. UiAutomator2's screenshot is slower than adb's
**Step 3. Found by:** benchmarking both rather than assuming the newer path was
faster.

0.67s through the server's `/screenshot` (it base64s the image through HTTP)
against 0.13s for `adb exec-out screencap`. Screenshots go through adb even on
the UiAutomator2 backend. The same reasoning later applied to iOS, where
screenshots go through `simctl`.

### 11. `adb shell input text` cannot type
**Step 3. Found by:** trying it, after asserting it in a comment.

```
$ adb shell input text "O'Brien & Sons — 100% \"off\"; café"
/system/bin/sh: no closing quote        # field left empty
```

It types into whatever holds focus and dies on shell metacharacters. The
`uiautomator` backend therefore **declines** text entry and names the fix,
rather than implementing it badly. This is the clearest case of the
refuse-don't-approximate rule paying for itself.

### 12. Chrome is not a hybrid app
**Step 5. Found by:** using Chrome as the first WebView test subject.

Chrome renders into a compositor view, not an `android.webkit.WebView`, so
there is no native host rectangle to map coordinates into. Browsers are out of
scope by construction — that is Vibium's job. The real test subject was
Settings' third-party licenses viewer, which uses a genuine WebView.

**No longer true, measured 2026-09-27:** Chrome 124 on Android 15 reports its
page area as an `android.webkit.WebView` node bounded to the content below
the toolbar, and a tap through its `chrome_devtools_remote` context lands.
Browsers are still out of scope; they are not out of reach.
[APP-TYPES.md](APP-TYPES.md#mobile-web) has what was driven.

### 13. `uiautomator dump` fails while UiAutomator2 is running
**Step 5. Found by:** trying to dump the hierarchy for debugging and getting a
0-byte file.

The UiAutomator2 server holds the `UiAutomation` connection; `uiautomator dump`
cannot get it and fails while reporting nothing useful. Stop the daemon before
dumping by hand.

### 14. Unix socket path length
**Step 2. Found by:** the guard firing during tests.

`sockaddr_un.sun_path` caps at ~104 bytes. Go's `t.TempDir()` on macOS embeds
the test name under `/var/folders/...`, so tests with long names overran it
while short-named ones passed — three failed, three did not, from one cause.
The guard ported from Vibium caught it with an actionable message. Tests use a
short temp dir; real users with a deep `MOBIUM_HOME` get the same message.

### 15. iOS mapped zero elements
**Step 4, verified late. Found by:** the first `map` against a simulator.

The iOS actionability rule whitelisted element types a user "operates
directly". The iPhone home screen is 23 `XCUIElementTypeIcon`, and `Icon` was
not on the list, so `map` returned *No actionable elements found* — the
whole screen invisible.

Two further things the fixture had wrong, both invented in good faith from
WebDriverAgent's documentation:

- **`hittable` is not emitted at all.** The parser was built around it as iOS's
  answer to "would a tap reach this". A real simulator never sends it.
- **What *is* emitted is `accessible`** — the platform's own answer to "is this
  one distinct thing a user deals with". On the home screen it selects the app
  icons and the search pill and nothing else, which is exactly right.

The fix replaces the type whitelist with `accessible && visible && sized`,
minus a small set of content types (StaticText, Image, and structural
containers). Enumerating what counts is the same mistake `role=link` was on
Android; asking the platform is not.

The hand-written fixture is gone, replaced by a verbatim capture from an
iPhone 17 Pro on iOS 26.5. It maps zero elements under the old rule, which is
how the bug is now pinned.

### 16. `role=input` matched prose
**Step 4, same session. Found by:** `find role=input` returning four matches on
Safari's start page, three of them paragraphs of explainer text.

iOS renders read-only prose as `XCUIElementTypeTextView`, the same type used
for editable multi-line fields. An agent trusting `role=input` would try to
type into an explainer. `TextView` is no longer part of the `input` role;
`TextField`, `SecureTextField` and `SearchField` remain. The cost is that a
genuinely editable TextView is missed by `role=input` — it is still mapped and
tappable, and a testid or label reaches it.

### 17. iOS coordinates were points; screenshots were pixels
**Found by:** noticing the two numbers could not both be right, after the
first successful iOS run.

WebDriverAgent reports geometry in **points** (402x874 on an iPhone 17 Pro)
while `simctl` screenshots are **pixels** (1206x2622 — exactly 3x). Taps were
internally consistent, because WDA consumes points too, so nothing failed. But
a `map` bound and a screenshot pixel did not correspond, and Android has no
such split — both are pixels there.

The dangerous shape of this one is that it never errors. An agent screenshots
a screen, reads a button at pixel (706, 2416), calls `tap 706 2416`, and on
iOS lands a third of the way down the screen. On Android the same code is
correct.

Fixed by making **device pixels the coordinate space on both platforms**: the
iOS tree is scaled up on read and gestures are scaled back down before they
reach WebDriverAgent. The factor comes from WDA's own `/wda/screen`, which
reports `"scale": 3` — one cheap call at session start, rather than parsing
`simctl io enumerate` or guessing from a device profile. An implausible value
is refused rather than silently multiplying every coordinate.

Verified on the device: `tap label=Safari` moved from (157, 805) to
(471, 2416) — exactly 3x, and matching where the screenshot showed the icon —
and tapping the raw pixel coordinate (706, 2416) read off a screenshot opened
Messages.

### 18. A tool call could not outlast its own setup
**Found by:** the first `app_current` after a daemon restart failing with
`context deadline exceeded`, having done nothing wrong.

`callTimeout` bounded a whole tool call at 90s, and `wdaReadyTimeout` bounded
just the wait for WebDriverAgent to answer at 90s as well. A cold start could
therefore spend the entire call budget waiting for the server and have nothing
left for the install before it, the session, or the work itself.

Nested timeouts have to be ordered, and this pair was not. `callTimeout` is now
240s, with a guard test asserting it stays at least twice every readiness wait.
Both drivers also report progress before waiting, which explains the pause and
extends the client's read deadline past it.


### 19. A wait found the element but could not hand back a ref
**Found by:** running `mobium wait "text=Network & internet"` on the emulator.

The wait succeeded and reported no element, while the ref table it had just
written did contain the row — a `tap @e3` straight afterwards worked. The wait
matched the `TextView` carrying the text; `map` collapses the row and hands out
a ref for the clickable parent, so an exact node match found nothing.

Waits now walk up to the nearest mapped ancestor. This is what makes
`wait_for` a replacement for `sleep; map` rather than a call in front of it.

### 20. `am start` returns before the app is on screen
**Found by:** driving the Python client, once it had lifecycle methods to
drive with.

`launch("com.android.settings")` followed by `current()` answered
`com.android.chrome` — the app being replaced. `am start` reports that the
intent was dispatched, not that anything is in the foreground; the same is true
of `simctl`, and `open_url` had it too.

Both now wait for the app to arrive, bounded at three seconds. A launch knows
what it is waiting for. A URL does not, so it settles for either a change of
app or a change of screen, and treats a link that does nothing visible as fine
rather than as a failure. A launch that ends up behind a permission dialog or
a chooser now says so instead of reporting plain success.

The general form is worth keeping: **a command that starts something is not
finished when it returns.**

### 21. Nothing in the hierarchy says which way a container scrolls
**Found by:** a vertical swipe on the launcher opening the app drawer instead
of scrolling, then measuring rather than guessing.

The plan was to detect the scroll axis from child bounds overflowing the
container. That signal does not exist: Android clips child bounds to the
parent. Measured across every scrollable node on the launcher and on two
Settings screens, **overflow was zero in both axes every time** — a horizontal
pager and a vertical list are indistinguishable from outside.

So scrolling was vertical only, and said so. The damage from guessing wrong is
caught after one swipe by watching the foreground app: leaving the app
mid-scroll means the swipe navigated rather than scrolled.

**Horizontal scrolling landed on 2026-09-18, and the measurement above is
exactly why it works the way it does.** The signal still does not exist, so
nothing infers the axis — `app_scroll_to` takes `direction` as `left` or
`right` and the caller supplies what only the caller knows. That is the whole
change: one branch in `swipeWithin` and a wider set of accepted directions.
The years of nothing happening here were not a missing gesture, they were the
wrong question — the tool had been waiting to *detect* something undetectable
instead of asking.

Verified on both platforms the same day, and the two needed different
locators for the same card: React Native renders one labelled `Pressable` as a
single labelled node on Android and **three** on iOS, so the plain label is
ambiguous there and mobium refuses it rather than tapping the first. That is
the uniqueness rule doing its job on a hierarchy shape it had not met — the
same family as [55](#55-text-entry-failed-on-a-field-map-had-just-handed-out-a-ref-for).

The navigation guard matters more on this axis than the one it was written
for: a horizontal drag starting at a screen edge is the system back gesture on
both platforms, which is why the swipe starts a quarter of the way in and why
the check asserts that the *wrong* axis fails rather than finding the card by
luck.

This is the clearest case yet of the working rule earning its place. The
heuristic was reasonable, cheap to implement, and would have shipped.

### 22. A ticking clock defeated end-of-list detection
**Found by:** noticing that the scrollbar was already at the bottom while
mobium swiped at it fifteen times.

Scrolling detects the end of a list by checking whether anything moved. The
check compared everything the container was showing — text and bounds — and
the About screen has an uptime counter that ticks every second. Every
comparison found a difference, so the list looked like it was moving forever.
Cost: 15 swipes and ~20s where the answer was available after 1.

Position is now the evidence, with one changed string treated as a clock
rather than as movement. Geometry alone would not do either: a recycled list
can hand its rows back at identical coordinates and change only what they say.
So it takes both — moved if anything is in a different place, **or** if more
than one thing is saying something new.

### 23. Resolving a locator is not the same as being able to touch it
**Found by:** a test, which is rare enough here to be worth noting.

A row scrolled past the end of its list is still in the hierarchy with real,
non-empty bounds. `pickOne` was happy with it, so a tap computed a center point
below the visible area and touched nothing. The test written for scroll-then-tap
failed because no scrolling happened at all: resolution had already "succeeded".

Actions now check that the resolved node lies wholly inside its scroll
container, and scroll when it does not. A related distinction came out of the
same change: a miss may be below the fold and worth scrolling for, while an
ambiguous match is already on screen twice and scrolling can only make it worse
while leaving the list somewhere else. Only a miss scrolls.

### 24. Two tools returned untyped maps
**Found by:** reading the code while giving the clients something to decode.

`app_install` and `app_open_url` answered with `map[string]string`, which
`structured.go` opens by saying the wire shapes are a contract chosen once and
changed deliberately. Untyped maps are how a contract stops being one. Both
have declared views now.

### 25. The dump backend cannot read a screen that never goes idle
**Found by:** chasing defect 22, then checking rather than assuming.

`uiautomator dump` waits for the screen to stop changing before it snapshots.
The About page on a Pixel 7 shows an uptime counter, so it never does:
reproduced three times out of three, with no UiAutomator2 instrumentation
running, `ERROR: could not get idle state.` and no file. The `uiautomator`
backend cannot read that screen at all. UiAutomator2 reads it — not because it
does not wait for idle, as was written here, but because a counter that ticks
once a second leaves it idle often enough; continuous motion does not
(CHALLENGES 109).

Worse, the reason was invisible. Defect 13's fix reads uiautomator's failures
off **stdout**, because that is where it puts "killed" — but this one goes to
**stderr**, and `ADB.Run` only surfaces stderr that adb itself prefixes with a
lowercase `error:`. The user saw `uiautomator dump produced no file:` with
nothing after the colon.

`ADB.ShellDiagnostics` now returns both streams from a single run, and the
backend names both the cause and the fix. The general lesson is the same as
defect 13's, one level down: a device-side command splits its diagnostics
across both streams and follows nobody's conventions, so read both.

### 26. `pm grant` succeeds loudly at doing nothing
**Found by:** probing the command's failure modes on the emulator before
building on it.

Three separate behaviors, none of which a caller can see:

| Asked for | What happens |
| --- | --- |
| a permission the app never declared | exit 0, no output, **no change** |
| a non-runtime permission | `SecurityException` on **stderr**, exit 0 |
| an unknown permission name | `IllegalArgumentException` on **stderr**, exit 0 |

The first is the dangerous one: `pm grant com.android.settings
android.permission.BODY_SENSORS` reports success and Settings still does not
have it, because it never asked for it. Anything that trusts the command is
reporting its own hope.

So permissions are read back. `dumpsys package <pkg>` has a `runtime
permissions:` block listing every runtime permission the app declares with its
granted state, which serves as both the set `all` expands to and the check
afterwards. A permission the app does not declare is reported as skipped, and
a write that reports success while the state does not move is an error.

The stderr half is defect 25 again in a different command, and the fix from it
— `ADB.ShellDiagnostics` — is what makes those two visible.

### 27. simctl has the same trap, and our client had the same hole
**Found by:** running the same probes against a booted iPhone 17 Pro rather
than assuming Apple's tooling is better behaved.

`simctl privacy <udid> grant nonsense <bundle>` prints "An error was
encountered processing the command" to **stderr** and exits **0**.
`Simctl.Run` only reported stderr when the exit code was non-zero, so every
such failure across every simctl call — launch, install, openurl, privacy —
had been invisible. It now treats that marker as a failure whatever the exit
code says.

And a second silent one: `simctl privacy` accepts a bundle id for an app that
is **not installed**, and does nothing, successfully. Unlike Android there is
no privacy state to read back, so the check has to happen first — granting to
an app that is not there is now refused rather than reported as done.

The pattern across 25, 26 and 27 is worth stating plainly: **every device
tool in this project — adb, pm, uiautomator, simctl — reports at least some
failures on stderr while exiting 0.** Treat a zero exit as meaning nothing at
all.

### 28. `mobium daemon stop` returned before the daemon stopped
**Found by:** hitting it three times while doing other work, then reproducing
it deliberately instead of retrying past it.

`daemon stop` sent the shutdown request and returned as soon as it was
acknowledged, while the daemon was still tearing down — closing a UiAutomator2
session takes about a second. The next command's auto-start then found the
socket still held, failed to bind, and sat waiting ten seconds for a daemon
that had already given up. `stop` followed immediately by any command failed
**five times out of five**; one second of sleep in between hid it completely.

`Shutdown` now waits for the process to actually exit before returning, and
cleans up after it. The contributing guide says to run `mobium daemon stop`
after every rebuild, so this was in the path of the project's own instructions.

The general form is the one from defect 20, mirrored: **a command that stops
something is not finished when it returns either.**

### 29. An ambiguous locator was reported as missing
**Found by:** verifying waits and scrolling on iOS, which is exactly what that
roadmap item existed to do.

`scroll-to "label=Privacy & Security"` on an iPhone 17 Pro answered "no
element matches … scrolled down to the end of the list without finding it".
The row was on the screen. What had actually happened is that a Settings cell
and the static text inside it carry the *same* label, so the locator matched
twice, and the scroll loop treated every resolution failure alike: not here
yet, keep swiping. It pointed at the wrong place twice before the hierarchy
was read.

Two of something is not none of it, and no amount of scrolling turns one into
the other. Resolution failures are now told apart — nothing matched, matched
but not yet in view, or matched too much — and only the first two are worth
scrolling for. The third fails immediately with the real reason.

Worth noting where the defect was: **in the shared layer, not the iOS one.**
The same ambiguity is possible on Android and simply had not come up, because
Android's `collapseByBounds` merges a label with its row when they share a
rectangle, and here the cell and its text have different bounds. Verifying a
feature on a second platform found a bug that belonged to both.

### 30. adb's failure reason lands on whichever stream it feels like
**Found by:** uninstalling a system app, to check the guard worked.

`mobium uninstall com.android.settings` failed — correctly — with
`adb uninstall com.android.settings: exit status 1:` and nothing after the
colon. The reason, `Failure [DELETE_FAILED_INTERNAL_ERROR]`, was on **stdout**
while `ADB.Run` built its error from stderr, which was empty.

This is the fourth spelling of the same problem, after defects 25, 26 and 27:
`am start` puts errors on stdout with exit 0, `uiautomator dump` splits them
across both with exit 0, `pm grant` uses stderr with exit 0, and `adb
uninstall` uses stdout with exit **1**. There is no convention. `Run` now
falls back to stdout when a non-zero exit leaves stderr empty, which fixes it
for every adb command at once.

### 31. A hierarchy read fails while the screen animates, and that was fatal
**Found by:** tapping the same row repeatedly while adding stable-bounds
waiting, which produced toasts, which animate.

UiAutomator2 intermittently answers `unknown error: Cannot set
AccessibilityNodeInfo's field 'mSealed' to 'true'` while the screen is
changing, and succeeds a moment later — observed failing and passing
alternately on the same command. Mobium treated any snapshot error as final,
on the reasoning that a device which cannot be asked will not answer later.
That reasoning is wrong for this one, and stable-bounds waiting made it worse:
the extra snapshot it takes is by definition taken when something is moving.

Snapshot errors inside an action's retry budget are now retried, with the last
one kept so a device that is genuinely unreachable still reports what it said
rather than a generic timeout.

### 32. `doctor` could not run in the situation it exists for
**Found by:** testing it against a deliberately broken environment, which is
the only way this would ever have shown up.

The first version of `mobium doctor` went through the daemon like every other
command. Pointed at a `MOBIUM_HOME` long enough to push the Unix socket path
over the OS limit, it failed with *that* error instead of producing a report —
the error it exists to explain. A diagnostic that only works on a healthy
machine is not a diagnostic.

It runs in-process now. It touches no device and needs no shared session, so
there was never a reason for it to need the daemon beyond consistency with
commands that do.

### 33. Visibility was judged against a scroll container the element is not in
**Found by:** the first tap on a real phone, a Pixel 8 Pro — which is exactly
what real-device support was supposed to be checking.

`mobium wait "label=7"` found Calculator's 7 key immediately. `mobium tap
"label=7"` answered "is on the screen but never scrolled fully into view",
having swiped at something first.

Calculator's only scrollable is the history strip across the top,
`[0,0][1008,285]`. The 7 key is at `[9,1218][249,1452]`, outside it and with
**no scrollable ancestor at all**. The check added with scroll-to-tap took
"the largest scrollable on screen" and asked whether the element was inside
it — so any element not in that container read as scrolled out of view, and
the fix it tried was to scroll a list the element has nothing to do with.

The container that matters is the one the element is *inside*. Visibility is
now judged against the element's nearest scrollable **ancestor**, and an
element with none cannot be scrolled to however many scrollables the screen
has elsewhere.

This would have hit any screen with a fixed button bar below a scrolling list,
which is most of them. Neither the emulator screens used to build scrolling
nor any fixture caught it, because both happened to put the target inside the
one scrollable on screen. A real phone found it in the first minute.

### A working end-to-end check: 1 + 1 = 2
**Not a defect.** Recorded because the two false results on the way to it are
the more useful part.

The flow — launch Calculator, clear, tap 1, +, 1, =, read the answer — now
passes identically on a Pixel 7 AVD (Android 15, 1080x2400 at 420dpi) and a
Pixel 8 Pro (Android 17, 1008x2244 at 360dpi), from the same script with no
device-specific branches. 6.4s on the emulator, 10.8s on the phone. It lives
at [checks/calculator.sh](checks/calculator.sh).

**The first version passed for the wrong reason.** It read "the first line of
the screen" as the display, and reported the expected value at every step —
including *before any digit was pressed*. The top line is Calculator's history
strip, still showing the previous run's answer. A test that passes before you
do anything is worse than one that fails, and this is the same shape as the
tautological assertion recorded above: the assertion has to name the element
it means. There are two `2`s on that screen, `id/history_result` and
`id/result_final`, and only one of them is the answer.

**The second version reported an empty field.** `testid=...id/formula` matches
three nodes — Android puts the same resource-id on a container and its
children — and the shell function hid the resulting ambiguity error behind an
empty string. Two lessons for the price of one: narrow with `,role=input`, and
never write `2>/dev/null || printf ''` around a command whose failure is the
thing you are trying to observe.

A third wrinkle that was not a bug: an empty formula field has no text, so
mobium falls back to its accessibility label, which Calculator sets to "No
formula". Reading a label where you expected text is correct behavior and
worth expecting.

### A second check, and a second device that disagreed
**Not a defect.** [checks/clock-timer.sh](checks/clock-timer.sh) sets a Clock
timer to 1m23s and reads it back. It passes on a Pixel 7 AVD at Android 15, the
same AVD profile at Android 17, and a real Pixel 8 Pro at Android 17.

An Android 17 emulator was built specifically so the phone could be compared
against something differing only in being real. The result is worth having:

| Device | OS | calculator | clock-timer |
| --- | --- | --- | --- |
| Pixel 7 AVD | Android 15 | 6.4s | 5.5s |
| Pixel 7 AVD | Android 17 | 6.9s | 5.6s |
| Pixel 8 Pro | Android 17 | 11.3s | 16.5s |

**Two major OS releases cost nothing measurable.** Being real costs about 2x.
Nothing in the parser, the coordinate model or the locator vocabulary needed
changing across Android 15 to 17.

**The app itself was the real difference.** Clock is 7.5 on the emulators and
9.1 on the Pixel, and they disagree: the tab is "Timer" or "Timers"; the
duration is one field `id/timer_setup_time` reading `00h 01m 23s` or three
fields `id/hour_text` `id/minute_text` `id/second_text`; and on a phone with
saved timers the keypad is behind "Add timer" rather than being the tab. The
keypad ids are identical in both. Addressing everything by resource-id and
normalizing the display is what makes one script work on both — and no amount
of testing against a single emulator would have surfaced any of it.

Three mistakes of mine on the way, all in the check rather than in Mobium, and
all the same family as the ones above:

- **`label=1` matched nothing** on the timer keypad, whose digits carry text
  and no content-desc — and the taps were silenced, so three keys did nothing
  and the failure surfaced later as a wrong total. This is the second time in
  one session that hiding a command's error cost more than the error would
  have.
- **`label=Timer` matched eleven elements** on a phone whose saved timers are
  all named "…Timer". The tab has its own id.
- **A glob matched an error message.** The one-field layout was detected with
  `*h*m*s*`, which also matches "No element matc**h**es testid=co**m**.google…
  desk**c**lock", so the phone parsed an error as a duration and produced an
  empty reading. Detection patterns need to be tight enough not to match prose.

And one shell trap worth knowing: under `set -e`, a probe read that fails
inside `$(...)` kills the subshell, so a function that tries one locator and
falls back to another returns nothing at all rather than the fallback.

### 34. The dump backend left a copy of the screen on the device
**Found by:** cleaning up after a session on a real phone, and checking what
was left rather than assuming nothing was.

`uiautomator dump` writes the hierarchy to a file on the device and Mobium
read it and walked away. Found on a Pixel 8 Pro afterwards: 46KB of
`/data/local/tmp/mobium-dump.xml`, still there, containing a full
serialization of whatever screen had last been captured — on somebody's actual
phone that means message previews, calendar entries, whatever they were
looking at.

Never noticed on an emulator, where a leftover file in a disposable image is
not a thought anyone has.

The file is now deleted after every read, before the error handling, so it
goes whether or not the read worked. The deletion is best effort and
deliberately unreported: a snapshot that succeeded must not be reported as a
failure because the tidying up did not.

### 35. The clean-stop check passed while things were running
**Found by:** using it. Both flaws were in the checking, not the thing checked,
which is the only reason they survived being written down as good practice.

The emulator-helpers check matched `$ANDROID_HOME/emulator` and set its count
to zero whenever that variable was unset — which it usually is in the shell
doing the checking. So it reported "none" while `netsimd` and the emulator's
crashpad handler were both running, and the script concluded "clean". **A
check that silently passes when its own input is missing is worse than no
check**, because it is trusted. Matched by process name now, and it
immediately found the two it had been missing.

The daemon check matched the text `mobium daemon`, which also matches the
script doing the checking, an editor with the source open, and any shell whose
command line happens to contain it. It now requires the executable to be
`mobium` and its first argument to be `daemon`.

### 36. A read goroutine per call, leaking one on every cancellation
**Found by:** reading the code just written, before running it.

The first external-driver transport started a goroutine per call to read the
reply, so a canceled context could return while its goroutine stayed blocked
on the driver's stdout. Nothing killed it. The next call started a second
reader on the same pipe, and from then on two goroutines were racing to
consume the same byte stream: whichever won got the message, the other got the
next one, and a caller would eventually be handed a reply to somebody else's
request.

**It would not have failed a test.** Every test that cancels also stops using
the driver, and the tests that keep going never cancel. It needed a deliberate
sequence — cancel one call, then make another on the same session — which is
exactly what an implicit-wait timeout does in real use. Replaced with a single
reader goroutine started with the process and a channel of lines, so there is
never more than one reader by construction rather than by discipline.

### 37. A reference driver that used `monkey` to launch an app
**Found by:** running it. `mobium launch com.android.settings` answered
`** SYS_KEYS has no physical keys but with factor 2.0%.` and the next map
showed the app drawer.

`adb shell monkey -p <pkg> -c android.intent.category.LAUNCHER 1` is the
one-liner everyone reaches for. It is a **fuzzer**, not a launcher: it prints
chatter on stdout, it sends a random event as well as the launch, and it exits
0 for a package that does not exist. My error check looked for `Error` in the
output and the chatter sailed past it.

The correct form is what Mobium's own backend does — resolve the launcher
activity with `cmd package resolve-activity --brief`, then `am start -n` it,
then check for `Error:` on **stdout** because `am start` exits 0 regardless.
Recorded here rather than only fixed, because the reference driver is the
worked example a third party copies, and it should demonstrate the rule
(defect 30) rather than violate it.

### 38. `uiautomator dump`'s reason was discarded, so the failure said "no output"
**Found by:** driving Settings > About through the reference driver.

The reference driver read only stdout. `uiautomator dump` puts "killed" on
stdout and `ERROR: could not get idle state.` on **stderr**, exits 0, and
writes no file — so the driver reported `uiautomator dump produced no file: no
output`, which names the symptom and hides the cause. The screen was
unreadable because the About page's uptime counter ticks every second and the
dump mechanism waits for idle; that is defect 25, reproduced independently in
a driver written from the specification alone.

Two things worth keeping from it. **The limitation is in the mechanism, not in
Mobium** — an independent implementation of the same approach hit the same
wall in the same place. And **the protocol carried the diagnosis intact**: the
driver's own sentence, including its advice to use `--driver uiautomator2`,
reached the user verbatim with the driver's name in front of it, which is what
the pass-through rule in [the driver protocol](../examples/drivers/PROTOCOL.md)
is for.

### 39. A visibility field that nothing consulted
**Found by:** asking why `Node.Displayed` was written by both parsers and by
the wire decoder and read nowhere. Not by a failure — nothing was visibly
wrong, on either platform, which is why it survived this long.

`Actionable` — the filter that decides what `map` puts in front of an agent —
tested bounds and interactivity and never visibility. Whether that mattered
turned out to have three different answers, and none of them is guessable from
the code:

- **Android says nothing.** Measured across ten screen states on a Pixel 7 AVD
  — launcher, Settings, Settings scrolled to the bottom, a search overlay, the
  notification shade, Clock, Chrome, Contacts, Photos, Calculator — **590
  nodes, every one `displayed="true"`, not one false.** UiAutomator2 filters
  non-displayed nodes out of `/source` before serializing, so the attribute is
  a constant. The `uiautomator dump` format has no such attribute at all.
- **iOS means it.** 120 of the 201 nodes in the SpringBoard capture are
  `visible="false"`. It was already mostly accounted for, because the iOS
  parser folds it into `Clickable` — but *only* into `Clickable`, so an
  invisible scroll view or switch, whose `Scrollable`/`Checkable` come from the
  element type rather than from visibility, would still have been offered.
- **An external driver may send it**, and after
  [the driver protocol](../examples/drivers/PROTOCOL.md) the
  protocol documents `displayed:false` as meaningful. It was not: a driver
  could have said false and been mapped anyway. A protocol that documents a
  field which changes nothing is worse than one that omits it.

`Actionable` now requires `Displayed`. It changes no entry in any of the four
captured hierarchies, and `mobium map` on a live emulator produced the
identical 11 lines before and after — which is the point. It is a guard
against a screen not yet seen, not a fix for one already seen, and the third
bullet is the one that was actually broken.

**The probe that found nothing, twice.** The first version launched each app
with `monkey` (defect 37, freshly recorded and freshly repeated) and reported
0 hidden nodes across four apps — with **32 nodes every time**, because it
never left the launcher. A clean zero from never having looked. It now asserts
the hierarchy's `package` matches the app it asked for before believing any
count. The second version probed only fresh launches, which is where a hidden
node is least likely; the states worth probing are overlays, scrolled lists and
the shade.

**And a footgun found on the way.** The zero value of `Displayed` is *hidden*,
so the moment `Actionable` consulted it, five existing table-driven tests that
built a `Node` by hand went dark. Non-test code was fine — every parser sets
it — except the Android synthetic root, which did not while the iOS one did.
Both parsers now set it and the field says so in its own comment. A boolean
whose zero value is the unusual case will be got wrong by whoever writes the
next parser.

### 40. One feed card became a 876-character label, with HTML in it
**Found by:** pointing Mobium at the first app that did not ship with the OS
(rollout gate G4, Wikipedia's Android client). It was the first screen mapped,
and it produced the defect immediately.

`map` returned thirteen entries. Twelve had labels of 2 to 12 characters. The
thirteenth was **876**, which is 91% of that map's entire text from one entry,
and it began:

```
<span lang="en" dir="ltr"><span class="mw-page-title-main">Thomas Hardy …
```

Three separate faults, all in `describe`/`descendantText`:

- **Length was never bounded.** Depth was capped at 3 and had been since the
  beginning; breadth and total length were not. A feed card is a clickable
  container holding a title, its Save and Share buttons, a subtitle and a full
  article lede, so the label became the entire card.
- **Raw markup passed straight through.** The app puts HTML in an
  accessibility string. Mobium handed it to the caller verbatim, so an agent
  reads tag soup and a `text=` locator would have to spell out the markup.
- **Parts repeated.** The search field rendered as "Search Wikipedia Search
  Wikipedia Voice input search" — the hint text and the content description say
  the same thing, and the mic button's label was appended for good measure.

Now: labels are bounded at 120 runes with a word-boundary truncation and an
ellipsis, markup is stripped when the string actually contains a tag, and
repeated parts are dropped. The card reads `Thomas Hardy (Royal Navy officer,
died 1732) Save Share British Royal Navy officer and politician (1666–1732)…`
at 110 characters. Truncating is safe: `Map` derives each entry's locator from
the node rather than from the printed label, so a shortened label never changes
what a `@ref` resolves to.

**This is what gate G4 was for.** The recorded risk was "map output is unusable
on large commercial apps", and every labeling rule had been tuned on Settings,
Calculator and Clock. The first real app broke it on the first screen.

**And the fix rests on one app.** Measured afterwards against the four captured
hierarchies: the longest label on any of them is **27 characters against a 120
cap**, not one comes within a third of it, and not one contains markup. So
`truncate` and `clean` are **unreachable from every fixture in the repository**
— their only evidence is Wikipedia, plus unit tests written from Wikipedia's
own strings, which is the same evidence twice. That is not an argument against
the fix; it is the reason the next real app matters more than the next feature,
and the argument for a regression pack of real screens, now in
`internal/uitree/testdata/`.

### 41. A locator inside a WebView was reported as an unknown ref
**Found by:** the same session. `tap 'text=Charles Babbage'` while switched
into Wikipedia's article WebView answered:

```
error: unknown ref text=Charles Babbage in WEBVIEW_org.wikipedia — run app_map again
```

Locators genuinely do not work inside a WebView — the web map matches on the
CSS selector behind each ref, and there is nothing for `text=` to resolve
against — so the refusal was right. **The advice was impossible.** Re-running
`app_map` can never help, because the problem is not a stale ref; it is that a
locator was passed where only a ref works. Following the message literally is
an infinite loop.

The message now names the actual problem, and says both what to use instead
and how to get back to where locators do work. The generalisation is worth
more than the fix: **an error that suggests a remedy must be one that can
succeed.** A wrong remedy is worse than none, because it is obeyed.

### 42. Composite labels are not addressable by the text they show
**Found by:** writing the G4 check, where `tap 'text=Ada Lovelace English
mathematician (1815–1852)'` failed against a row whose label reads exactly
that.

The label is composed from two child nodes — the title and the description —
so no single node carries the whole string and `text=` cannot match it. Working
as designed, and the design is right: the alternative is to stop composing
labels, which would make every result row read "Ada Lovelace" and be ambiguous.

Recorded because the map gives no hint which labels are composite, and the
natural reading of a map line is that its label is text you can match. The
answer is the ref that `map` prints on every line, which is why it prints one.
Not fixed; documented in the skill so an agent picks the ref rather than
retyping the label.

### 43. `map` printed a password in plaintext
**Found by:** the second and third real third-party apps.
Typing into Aegis's master-password field and running `map` again gave:

```
@e2 Sup3rSecret! (input)
```

Android marks the node `password="true"` **and** puts the typed value straight
into its `text` attribute. Mobium parsed that flag into `Node.Password` and
consulted it nowhere — the same shape as
[defect 39](#39-a-visibility-field-that-nothing-consulted), found two days
earlier: a field the platform provides, parsed, and never read.

It leaked in three places, not one:

- **`map`** — the label. This is the bad one: `map` is broadcast output, and it
  goes to agent transcripts, CI logs, MCP responses and anywhere a user pipes
  it. Nobody consented to that.
- **`text <target>`** on the field — a targeted read, but the value still lands
  in a log.
- **`text`** with no target, the whole-screen read — the one most likely to be
  piped somewhere.

All three now honor the platform's marking. The label becomes the field's
resource id (`text_password`), which is stable, descriptive, and keeps a
confirm field distinguishable from the first — two identical labels would be
worse than useless. `roleOf` reports **`password`** rather than `input`, and
`role=password` matches on both platforms, since iOS names the type outright
(`XCUIElementTypeSecureTextField`) where Android sets a flag. Reads return
`•••••••••••• (12 characters, hidden: this is a password field)`, keeping the
length because "did my typing land" is the real question and the answer to it
is not secret.

**What cannot be distinguished from outside**: Android reports the *hint* in
`text` for an empty field and the *value* for a filled one, with no flag to
tell them apart, so the hint is masked too. Losing a hint is cheap.

### 44. A test that could not fail, and said so in Japanese
**Found by:** mutating the code to check the test bites — the discipline that
[defect 39](#39-a-visibility-field-that-nothing-consulted) taught.

`TestTruncationCountsCharactersNotBytes` was written to prove `truncate` cuts
on characters rather than bytes. Its sample was
`strings.Repeat("日本語のテキスト", 40)`. That repeat is **24 bytes long**, so a
120-byte cut lands exactly on a character boundary every time: replacing
`string([]rune(s)[:120])` with `s[:120]` passed every assertion.

Two faults. The sample was accidentally aligned, so nothing was ever split; and
the check looked for U+FFFD, which malformed bytes do not contain — invalid
UTF-8 is invalid bytes, not replacement characters. Now the inputs are
deliberately misaligned by one and two bytes, the check is `utf8.ValidString`,
and there is an assertion that a label of 3- and 4-byte characters keeps close
to `maxLabel` *characters* rather than that many bytes' worth. Re-mutated: it
fails on five of seven samples and names both failure modes.

**The general form, for the third time in a week:** a test that has only ever
been green is a claim, not evidence. This one was written *by* the rule and
still needed the rule applied to it.

### 45. The second connection to webinspectord waits ten seconds
**Found by:** listing WebView contexts twice in a row and timing it.

The first connection to a simulator's Remote Web Inspector receives its first
byte in **0.17s**. Every connection after it — same socket, same process, same
everything — waits **10.2 seconds** before webinspectord says a single word.
Measured repeatedly on an iPhone 17 Pro simulator, consistent to the tenth of a
second, which is what made it obviously a timeout somewhere rather than load.

Opening a connection per command would therefore have put a ten-second floor
under `app_contexts` and every context switch. The connection is now held for
the life of the session, which takes the same sequence from 10.4s to **0.00s**.

**It was also diagnosed wrongly three times first**, each time by reasoning
instead of measuring: that unrelated chatter was extending a quiet window, that
a late-connecting application was being waited on, that the socket lookup was
slow. All three were plausible, all three were wrong, and a trace printing the
arrival time of every message settled it in one run. The measurement was
cheaper than any of the guesses.

### 46. Everything in this protocol happens once per connection
**Found by:** holding the connection open, which fixed defect 45 and broke two
other things immediately.

Remote Web Inspector is a conversation, not a request-and-reply protocol, and
three of its messages are announcements that are never repeated:

- **The application list.** `_rpc_reportConnectedApplicationList:` arrives once,
  just after the announce. Re-announcing on a connection that has already done
  it produces nothing at all, so the second listing on a held connection came
  back empty. The application set is learned once and kept; only
  `_rpc_forwardGetListing:` is re-sent.
- **A page's target.** `Target.targetCreated` is announced once per page per
  connection. Attaching to a page, leaving, and attaching again failed with
  "the page never announced a target" — on a page plainly there — because the
  second `_rpc_forwardSocketSetup:` is accepted and simply never answers.
  `_rpc_forwardDidClose:` on detach is what makes the next attach work. Nothing
  complains if you skip it, until you re-attach.
- **And a page's target belongs to one debugger at a time.** A mobium daemon
  left switched into a page blocks Safari's own Web Inspector, and vice versa —
  which is exactly how this was found: a test failed against a simulator that
  was fine, because the daemon from a CLI session still held the page. The
  error now says so and names `mobium daemon stop`.

### 47. Safari's WebView element is not the page's content area
**Found by:** comparing where mobium said a link was against where the native
hierarchy said it was. The page put "Learn more" at CSS y=254; the native tree
had it at device y=948; mobium computed **762** — 186 device pixels too high.
Enough to hit a different link, not enough to look wrong in a screenshot.

`XCUIElementTypeWebView` in mobile Safari covers the **entire window, chrome
included**: 874 CSS points tall against a 714-point viewport. The 160-point
difference is the top and bottom chrome, and the split between them is not
recoverable — **the page cannot see its own inset.** Measured: `screenY` is 0,
`visualViewport.offsetTop` is 0, `visualViewport.pageTop` is 0. No native
element has the content area's rectangle either; only the full window and the
toolbar buttons are exposed.

This is defect 6's cousin. There the layout viewport was mistaken for the
visual one; here the *host element* is mistaken for the content.

`NewFrame` now checks the two agree and **refuses rather than approximating**,
naming both numbers. An embedded WKWebView — the case this feature is actually
for — has a frame equal to its content, so it is unaffected, and a tolerance of
4 CSS pixels absorbs a scrollbar or a rounded density. `app_map` and `app_text`
still work when the check fails, because the refs, labels and roles are exactly
right and only the rectangles are unknowable; the map says so in a footnote
rather than failing outright. Withholding what is correct in order to protect
what is advisory would be the wrong trade.

### 48. `dumpsys` reports rotation in degrees and `user-rotation` in quarters
**Found by:** the first rotation, which failed with `the display reported
rotation "90"` — a message blaming the device for my own unit mismatch.

`mCurrentRotation=ROTATION_90` is **degrees**. `settings get system
user_rotation` and `cmd window user-rotation lock <n>` are **quarter turns**.
Both are small integers, both are called "rotation", and reading one as the
other accepts 0 and rejects everything else — so a portrait screen looks
perfectly healthy and the very first rotation fails.

`dumpsys window` makes it worse by printing three spellings in one output:
`mRotation=1` (quarters), `mRotation=ROTATION_90` (degrees) and
`mCurrentRotation=ROTATION_90` (degrees), plus `mRotation=undefined` on a
display that is not the real one. Matching the wrong field gives a number that
is *sometimes* right, which is the worst kind of wrong. `dumpsys window
displays` narrows it, and `mCurrentRotation` is the one that means "what is on
screen now" rather than "what was asked for".

**And the error I wrote for it made a diagnosis it could not support.** When
the mutation test re-introduced this bug, the failure read "an activity that
locks its own orientation cannot be rotated from outside" — confident, and
about the wrong thing entirely. It now states the fact and offers that cause as
the usual one rather than asserting it. Same lesson as
[41](#41-a-locator-inside-a-webview-was-reported-as-an-unknown-ref): a wrong
explanation is obeyed.

### 49. Android stores a language the app cannot speak, and says nothing
**Found by:** probing `cmd locale set-app-locales` before building on it.

`cmd locale set-app-locales <pkg> --locales zz-ZZ` is accepted, stored, and
reads back as `[zz-ZZ]`. The app then renders in its default language. Android
exposes nothing that distinguishes "pinned to a language it has" from "pinned
to a language it does not", so **reading the setting back confirms the device
stored the tag and nothing more** — the exact shape of `pm grant`
([26](#26-pm-grant-succeeds-loudly-at-doing-nothing)) with the failure moved one
step further away.

Setting an unknown package is the same shape from the other side: `Unknown
package com.nope.nothing for userId 0`, on stdout, **exit 0**. Detected by the
message, because there is nothing else to detect.

So `app_locale` says what it actually confirmed: *"the device stored it;
whether <app> has that translation is not something Android reports, so check
the screen."* Longer than "done", and the difference between the two is the
whole value of the tool.

### 50. A limitation nobody had tested, written down three times
**Found by:** being asked to clarify it. The claim — "neither `simctl location`
nor `adb emu geo fix` can be read back, so nothing can confirm a set took" —
appeared in three documents, each copying the last. No test, no
transcript, no measurement anywhere behind it.

Half of it is wrong.

**Android can be read back.** Not through `adb emu geo fix`, which is what the
note was about: that pushes a fix into the emulated GPS, where it lands nowhere
until an app *requests* location. The provider sits at `ProviderRequest[OFF]`
with `last location=null`, which looks exactly like "unverifiable" and is
actually "nobody asked". The mechanism that works is the **test provider**:

```
adb shell appops set 2000 android:mock_location allow
adb shell cmd location providers add-test-provider gps
adb shell cmd location providers set-test-provider-enabled gps true
adb shell cmd location providers set-test-provider-location gps --location 51.5074,-0.1278
adb shell dumpsys location   # last location=Location[gps 51.507400,-0.127800 … mock]
```

Set, moved, read back and removed, all observable — and it works on **real
devices**, which `geo fix` does not.

**iOS genuinely cannot.** `simctl location` has `set`, `clear`, `run` and
`start`, and no `get`.

Two things worth keeping. **An untested limitation is a rumor**, and this one
had been promoted to a fact by repetition across three documents — the same
failure as a fixture that nobody checked, one level up. And **"it cannot be
read back" often means "it has not been read back the right way"**: the first
mechanism anyone reaches for is not always the one the platform intends.

### 51. Reading the hierarchy during an animation fails, and nothing retried it
**Found by:** opening the notification shade and mapping straight away, which
is exactly what a caller does.

```
error: unknown error: Cannot set AccessibilityNodeInfo's field 'mSealed' to 'true'
```

Accessibility nodes are being recycled underneath the read while the screen
moves. It is not a state anyone can wait out deliberately — a caller has no way
to know an animation is running — and it succeeds a moment later.

The dump backend has retried its own transient failure ("null root node") since
early on. **UiAutomator2 had no equivalent**, so this reached the user looking
like a broken device, with a message that names an internal Android field. It
now retries the same way, and only for the failures that mean "the screen was
moving": a connection refused or an invalid session still surfaces at once,
because retrying those turns a clear failure into a slow one.

A pair of habits paid off here. The check caught it because it maps
*immediately* after opening the shade rather than sleeping first, and the fix
was found by retrying by hand three times and watching it succeed — the same
"is it transient or is it broken" question as defect 31, asked of a different
backend.

**Still not fully fixed, seen again on 2026-09-14.** Running
`checks/device-state.sh` twice in a row after an unrelated refactor, the shade
step failed the first time — "could not read the UI hierarchy after 3
attempts, each failing while the screen was changing" — and passed the second
with nothing changed in between. So three attempts is sometimes not enough for
the shade animation specifically, and the retry budget is a guess rather than
a measurement.

Worth noting what the failure did right: it said how many attempts it made and
that each one failed *while the screen was changing*, which is what made it
identifiable as this defect in seconds rather than as a rename regression. An
error that names its own category is why the second run was a check rather
than a hope. The budget should be measured against the shade before it is
raised, since a bigger number chosen by feel would just move the flake.

### 52. A shell command with a space in it, truncated to one word
**Found by:** posting a notification whose body was "Another body" and reading
back `android.text=String (Another)`.

`adb shell` runs its argument through a shell **on the device**, so an
unquoted value with a space becomes two arguments and everything after the
first is dropped — silently, because `cmd notification post` is perfectly happy
to be given extra arguments. This is the same family as the `input text`
mangling that made the dump backend refuse text entry ([defect 9]), but here it
is fixable rather than fatal: single-quoting, with embedded quotes closed and
reopened, survives spaces, apostrophes, double quotes, `$` and `%` — each
verified end to end against a device rather than reasoned about.

The general form: **anything passed to `adb shell` is shell input on the far
side.** A value that came from a caller must be quoted for that shell, not for
the one it was typed into.

---

### 53. A dropped flush left the client waiting for a reply that never left the buffer

`mobium pipe` writes each reply through a `bufio.Writer` and called `Flush`
without looking at what it returned. When the flush fails the reply is sitting
in a buffer the client will never see — and a client that asked for a result
waits for it forever, with no error on either side. The loop meanwhile carried
on answering into a pipe that was not there.

Found by `errcheck` rather than by anything going wrong, which is the argument
for the linter: nothing about the code looked suspicious, and the failure only
appears when a client dies mid-call.

Fixed by returning the error and stopping. The deferred flush on the way out
is deliberately still ignored, with a comment: by then stdin has closed and
there is nobody left to tell.

### 54. A WebView attach leaked the handshake response on failure

`dialer.DialContext` in `internal/webview/cdp.go` discarded its second return
value with `_`. On a *failed* websocket handshake gorilla hands back the HTTP
response, whose body is the caller's to close; on success it is nil. So every
failed attach leaked a body.

Invisible until a long session runs out of file descriptors, which is exactly
the kind of defect this project only ever finds on a device, days later. Found
by `bodyclose` in seconds.

### 55. Text entry failed on a field `map` had just handed out a ref for

**Found by:** the first React Native app this project has driven. Every prior
Android app was Google's, Wikipedia's, F-Droid's or Aegis's, and every one of
them reports a resource-id in the fully qualified form `com.example:id/name`.

React Native reports a bare one. A `testID` of `password` arrives as
`resource-id="password"`, with no package. `map` handled it correctly — the
node, the label, the `role=password` and the emitted `testid=password` locator
were all right — and then `app_type` on that very ref failed with

```
locate element (id=password): no such element
```

`UIA2.elementFor` passed the id straight to UiAutomator2's `id` strategy,
which **qualifies a bare name with the package under test**. So the server
searched for `dev.mobium.mobiumapp:id/password`, found nothing, and text entry
failed on an element that was plainly on screen.

The shape of it is what makes it worth recording. `tap` on the same ref
**worked**, because a tap uses coordinates from the snapshot the caller
already resolved, and `mobium tap 'testid=password'` worked too, because
`Locator.Matches` compares the short form as well as the long one — that
fallback exists, and the one path that bypassed it was the one that asks the
device to find the element for it. So the bug needed all three of: Android,
text entry, and a field whose best locator is a testid rather than its own
text. An empty username or password box is exactly that, and nothing else
here had ever been one.

That is also why five Android apps and a year of use never hit it. The rule
"every locator `map` hands out must resolve to exactly one node" was true of
the locator and false of the lookup performed on its behalf, which is a
distinction no test asserting the locator could catch.

`elementFor` now falls back to the position when the id lookup misses, which
is not a guess — it is the same XPath rebuild already used for every node with
no resource-id at all. The regression test drives a server that rejects the
`id` strategy the way the device does, and reverting the fix fails it.

### 56. Clearing a mock location reported success and changed nothing visible

**Found by:** running the tool once end to end. `location --clear` said the
device would report its own position again, and the very next read still
answered *"from an injected fix"*, with the same coordinates.

Both statements were true, which is what made it a defect rather than a bug
report. `cmd location providers remove-test-provider gps` does remove the
provider — **and Android keeps the last known location it supplied.**
`dumpsys` goes on reporting that fix, still tagged `mock`, from a provider that
no longer exists, until something requests a fresh one. The cache is not
clearable from outside.

So "was this fix injected" and "is anything injecting now" are different
questions, and the first implementation asked only the first one and printed
its answer as if it covered both. `Fix` now carries `Mock` *and* `Mocking`,
read from two different parts of the same `dumpsys` output: the position line
and the provider label, which goes `gps provider [mock]:` → `gps provider:` and
is the only thing that changes when the provider is removed.

The clear path is verified against **the provider label, not the position** —
the position being the one thing guaranteed not to change. Verifying the
obvious field would have produced a check that fails forever on correct
behavior.

Same family as [49](#49-android-stores-a-language-the-app-cannot-speak-and-says-nothing):
a platform will answer a narrower question than the one being asked, and the
tool has to say which one it answered.

### 57. A negative coordinate is a command-line flag

**Found by:** typing London. `mobium location 51.5074 -0.1278` failed with
`unknown shorthand flag: '0' in -0.1278`, before any of the code above ran.

Every POSIX command line reads a leading `-` as a flag, and half the planet has
a negative coordinate. The first fix — `SetInterspersed(false)`, which stops
flag parsing at the first positional argument — makes a negative *longitude*
work and does nothing for a negative *latitude*, since that one comes first and
is still the first thing parsed. Sydney, which was already the second city in
the check, is exactly that case.

`--lat` and `--lon` are the forms that always work, because pflag consumes the
next argument as a flag's value whatever it starts with. Both are supported and
the help says which is which: the positional pair is a convenience that handles
one of the two signs, and the flags handle both.

Worth recording because it is the cheapest possible defect to ship — it needs
no device, no platform knowledge and no concurrency, only a southern-hemisphere
test case. There was one in the check by luck rather than design.

### 58. Eight hierarchy dumps on the SD card, from a checklist that omitted it

**Found by:** going back to close out a loose end. A stray `uiautomator dump
/sdcard/d.xml` had been run twice during defect 55 and cleaned up once, so the
question was whether one `d.xml` was still there. It was not — that dump never
wrote anything. `/sdcard` held **eight other hierarchy dumps** instead:
`c.xml`, `k.xml`, `l.xml`, `s.xml`, `t.xml`, `w.xml`, `w2.xml`, `w3.xml`,
4–23KB each, dated across two sessions five days earlier.

None of them came from mobium. The dump backend writes
`/data/local/tmp/mobium-dump.xml` and deletes it after every read, and that
directory was clean every time it was checked. All eight came from
`uiautomator dump` typed by hand, which writes to `/sdcard` when nobody gives
it a path — and `/sdcard` was not on the checklist. `/data/local/tmp` was, so
the one place that was being watched was the one place nothing was leaking.

They held Chrome, Calculator, Settings, Clock and the launcher. No password
field in any of them, checked rather than assumed, because this emulator has
run Aegis and [43](#43-map-printed-a-password-in-plaintext) is about
exactly that. On an emulator the whole thing is untidiness. The same commands
against a phone would have left five days of somebody's screens in a
world-readable directory, which is [34](#34-the-dump-backend-left-a-copy-of-the-screen-on-the-device)'s
rule with the numbers filled in.

The check now covers that directory, and the rule for it is **structural
rather than a list of names**, because a list of names is what failed: a stock
Android `/sdcard` root holds only directories, so any plain file there was put
there and left. Verified by planting each kind and confirming the check could
come back dirty — a loose `.xml` fails, anything else is named without being
judged.

Three lessons, and the middle one is the uncomfortable one. A checklist nobody
runs finds nothing. **A checklist that is run but incomplete is worse, because
it produces a clean result** — the same shape as the spelling sweep's missing
`*.sh`, found the same day. And the tooling was never the problem here: every
artifact any of these checks has caught was left by a command somebody typed,
which is an argument for automating the checking rather than for trusting the
code.

### 59. A route looked like it ignored `clear`, and the counter was the wrong signal

**Found by:** asserting that stopping a route stopped it. The app's fix counter
read 24 while a route ran, then 26, then 32 after `location --clear` — still
climbing at about one a second, exactly as though nothing had stopped.

Nothing was wrong. `dumpsys` showed `gps provider [mock]` gone one second after
the clear and still gone ten seconds later, so the route had stopped
immediately. **The counter counts deliveries, not movement.** Android goes on
handing a watching client the last known position after the provider that
supplied it is removed, so the app kept receiving — the same fix, over and
over. A counter that only ever goes up cannot distinguish "moving" from
"receiving the same thing repeatedly", and it was being read as though it
could.

The signal for a route is the **coordinate**, and the check now compares that.

The second half is the one that would have shipped a wrong assertion.
"Clearing stops the route" does not mean the position stops changing: clearing
reverts the device to reporting its **own** position, and on Android that is a
different coordinate from wherever the route had reached — one more change,
arriving after the stop. An assertion that the position is unchanged across
the clear fails on correct behavior. What holds is that it *settles*: three
samples after the revert were identical, and `app_location` agreed the fix was
no longer injected.

Both halves are the same mistake in different clothes — measuring something
adjacent to the question and reading it as the answer. The first was caught by
checking the platform's own view instead of the app's; the second by watching
one more sample rather than stopping at the one that looked wrong.

### 60. The Android clipboard can be written and not read, and the failure is an empty string

**Found by:** probing before building, which is the only reason this is a
finding rather than a defect.

`adb shell cmd clipboard set-text` reports **"No shell command
implementation"** on Android 15, although `cmd -l` lists the service — so that
route does not exist. UiAutomator2's own endpoints do: a POST to
`/appium/device/set_clipboard` with base64 content succeeded, and
`/appium/device/get_clipboard` answered `""`.

It answers `""` whatever the clipboard holds. Since Android 10 only an app with
focus may read the clipboard, and the UiAutomator2 server has no activity of
its own; foregrounding a different app does not help, because the restriction
is on the *reading* app. Working around it takes a separate helper
application. Mobium installs no such thing, which is the whole bet, so the read
is genuinely unavailable here.

**That empty string is the trap.** Returning it would report "the clipboard is
empty" for a clipboard that is full — a different claim, usually false, and
indistinguishable from the true one. The read refuses on Android instead, and
says why and what to do. It is the same rule the dump backend follows when it
declines text entry rather than mangling it.

The write is real, and was verified the only way left: set the clipboard, tap
a field in MobiumApp, send `KEYCODE_PASTE`, and read the field back —
`paste-probe-42` arrived. That is a better check than a read-back would have
been, because it proves the clipboard reached an *app* rather than reaching the
tool that set it, and it is what [checks/mobium-app.sh](checks/mobium-app.sh)
asserts on Android.

iOS has none of this: `simctl pbcopy` and `pbpaste` both work, so there the
write is confirmed by reading it back. The asymmetry runs the opposite way to
geolocation's, where iOS is the platform that cannot read — which is the
argument for asking a driver what it can do rather than assuming a platform is
uniformly more or less capable.

### 61. iOS dropped a keystroke and `type` reported success

**Found by:** building a screen with two text fields and typing into both, one
after the other. `lose-999` arrived as `lse-999`.

Reproduced deliberately rather than shrugged at: five runs of type-then-type,
**four of them dropped exactly one character**, always from the middle of the
string — `keep-1` as `kep-1`, `lose-5` as `lse-5`. The same strings typed in
isolation, with a pause between them, were correct five times out of five. So
it is a race with the focus change and not anything about the text. Measured on
an iPhone 17 Pro simulator running iOS 26.5.

XCUITest types through the keyboard, and a keystroke sent while focus is still
moving lands nowhere. The request is correct, the response is a success, and
the field holds something else — **the exact shape this project is organized
against**, and it had been shipping in every iOS text entry since the backend
was written.

`WDA.SetText` now reads the value back and retries once. After the fix, five
runs of the same sequence: zero mismatches. Two attempts rather than three
because the failure is a race and the second try has always been enough;
retrying more would mostly slow down a genuine failure.

Three things this turned up on the way, each worth more than the fix:

- **A password field cannot be verified this way**, and trying broke the login
  check immediately: `hunter2` reads back as `•••••••`, so the comparison can
  only ever disagree. Password fields are exempted, which is the same masking
  [43](#43-map-printed-a-password-in-plaintext) is about seen from the other
  side — there the problem was that a password *could* be read.
- **A fixture that stubs an endpoint empty makes a real check impossible.** The
  fake server answered `""` for the value attribute, so every type failed the
  moment the code started reading it back. The fake now models the field:
  a write stores, a read returns, a clear empties.
- **A test that asserts on "the last call" breaks when a step is added after
  it.** The existing one did, and now finds the call by what it is instead.

Android is unaffected, measured rather than assumed — which is why the
verification sits in the iOS backend and not in the shared W3C client.

The uncomfortable part: this was found because somebody outside the project
said permission popups were the pain worth testing, and what actually broke on
the way to building that was unrelated to popups entirely. The value was in
driving a new screen at all.

### 62. An unanswered dialog bricks the simulator, and the error blamed the install

**Found by:** a probe that died with a permission prompt on screen. Every run
afterwards failed with

```
WebDriverAgent did not become ready within 1m30s — check that
com.facebook.WebDriverAgentRunner.xctrunner is installed and can launch
```

It was installed and it could launch. **A system modal blocks the runner from
starting at all**, and WebDriverAgent is an app like any other. So an aborted
run leaves the simulator unusable for every run after it, and the message
sends whoever meets it to check an installation that was never the problem.
Settled by looking at the screen: the dialog was sitting over the home screen,
the app long since terminated out from under it.

`shutdown` then `boot` clears it. The error now says so, and names that cause
first because it is the likelier one — this is the rule about a remedy that
must be able to work, met from the other side. The check also dismisses a
stray dialog before it starts, which costs nothing when there is none.

The uncomfortable part is how long it took. The tool reported a cause, the
cause was plausible, and I read it three times before looking at the actual
screen — which a screenshot answered in one glance.

### 63. `accept` denied the permission and `dismiss` granted it

**Found by:** checking what the new alert tool actually did, rather than what
it was called.

On iOS 26.5, from a fresh permission reset each time:

| | outcome |
| --- | --- |
| `app_alert accept` | permission **denied** |
| `app_alert dismiss` | permission **granted** |

Backwards, and reproducibly so.

**The first explanation written here was a guess, and it was wrong in a way
worth keeping.** It said accept presses the *affirmative* button and Apple puts
"Don't Allow" last — a semantic story, and it came with the untested claim that
the mapping "is correct for an OK/Cancel alert". Nothing had measured that.

Measuring it needed an ordinary alert to point at, which is why MobiumApp grew
an App Alert screen. With one:

| dialog | buttons, in order | `accept` pressed | `dismiss` pressed |
| --- | --- | --- | --- |
| two-button alert | Cancel, **Delete** | Delete | Cancel |
| permission prompt | Allow Once, Allow While Using App, **Don't Allow** | Don't Allow | Allow Once |

**accept presses the last button and dismiss presses the first.** Positional,
not semantic, and it explains both rows exactly — where the affirmative story
only explained the one it was invented for. On a conventional alert the
affirmative *is* last, so accept does what anyone would expect; on Apple's
permission prompt the denial is last, and it does not.

Verified on both platforms: the two-button case behaves identically on Android
and iOS, so the difference is the dialog's button order rather than the
platform. Android's permission `accept` grants, checked by reading the
permission back, where the system and the app agreed.

**The fix was not to invert it on iOS.** That would repair the permission case
and break every ordinary alert, which is approximating — and this tool would
then be wrong in a way nobody could predict from the name. What `accept` means
is the dialog's business.

So the tool says what it did and refuses to imply what it meant, in all three
places a caller might read: the answer text, the MCP description and the CLI
help. To choose an outcome you tap the button, which `map` returns like any
other element — and [checks/mobium-app.sh](checks/mobium-app.sh) now does
exactly that, using `app_alert` only to *detect* the dialog, which needs no
knowledge of what the buttons say.

That split is the useful shape here. **Detecting a dialog is language-
independent and choosing an outcome is not**, and a check that conflated them
passed on an English device and would have failed the moment somebody set the
device to Japanese — which this project does on purpose.

### 64. WebDriverAgent wants `value` where the spec says `text`

**Found by:** typing into an iOS prompt for the first time. The W3C-correct
request — `POST /alert/text` with `{"text": "…"}` — came back with

```
invalid argument: Missing 'value' parameter
```

WebDriverAgent still expects the older JSONWP `value`, an array of characters.
Sending both satisfies either server and is what `setElementValue` has always
done for element text, so this is the same workaround one layer along rather
than a new idea.

The Android half is a different answer to the same question, and worth stating
as a capability rather than a failure: **UiAutomator2 does not implement alert
text entry at all** — `unknown command` — and React Native has no prompt on
Android to type into, `Alert.prompt` being marked `@platform ios`. So there is
nothing there to reach, and `app_alert` says which of the two it met: a server
that cannot, or an alert with no field. A single message covering both would
have sent an Android user looking for a text field that the platform never
offers.

### 65. `map` knew whether a checkbox was ticked and never said

**Found by:** building a screen with checkboxes and radio buttons, because
nothing here had ever driven one. Three defects were sitting in it.

**The state was dropped at the last layer.** Android reports
`checkable="true" checked="true"`, the iOS parser derives the same, `Node`
carries it and so does the wire format — and `Entry`, which is what `map`
returns, had no field for it. So a ticked box and an empty one printed the
identical line, in text *and* in JSON. Proven rather than reasoned: tapping a
checkbox changed the device's attribute and the app's own state, and the map
output before and after was byte-identical.

That matters more than it sounds. **Tapping a checkbox is a toggle**, so a
caller that cannot read the state cannot reach one — it can only flip whatever
is there and hope. `Entry.Checked` is a `*bool`: nil for things that have no
such state, because reporting a button as "unchecked" is a different lie.

**On iOS the label was the state.** The parser assigned `Text: value`, and a
value is not always text:

| element | `value` | `label` | `map` printed |
| --- | --- | --- | --- |
| Switch | `0` | Dark mode | `@e7 0 (switch)` |
| RN checkbox | `checkbox, unchecked` | Email me | `@e2 checkbox, unchecked (button)` |

The right label was sitting in `label=` both times. **Every iOS switch has
mapped as `0` or `1` since the backend was written** — ours is not special, and
nothing had noticed because no check had ever mapped a screen with a switch on
it. `value` now populates text only when it is text.

**And the roles disagreed across platforms.** Android read `checkbox` and
`radio` from the widget classes; iOS has no element type for either, so React
Native renders them as `XCUIElementTypeOther` and states the role in the
accessibility value instead. `role=checkbox` therefore worked on one platform
and not the other — in a tool whose whole design is one locator vocabulary
compiled per platform.

A `DeclaredRole` on the node fixes it, and it is not a heuristic: the app is
saying what the control is, which is better evidence than any table here. The
only judgment is vocabulary — VoiceOver says "radio button" and the locator
vocabulary says `radio`, so the two are mapped, which is exactly what
"compiled per platform" means.

The three share a shape worth naming. Each was a piece of information the
platform supplied, which mobium read correctly, and then lost or misused on the
way out. None of them would have been found by reading the code, because the
code looks reasonable at every step: `Text: value` is right for a text field,
`Entry` has the fields a map entry obviously needs, and iOS genuinely has no
checkbox type. It took a screen with a checkbox on it.


### 66. A negative angle is a command-line flag, again

**Found by:** the first thing anyone types at a new `rotate` command —
`mobium rotate -90`, to turn the other way. It failed with `unknown shorthand
flag: '9' in -90`, which is defect 57 verbatim, one command later and in the
same repository that had already written it down.

The fix is the same fix: `--degrees` is a flag, so pflag consumes the next
argument whatever it starts with, and the positional form stays as the
convenience it was. That part took a minute.

What is worth the entry is that the earlier write-up ended with "it is the
cheapest possible defect to ship" and did not stop the same defect shipping
again. A defect report is a record, not a guard. Any command whose argument can
legitimately be negative — an angle, a latitude, a scroll delta, a temperature
— needs a flag form, and the only thing that will actually enforce that is a
check that looks at the argument rather than a paragraph that asks a reader to
remember.


### 67. WebDriverAgent honors a pause only while the finger is down

**Found by:** a double tap that the page refused to call one. `app_tap
--double` on Android made a WKWebView's counterpart fire `dblclick`
immediately; on iOS the same code produced two clicks and no double, every
time.

Instrumenting the page gave the answer in one line. The touch stream was
`down 43452, up 43519, down 43519, up 43586` — two taps, 67ms each, and
**zero milliseconds between them**. The 120ms pause between the taps had been
dropped on the floor. WebKit's minimum is around 40ms, so the second tap was
discarded as a bounce, which is exactly what it should do with two taps that
arrive at the same instant.

The rule is narrower than "WebDriverAgent ignores pauses", and the narrowness
is the useful part: a pause between `pointerDown` and `pointerUp` **is**
honored there. The drag's opening and closing holds prove it — wall-clock
timing of a drag grew by twice the hold for holds of 300, 700 and 2000ms
(2.45s, 3.26s, 5.88s). It is specifically a pause while the pointer is up
that goes missing.

Spending the interval on a timed `pointerMove` to the coordinates the finger
already occupies was the obvious workaround and is worse: WDA read the move as
a second contact, and the page saw two `touchstart`s before either `touchend`.

So iOS does not build this gesture from a chain at all. WebDriverAgent ships
`/wda/doubleTap`, the platform's own primitive, and the backend calls it —
which is the driver layer doing its job rather than a parallel implementation.
A backend exists to say how a gesture is expressed on its platform, and asking
for a primitive the platform provides is categorically better than assembling
something that imitates it. The timing is then Apple's rather than a constant
chosen here.

### 68. A WebView cannot witness a drag, and fails exactly at 500ms

**Found by:** building the witness before the gesture had been measured. The
rotate page computes its own angle from `touchmove`, so a page seemed like the
obvious place to measure a drag too: time the finger before it moves, count the
moves, measure the travel, check where it came up.

It reported nothing at all. `touchstart` fired and `moves` stayed at zero,
while a swipe over the same two points produced nine moves and a clean set of
numbers — so the page could see a gesture, just not this one.

Walking the hold down found a hard edge:

| hold | moves the page saw |
| --- | --- |
| 200ms | 23 |
| 400ms | 25 |
| 480ms | 23 |
| **520ms** | **0** |
| 700ms | 0 |

That is Android's 500ms long-press timeout, `ViewConfiguration.getLongPressTimeout()`.
Past it the WebView claims the press as a long press of its own and the page
stops receiving `touchmove` entirely.

Which makes a WebView unable to witness the one case worth witnessing. A drag
holds *past* that timeout on purpose, because that timeout is what a
drag-to-reorder list arms on — the same number, load-bearing on both sides.
**A control that can only see the gesture when it is configured wrongly is not
a control**, and a check built on it would have passed while asserting
something no user would ever do.

The witness is native now. The general lesson is the one this file keeps
relearning from a new direction: choose the witness after establishing it can
see the thing, not because it worked for the last gesture.

### 69. A move event is not movement

**Found by:** the same native witness reporting a 700ms opening hold on Android
and a 28ms one on iOS, for one gesture delivered by one code path.

iOS delivers touch-move events for a finger that is holding still. The drag's
opening hold produced 91 move events against a swipe's 47, the first arriving
28ms in — so "time until the first move event" measured when the platform
started talking, not when the finger started moving. Android sends nothing
while the finger is stationary, so the same measurement was right there, which
is the worst case: a witness that is correct on one platform and quietly wrong
on the other reads as a platform difference in the *tool*.

The clock now starts at the first event that has displaced the finger by more
than 2dp. That fixed Android and left iOS still under-reporting, at 105ms for
a 700ms hold — because React Native grants the pan responder partway through
the hold, so the timer had not started when the hold began.

The opening hold is genuinely delivered on iOS; it just cannot be measured
from inside the app. Wall-clock timing settles it from outside, growing by
twice the hold as the hold is raised, and that is what the check asserts. Two
witnesses disagreeing is not a reason to pick the convenient one — it is a
reason to find the measurement neither of them can distort.

### 70. iOS has no witness for a double tap at all

**Found by:** trying every one available, after 67 had fixed the gesture
itself.

A WKWebView does not synthesize `dblclick` from XCUITest-injected touches.
Not from a W3C chain, and **not from WebDriverAgent's own `/wda/doubleTap`** —
the platform's own primitive, delivered back to back with no gap the page can
see. A React Native `Pressable` is no better from the other side: it coalesces
the two taps into a single `onPress`, so the app that receives them cannot
report two.

Android confirms the gesture directly — the page fires `dblclick`, and two
ordinary taps a second apart do not — so the asymmetry is real rather than a
gap in the effort spent on each.

What the check asserts on iOS is therefore weaker, and says so where it runs:
a double tap arrives as one press where two taps arrive as two. That is
indirect, it is asserted rather than skipped, and the reason is the one this
project keeps arriving at — a silent skip cannot be told from an untested one.
The tool's own answer says the same thing, in the same shape as `zoom` and
`rotate`: the gesture was delivered, and on this platform that is all anything
here can confirm.

The Pressable half of this was wrong, and 149 is why: it was reporting
WebDriverAgent's double tap, not coalescing a real one. A person's double tap
reaches it as two presses, and so does mobium's now; the Pressable is the
witness iOS lacked. The WKWebView half stands.

### 71. The first read after an app switch took 61 seconds, and read the wrong app

**Found by:** the first `launch` on a real iPhone — an iPhone 15 Plus, iOS
26.6.2, on 2026-09-22. It took 62.6s and answered "launched
com.apple.Preferences, but com.apple.mobilecal is in the foreground", which
was false by the time it was printed.

Reproduced without Mobium, with nothing but WebDriverAgent's HTTP endpoints:
launch Calendar over Settings, read `/source` — **61.4s, and the tree returned
was Settings'.** The next read took 1.8s and was Calendar. Four trials, four
stalls. The simulator never showed it.

The runner's own log named the cause: `Fetching of XC_kAXXCAttributeIsElement
value for XCUIElementTypeOther took 60.009s`. For a while after a switch iOS
lists both apps as active — and `/wda/apps/state` reports **both** in the
foreground state, 4 — so WebDriverAgent, left to choose, snapshots the app
being suspended, and one attribute fetch against it sits out a 60-second
accessibility timeout.

Two things that looked like fixes were measured and were not:

- `accessibilityDeadline`, WebDriverAgent's own guard for an unresponsive app,
  set to 3s: four stalls in four trials either way. The fetch that hangs is not
  one it wraps.
- waiting for the new app's state to reach foreground before reading: it is
  already there on the first poll, and so is the old one.

What worked is telling it. `defaultActiveApplication`, set to the app about to
come forward *before* the switch, read the right app in 0.8–3.4s across nine
switches and three app pairs; set to the home screen before Home, 5.9s. A hint
left pointing at the previous app made the next switch read stale and stall
again, so it is set per switch, not once.

**It covers only the switches Mobium starts** — `launch`, and `press home`.
A tap that opens another app, a notification, a link that leaves the app:
none of those say in advance where they are going, and after any of them the
first read on a phone can still stall. `docs/checks/ios-device.sh` times every
switch it makes and fails past 40s; with the hint disabled it failed on the
second launch, at 64.8s, which is what makes it a check rather than a hope.

**When the hint comes off matters as much as that it goes on** (2026-09-30).
A launch confirmed by a read without `visible` took the hint off sooner than
the full read had, and the next read hung for a minute once in eleven
launches and once in fifty. Both were launches over another app, confirmed
at 0.45s. Polling both apps' states through a switch on the iPhone 15 Plus
showed why: from about 0.33s to 0.64-0.89s iOS reports both the app coming
and the app being left as in front, and a read pinned to the new app sees
it there. The hint now comes off only once the app being left no longer
reads as in front — SpringBoard, which always does, is not waited for — and
a hundred launches over fresh sessions had no hang, at a median of 0.89s.

### 72. `scroll-to` gave up on an off-screen row before its first swipe

**Found by:** scrolling to Legal & Regulatory in Settings > General on the
iPhone. `scroll-to 'label=Legal & Regulatory,role=button' --direction down`
answered "matched an element with no on-screen bounds" and did not swipe.

A real iPhone keeps rows below the fold in the hierarchy, and reports them
unevenly: the **cell** keeps its real frame (y=1530, `visible="false"`), while
the **button inside it** is at 0,0 with zero width and height. `pickOne`
treated a single match with empty bounds as a hard error. It is the same
answer as "not here yet" — it is off screen — and wants the same response,
which is to scroll, so it is now the not-found error that `scroll-to` and the
implicit scroll act on, with a remedy that can work rather than "run app_map
again".

Shared code, not the phone's: Android and the simulator had simply never
produced a zero-bounds match. The phone had one on its first long list.

### 73. `devicectl` uninstalls an app that is not installed, successfully

`xcrun devicectl device uninstall app com.example.not.installed` exits 0 with
`"outcome": "success"`. Mobium already confirmed an uninstall by listing
afterwards — and an app that was never there passes that check trivially, so
`uninstall` reported "uninstalled com.example.not.installed". The phone's
device layer now checks the app is installed first. The same family as 25 and
26, from the tool Apple ships for real devices.

### 74. `devicectl`'s app list leaves out the App Store

On a phone holding 16 App Store apps, `devicectl device info apps` listed the
6 a developer had installed, and **`--include-removable-apps` listed the same
6**. Only `--include-default-apps` or `--include-all-apps` included the App
Store apps, along with 69 of Apple's. So `apps` always fetches everything and
filters out Apple's unless `--system` asked for them — the same question the
simulator and Android answer.

### 75. A refusal on an iPhone explained Android, and advised something that also fails

`clipboard` on the phone answered with the Android reason for not reading a
clipboard — "the UiAutomator2 server has no activity of its own" — and
suggested passing text to write one instead. A phone cannot write it either:
both directions go through `simctl` on a simulator and `devicectl` has
neither. Obeying the advice would have failed a second time. The refusal now
gives the phone's own reason.

### 76. The fix for 71 hid system dialogs from `map`

**Found by:** MobiumApp on the iPhone, the same day. The app's location prompt
was on screen and `alert` reported it word for word — and `map` listed only
the app underneath: Back, the two drafts, "Ask for location". Tapping "Allow
While Using App" answered "no element matches". `current` said
`dev.mobium.mobiumapp`.

It was 71's hint, still in force. `defaultActiveApplication` had been set to
MobiumApp by the launch and never taken off, so WebDriverAgent went on reading
MobiumApp — and a system dialog is not the app's window, it is SpringBoard's.
Set back to `auto`, the same read mapped Allow Once, Allow While Using App and
Don't Allow, and `current` said `com.apple.springboard`.

So the hint is now a per-switch thing, not a setting: it goes on before a
launch or Home, and comes off on the first read that shows the app it named —
or at once, if the launch or the press failed. The stall stays fixed, because
it is a property of the switch rather than of the hint's absence: afterwards
[checks/ios-device.sh](checks/ios-device.sh) passed with every switch under
14s, and [checks/mobium-app.sh](checks/mobium-app.sh) passed on the phone,
permission dialog included.

The general shape is worth more than the instance. A fix that changes a
setting for one moment and leaves it set has changed it for every moment
after, including ones nobody measured.

### 77. A selected button was named "1"

**Found by:** Wikipedia's onboarding on the iPhone, the first App Store app
driven on iOS. The last personalization page offers two choices as plain
buttons, and `map` printed `@e2 1 (button)` for the chosen one next to a
correctly labeled "Personalized content".

The chosen button carries `value="1"` and the Selected trait; the other has no
value at all. `map` prefers an element's value to its label, which is right
for a text field and was already wrong for a Switch (65). A bare 0 or 1 next
to a real label, on anything that is not a text field, is now read as a state
rather than a name. A text field is exempt: "1" is what a quantity field
holds.

### 78. The featured article, and every link in an article, had no entry in `map`

**Found by:** the Wikipedia feed. The featured article's card was on screen and
tappable, and `map` listed its "Save for later" and overflow buttons and not
the card itself.

`accessible` is the signal `map` has trusted on iOS since 15, and here it was
on the wrong element. The cell — the thing a finger taps — is
`accessible="false"`; the title *text* inside it is accessible, and text is
content, not a target. So nothing picked the card up.

An article had the same shape one level down. WebKit publishes a page's links
in the native tree as XCUIElementTypeLink with real frames, and marks each one
not accessible with the text inside it accessible: eight visible links on the
first article, none in `map`.

Both are element types that exist to be tapped, so both are now targets when
the platform put `accessible` on their contents instead: an innermost cell
with text in it — not a section cell wrapping a whole row of cards — and a
visible, labeled link, which is also reported with the role `link` on iOS
(Android keeps not calling anything a link, for the reason in defect 1).

The link half matters more than it looks. A phone's WebViews cannot be
attached yet, and these native links are what let
[checks/third-party-app.sh](checks/third-party-app.sh) follow one anyway —
from Ada Lovelace's article to Charles Babbage's, with an ordinary tap.

### 79. A label with a newline in it printed as two lines

A search result read "Delilah S. Dawson Redirected from: Ava Lovelace" on one
line of `map` and "American author (born 1977) (button)" on the next — the
app's text had a newline in it, and `clean` collapsed whitespace only when it
had also found markup. `map` is one entry per line, and anything reading it
line by line saw two entries, one without a ref. Whitespace is collapsed in
every label now, on both platforms.

### 80. A locked phone meant a two-minute wait and the wrong answer

The phone locked itself during a pause. The next command started the runner,
and `xcodebuild` logged "Unlock <name>'s iPhone to Continue … because the
device is locked" — and then waited rather than exiting, so Mobium waited out
its full two minutes and answered with a generic "did not answer". The cause
was in the log the whole time. The runner's log is now read while waiting, and
a locked phone is reported at once, with the fix.

### 81. The advice for a missing Safari page was a simulator command

On the phone, with Safari open and no page listed, `contexts` said to run
`xcrun simctl spawn booted defaults write com.apple.mobilesafari …` — a
command that reaches the booted *simulator*, if there is one, and never the
phone. Obeyed literally, it changes the wrong device and nothing improves. The
hint was chosen by backend, and WebDriverAgent is both. It now asks the driver
whether it is on a phone, and there points at Settings > Apps > Safari >
Advanced > Web Inspector.

### 82. A Settings switch mapped three times, and only the nameless entry flipped it

Turning on Reduce Motion by its `map` entry failed on the phone: `check @e4`
tapped `Reduce Motion (switch)` and read back "still unchecked". iOS 26's
Settings draws a switch row as a switch inside a switch, inside a cell. The
outer switch carries the name, the ID and the state, and its frame is the
whole row. The inner one is the toggle, with no name. `map` listed three
entries: the cell (made a target by defect 78's rule for cells with text), the
outer switch, and the inner one, printed as `XCUIElementTypeSwitch` and
located only by path. Every action aims at a node's center, and the first two
centers land on the words, which do nothing when touched. **Only the entry
nobody would pick worked.** `check` caught it because it reads back; a plain
`tap` would have reported success.

The parser now folds the row into one entry: the outer switch takes the
toggle's frame, the toggle stops being a target, and a cell holding a switch
is no longer one. `map` reads `@e3 Reduce Motion (switch, checked)`, derives
`testid=REDUCE_MOTION,role=switch`, and flips the switch, confirmed on the
simulator by `com.apple.Accessibility ReduceMotionEnabled` and on the phone by
reading the switch back. The fixture is the captured screen,
`ios26-settings-motion.xml`, and the test failed before the fix.

One false lead on the way, recorded because it cost time. A hand-written
`label=Accessibility,role=button` matched nothing and looked like `map` and
`role=` disagreeing. It was a previous failed `tap` that had scrolled the row
off screen while looking for something else. Both locators agree on a fresh
screen.

### 83. A relaunched app's WebView vanished until the daemon restarted

**Found by:** the reorganized gestures check, which starts MobiumApp fresh for
every section. The Rotate page was on screen and `contexts` listed nothing —
nor the WebView Demo, nor Safari — which looked like the simulator's web
inspector had wedged, and for a while was taken for exactly that.

It was Mobium. The held Remote Web Inspector connection (defects 45 and 46)
learned the application list once, at setup, and a relaunched app is a new
process with a new identifier: webinspectord announces it with
`_rpc_applicationConnected:` and the old one with
`_rpc_applicationDisconnected:`, and nothing was listening after setup, so the
connection went on asking the dead process for pages. A fresh daemon saw the
page at once. The inspector now keeps one subscription for the life of the
connection and applies both announcements to its set; subscribed before the
announce, so nothing said in between is lost.

The control that separated the causes: relaunching *without* ever attaching
lost the page too, which ruled out the attach-and-detach path defect 46 is
about.

### 84. UiAutomator2 forgets a finger that pauses

**Found by:** press-tap, on the Multi-Touch pad, which said "one finger" for a
gesture the tool reported as two. Android's own pointer-location overlay
agreed — `P: 0 / 1`, against `P: 0 / 2` for a two-finger tap, its control.

UiAutomator2 (10.6.6, `ActionsExecutor.executeMotionEvents`) builds each W3C
step's `MotionEvent` from the fingers that have an event in that step, and
counts the fingers already down from the same events. A holding finger that
pauses has no event, so while the second finger landed it was not counted and
the landing went out as a fresh one-finger `ACTION_DOWN`. Fixing that exposed
the second half: while the second finger rested it was dropped from the
`MOVE`, Android took it as gone, and its lift — sent later as
`ACTION_POINTER_UP` for a pointer nobody was tracking — never arrived. The
pad's raw event log showed it (`down 1@300, up 0@500`, no `up 1`), and a
harness sending shapes straight to the server found the rule: **a finger that
is down never pauses; it holds still by moving to where it already is.**

Pinch had used two fingers for months without meeting either half, because
both its fingers move in every step. The pad needed two fixes of its own on
the way: it had waited for every finger it had seen to lift, so one lost lift
froze it for every reading after, and it now closes a gesture when the
platform reports no fingers down and says which lift it never saw.

### 85. WebDriverAgent touches a late finger's target before the gesture

**Found by:** the same pad on the iPhone 17 Pro simulator, whose event log
read `down 0@0, down 1@0, up 1@0, down 1@300, up 1@384, up 0@517` for a
press-tap. The real timeline is right; in front of it is a zero-length touch
at the second finger's target, at time zero.

WebDriverAgent turns a chain's first move into the touch itself
(`initForTouchAtPoint`), and a chain that started with a move and then waited
first arrived as three fingers — so the second finger now pauses until its
moment. That removed the extra finger and not the phantom. It survived every
shape tried, including the simplest possible chain sent straight to
WebDriverAgent (`pause 300, move, down, up`) and one whose first action was a
300ms move: XCTest gives any finger that lands after the gesture has begun an
instant touch at its target at the start. The same raw chain on the iPhone 15
Plus (iOS 26.6.2) gave the same phantom, so it is not the simulator's. React Native reuses the identifier,
so the pad first merged the two into one finger; it now keys a finger by its
identifier and by how many times that identifier has landed.

An app can take that instant touch as a tap, so press-tap would also be a tap
on its target, before the press it belongs to. Mobium refuses press-tap and
press-drag on WebDriverAgent with that reason (`unsupported`, exit 5), and the
check asserts the refusal so a WebDriverAgent that fixes it shows up as a
changed answer. Two- and three-finger taps land every finger together and
work.

### 86. On Android 16 and later, one press-tap breaks injected touch for the whole device

**Found by:** the gesture check on a Pixel 8 Pro (Android 17), where
press-tap failed with UiAutomator2's "Unable to perform W3C actions" — and
then so did everything after it, plain taps included, and `adb shell input
tap` too.

UiAutomator2 (10.6.6, the newest release) stamps each event with the down time
of the finger that produced it (`startTimestamp + event.startDelta`, the
chain's own `pointerDown`). Android's contract is one down time per gesture,
the first finger's. Android 17 verifies injected input: it accepted the second
finger's `POINTER_DOWN`, then rejected every later event as "Down time went
backwards" — including both lifts. So both fingers stayed down in the input
dispatcher's state for injected touches, and every injected gesture after it
was rejected as "Invalid DOWN event - pointers already down: {0, 1}". Not per
session, not per process: a UiAutomator2 restart did not clear it, and
neither did Home or a single-pointer `CANCEL` (rejected: "the pointers don't
match").

**The recovery is a two-finger tap.** Its landing is rejected like everything
else, but its closing `POINTER_UP` and `UP` carry both pointers and are
accepted, which empties the state; a plain tap works again at once. That is
also why the Pixel "recovered" once earlier, unexplained at the time.

The same failure on an Android 17 AVD; none on the Android 15 AVD, which does
not verify. No chain shape avoids it, because a late finger's down time is
its own by construction in the server. So UiAutomator2 refuses press-tap and
press-drag above API 35, naming the reason; Android 16 (API 36) is refused
unmeasured, because a wrong guess costs the device's touch input rather than
one gesture. The check reads the API level and asserts the gesture or the
refusal accordingly. Multi-finger taps land together and are unaffected.

Two findings on the way, both about witnesses rather than tools:

- **A slow emulator shortens a drag's closing hold, for real.** On the
  Android 17 AVD UiAutomator2 injected the travel's moves about 33ms apart
  instead of its scheduled 20, so the 800ms travel ran ~1.3s; the lift is
  scheduled against the clock and came on time, and the 700ms closing hold was
  delivered as ~180ms. The Pixel 8 Pro on the same Android held 719ms. `drag`
  reports the hold it asked for, not the one delivered.
- **The drag witness measured the closing hold wrongly**, from the JS clock
  and from distance to the *start* — so every move event Android sends for a
  finger resting at the far end counted as travel. It now uses the native
  event timestamps and movement since the previous event. Finding this is
  what exposed the real shortening above.


### 87. A line logged between two reads of the device log vanished

**Found by:** the first run of `app_logs --source device` on the Pixel 7 AVD,
before it shipped, with a probe line written between two reads.

A device-log read returns what arrived since the last one, capped at `lines`,
keeping the newest; the next read starts after the newest line returned. On
the AVD, UiAutomator2's startup logged well over a hundred lines between two
reads, one per `/status` poll, so the cap kept the last hundred, the mark
moved past them, and the probe — older than all of them — was never returned
by anything. Nothing said so. A caller waiting for one line would have read
"no" from a device that had logged it.

The limit still keeps the newest, which is what an agent reading "what just
happened" wants; the read now **counts what it dropped**, in `skipped` and in
a first line of text saying how many and how to narrow the read. Measured
after the fix: 150 lines written between two reads against a limit of 20
reported 135 skipped, the arithmetic exact.

Two measurements made the design, and are what `docs/checks/crashes.sh`
asserts:

- **logcat's own time filter cannot be trusted to exclude anything.** `-T`
  with a time later than every entry still printed the newest line, on
  API 35. The bound is applied in Mobium, strictly after the mark, by the
  device's clock; `-T` is passed only to keep the transfer small.
- **An app is filtered by UID on Android and by executable on iOS, never by
  pid**, because a crash restarts the process and the lines worth reading are
  on both sides of it. Settings shares UID 1000 with the system, so a read
  narrowed to it is the system's too; the read says so.

### 88. A simulator's UDID was "no device", and the advice listed it

**Found by:** driving the iPhone 17 Pro simulator with `--device <UDID>` and
the default backend, while verifying 87.

`mobium --device 457C7DC2-… current` failed with `no device with serial
"457C7DC2-…" (see \`mobium devices\`)` — and `mobium devices` lists that
simulator, booted, on its second line. Each backend's lookup knows only its
own platform: the default is Android's, which asked adb, and adb has never
heard of a simulator. The listing asks both. So the error was true of adb,
false of the machine, and its remedy sent the caller to a page that
contradicted it; followed literally, it loops, which is the rule about
remedies this project already wrote down once. The reverse — an emulator's
serial with `--driver wda` — failed the same way, as did a real
iPhone's UDID.

A missing device for a named serial is now checked against the other
platform before it is reported, and the error names the backend to use,
spelled for both front doors: `pass backend "wda" (on the CLI,
--driver wda)`. It is `invalid_argument`, not `no_device`,
because the device is there and the combination is what is wrong. A serial
neither platform knows keeps the old answer, which is right for it.
Choosing the backend from the serial automatically would remove the error
altogether, and is a larger change to what an unset backend means.

Since 2026-09-30 it does, for the one case where nothing was asked: a device
named with no driver named. When Android's lookup does not find it and it is
an iPhone or a simulator, the call goes to `wda`, the only driver either has.
A driver named explicitly is still the caller's choice, and one that cannot
drive the device is refused as above, now adding that naming no driver
works. Measured: the iPhone 15 Plus by UDID alone started in 5.4s and the
next call took 0.5s; an iPhone 17 Pro simulator by UDID alone started; and
the iPhone with `--driver uiautomator2` was refused.

### 89. A stuck tool call made the daemon impossible to stop, and invisible

**Found by:** the deadlock introduced — and fixed — while building 87: a
tool call that blocked forever. SIGTERM did not stop the daemon, `kill -9`
did.

Shutdown closes the listener, waits up to 10 seconds for in-flight calls,
then closes every device session under the handlers' lock — the lock a
stuck call holds for as long as it is stuck. So shutdown waited forever. And
it had already closed the listener, which unlinks the socket, so every
client saw no daemon, tried to start one, and was refused: the PID file
named a process that was still alive. A daemon that cannot be stopped and
cannot be reached is the worst of both, and the only way out was `kill -9`.

Closing the sessions is now bounded at 20 seconds; past that the daemon
says on stderr that a session was left open, removes its PID file and
exits. A second signal exits at once. Leaving a session open is a real cost
— an instrumentation may keep running on the device — and it is said rather
than hidden, with the cleanup in SHUTDOWN.md. A test makes closing hang and
was shown to fail on the old code, after its full five seconds.

### 90. An empty read put `null` where a list belonged

**Found by:** the phone half of `docs/checks/crashes.sh`, whose first read
of a phone's log — made the moment its capture started — came back empty,
and whose JSON parse then failed on `len(None)`.

`app_logs` and `app_crashes` put a Go slice straight into their structured
result, and an empty Go slice encodes as `null`. The Python client read
`data.get("entries") or []` and never noticed; the Go client decodes `null`
into an empty slice and never noticed; anything that trusted the field to be
a list, as the check did, broke on the one answer an assertion most often
wants — "nothing". Both views, and the WebView console's, now send `[]`.

### 91. Every client raised its base exception for every failure

**Found by:** `docs/checks/clients.sh`, the first end-to-end run of all five
clients against a device, written for the release checklist on 2026-09-25.
Its one deliberate failure — a tap on a locator that matches nothing — came
back as `MobiumError`, `MobiumException` and a bare Go error in all five,
never as the `no_such_element` type each client maps that code to.

The code never reached them. Clients spawn `mobium pipe`, and `pipe` turned
a failed call into a result with its text and no `structuredContent`, where
the MCP server built the same result with the structured half. Two front
doors, two copies of one answer, and the copy every client used was the
incomplete one. Nothing had caught it: the error codes' measurements went
through the CLI, which reads the daemon's error itself, and each client's
error tests feed a hand-built result to its mapper, which is exactly the
half that works.

Both now call `agent.ErrorResult`, and the check drives all five through
`pipe`. Measured after the fix: all five raise their own exception.

### 92. The advice for a missing driver could not work with a daemon running

**Found by:** `docs/checks/external-driver.sh` in the release batch, which
failed at once with "no mobium-driver-adb on your PATH" — while exporting
`MOBIUM_DRIVER_ADB` three lines earlier.

The daemon looks a driver up, in the environment it started with; one left
running by an earlier command never sees a variable set afterwards. The
check had always been run first, on a fresh daemon. And the product's error
gave the same advice the check followed — "set MOBIUM_DRIVER_ADB=…" — which,
with a daemon up, cannot work however often it is followed. It now adds
"then run `mobium daemon stop`", and says why; the check stops the daemon
before it starts.

### 93. Every Android session ended by leaving a crash report on the device

**Found by:** `app_crashes` itself, run on a Pixel 8 Pro (Android 17) while
hardening it: of the phone's recorded crashes, most were
`io.appium.uiautomator2.server` — `RuntimeException: Error while
disconnecting UiAutomation`, caused by `DeadObjectException` — each one at
the moment a Mobium session had ended.

Teardown cancelled the host of `adb shell am instrument`, which takes the
UiAutomation connection down with it; the instrumentation then finished,
tried to disconnect over the dead connection, and crashed on the way out.
Android files that in dropbox. So Mobium's own clean shutdown was writing a
crash report onto the user's phone every time — nine on this one before it
was seen, and one per session, reproduced by counting dropbox entries
across a single start and stop. It had been visible on the emulator all
along too, as 30-odd entries CHALLENGES recorded under "not defects",
reading them as the server's problem rather than Mobium's.

The server's own `/shutdown` does not exist in 10.6.6 (measured: unknown
command). A force-stop of both its packages, before the host is cancelled,
ends it with no record. Measured after the
fix: three sessions, three stops, and a SIGTERM with a session live, the
dropbox count unchanged at 12 and no server process left. Only a server
this session started is stopped, as on iOS.

### 94. A real ANR was summarized by its memory-pressure heading

**Found by:** the same phone's dropbox, which held two ANRs — the first
real ones Mobium had read; the emulator had none.

The summary looked for the ANR's `Subject:` line in the entry's header, the
lines above the first blank one, which is where the format was assumed to
put it. Android 17 puts it at line 47, after a blank line and the memory,
CPU and I/O pressure dumps, so both ANRs would have been summarized as
`----- Output from /proc/pressure/memory -----`. The Subject is now looked
for anywhere in an ANR; a trimmed copy of the real entry is the fixture —
cut before the CPU-usage list, which names the phone owner's apps.

### 95. A launch onto a locked phone reported success

**Found by:** the client flows on the Pixel 8 Pro, rerun after its screen
had gone to sleep during an emulator run: Python and JavaScript failed at
their first step — `launch` returned, and the foreground was not the app.
Go, Java and .NET, running a minute later, passed; by then the phone was
awake.

Reproduced on purpose: with the screen asleep and locked, `am start`
succeeded, no activity was resumed, `current()` answered
`com.android.systemui`, and `launch` returned success. Its text did say
"launched …, but com.android.systemui is in the foreground", and its
structured result named the app that was not on screen, which is what every
client reads. Everything after it then acts on the lock screen. A phone left
idle during a run gets there on its own, so this is the ordinary case; the
iPhone met the same thing as defect 80.

`launch` and `open` now ask whether the device is locked when what they
started is not in front, and refuse as `device_not_ready` if it is. Anything
else in front — a permission dialog, a chooser — stays a note, being the
app's own doing. The remedy names `mobium lock unlock` and `app_lock` with
state "unlock", which dismisses a lock with no credential and says so when
there is one; both halves were followed on the Pixel, and a test ties the
wording to the schema, because the first draft named a `lock off` command and
a `locked` argument, neither of which exists.

### 96. A log narrowed to an app left out the line saying it had hung

**Found by:** hardening crashes and ANRs against the distinction drawn in
[ANR vs crash](https://github.com/lana-20/anr-vs-crash) and
[logcat vs bugreport](https://github.com/lana-20/android-crash-anr-logcat-bugreport):
an ANR caused on purpose in MobiumApp on the emulator, then
`mobium logs --app dev.mobium.mobiumapp`.

Android logs `ANR in dev.mobium.mobiumapp`, its reason and its parent from
system_server, under UID 1000 — not from the app. The app filter reads logcat
by the app's UID, so it returned **none** of those lines: zero, measured,
while the unfiltered device log had all three. Narrowing to the app is the
natural thing to do and it hid the one line that mattered. The read now asks
logcat for the app's UID and the system's, and keeps system_server's lines
that name the package — nothing else that shares UID 1000, and nothing when
system_server's pid cannot be read. A real `-v epoch,uid` capture, with the
ANR lines in it, is the fixture.

### 97. The ANR dialog could be read and not answered, and the error said nothing

**Found by:** the same controlled ANR. `app_alert` read it — "MobiumApp isn't
responding", Close app, Wait — and both `accept` and `dismiss` failed with
UiAutomator2's `invalid element state: The expected button cannot be
detected on the alert`, which names no way through. Tapping "Close app" by
its `map` ref closed the app, measured. The refusal is now `unsupported`
and says to tap a button by its ref, keeping the server's error behind it.

### 98. On iOS too, a log narrowed to an app left out the line saying it died

**Found by:** carrying defect 96 over to iOS, on the iPhone 17 Pro simulator
and the iPhone 15 Plus. When an app dies, the line that says so is not the
app's: it is runningboardd's, the process that starts, suspends and ends
apps — `[app<com.apple.Preferences((null))>:80109] termination reported by
launchd (2, 6, 6)` on the simulator, the 6 being SIGABRT, and the same
shape on the phone for MobiumApp's Crash Demo. The app filter kept only the
app's own process, so it never held that line.

Keeping every system line that mentions the app would drown it: a dozen
processes named Settings 400 times in eight seconds — SpringBoard's audio
checks, badges, location polls. So, as on Android with system_server, only
the lifecycle process's lines that name the app as `app<bundle-id>` are
kept: on the simulator 170 of 1,210 lines in the window measured. The
phone's relay gets the same rule, and now also refuses a level the unified
log has no slot for, as the simulator did. Its capture restarting after the
stream drops — reachable on a real phone only by pulling the cable — is
tested against a fake phone that hangs up.

### 99. `app_type` echoed the password it had just typed

**Found by:** the password test written for `app_keyboard`, which failed on
the new tool's confirmation — `typed "hunter2" into password` — and then on
`app_type`'s, which had said the same thing since it was written:
`typed %q into <target>`. The value was the caller's own, but echoing it puts
the password into every transcript and log the result reaches, which is the
exact thing the rule against printing a password field exists to stop
(defect 43). Both now report the length and say it is not echoed, and the
structured result carries no copy. Tests type "hunter2" and require it
nowhere in the result.

### 100. A relative path was saved relative to the daemon

**Found by:** reading how `app_screenshot` resolves its path while building
`app_record`, then measuring it: a daemon started from one directory, and
`mobium screenshot -o rel.png` run from another, saved `rel.png` into the
first and reported that path as a success. The daemon resolved the path
against its own working directory, which is wherever it happened to start —
the same for the default `screenshot-<time>.png`, for `install <apk>` and
for `location --gpx`, and for every client, which reaches the daemon through
`pipe` with its arguments untouched.

Only the caller knows its directory, so the caller resolves it. The tool
layer names every path argument in `agent.PathArguments`; `daemonCall` —
the one function the CLI and `pipe` share — makes them absolute before the
call leaves the caller's process; and a test fails if a schema grows a
path-like argument the table does not list. Measured after: the screenshot
landed where it was taken.

### 101. A phone refused without saying why, and the check called it a reason

**Found by:** `docs/checks/record.sh` on the iPhone 15 Plus, which asserts
that a phone's refusal to record names its reason, and got "the
wda backend cannot record the screen" — true of the phone, false
of the backend, and silent on why.

The reasons had existed all along, in WebDriverAgent's table of what only
simctl does, but only the driver's own last-line guard used them; the tool
layer refused first, generically, for appearance, permissions, location,
the clipboard write and recording alike. And `ios-device.sh`, whose comment
promised "each with the reason", matched that generic wording — "cannot
read or change the appearance" — so it passed on refusals that gave none.
A driver can now say why it declines a capability (`mobiumdriver.Declined`),
every simulator-only refusal asks first, and the check asserts the reason:
"switch light and dark (simctl ui)".

The same gap was wider than the phone. Listing every built-in backend's
missing capabilities against whether it could say why found eight more
refusals with no reason: orientation, a per-app language, the timezone,
notifications, calls and messages on iOS, simulator and phone alike, and
the clipboard write and dialogs on the adb-only backend. Each now names
the reason, worded for what is known — orientation and language are "not
built yet", because WebDriverAgent has an orientation endpoint and a
simulator takes `AppleLanguages`; calls and messages are "cannot", because
a simulator has no telephony and a phone cannot be made to ring from
outside. Every generic refusal in the tool layer asks the driver first, and
`TestEveryBuiltInRefusalSaysWhy` fails for a backend that lacks a
capability and cannot say why; removing one reason makes it fail.

### 102. An empty password field was reported as a hidden password

**Found by:** the revamped login demo in MobiumApp, on a fresh screen:
`text` said the password field held `•••••••• (8 characters, hidden: this is
a password field)`. It held nothing. Its placeholder is "password", eight
characters, and both platforms put an empty field's placeholder where its
text goes.

`Redact` masked it because its comment said the two could not be told apart —
"Android reports both in `text` with no flag". Both platforms do flag it.
UiAutomator2 sends `showing-hint="true"` beside `hint`, and WebDriverAgent
reports an empty field's placeholder in plain text where typed text on a
secure field is bullets. The parser now records the placeholder and the
flag, and an empty field reads `(empty, showing its placeholder)`; the raw
source leaves a placeholder unmasked for the same reason. The dump backend
sends neither, so there it is still masked.

On the way, a measurement went through the tool under test. iOS appeared to
mask the placeholder too — `value="••••••••"` — until curl against
WebDriverAgent showed `value="password"`: the bullets were `mobium
source`'s own redaction, added that day. The first fix was built on that
reading and had to be taken out.

### 103. Typing into a password field on iOS appended to it

**Found by:** the login demo, where a wrong password followed by the right
one never logged in. The field held 17 characters after "wrongpass1" and
then "hunter2", while `type` reported typing seven.

WebDriverAgent types through the keyboard, so setting a value appends. An
ordinary field was right anyway, because `SetText` reads it back, sees the
mismatch, clears and retries. A password field was exempt from the read-back
— it reads as bullets and could only ever disagree (defect 61's rule) — so
nothing noticed; since defect 159 its length is read back. It is now cleared before typing, which makes `app_type`
replace a field's contents on iOS as UiAutomator2 does on Android, password
or not; the test fails if the clear is removed.

Since 2026-09-27 that is `app_fill`, Vibium's name for it, and `app_type`
adds to a field, as Vibium's type does. Neither server can type at the
cursor, so an append is the field set to what it held plus the text, through
the same cleared and confirmed `SetText`; a password that holds anything is
refused, since it cannot be read back to add to. The login demo, which found
this, now fills its fields.

### 104. `testid=password` matched seven elements

**Found by:** the login demo on iOS, once the form had visible labels. A
hand-written `testid=` matched by case-insensitive substring, and on iOS a
label's identifier is its own text, so `testid=password` matched the field,
its "Password" heading and a hint that mentions the password. On Android the
ordinary naming pattern did it on its own: `username` beside
`usernameError`.

A test ID is an identifier, so it now resolves exact first: if exactly one
node's ID is the value, case and all, that node is the answer; otherwise
substring matching applies as before, so every partial-ID locator that
worked still works. Chosen over always-exact for that reason. The existing
test that expected `testid=submit` to be ambiguous beside `submit_wrapper` now
expects the exact one.

### 105. A tap on a button under a dialog reported success

**Found by:** the login demo, then measured on purpose. With "Save
Password?" over the app, `tap testid=logoutBtn` answered `tapped
testid=logoutBtn` and nothing happened; with the app's own alert up, the
same for the Back button.

iOS puts an app's alert, and a sheet such as Save Password, into the app's
own tree beside everything it covers, and WebDriverAgent reports each covered
element `visible="false"`. `map` honored that and listed only the dialog's
buttons; a locator resolved anything. A target outside a visible
`XCUIElementTypeAlert` or `Sheet` is now refused as `device_not_ready`,
quoting the dialog, and not scrolled for — a swipe under a dialog is the
last thing wanted. The other shape needed its own fix: Android, and
SpringBoard's prompts on iOS, give the dialog's window alone, so a target
behind one is simply not found, and the answer was "run app_map again". On
that miss `app_alert` is now asked once, and the error says a dialog is up
— without claiming the target is behind it, because a misnamed button of the
dialog's own (`label=CANCEL` for a caption that is text) misses the same way.

Measured on the Pixel 7 AVD and the iPhone 17 Pro simulator: a permission
prompt on each, the app's own alert on each, and Save Password on iOS. The
two iOS captures are fixtures now, `ios-app-alert.xml` and
`ios-save-password.xml`.

### 106. `accept` and `dismiss` are not positional either

**Found by:** MobiumApp's new Dialog Demo, which raises one-, two- and
three-button alerts, an action sheet and permission prompts, and reports
which button the app received.

CHALLENGES 63 replaced "accept presses the affirmative button" with "accept
presses the last button and dismiss the first", and the project's rules stated
it as measured on both platforms. It was measured on two-button alerts, where
position, role and meaning all agree, and so it could not have failed. With
more buttons:

| Dialog | `dismiss` pressed | `accept` pressed |
| --- | --- | --- |
| Android, three-button alert (Cancel, Don't Save, Save) | Don't Save — the middle | Save |
| Android, notification permission (Allow, Don't allow) | Don't allow | Allow — the first |
| iOS, three-button alert (Don't Save, Save, Cancel) | Don't Save — the first | **Cancel** — the last |
| iOS, action sheet (Copy Link, Delete Photo, Cancel) | Cancel — the last | Copy Link — the first |

Android presses the dialog's positive and negative buttons, whatever their
position; React Native gives a three-button alert a neutral, a negative and a
positive one, in that order. iOS presses an alert's last and first buttons —
and puts Cancel last in a three-button alert — but a sheet's first and last.
The rule written down now is the only one that survives every row: the two
answer a dialog, and choose nothing. `app_alert`'s description, the project's rules and
the three docs that repeated the old rule are corrected, and the next part
of the dialog mechanism names buttons rather than leaning on either verb.

The same screen measured two more things. **The share sheet is not a
dialog to `app_alert`** on either platform — on Android it is its own
activity (`com.android.intentresolver`), and on iOS a view hosted in the app
— so it has to be answered by its buttons. And **Save Password is narrower
than it looked**: the login screen raises it for every new username, and the
Dialog Demo's own username-and-password form, submitted the same way, raised
nothing within five seconds.

### 107. A tap on a button under the keyboard reported success

**Found by:** the iOS login demo, where Log In sat under the keyboard; the
app's layout was fixed then, and the Dialog Demo's pinned button is the
case kept on purpose.

The keyboard is the dialog case one layer over, in the same two shapes. On
iOS the keyboard is an `XCUIElementTypeKeyboard` in the app's tree, and what
it covers stays there, marked not visible — the field being typed into
included — so a locator resolved the button and the tap landed on a key. A
target whose center the visible keyboard covers is now refused as
`device_not_ready`, naming both ways to hide it (`--hide`, and `--key enter`
on an iPhone, which has no hide key). A keyboard left below the screen's
edge, as a simulator does with a hardware keyboard attached, covers nothing.
On Android 15 what the keyboard covers leaves the tree — edge-to-edge is
enforced, so `adjustResize` no longer shrinks the window — so the answer was
"no element … run app_map again"; after a miss that scrolling did not
recover, the keyboard is asked, and the error says it may be covering the
target. Measured on both, and following the named remedy reached the button
each time.

Two corrections followed the same day. The refusal first fired on the
login demo's Log In, which the keyboard did cover — but inside a scroll view
the form had shrunk above the keyboard, so the right answer was to scroll to
it, as Mobium had done before; it now refuses only where scrolling cannot
help. And the Dialog Demo's check once found the tap going through, because
the simulator had switched to its hardware keyboard: the software keyboard
was still in the tree, below the screen's edge, and `app_keyboard` reported
it `shown`. "Shown" means on screen now, on the same test the refusal uses.

And Android's half had leaned on something that is not always so: that
UiAutomator2 leaves out what the keyboard covers. On a headed emulator the
Mac's keyboard is a hardware keyboard, and Gboard shows no keyboard at all —
only a floating toolbar, a pill at the left edge — and the covered button
stayed in the tree while nothing covered it. What decides it is the input
method window's **touchable region** in `dumpsys window InputMethod`: the
full keyboard's rectangle, or just the pill's (21,965)-(169,1525) and a
strip along the bottom. Mobium reads it — only while something on screen has
focus, since it costs a dumpsys — and refuses a target inside it, on
Android as on iOS; its frame, the whole screen below the status bar, would
have covered everything and meant nothing.

### 108. Retrying a dropped keystroke at the same speed dropped it again

**Found by:** the iOS login demo, run five times in a row once the dialog
and keyboard fixes were in. Two runs of three failed with `typed "nobody"
and the field holds "nbody"` — the second character lost, on both of
`SetText`'s attempts.

CHALLENGES 61's retry assumed the drop was a race that a second try would
usually win, and on a still form it did. The login form is not still: it
re-validates on every keystroke and its notice under the field changes, and
whatever that costs the keyboard, it cost it at the same point each time.
WebDriverAgent takes a typing speed with each request, `frequency`, and types
at 60 without one. `SetText` now tries three times, at the server's speed,
then 20, then 6 keys a second, and says how slowly it last tried when it
gives up; `app_keyboard`'s typing retries the same way. Five runs of five
passed afterwards. Whether the drops still happen at 60 and are recovered,
or stopped, was not counted; nor is it known what in the re-render loses the
key (the roadmap's race item 2).

### 109. On Android, a screen in motion could only be read once it stopped

**Found by:** the confetti on MobiumApp's Motion Demo, which exists to put
a screen in continuous motion on purpose. The first read during a
three-second burst took 3.75s and read the burst as over; a `find` during a
slower one took 11s, and the piece it aimed at had fallen off the screen by
then. iOS read the same burst in 0.56s, mid-fall.

Mobium never set UiAutomator2's `waitForIdleTimeout`, so the server's
default applied: before every read it waits up to 10s for the app to stop
changing. This project had written the opposite — "UiAutomator2 does not
wait for idle" (defect 13, and the dump backend's own error message) —
because the screen that was measured, the About page, ticks once a second
and is idle in between. Continuous motion never is.

Turning the wait off, WebDriverAgent's behavior, was the first fix, and it
broke launch: a tap right after `launch` landed on a row that was still
moving into place, and reached its screen 1 time in 5. The idle wait had
been quietly giving Mobium a settled screen after every transition. Measured
on the Pixel 7 AVD, five tries each:

| `waitForIdleTimeout` | After launch | Reads mid-burst |
| --- | --- | --- |
| 10000, the default | 5 of 5 | 3.73s, read as over |
| 0 | **1 of 5** | — |
| **500** | **5 of 5** | 1.3–1.7s, read as falling |

It is 500 now, set when the session opens and on every reopen, and read
back. With it a piece of confetti that never holds still is refused as
"still moving", as it is on iOS, instead of being waited out until it has
gone. The confetti also measured what Reduce Motion is on Android to React
Native: `transition_animation_scale` at 0, not `animator_duration_scale`.

### 110. With the idle wait capped, a coasting screen outlasted the retries

**Found by:** the first run of `mobium-app.sh` on the Pixel 8 Pro after
CHALLENGES 109. After the pager section's swipes the pager coasts, and the
next `map` failed three times with `Cannot set AccessibilityNodeInfo's field
'mSealed'` — the error that means the screen was changing (CHALLENGES 51).

Three tries 700ms apart had been enough only because UiAutomator2 used to
wait up to 10s for the app to go idle before each read; capped at 500ms, a
read lands in the motion, and a real phone coasts longer than the emulator.
UiAutomator2's reads now retry these failures — only these — for up to 6s,
300ms apart, and say how many attempts over how long when they give up. The
check's `ref` helper also stopped hiding the failure: it had crashed with a
Python KeyError, and now prints what `map` said.

### 111. The location check printed a real phone's position

**Found by:** the same run. `mobium-app.sh` had only ever run its location
section on an emulator, where clearing a simulated position leaves nothing
to report. On the Pixel, clearing it handed back to the phone's GPS, the
check's "still moving after a clear" assertion failed — correctly, since a
real fix does move — and its message printed the phone's own coordinates
into the log.

This project already refused to open the Location screen on a real iPhone
for exactly that reason; the rule had not been carried to Android phones.
On one now the section still runs — simulated positions are test data —
but prints nothing observed, asserts only that the position left the route
after a clear, and leaves the screen at once.

### 112. A tap on a target sliding in gave up as it arrived

**Found by:** `docs/checks/autowait.sh`, written to test auto-wait on purpose,
on its first run: on the Pixel 7 AVD, a tap on the Motion Demo's honoring
target straight after Replay failed with `is still moving after 2s`. The
target slides in over two seconds with an ease-out, so its last pixels creep
into place right up to the end, and the settle budget was two seconds.

It had never failed because it never saw the motion: until CHALLENGES 109,
UiAutomator2 blocked every read until the screen was idle, so Mobium read the
slide's end and nothing before it. The budget's comment said two seconds
"covers the standard Android and iOS screen transitions", which is true of
transitions and not of a deliberate animation. It is five seconds now, and the tap lands 2.6s after
Replay on the emulator and 2.1s on the iPhone 17 Pro simulator. Something
still moving after five seconds is continuous motion, and the exposed
confetti is still refused as that.

### 113. A tap after writing the clipboard opened Quick Share

**Found by:** the paste step added to `docs/checks/dialogs.sh`, on the Pixel 7
AVD (API 35). After `mobium clipboard`, the next tap — on Home's Dialog Demo
button at (540, 2225) — reported success, and the foreground was
`com.google.android.gms/.nearby.sharing.send.SendActivity`: "Who can share
with you", Continue. Twice, before the cause was looked for rather than
cleared.

Android 13 and later put up a preview of the clipboard after every write, at
the bottom left, as a window of its own — `ClipboardOverlay` in `dumpsys
window`, touchable at x −31 to 686 and y 1985 to 2352 — so it is in no
hierarchy UiAutomator2 reads, and the tap landed on its share chip instead of
the app. It closed by itself in about seven seconds; back does not close it,
the key going to the app underneath, which left for the launcher.

An action now asks for the preview's touchable region (`dumpsys window
ClipboardOverlay`, about 20ms, 75 bytes when there is none) and waits for it
to leave a target it covers, up to ten seconds — its own budget, since an
action's implicit wait is two and the preview stays seven. The same tap now
lands on the Dialog Demo, seven seconds later. A preview that outstays the
budget is refused as `device_not_ready`, saying it closes by itself.

### 114. A scroll that nearly had its target swiped past it

**Found by:** `dialogs.sh` on the iPhone 17 Pro simulator, once the Dialog
Demo had grown two buttons: a tap on Location permission failed with "on the
screen but never scrolled fully into view — scrolled down to the end of the
list (3 scrolls)". The button was at y 2190–2334 and the list ended at 2301:
33 pixels short. The scroll loop swiped half the list, a swipe coasts on
iOS, and the button went from just below the fold to 81–228, above the top.
Every swipe after that was against the end, so the loop reported the end,
with the button in plain sight.

Once the target has been found — it resolves, with real bounds, just not
wholly inside its container — the loop now swipes only the distance it is
out, plus an eighth of the list, from whichever side it is on. Three taps
from a fresh screen landed three times. The full swipe is still what
searches for a target nobody has found yet.

### 115. A tap under the app's own overlay reported success

**Found by:** MobiumApp's Obstruction Demo, built for the purpose after the
Dialog Demo's pinned button was seen covering the camera button while both
platforms called both visible. Each case is a target with a known cover,
every cover itself pressable, and one line saying which of them a tap
really reached. On a Pixel 7 AVD and an iPhone 17 Pro simulator, `tap`
answered "tapped" eight times out of eight, and six of those touched
something else: the full cover, the cover over the center, a plain view that
swallowed the tap, an overlay hidden from accessibility, a translucent scrim
and a toast.

Mobium already refused a target under a system dialog (105) and under the
keyboard (107). The app's own view was the kind it could not see, and what
decided the fix was what each tree can tell apart:

| Cover | Tap went to | Android tree | iOS tree |
| --- | --- | --- | --- |
| a pressable over all of it | the cover | a clickable Button | a Button; target `visible=false` |
| a pressable over the center | the cover | a clickable Button | a Button; target still visible |
| over the edge, center clear | the target | nothing over the center | nothing |
| `pointerEvents="none"` | **the target** | a non-clickable ViewGroup | an Other; target `visible=false` |
| a plain view, no handler | **nowhere** | a non-clickable ViewGroup | an Other; target `visible=false` |
| a pressable hidden from accessibility | the overlay | a clickable, unnamed ViewGroup | **not in the tree** |
| a translucent scrim | the scrim | a clickable Button | a Button; target `visible=false` |

The pass-through and the plain view are identical in both trees — attribute
for attribute on iOS, and by nothing but `drawing-order` on Android — yet one
lets the tap through and the other eats it. So nothing in a tree says
whether a cover takes touches, and a rule that refused whatever is drawn over
the point would have refused the pass-through case, which works. And iOS's
`visible` is visual, not touchable: it hides the pass-through target and
shows the one whose center is covered.

What each tree can say is whether a cover is itself a **control** — clickable
on Android, a control's element type on iOS, where `Clickable` also holds for
an accessible Other. So, before a tap, long press or `check`: a control over
all of the target is waited for within the implicit wait, which is what makes
the toast work, and then refused as `element_not_reachable`, naming the cover;
a control over the center only is aimed around, at the clear point nearest
the center; and something over the point that is not a
control is tapped through and reported in the result, `cover` in the
structured half. Measured again after the change, every row went the way
the table says it should, on both platforms, except one: on iOS the overlay
hidden from accessibility is still tapped, because WebDriverAgent's tree does
not contain it. That is recorded as the rule's blind spot, and a test
asserts it so it stays known.

**Closed on a simulator, 2026-09-29, by asking UIKit:** `mobium hit-test`
attaches lldb to the app and asks UIKit's own `hitTest:withEvent:` which
view a touch at the point `tap` would use goes to, and fails when it is not
the target, naming it and saying it is hidden from accessibility. Opt-in,
since the attach stops the app for about two seconds; a real iPhone is still
blind. `docs/checks/hit-test.sh` holds it to what a touch at the same point
really reached, case by case, and all seven agreed
([the hit test](guides/autowait.md#what-it-does-not-see)).

The first version reported a non-control cover over nearly every target on
every captured screen — iOS puts a later, transparent XCUIElementTypeWindow
or Other over whole screens, and the Android launcher its drag layer. A
note on everything is a note on nothing. One is now made only for a cover
inside the target's parent, no more than twice its size, over a target that
is not a scroll container. Run over the twelve other captured screens — three
real apps, the launcher, SpringBoard, Settings, alerts and the keyboard — the
rule changes nothing, and a test holds it to that, with the obstruction
screens as the control that it can fail.

### 116. After an accessibility setting changed, every WebView lookup took 25 seconds

**Found by:** testing MobiumApp's Motion Demo on the iPhone 15 Plus with
Reduce Motion off and then on. The web page reported its tap arriving 78
seconds after it loaded. Timed step by step, `contexts` took 25.4s and an
attach 50.5s — constant to a tenth of a second, which is a timeout firing,
not a slow phone — and both were back to 0.4s once the daemon restarted.

It was not Reduce Motion: the stall followed any real change to an
accessibility setting, in either direction, and a trip to Settings that
changed nothing did not cause it. A probe that timed each application's
answer to a listing named the cause. Before the change, six applications
were known to the web inspector and all answered at once. After it, a
seventh had joined — `com.apple.AppStore.Widgets` — and it answers no
listing. `listPages` waited for every known application to answer, up to
`rwiSetupTimeout`, 25 seconds: `contexts` lists once, and an attach lists
and then sets up, so it waited twice. The live applications' pages were
there all along, which is why it eventually worked.

A listing now waits up to the setup timeout for the first answer, since a
cold WebView can be slow, and then gives the rest two seconds from the last
answer. An application that misses that is remembered as silent: it is
still asked each time and not waited for. The first version stopped there
and was wrong in a way a test run thirty times showed — a silent
application that started answering always answered after the listing had
stopped listening, so it stayed silent and its pages were never seen
again. The inspector's watch, which sees every message on the connection,
now clears the mark on any listing it answers, late or not. Measured again
on the phone after a real change each way: `contexts` 2.5s once, while the
silent application is found, then 0.4s; attach 0.4s throughout.

The code was the same in every earlier version; nothing had changed an
accessibility setting on a phone while a daemon held the connection.

### 117. Putting Increase Contrast back left the simulator changed

**Found by:** the round trip `app_accessibility` promises — every raw value
back as it was once the session ends — diffed on an iPhone 17 Pro simulator.
Every setting read back as it had been, and the preferences domain still
differed: two keys that had been absent, `DarkenSystemColors` and
`PointerIncreasedContrastEnabled`, were now there, both 0.

`simctl ui increase_contrast` stores the setting in both, and turning it off
writes them as 0 rather than removing them. The setting means the same; the
simulator is not the same, and "put back as it was" is the claim. The undo
now switches it back through simctl, which is what tells a running app, and
then restores both keys raw, deleting any that were absent. On Android the
same promise needed the same care from the start — most of these settings are
unset on a fresh emulator, not off — and a snapshot deletes them again.

### 118. A tap inside a WebView was never checked, and went wherever the page put it

**Found by:** MobiumApp's Actionability page, the Obstruction Demo's web
counterpart, built once native actions had learned to wait and to refuse.
In a WebView none of that applied: a tap resolved its ref to the element's
rectangle and touched the center. On a Pixel 7 AVD and an iPhone 17 Pro
simulator, `tap` answered "tapped" nine times out of nine:

| Target | What the tap reached |
| --- | --- |
| sliding in over two seconds | the target, 0.6s in, while it moved |
| disabled | nothing |
| disabled for two seconds after Arm | nothing — tapped while still disabled |
| `aria-disabled` | the target's handler |
| fully covered | the cover |
| center covered | the cover |
| under `pointer-events: none` | the target — the negative control |
| under a plain div | nothing |
| below the fold | nothing — touched at y=6221 on a 2400-pixel screen, and on iOS the three lowest missed the WebView altogether |

Every web tap, multi-finger tap and drag source now goes through Vibium's
checks, run in the page: visible, enabled (`disabled`, `aria-disabled`, a
disabled fieldset), holding still across two readings 50ms apart, and
receiving events — `document.elementFromPoint` at the in-view center. Two
things are Mobium's: the element is scrolled into view first, and a covered
center is aimed around at the clear point nearest it, as a native target is
(115). A check that fails is waited out within the implicit wait and then
refused as "`@e7` failed check receivesEvents: covered by "full cover"".
`aria-disabled` is refused although the page's handler would have run,
because Vibium refuses it and a page that honors the attribute
would not act. Unlike a native tree, a page hit-tests, so the plain div that
a native screen can only report is refused here.

Measured again after the change on the emulator, the simulator and a Pixel 8
Pro: every row as intended. `docs/checks/web-actionability.sh` asserts each
from what the page says it received, and fails against the previous binary
at the first case.

### 119. Typing into a WebView blamed the map, and a checkbox was named "on"

**Found by:** MobiumApp's Web form page, built to measure typing into a page
once taps there were checked (118). `type` had no WebView path: it looked a
web ref up in the native table, found it empty, and answered "unknown ref @e1
— the last map found no elements" for a field the page's map had just
listed. Following that advice — map again — could never help. Nothing ever
reached the page, on an emulator or a simulator.

`type` inside a WebView is now Vibium's fill. The page is asked whether the
field can take text — visible and in view, enabled, editable: not
`readonly`, not `aria-readonly`, and an input type that holds text — and the
value is set through the element type's own native setter, which is what a
framework's controlled input listens to, with the `input` and `change`
events typing would have caused. The page then reads it back. A password is
confirmed by the page and never sent back or echoed: the answer says only
whether it matches. Text a device shell would mangle — an apostrophe, an
ampersand, non-ASCII — arrives exactly, since it travels as a JSON string.

The same page showed `map` naming its checkbox "on" — the input's `value`,
which a checkbox sets to "on" by default. A checkbox or radio is now named by
its label, as a person reads it; every other field keeps what it shows.

`docs/checks/web-type.sh` checks each field from what the page holds, and
fails against the previous binary at the checkbox's name.

### 120. An Android settings switch was named for its resource id

**Found by:** driving Android's Settings > Accessibility for
`app_accessibility` on a Pixel 7 AVD. Display size and text mapped "Bold
text (button)" and, beside it, "switchWidget (switch, unchecked)"; Color and
motion did the same for Remove animations. A settings row is a clickable
layout holding its title and, in a side frame, a switch that is not
clickable, has no text and carries the state — so map offered a row with no
state and a state with no name, reachable only by position. It is CHALLENGES
82's shape, on the other platform.

Android's parser now folds such a row into one control, as the iOS parser
folds its switch rows: named by the row, with the switch's role and checked
state. The row stays the target, because on Android the row is what a tap
operates — tapping it toggles the switch — where on iOS the words do nothing.
Only a row with exactly one such widget and no other control inside is
folded: Dark theme's row opens a page and has its own named, clickable
switch, and still maps as the two controls it is. `check` on the folded
Bold text row turned it on — Android stored 300 — and `uncheck` off again.

### 121. The clients hung, crashed and leaked on an ordinary bad day

**Found by:** driving each client's real connection against a stand-in
`mobium pipe` on a real subprocess, before the .NET, Java, Python and
JavaScript clients went public, and then checking the one case that mattered
most against the real pipe. Nothing here needed a device; all of it would
have reached users. Five clients, written against one protocol, each had its
own subset:

| What | Where | What happened |
| --- | --- | --- |
| Two threads calling at once | Java | two requests arrived on one line; mobium could parse neither and every call failed |
| | .NET, Python | unlocked; Python's never failed in 160 calls, because the pipe answers in order |
| A daemon that stops answering | all but Go | the call waited forever, with no way to bound it |
| A refused handshake | Java, .NET, Python, JavaScript | the `mobium` process was left running — in Node, holding the event loop open so the script could not exit |
| `NaN` in an argument | Python | written bare; the pipe answered "Parse error" **with no id**, the client skipped it as a notification, and `set_location(nan, 0)` waited forever — measured against the real pipe |
| | JavaScript | written as `null`, refused as "latitude must be a number" |
| A line that is JSON but not a message | Python, JavaScript | `null` on stdout raised in Python and, in Node, threw inside an event listener — which ends the caller's process |
| A malformed `\u` escape on stdout | Java | `NumberFormatException` escaped the catch written for unreadable lines and ended the call |
| Deep nesting | .NET, Java | a stack overflow — uncatchable in .NET, where it ends the process |
| A write after `close()`, or a binary that cannot start | JavaScript | an `error` event with no listener: the caller's process ended |
| `2^31` read as an int | .NET, Java | wrapped negative: a coordinate on the other side of the screen |
| Looking for the binary | all five | `./bin/mobium`, `../bin` and `../../bin` relative to wherever a test started, and any relative `PATH` entry, were searched — a binary anyone could plant |

The id-less error is the one that generalizes. JSON-RPC answers a request it
cannot parse with an error and no id, because it could not read the id. Every
client skipped any message without its own id, which is right for a
notification and wrong for that, so any unreadable request — not only `NaN` —
left its call waiting forever. The pipe answers one request at a time, in
order, so an id-less error is the answer to the oldest request in flight, and
all five clients now fail that call with it.

The rest is the Go client's design carried to the other four, changed only
where the language must: one call at a time on the pipe; an optional call
timeout that **ends the connection** rather than abandoning a call, since a
late answer would be read as the next call's; a connection that stays broken
and says why; a failed handshake that stops the process it started; a `close`
that is safe twice and ends a waiting call; a JSON reader that follows the
grammar, caps nesting and refuses to wrap an integer; and a binary searched for
only by explicit path, `MOBIUM_BIN_PATH` and absolute `PATH` directories.
Each client's tests start a fake `mobium` and check every row above; each
protection was removed in turn to see its test fail, and JavaScript's suite has
a watchdog, because the regression these tests guard against is a wait that
never ends.

### 122. A scroll right after going back found nothing that scrolls

**Found by:** `docs/checks/clients.sh` on a Pixel 7 AVD, after the session
work of 2026-09-27. Each client's flow presses back from Network & internet
and scrolls Settings' main screen to About, and one client in five started
failing there — a different one each run — with "no element matches
text=About and nothing on this screen scrolls". Driven through the Python
client in a loop it failed 3 times in 15, each about 60ms after back; the
list was there a moment later. The same loop against the build before that
work failed none in 15.

The race was older than the change that exposed it. `app_scroll_to` decided
"nothing scrolls" from one reading, while every action already retried its
target through the implicit wait. It was hidden because every call naming a
device first ran `adb devices` to resolve it, and that round trip was long
enough for the screen to settle. Letting a call with no driver named reuse
the session already open — needed so a session started with platform "ios"
does not have its next call sent to Android — removed the round trip, and
the verdict was reached mid-transition.

"Nothing scrolls" now gets the implicit wait too, ending as soon as the
target resolves or a scroll container appears. It costs nothing when the
screen is ready: scroll-to About took 3.0-4.3s for its two swipes, the same
as before. 0 of 15 failed, and three runs of `clients.sh` passed all five
clients. A delay put back in front of the call would have hidden it again,
which is what the adb round trip had been doing.

### 123. On Windows, disposing a .NET connection could crash the call it ended

**Found by:** the `windows-latest` CI job, on the fourth pull request it ran
for, having passed the three before. `DisposeIsIdempotentAndEndsAWaitingCall`
disposes a connection from one thread while another waits on a call, and the
run ended with an unhandled `COMException`: "The handle is invalid."

The waiting call, reaching the end of stdout, read the process's exit code to
put it in its message. `Dispose` on the other thread closes stdin, waits for
mobium to exit and disposes the `Process` — and when that came first, the
read touched a closed handle. On macOS and Linux that throws
`InvalidOperationException`, which was caught; on Windows it is a
`COMException`, which was not. The same read sat in the error path that ends a
connection, guarded the same way.

The exit status is now read best-effort, catching what each platform throws
for a process that has exited, been killed or been disposed; the call fails as
the disposed call it is. Only Windows could show this, and only when the two
threads crossed — which is what that CI job exists for.

### 124. map said a box was ticked, and no client could read it

**Found by:** driving every Go client method against MobiumApp's Form Demo on
a Pixel 7 AVD. `map`'s text read "Email me (checkbox, unchecked)" and "Free
(radio, checked)"; its structured result — what every client receives — had
the role and no state. `uitree.Entry` carried `Checked`, and `elementView`
never copied it onto the wire, so a test in any of the five languages could
act on a checkbox and not ask whether it was ticked.

The element now carries `checked`, absent for anything with no such state,
and each client reads it as a nullable boolean — null, not false, for a
button, because "no state" and "unchecked" are different answers. A WebView's
checkboxes report no state in either form yet.

### 125. The Go client read two fields the daemon had stopped sending

**Found by:** the same run. `Doctor` returned an empty string — it read a
`report` field the tool never sent, where the report is the tool's text — and
`Screen` reported every screen as 0x0, reading `width` and `height` after the
daemon had renamed them `width_px` and `height_px` for their unit. A JSON key
that is never sent decodes as a zero, so neither failed; both were simply
wrong. The other four clients read results as maps and were unaffected.

Both read what the daemon sends now, and `apisurface` pairs every Go client
type with the daemon type it decodes and fails the build when the client
reads a key that type does not send — the check that would have caught the
rename the day it happened. A Go type with JSON tags and no pairing fails it
too, so a new one cannot slip past.

### 126. On iOS a drag never held before it moved

**Found by:** the same run on an iPhone 17 Pro simulator. MobiumApp's drop
zone measures the hold before a drag moves: asked for 1500ms, it read 183ms,
three times in three, while the hold before the finger lifted arrived in full
at 1517ms. The drop still succeeded, which is why `GESTURES.md` had drag down
as working on iOS; but a list that arms reordering on a long press would never
have armed.

WebDriverAgent shortens a pause straight after pointerDown. The opening hold
is now sent to it as a move to the point the finger is already on, lasting
the hold — CHALLENGES 84's lesson on the other platform — and arrives as
1517ms, three times in three. Android's pause arrives in full (1522ms) and is
unchanged.

### 127. On iOS, opening a link answered with the app it was leaving

**Found by:** the same run: `open` reported MobiumApp as the app that opened
`https://example.com`, five times in five, and Safari was in front two
seconds later. Traced every 150ms, MobiumApp's screen changed 400-850ms after
the call, the wait took that change as the answer, and Safari arrived
500-900ms after it.

The wait could not tell a deep link landing in the same app from the app on
its way out, because until the other app arrives the two look alike. A new
screen in the same app now counts only once it has held still for a second,
which a departing app's does not: `open` reported Safari five times in five,
Chrome on Android as before, and a same-app deep link still lands.

### 128. iOS 26's share sheet reports its elements where they are not

**Found by:** installing a web app from Safari on the iPhone 17 Pro
simulator. A tap on the ref for "Add to Home Screen" opened Find on Page,
twice, and a tap on "View More" closed the sheet; each was reported as
tapped. A screenshot beside `map` showed why: every element of the sheet is
reported offset from where it is drawn — by about 1450 pixels while the
sheet is half open, and by a constant 190 once it is expanded, so the center
of "Add to Home Screen" is the center of the row above it.

**The same on a real iPhone 15 Plus (iOS 26.6.2)**: offset by about 1140
pixels half open and 180 expanded — 60 points against the simulator's 63 —
and the ref for "Add to Home Screen" again opened Find on Page while
reporting the tap. Half open, the sheet's reported rows fall on its row of
suggested contacts, so a mis-tap there would start a message to a real
person; nothing was tapped by ref in that state.

The sheet is drawn by another process, and the hierarchy shows the seam:
`ShareSheet.RemoteContainerView` at 9,477 with a node under it at 0,0 and
exactly its size, and every row below that relative to the container. In
screen coordinates a node the size of its parent and inside it must share
its origin, so a node at 0,0 under a parent that is not at 0,0 is a reset.
`ParseIOS` now adds the parent's origin to everything under such a node,
leaving nodes with no size where they are. Nothing is named, so any remote
view with the same habit is corrected. Of the nine captured iOS hierarchies
only the two share-sheet ones contain the pattern. After the change, the
ref for "Add to Home Screen" opened the Add to Home Screen dialog, and "View
More" expanded the sheet, tapped at 1000,2386 against 999,2374 read off the
screenshot.

### 129. A late answer to a listing was heard, then overwritten

**Found by:** `make ci` failing in `internal/webview` on a machine busy with
a simulator — the test for [116](#116-after-an-accessibility-setting-changed-every-webview-lookup-took-25-seconds)'s
fix, one run in five, with "PID:2 answered and is still marked silent".

An application that misses a listing is marked silent, and the watch clears
the mark the moment it hears that application answer. But the listing marked
everything that had missed it *after* it stopped waiting, so a reply that
arrived in between was cleared and then marked again. An application that
always answered just too late would have stayed silent, and its pages
unlisted, which is the case the mark exists to recover from. The watch now
counts each application's answers, and a listing marks one silent only if
nothing arrived from it while the listing ran. 60 runs in 60 pass; the old
code fails the same test.

### 130. A client that went away left its session open

**Found by:** reading what happens to a session when its client exits, then
measuring it on the iPhone 17 Pro simulator.

Nothing tied a session to the client that started it. A script that
returned without `quit()`, or crashed, closed its pipe exactly as `close()`
does, and the pipe returned with the session still open on the device until
the daemon went idle for 30 minutes. Ctrl-C was worse: it reaches the whole
process group, so `mobium pipe` died with the client, and a `with` block or
a `finally` that called `quit()` on the way out failed with "mobium closed
the connection" and left the session open anyway — measured.

The pipe now remembers the sessions its client started (not ones it found
open, which `start` reports as reused), and ends them when the client goes,
unless the client said it was leaving on purpose: `close()` in all five
clients sends a `mobium/detach` notification first, so "close leaves the
session open" still holds. The pipe ignores SIGINT, since the client may be
handling it, and treats SIGTERM and SIGHUP as the client going. On the
simulator: a script exiting without `quit()` has its session ended, `close()`
keeps it, `kill -9` of the client ends it, and Ctrl-C around a `with` block
quits cleanly — and with the SIGINT ignore taken out, the same Ctrl-C fails
as before.

### 131. After a daemon crash, the simulator showed WebDriverAgent's empty window

**Found by:** `kill -9` of the daemon mid-session on the iPhone 17 Pro
simulator, then any command.

The runner the dead daemon launched kept running and kept its port. The next
daemon's `simctl launch` of it did not restart it: it brought the runner's
empty window to the front and left it there, so `current` answered
`com.facebook.WebDriverAgentRunner.xctrunner`, `map` found nothing, and a
screenshot was black — three runs in three. A runner already running when a
session starts can only be a leftover, so the simulator start now stops it
first: three runs in three, a new runner each time and SpringBoard in front.

### 132. After a daemon crash on Android, a forward leaked and the next call could read EOF

**Found by:** the same crash on an Android 15 emulator.

Two things were left. The dead daemon's adb forward to UiAutomator2's port
stayed, beside the next session's, and outlived even that session's clean
stop. And the old server went on answering its port for a moment while the
new instrumentation replaced it, so the new session could open on the server
about to die: 2 calls in 15 straight after the restart failed with EOF. The
start now force-stops a leftover server, as teardown does, and removes this
device's forwards to that port — only that port's, since another tool's
forwards are not Mobium's to remove. 0 calls in 30 failed, and no forward
was left after the stop; a clean start takes the same 0.71s as before.

### 133. `daemon stop` said "stopped" while the daemon was still stopping

**Found by:** reading the stop path against the daemon's shutdown bounds.

`stop` waited 5 seconds for the daemon to exit and then reported success
either way, while the daemon allows itself 10 seconds for calls in flight
and 20 more to close sessions. A slow teardown therefore reintroduced
[28](#28-mobium-daemon-stop-returned-before-the-daemon-stopped) quietly. The wait now covers the daemon's own bound, ends as soon
as the daemon removes its PID file — the last thing its shutdown does — and a
daemon still running after it is reported with a `timeout` code, not as
stopped. The daemon also handles SIGHUP now, so one started in the
foreground tears down when its terminal closes instead of dying on the spot.

### 134. On iOS, ending a session left a route playing

**Found by:** reading what a session's end does against what it says.

The end reported "a recording or route stopped", and on a simulator the
route was simctl's to play and nothing stopped it; the position went on
moving along it. Clearing is simctl's only way to stop a route, so an end
during one now clears the simulated location — which, unlike Android, where
the last fix is kept, leaves no position at all. A route that has already
finished is left where it ended. Checked by a test; iOS has no way to read
the simulated position from outside an app, so the simulator itself was not.

### 135. A real iPhone's runner, orphaned by a crash, was never stopped

**Found by:** the positive control for `clean-stop.sh`'s new runner check,
on an iPhone 15 Plus (iOS 26.6.2): `kill -9` of the daemon mid-session.

The check fired — the xcodebuild the dead daemon started was still running.
Then the next daemon found WebDriverAgent answering and used it, as it uses
one started from Xcode, as someone else's: its session's end left it, and so
did `daemon stop`. Nothing would ever have stopped it. A phone session now
takes over a runner that is certainly Mobium's and certainly orphaned — an
xcodebuild running this cache's WebDriverAgent build for this phone, whose
parent is pid 1, which is what a process becomes when its parent dies. Xcode's
runs a different build, and a live daemon's still has that daemon for a
parent; both are left alone. On the phone: the orphan was taken over and
stopped by the next session's end, and after another crash by `daemon stop`,
with a fresh runner starting normally in between.

### 136. On iOS, the role `map` printed was not one a locator could find

**Found by:** installing a web app from Safari on the iPhone 17 Pro
simulator: `map` listed "Add to Home Screen (button)", and
`label=Add to Home Screen,role=button` found nothing, so `scroll-to` with it
scrolled to the end of the list and gave up.

`map` names a node by what it does when no class names it — a touchable one
is a button, a scrollable one a list — and the locator matcher did that on
Android only. On iOS it matched element types alone, so every Cell, keyboard
key and React Native view printed as `(button)` and matched no
`role=button`: 93 entries across the captured hierarchies, plus one Android
GridView printed as a list. And since the label alone matched the Cell, its
Image and its StaticText, `map` fell back to a path 22 levels deep. The
matcher now applies the same fallbacks, except to a text field, and except
to a row that holds a real button — iOS Settings draws "About" as a Cell
around a Button of the same label, and calling both buttons made
`label=About,role=button` ambiguous. A test holds the rule over every
captured hierarchy; without the change it reports all 93. On the simulator,
`scroll-to` and `tap` with that locator reached the row and opened the Add to
Home Screen dialog, and `map` now derives the locator itself.

### 137. A web context's name was its position in the listing

**Found by:** switching to a Safari page by the name the previous listing
gave it, on the iPhone 17 Pro simulator, and attaching a different page.

Both transports numbered pages in listing order — iOS numbering all of them
once there were two — so a tab opening between two calls renamed the rest.
An installed PWA and the Chrome tab it came from were both
`WEBVIEW_com.android.chrome`, told apart by position alone. A session now
keeps each page's name by the page's own identity — its devtools socket and
CDP target on Android, its application and page on iOS — for as long as the
page is listed, and a new page takes the first free name. A page that goes
frees its name, so a relaunched app's WebView, which Android publishes on a
new socket, comes back under the name it had. On the simulator, opening a
second tab left the first page's name as it was, and switching by it reached
that page.

### 138. On iOS, `contexts` listed the pages of apps not in front

**Found by:** listing contexts on the iPhone 15 Plus with Wikipedia in front
and seeing Safari's page; reproduced with MobiumApp, three times in three,
after opening a page in Safari and coming back.

Every application webinspectord knew was asked for its pages, and every page
was listed, whichever app was on screen. Switching to one attached a page
that was not on screen, and a tap into it would have been aimed through the
WebView the front app shows. webinspectord says which app is in front:
`WIRIsApplicationActiveKey` is 2 for it, 1 while an app is leaving or
arriving and 0 in the background, measured as the phone switched. Safari,
left for MobiumApp, went from 2 to 1 and stayed at 1 with its page still
published, so "not 0" would not have been enough. The inspector now keeps
each application's flag, and a page is **behind** when its app reported one
that is not 2 and is not hosted by the app that is — WebKit's relation for
an in-app browser, which has not been driven here. When nothing reads 2, as
mid-switch, and for an app that reports no flag, nothing is hidden on a
guess. `contexts` lists only pages in front and names the rest on a line of
their own; `context` refuses one behind as `device_not_ready`, and the remedy,
`app_launch` its app, was followed on the phone and reached Safari's page.

Android had the same defect and no flag to fix it with: `/json/list` says
nothing of which page is on screen. It has the foreground package instead,
which a snapshot already carries, so a page is behind when its app is not
the one in front — a custom tab is Chrome's page in Chrome's window, so the
rule holds for it. On the Pixel 8 Pro with MobiumApp in front, the listing
before the change offered three Chrome tabs beside MobiumApp's page, and
after it named them on their own line.

### 139. On Windows, stopping the daemon never finished

**Found by:** the first run of the daemon tests on a GitHub-hosted Windows
runner, which hung for ten minutes in `TestDaemonWritesAndRemovesPIDAndSocket`.

go-winio's pipe listener, v0.6.2 and the latest, closes by sending its
goroutine one signal. While an `Accept` is waiting for a client, that wait
can take the signal instead; when the aborted connect then reports an error
other than the two go-winio expects, the goroutine goes back to waiting for a
signal already spent, and `Close` waits for it forever. The stack showed
exactly that: the listener idle in its loop, `Close` waiting on its done
channel, the daemon waiting on `Close`. A `mobium daemon stop` would never
have returned. A second `Close` reaches the idle goroutine, so the Windows
listener repeats `Close` every 100ms until one returns.

### 140. On Windows, the PID file could outlive the daemon

**Found by:** `TestDaemonShutdownOverTheWire` on the Windows runner, which
waited out the client's whole 35s grace about one run in three. A goroutine
dump taken when it did showed the daemon already gone.

A client waiting for the daemon to stop reads the PID file every 20ms, and on
Windows a file cannot be deleted while anything has it open. When the two
collided, `os.Remove` failed, the error was ignored, and the file went on
naming a daemon that had exited. A separate process's exit is still noticed,
so the CLI recovered; a daemon run in-process never was. `RemovePID` now
retries for a second, and a failure is said on stderr.

### 141. The Go client refused every `mobium.exe`

**Found by:** the Go client's tests on the Windows runner, every one of which
failed with "is not an executable mobium binary".

`FindBinary` checked a file's executable bit, and on Windows `os.Stat`
reports none, so `MOBIUM_BIN_PATH` could name no binary at all. A program on
Windows is known by its extension, so the client now reads `PATHEXT`, as the
shell does. The other four clients ask their platform, which already knew.

### 142. On Windows, the Go client missed mobium on PATH

**Found by:** the same client test once the previous fix let it run: a
`mobium.exe` in the current directory, and the real one on `PATH`, reported
as not found.

`exec.LookPath` on Windows looks in the current directory first, and on
finding a program there returns `ErrDot` rather than going on to `PATH`. The
client refused the one in the current directory, as it should, and then had
nothing. It now searches `PATH`'s absolute directories one at a time. The
test had passed its negative half on Windows by finding nothing, because the
planted binary had no `.exe`; it now plants one.

### 143. Bold text stayed on after the session had put it back

**Found by:** Lana, looking at her Pixel 8 Pro after `accessibility.sh` had
passed on it, and asking whether the text was still bold. It was.

An accessibility setting's undo restores the raw values it found, and a key
that was unset is deleted again. Deleting a key tells the running system
nothing: bold text put on and then deleted read back unset while the phone's
configuration still said `fontWeightAdjustment=300`, and every app went on
drawing bold text. The check compared raw values only, so it passed on
exactly this. An unset key is now restored by first writing what Android's
own switch writes for off — which the system hears — and then deleting it,
which leaves the row as found; measured on the phone: unset, and
`fontWeightAdjustment=0`. The check also compares the font weight the
system applies before and after, and with the old undo it fails, on the
Pixel, naming 300. The iOS simulator's undo also deletes keys it found
unset; whether a running iOS app hears that has not been measured.

### 144. A page stayed attached, and aimed, after its app left the screen

**Found by:** fixing 138, which refused switching to a page behind and did
nothing for a page already attached when its app went.

A web action turns the page's coordinates into the screen's through the
largest WebView in the native hierarchy — which, once another app is in
front, is that app's. A tap into the attached page was aimed through it and
reported. Every action that computes a web coordinate now asks, at that
moment, whether the page's app is still in front — WebKit's flag on iOS,
the foreground package on Android — and refuses if not, naming both apps.
On the Pixel, with MobiumApp's page attached and Chrome brought forward from
outside, the tap was refused. Its first remedy said to launch the app, and
following it tapped the native screen: launching detaches the page. The
remedy now says to switch back to the page after launching, and following
that landed the tap in it.

### 145. `wait` saw what a dialog covered, which everything else refused

**Found by:** the login demo on the iPhone 15 Plus, with iOS's "Save
Password?" sheet over it: `wait` reported the welcome text visible in 816ms,
and `text` refused it as under the dialog.

An action decides by the dialog: anything outside a visible alert or sheet
is covered (105). `wait` decided by each node's own visibility flag, which
iOS left true under the sheet — while Android, whose tree holds the dialog's
window alone, had the same wait time out. `wait` now uses the action's
predicate: what a dialog covers is not on screen, so waiting for it to be
visible times out naming the dialog, and waiting for it to be hidden
succeeds, on both platforms. The test is the captured sheet with the covered
target marked visible, as the phone had it; it fails without the change. On
the phone, under the sheet: visible timed out naming "Save Password?",
hidden held in 745ms, and once the sheet was answered the text was visible
in 471ms.

### 146. A row cut off by its list was reported as a tiny touch target

**Found by:** formflux's device test on a Pixel 9 Pro Fold emulator, which
reported one finding at the new `fold-open` profile and none anywhere else:
a 2076×52px row in Android's own Settings, below the 48dp minimum.

It was "Sound & vibration", 215px tall like every row beside it, sitting at
the bottom edge of Settings' list with 52px of it showing. Android reports a
child's bounds clipped to its scroll container (the rule the scroll code
already follows), and the touch-target check measured the sliver. Any
profile can produce it; `fold-open` was where a row happened to be cut with
less than 48dp showing. A dimension that ends at an edge of the node's
nearest scrolling ancestor is now not judged, since its real size was not
reported. The test is that Settings screen, captured: the cut row is no
longer reported, and the same row moved into the middle of the list still
is. The device test only logged findings, so its zeros afterward showed only
that the check ran; `TestDeviceCatchesThePlantedTargets` now asserts, on
MobiumApp's Layout Demo — a 24dp target reported at all eight profiles, and a
bar an eighth of the screen wide reported exactly at the two below 384dp. With
the touch minimum lowered to 20dp it fails at the first profile.

### 147. WebDriverAgent never started on an iPad simulator

**Found by:** booting the iPad mini (A17 Pro) simulator for a formflux
profile: WebDriverAgent did not answer within 90 seconds, twice, with no
dialog on screen, and the iPad Air failed the same way while the iPhone 17
Pro started in 6 seconds.

Mobium launches the prebuilt runner and waits for its server. Side by side,
both runners logged "Running tests..."; the iPhone's then said "Continuing to
run tests in the background" and its server came up, and the iPad's said
nothing more. On iPadOS 26 the runner stays in the foreground as a window,
and XCTest does not proceed until it leaves: launching another app on the
iPad brought the server up three seconds later. If the runner has not
answered in ten seconds, Mobium now opens and closes Settings, which leaves
the home screen in front; an iPhone answers first and never sees it. On the
iPad mini the first call then answered in 13 seconds, and Settings mapped and
was driven. The iPad's home screen is a question of its own: WebDriverAgent
reports the Dock's folder service as the app in front there, with nothing to
map, where an iPhone reports SpringBoard.

### 148. A second simulator's daemon drove the first simulator

**Found by:** booting a second iPhone simulator beside the first, each on a
daemon of its own as parallel runs are told to: the second launched Safari
on its simulator, then read the first simulator's screen and reported
Settings as its foreground app.

Launching goes through `simctl`, which names the simulator; everything else
goes to WebDriverAgent's server, which every simulator's runner opens on the
Mac itself, at 8100. Only one can hold the port, so the second daemon's
reads, and its taps, reached the first simulator's runner — while it said
nothing was wrong. Its video stream collided the same way at 9100. Each
simulator's runner is now launched with free ports of its own, passed as
`USE_PORT` and `MJPEG_SERVER_PORT` through `simctl`'s `SIMCTL_CHILD_`
environment, and the daemon talks to that port. Two iPhone simulators on two
daemons then read their own screens, and ten `map` calls on each at once
never crossed. `autowait.sh`, which called port 8100 itself to set Reduce
Motion, uses mobium's own setting instead. Android was never affected: its
server listens on each device's loopback, reached through `adb forward
tcp:0`, which picks a free Mac port per device; two emulators on two daemons
got 52360 and 52527 and never crossed either.

### 149. `double-tap` reached a React Native control on iOS as one press

**Found by:** a person, as the control ROADMAP had asked for: Lana
double-tapped MobiumApp's Press target on the iPhone 15 Plus and it counted
two presses 200ms apart, where `double-tap` on the same target counted one.

Mobium used WebDriverAgent's own double-tap endpoint, Apple's primitive, and
a React Native `Pressable` reports that as one press — which 70 had taken
for the Pressable coalescing any double tap. The W3C chain Android uses could
not replace it: WebDriverAgent drops a pause while the pointer is up, so the
taps arrived together. Spending the gap on a timed move to the same point
instead is honored: on the iPhone 17 Pro simulator it reached the Pressable
as two presses 167ms apart, three times in three, the second landing with no
other finger down. On a WebView page the same chain delivered one click at
every gap tried up to 250ms, WebKit reading the move as a second contact,
while the endpoint delivered two. So a point on a WebView still gets the
endpoint, and any other point the chain, with Android's 120ms gap: the
Pressable then counted two presses 200–202ms apart, three times in three, and
the page two clicks. `gestures.sh` now asserts that on iOS — two presses
inside the double-tap window, and two separate taps outside it — where it
used to assert one press. Neither form makes a WebView fire `dblclick`. On
the iPhone 15 Plus afterwards: two presses 197ms apart, against the person's
200, two separate taps 1298ms apart, and the page two clicks, three times in
three.

### 150. The .NET package could not be built from a clone

**Found by:** following the quick start on a fresh Linux machine, which
clones the repository as the .NET page says to: `dotnet pack` failed with
`NU5019: File not found: .../Mobium/icon.png`.

The package's icon was added with the package metadata (#11), and
`.gitignore` ignores every `*.png`, because screenshots land in the tree
constantly during device work — so the icon was never committed. Every
check that packed the client, the one that installed it into a clean project
included, ran in a checkout that had the file on disk, so none could fail.
The icon is excepted beside the branding, and committed. A clean clone is
the fixture a user has; a working tree is not.

### 151. A per-app permission reset that could not see what it failed to clear

**Found by:** running `mobium-app.sh` after `dialogs.sh` on the Pixel 7 AVD,
the first time both reset MobiumApp's permissions alone. The location check
failed: the app had approximate location only, and the reset had said it put
every permission back to asking.

Android prints a permission flag it has no name for as a number, and after a
person chooses approximate location the line ends `|524288]`. The pattern
read the flags as capitals and pipes, so it failed to match the list at all
and every flag on the line read as absent: the reset saw no `USER_SET` to
clear, and its read-back — the same parser — saw none left. It now reads
whatever sits between the brackets. The number is the choice of precise or
approximate, which the next prompt preselects; `pm clear-permission-flags`
takes five named flags and not that one, and nothing else clears it for one
app, so after an approximate choice the reset now says it kept it. After a
precise choice the prompt preselects precise, as on a fresh install, and
nothing is said. A read-back that shares its parser with the write is not a
second opinion.

The Pixel 8 Pro, on Android 17, added two things the AVD had not shown. It
names the flag — `SELECTED_LOCATION_ACCURACY` where Android 15 printed the
number — so both are read. And it grants MobiumApp `ACCESS_LOCAL_NETWORK` by
itself, `REVOKE_WHEN_REQUESTED` with no answer recorded, which `pm revoke`
leaves granted while exiting 0; the read-back refused the reset, rightly,
and `dialogs.sh`, discarding the reset's output under `set -e`, stopped
after its share-sheet row with nothing on screen. Such a permission is now
kept and named, since a fresh install has it too, and the check says why a
reset failed.

### 152. The simulator's WebDriverAgent answered anyone on the network

**Found by:** measuring the open items of the threat model on the iPhone 17
Pro simulator, with the Mac's firewall off, as it was.

A simulator shares the Mac's network stack, and WebDriverAgent listened on
every interface — `*:port` in `lsof`. From the Mac's own LAN address, its
`/status` answered with the live session's id, `/source` returned the whole
screen, and the session could have been driven; anything on the same network
could do the same while a session ran. WebDriverAgent binds one interface
when `USE_IP` names it, and Mobium already passed `USE_PORT` the same way
(148), so the runner is now launched with `USE_IP=127.0.0.1`: afterwards the
server listened on `127.0.0.1` only, and the LAN address was refused. Its
MJPEG screen stream did not follow — WebDriverAgent 16.12.8 creates that
socket without an interface, whatever `USE_IP` says — and still answers from
the network. Mobium does not use the stream; closing it needs WebDriverAgent
to honor `USE_IP` there too, and is open in the threat model.

### 153. A real iPhone's WebDriverAgent answered anyone on its Wi-Fi

**Found by:** the threat model's open item, measured on the iPhone 15 Plus:
the runner's `/status`, which reports the phone's Wi-Fi address, was asked
again at that address from the Mac, over the network rather than the cable.

It answered with the live session. The runner listened on every interface of
the phone, so while a session ran, anything on the same Wi-Fi could read the
screen and drive the phone — tap, type, open apps — with no credential; its
MJPEG stream on 9100 answered the same way. It stopped within two seconds of
the daemon stopping, so the exposure was a session long. The Mac's firewall
cannot help: this is the phone's interface. The runner is now started with
`TEST_RUNNER_USE_IP`, which xcodebuild hands it as `USE_IP`, set to the
phone's address on the CoreDevice tunnel — the only address Mobium uses.
That address exists only while something asks for the device, and changes
with every connection (it dropped after about 20 seconds idle and came back
different), so Mobium brings the tunnel up with `devicectl device info
details` just before launching, refuses to start rather than start unbound
when no address comes, and relaunches once if the tunnel reconnects during
start-up. Afterwards the server answered on the tunnel and refused the Wi-Fi
address. The stream still answered on Wi-Fi: WebDriverAgent never applies
`USE_IP` to it, the same as on a simulator (152). Its socket can bind one
interface and the server never tells it which, so Mobium, which builds the
phone's runner from the pinned source, adds that one line to `FBWebServer.m`
after verifying the source and before building, refuses to build if the line
it attaches to is gone, and rebuilds a phone's runner built before the patch.
After the rebuild both ports answered over the tunnel and both refused the
Wi-Fi address, and `ios-device.sh` passed. The simulator's runner is
the prebuilt release, unpatched; there the Mac's firewall is the
mitigation (152).

A runner already running when a session starts is used rather than started
again, and one from before this fix listens everywhere. So one found running
is asked for the phone's Wi-Fi address and tried there from the Mac: one of
Mobium's own that answers is stopped and replaced by one bound to the tunnel,
and anyone else's — Xcode's, a terminal's — is refused as `device_not_ready`
with the reason, and left running, since it is not Mobium's to stop. Both
measured on the iPhone 15 Plus with an unbound runner started by hand: ours
answered on Wi-Fi before and not after, with one runner left; the other was
refused and still running.

### 154. A real Android phone's UiAutomator2 server answered anyone on its Wi-Fi

**Found by:** the threat model's open item, measured on the Pixel 8 Pro,
Android 17, on the same Wi-Fi as the Mac.

The UiAutomator2 server listens on every interface of the phone — `*:6790`,
and its MJPEG stream on `*:7810` — and from the Mac, over Wi-Fi rather than
adb, `/status` answered "ready to accept commands" and the stream answered
too: while a session ran, anything on the network could read the screen and
drive the phone, with no credential. Both stopped within two seconds of the
daemon stopping. Its Wi-Fi was on `wlan1`, not `wlan0`, and the first probe,
looking at `wlan0`, found no address and "no answer" — which was not a
result, as a probe that could not have succeeded never is. The phone's
global cellular IPv6 address did not answer from the internet either, and
that is not a result for the same reason: nothing known to answer there was
tried first.

Unlike WebDriverAgent (153), the server has no setting that chooses an
interface: it calls Netty's `bind(port)`, and Mobium installs the
prebuilt APK rather than building it. Binding it to `127.0.0.1` would lose
nothing — `adb forward` reaches the server on the device's localhost — so
the fix is upstream: a bind address the server reads, as WebDriverAgent
reads `USE_IP`. Until then the dump backend, `--driver uiautomator`, is the
way to drive a phone with nothing listening: measured on the same phone, a
dump session added no listening socket, where a UiAutomator2 session added
both.

### 155. A notification banner hid the app under it on iOS

**Found by:** MobiumApp's OTP Demo on the iPhone 17 Pro simulator, iOS 26.5,
reading the outcome right after the app posted its code as a notification.

While the banner was up — about five seconds — every read answered with
SpringBoard: `current` named it, `text testid=otpOutcome` found nothing, and
`map` listed the banner and nothing of the app behind it, which was still in
front and still taking touches. WebDriverAgent reads the application it
believes is active, and while a banner shows it believes SpringBoard. Worse,
a tap aimed at the app's Back button under the banner was reported as done
and did nothing: the check that refuses a target under a dialog (105) did not
count the banner, because it is an `XCUIElementTypeOther` reported not
visible, and both of those excluded it.

The banner names its app: SpringBoard's tree holds a hidden
`card:<bundle>:sceneID…` next to the `NotificationShortLookView`. So a read
that finds one asks WebDriverAgent for that app's tree instead, by the same
`defaultActiveApplication` hint a launch uses (71), puts the hint back, and
grafts the banner onto the app's tree as a control on top — the code it
carries stays readable in `map`. An action whose target is under it waits up
to ten seconds for the banner to go before refusing, rather than the dialog
budget; measured, a Back tap under a fresh banner waited about 5.5 seconds and
then navigated, two runs in two.

The first version fixed reads only, and `docs/checks/otp.sh` found the two
other places WebDriverAgent consults what it believes is active. `type` looks
its field up through WebDriverAgent, which searched SpringBoard and answered
"no such element" for a box the app's tree had just listed; a lookup that
fails is now asked again as the node's own app. And `background` sent the app
away and brought SpringBoard back, leaving the app in the background and the
call reporting that it never returned; it now names the app it is
deactivating. Both measured under a fresh banner on the simulator, two runs
in two, and failing before. On the iPhone 15 Plus, iOS 26.6.2, `otp.sh` read
the code through the banner and passed. It needed MobiumApp's notifications
switched on in Settings: the phone had kept an earlier denial through a
reinstall, where a simulator asks again.

### 156. Typing into a field that moves focus on was misread as dropped keystrokes

**Found by:** the same screen, typing a whole six-digit code into the first
of six one-digit boxes on the simulator — which is what a person's password
manager does, and what a test author writes first.

The app moves focus to the next box as each digit arrives, so XCUITest's
keystrokes followed it: the first box held `1`, the rest the other digits, or
all but one of them. `SetText` read the first box back, saw `1` where it
typed `123456`, and took it for iOS dropping a keystroke (61) — so it retried,
typing the whole code again into boxes that already held digits, and then
reported a failure that blamed the platform. Every part of that was wrong.

Now the first mismatch looks at the text-entry fields after the target, in
document order: if the typed text is spread across them in sequence, the
typing is reported as done when every character arrived and, when one did
not, as `not_confirmed` naming what each field took and what never arrived,
with the remedy that works — type into each field in turn. Nothing is typed
twice. Measured on the simulator, six runs: five arrived whole and were
reported typed, one lost a digit and named it, and none retried. On the
iPhone 15 Plus the whole code arrived and was reported typed, in each of three
runs of `otp.sh`. Android is unaffected: it sets a field's text at once, and the app spreads it.

Since 2026-09-30 a lost character is typed again rather than only named.
Typed whole, the code lost a digit at a focus change in 2 runs of 10 on
the simulator, and in none of 10 on the iPhone, which types at half the
speed. Typing into each box in turn cannot race the focus change, since each
keystroke goes to the element it names. So on a loss the boxes are
cleared, last first, each is given its character and read back, and the row
must then hold the code in order. A box that still takes nothing is named
as before. Measured: 30 of 30 on the simulator, five of them recovered at
about 8s against 2s, and 10 of 10 on the iPhone. A fake that loses a digit
the same way fails the test with the old behavior and passes with the new.

### 157. An iOS background of a minute or more timed out

**Found by:** `docs/checks/otp.sh`, sending MobiumApp away for 62 seconds to
let its code expire, on the iPhone 17 Pro simulator.

`app_background` takes up to 180 seconds, and on iOS it failed at any length
past about one minute with "context deadline exceeded". WebDriverAgent's
`deactivateApp` answers only once the app is back, and every request to it
has a sixty-second client timeout. Android backgrounds by pressing Home and
waiting on this side, so the same 62 seconds passed there, and nothing ever
asked iOS for more than a few. That request is now given the duration on top
of its timeout; a unit test holds a request past a short timeout and shows it
fails without the extension first. Measured: the 62-second background
passed on the simulator and on the iPhone 15 Plus.

### 158. One check, two spellings, and a different sentence for every refusal

**Found by:** reading the code, while giving native refusals the WebView's
shape — not on a device.

A client that wanted to know why an action was refused had nothing stable to
read. A WebView's refusal said "X failed check receivesEvents: …" and put the
check in `details.check`; the same refusal on a native screen, a control
drawn over the target, said "X is covered by …" and put `receives_events` in
`details.check`. Of the other native refusals, two set a check and five set
none, and each had its own sentence. Now every refusal from a check an action
makes goes through one constructor: "X failed check C: reason — what to do",
with `check` and `reason` in details, C one of `visible`, `enabled`,
`stable`, `receivesEvents` and `editable`, and the error codes unchanged. A
miss is not a failed check and keeps its own words: nothing matched while a
dialog or the keyboard was up. A test holds the shape, and
`docs/checks/wait-states.sh` reads `details.check` from a real refusal on
both platforms.

### 159. A password typed on a real iPhone lost every letter, and was reported typed

**Found by:** `docs/checks/autowait.sh` on the iPhone 15 Plus, iOS 26.6.2,
failing to see Log In disabled — because the app had refused to sign in with
a password of one character.

Typing the ten-character "wrongpass1" into MobiumApp's password field left
one character, three runs in three, and `type` reported ten; the simulator
kept all ten. The phone's keyboard was Russian. On a real iPhone a password
field is typed key by key on the keyboard that is up, and a letter it has no
key for is dropped: the one character that arrived was the digit, "abc"
arrived as nothing, "abc12" as two, and "1234567890" whole. An ordinary field
on the same phone took Latin text whole, and nothing on the simulator has a
second keyboard, which is how it hid. Slowing to six keys a second changed
nothing; switching to the English keyboard with its globe key, all ten
arrived.

The loss was silent because a password field was exempt from the read-back
(defect 61's rule): it reads back as bullets and could never equal the text.
But one bullet is one character, and an empty field reads back as its
placeholder in clear — "password", eight characters, on the simulator and
the phone alike, equal to `placeholderValue` — so the length is readable,
and is now confirmed, on `type`, `fill` and typing into the focused field.
A short password is retried at the slower speeds and then reported as
`not_confirmed`, lengths only; when the keyboard on screen lacks keys for
some of the letters, it says how many and names a few keys it does have
("й, ц, у"), with the remedy measured to work. Nothing of the password is
ever printed.

### 160. Putting a phone's settings back one at a time ran out of time, and left four changed

**Found by:** `docs/checks/accessibility.sh` on the iPhone 15 Plus, the first
run after `app_accessibility` learned to go through the phone's Settings app.

On a real iPhone nothing outside changes an accessibility setting, so each
read or change is a trip through Settings — open it at its root, walk to the
page by the rows' identifiers, flip the switch and read it back, return to the
app — about eight to ten seconds. Each change kept an undo, and a session's
end ran them one after another. The check flipped all six switches and
stopped the daemon: the daemon's close had 20 seconds, spent them on two
undos, and exited, leaving Increase Contrast, Reduce Transparency, Button
Shapes and Differentiate Without Color on — on the phone of the person whose
phone it is. Putting them back by hand made it briefly worse: each manual
"off" was itself a change the new session promised to undo, and its end
turned two back on. The switches were finally unchecked in Settings with
`uncheck`, which keeps no undo, and read back.

Now a change records what it found, and the first undo to run puts back every
setting recorded, one Settings visit per page, while the rest find nothing
left to do: six settings on two pages are two trips, not six. The undos share
one 60-second budget, and the daemon's close has 75 seconds and `daemon
stop` waits 90 — running out leaves a person's phone changed, which is worse
than a slow stop. The same check then passed: every switch flipped, the app
heard the four it reports, and after the stop every switch and what the app
is told were back as they were. Two things found on the way, both before
this was committed: turning on Bold Text redraws Settings, so the switch
found before the tap was a stale element when read back, and it is now
looked up again on every read; and a read of all six took 45 seconds, one
trip each, and now takes 16, one visit reading every switch on its page.
### 161. The grid showed a phone's owner's name as its model

**Found by:** writing the grid guide, with the iPhone 15 Plus attached to the
Mac standing in for a node.

`mobium grid status` listed the phone with "Lana Begunova's iPhone" in its
MODEL column. The structured answer of `app_devices` — which the grid's
status and page, `MOBIUM_GRID_MODEL`'s matching and every client read — put a
physical iPhone's name where its model belongs, while the CLI's own line
showed both. A phone's name is its owner's, and on a grid every user's
status shows it; `MOBIUM_GRID_MODEL=iPhone 15` matched nothing, too. The
structured answer now carries the model, "iPhone 15 Plus", and only the CLI
line, read by the person at the machine, keeps the name. Measured after the
fix: the same `grid status` shows the model.

### 162. An unset device refused every project, not just its own

**Found by:** running `docs/checks/test-runner.sh` in the Android-only mode
its own header documents — not a device's doing.

A project can take its device from the environment, `"${MOBIUM_IOS_DEVICE}"`,
and an unset variable is refused by name. It was refused when the config
was read, so `mobium test --project android` failed over the iOS project's
variable, which that run never needed; every earlier run had happened to set
it. The variable is now noted when the config is read and refused only when
its project is run, and a test runs the Android project with the iOS one's
variable unset.

### 163. What a test changed outlived the test run

**Found by:** writing the network guide, reading the emulator's network after
a `mobium test` run on it.

After two tests that went offline and added latency, the run passed, and the
emulator still had 300ms added to every round trip. A session's end puts back
what it changed — network conditions, accessibility settings — and a test run
never ended its session: every project shared the CLI's own daemon and its
open session, which outlived the run. The same sharing made projects on two
devices queue behind one daemon that serves one call at a time. Each project
now gets a `mobium pipe` and, off a grid, a daemon of its own, named for the
run: its first call starts the project's session, closing it ends that
session, and the daemon is stopped after. Measured: after the same run the
emulator had no shaping and airplane mode off, and the suite on two emulators
took 40 seconds for 75 seconds of work, against 95 for 189 before. The pipe
had also been closed with `mobium/detach`, which tells a pipe to leave its
sessions open; it is closed without it now.

### 164. JavaScript's `tap` dropped a point's coordinates and its fingers

**Found by:** type-checking the documentation site's examples against
`index.d.ts`, and reading `index.js` beside it — not a device's doing.

`index.d.ts` declares `tap(target, options)` and `tap(point, options)`, and
`index.js` took `tap(target, point, options)`. So `tap({x, y}, {fingers: 2})`
read the options as the point: `x` and `y` went out undefined, the fingers
were dropped, and mobium refused the call as `invalid_argument` with no
target — on the one form of a two-finger tap the types advertise. A string
target with `{fingers}` had worked only through a special case for exactly
that. `tap` and `doubleTap` now take what the declaration says. The client's
own test sends all six forms through a stand-in daemon and checks the
arguments that arrive; on an emulator, the same call on the unfixed client
was refused and on the fixed one opened the row it tapped.

### 165. Four arguments no client could send, or not every client

**Found by:** writing the documentation site's examples — every scenario in
every client — where each gap had to be worked around by calling the tool by
name.

`app_alert` types `text` into a prompt before answering it, and no client's
`answerAlert` took any. Java and .NET could not give `app_call` a `number` or
`app_sms` a `from`, and required a notification's `title`, which the tool
defaults. Go's `IncomingCall` sent an empty action for `""`, outside the
tool's enum. `internal/apisurface` holds every tool to every client, and the
CLI's flags to the schema, but nothing holds a client's arguments to the
schema, so a missing one is silent until someone needs it. Each client now
takes all four; on the iOS simulator each typed into MobiumApp's prompt and
the app read back what it was sent, and on an emulator Java and .NET rang
from, texted from and posted untitled as asked, read back from the shade.

### 166. `text=` found a React Native button on Android and nothing on iOS

**Found by:** driving MobiumApp on the iOS 26.5 simulator through each
client, while checking #165 — `text=Dialog Demo` tapped the button on
Android and found nothing on iOS.

On iOS a node's text is its `value`, kept apart from its label on purpose —
a switch's value is `0` (CHALLENGES 65, 77). A React Native button has no
value, only the label VoiceOver reads, and no child exposes the text, so
`text=` could never find it; on Android the same label is a child
TextView's text. The README had promised label or value. Matching every
label would have made Settings' `text=General` ambiguous — the row is a
button labeled "General" around a text node reading "General" — so an iOS
node with no text of its own matches by its label only when nothing inside it
matches by its own. And React Native nests a Text in a Text, which iOS
reports twice at the same bounds, so `text=Back` found two on the Dialog
Demo on the unfixed build too; a match inside another at exactly its bounds
is now the same match. Captured hierarchies of MobiumApp's home and Dialog
Demo and of Settings hold all three cases. On the simulator the button is
tapped by its text and General still resolves to its one row; nine device
checks pass there and six on an Android emulator, all five clients among
them. `mobium-app.sh` had failed on the unfixed build too: it reached the
Dialog Demo by the ref `map` gave it, and on an iPhone 17 Pro the button is
below the fold, where `map` gives none — it taps by text now, which scrolls.

### 167. A scroll indicator was reported as drawn over the last row

**Found by:** adding MobiumApp's iOS home screen to the captured
hierarchies for #166 — the corpus test that holds ordinary screens free of
covers failed on it.

UIKit lists a scroll view's indicators after its content: "Horizontal scroll
bar, 1 page", 30 points deep over the bottom row. They take no touch, but
nothing in the tree says so, so a tap on "Storage Demo" would have reported a
view over its point. They are known by their shape rather than their label,
which is in the device's language — an Other that is not accessible, whose
value is a percentage — and are never covers now.

### 168. On Android, `cookies` found none on a page that held one

**Found by:** `docs/checks/web-storage.sh`, the first check of `app_cookies`
and `app_storage` against a page's own view, on MobiumApp's new Web storage
page on an Android 15 emulator.

The page — inline HTML loaded with the base URL `https://mobiumapp.test/`,
the way an app hands its WebView a string and an origin — saved a cookie and
read it back from `document.cookie`, and `mobium cookies` answered "no
cookies for https://mobiumapp.test/". `Network.getCookies` with no URLs
answers for the frame's URL as Chrome recorded it, and for a page loaded that
way that is `about:blank` — which is also what `contexts` lists for it — not
where the page says it is. The read now names the page's own URL, from
`location.href`, and is still scoped to it. Until this page existed, every
WebView MobiumApp showed had no origin at all, so neither tool had been
pointed at a page that could hold what it reads; the iOS simulator, through
WebKit's own cookie call, read the cookie from the start.

### 169. scroll-to stopped with its target a sliver at the edge

**Found by:** the documentation site's picture of `mobium scroll-to "label=Card
8" --direction right` on MobiumApp's Pager Demo, on an Android 15 emulator —
the answer said Card 8 was on screen, and the picture showed Card 7 with 101
pixels of Card 8 at the edge.

`app_scroll_to` stops when its target is wholly inside its scroll container,
and on Android that test can never fail once any of the target is in: a
child's bounds are clipped to its container, so a card 683 pixels wide of
which 101 showed reported `[937,476][1038,896]`, inside the pager. It was
reachable — a tap there presses it, which is all `mobium-app.sh` asked — but
not shown, and scrolling an element into view should bring the whole of it in. Two
signs mark clipped bounds together: flush against the container's edge on
the scroll axis, and shorter along it than a sibling of the same kind. A whole
row can sit flush, the same height as its neighbors, so flush alone is not
enough. When both are there, `app_scroll_to` now moves the list a third of
the container at a time for as long as the target grows; on the emulator Card
8 then ends all 683 pixels in view. iOS reports the whole frame, which the
measured nudge (CHALLENGES 114) already brought in; this changes nothing
there. Actions that scroll to their target are unchanged: they need it
reachable, which it was.

### 170. A dialog rule answered in front of an action, not in front of a wait

**Found by:** `tests/mobiumapp` run by `mobium test` on the iPhone 15 Plus.
"the demo account signs in, and out" failed at its wait for the welcome
screen, with the failure itself saying that screen was under "Save
Password?" — the dialog `login.test.json` declares a rule for.

A rule (`app_dialogs`) answered a dialog only when an action resolved its
target and found the dialog over it. Sign-in is a tap, and iOS raises the
sheet a moment after the tap has returned, so the next call to meet it was
the wait, which reported it and timed out. On a simulator the sheet does not
come, which is why the suite passed there. A dialog rule should run before
an auto-retrying assertion as well as an action, and a wait is Mobium's
assertion. So `app_wait_for` now answers a rule for a dialog over
its target, or, on Android, where the tree holds only the dialog's window,
when its target is not there at all. It is bounded as an action is, and
reported in `dialogs_handled` as an action is. A wait for the target to go
answers nothing, because the dialog over it already means it is gone. With
this, the suite passes seven of seven on the phone.

### 171. A real phone's id in every test report

**Found by:** the same run: each line of the list report named the project
`[iphone · <UDID>]`, and the JSON, JUnit and HTML reports carried the id too.

A report is made to be passed around, attached to a pull request or a
ticket, and a phone's id is its owner's (see 111). The documentation site
already dropped phones from what it published. A report now shows a real
phone by its model ("iPhone 15 Plus"), and an emulator or a simulator by its
id, since that tells apart several on one machine. The model comes from
`app_devices`, matched by the id Mobium resolved rather than the one the
config gave. A phone answers to its UDID, its CoreDevice identifier or its
name, and the listing has only the UDID, so the first version, which matched
the config's value, missed the phone it was run on. Neither id is in any
report of the run above.

### 172. An absolute testDir, and a --last-failed that missed and a --list that ignored it

**Found by:** pointing a config outside the repository at the suite in it,
to run that suite on a phone.

A config's `testDir` and `outputDir` were joined to the config's own folder,
so an absolute one was looked for inside it. Paths from a config are now
taken as written when absolute. Then `--last-failed` ran nothing after a run
that had failed two tests: the tests were remembered by their file path as
it was given, relative, and found again by discovery, absolute. A test is now
remembered by its file's absolute path, which is the same however it was
named and from whichever folder the run starts. On the Pixel, `--list` with
`--last-failed` named all seven tests of the suite when four others had
failed: it applied `-g` and not `--last-failed`, so what it said would run
was not what would run. It applies both now.

### 173. devicectl kept the old file and reported the copy done

**Found by:** probing CoreDevice's file service on the iPhone 15 Plus before
building `app_upload` on it.

`devicectl device copy to` says in its help that it skips "files that have
not been modified", and it decides that by size and modification time
alone. A changed file of the same size, stamped with the time of the copy
already on the phone, was "copied" with exit 0 and success in its JSON,
and the phone went on holding the old bytes. A size read-back, the check the
simulator path uses, cannot see it, because the size is the same. So on a
phone an upload goes from a fresh copy stamped now, and is confirmed by
copying it back and comparing SHA-256; a mismatch is tried once more a
second later, then reported as `not_confirmed`. The service also has no
delete — a directory copied with `--remove-existing-content` empties the
whole destination — so nothing removes one file, an upload replaces one by
name, and `docs/checks/files.sh` cleans a phone by reinstalling MobiumApp.
When an app is missing, devicectl says only that "the system failed to get
a list of files", so the app is looked up first and named in the refusal.

### 174. A check printed the phone's recent files

**Found by:** `docs/checks/files.sh` on the iPhone 15 Plus, the first time.

On a simulator the file picker opened on the folder list; on the phone it
opened on Recents, which are its owner's files. The check did not find its
upload there and failed, and its failure message quoted `mobium map` so a
person could see what was on screen. That printed the owner's recent file
names to the terminal. It is the failure CHALLENGES 111 describes, in a
check rather than in a tool. On a real phone the check now withholds
everything it would quote from the device — the map, the folder listing,
what the app says was picked — and asserts only whether. It also finds its
way from Recents through Browse and On My iPhone to the app's folder. A
failure that names the wrong cause was fixed on the way: a device that was
not connected was reported as MobiumApp not being installed.

### 175. Every phone message named its owner

**Found by:** the iPhone runs above, whose output began "starting
WebDriverAgent on Lana Begunova's iPhone".

161 took the owner's name for the phone out of `app_devices`' structured
answer and left it in two places: the `mobium devices` line, read by the
person at the machine, who needs it to pick a phone, and every message.
The message route was the one that travels. The start line, the refusals
for a phone that is not paired, locked or without Developer Mode, a tunnel
that did not come up, a phone no longer listed, WebDriverAgent failing to
start, the list of devices to choose between, and the device model a
session reports all named it, and all of it lands in logs, CI output and
`mobium test` reports. They now name the phone by `Phone.Label()`, its
model ("iPhone 15 Plus"), and "the iPhone" when there is none. The devices
line keeps the name, and a name given as `--device` still matches, since
that is input, not output. On the phone, the start line now reads "starting
WebDriverAgent on iPhone 15 Plus", and the owner's name is in none of the
output.

### 176. Every Compose icon button mapped twice

**Found by:** `mobium map` on Seal, a Jetpack Compose app from F-Droid, on
the first screen — the first Compose app this project had driven. Its three
icon buttons came out as six entries: "Settings", and "Settings (button)".

Compose wraps an icon button in a tooltip box, long-clickable so a long
press shows the tooltip, and lays it out a pixel off the button's own
bounds: `[32,95][158,221]` around `[33,96][159,222]`. `map` merges
actionable nodes that stack on one rectangle, and "one rectangle" meant
equal to the pixel. Nested nodes whose edges are within two pixels of each
other are now one target, tapped at the innermost clickable node and labeled
as before. Only an ancestor and its descendant merge; two neighbors that
happen to line up stay two. The same nesting on identical bounds is how the
launcher's "At a glance" widget was already merged; Compose missed it by one
pixel.

### 177. A Compose dialog was not a dialog to Mobium

**Found by:** Seal's first-launch "User guide", on the same screen. `mobium
alert` said no dialog was on screen with it up, and a tap on the Settings
button behind it failed with "no element matches … run app_map again" — a
remedy that can never work. A declared rule for it did nothing.

On Android the hierarchy holds the dialog's window alone, and Mobium asked
UiAutomator2's W3C alert endpoint whether something was asking. That
endpoint recognizes the framework's `AlertDialog` by its resource ids, and a
Compose dialog has none. The hierarchy says it anyway: its window does not
cover the screen — `[120,474][960,1937]` on a 1080x2400 display — where an
app's own window does, and "covers" rather than "equals", because one
Settings capture reports the display without its navigation bar, 2201
pixels tall, under a 2400-pixel window. So `Tree.Dialog()` returns a window
that floats, and from it: a miss under it is refused as `device_not_ready`
naming the dialog, with a remedy that works for it (its buttons, or a rule,
never `app_alert` accept); `alert` reads it, title and message without the
buttons' captions, and refuses to accept or dismiss it as `unsupported`,
because there is no button the platform picks; and a rule answers it. A rule
also found no button to press at first: a Compose button is a clickable node
whose words are on a child, so a rule's caption is now matched against the
label `map` gives a button as well as its own text.

### 178. `check` refused a Compose switch by its words

**Found by:** Seal's Look & feel settings. `map` printed "Dynamic color …
(button, checked)", and `mobium uncheck "text=Dynamic color"` answered that
it was not a checkbox, radio or switch.

A Compose switch row is one clickable, checkable node, and its label is a
child `TextView`. `text=` resolves to the words, which have no state, while
the ref from `map` is the row and worked. A tap on the words lands on the
row, so the row is what is meant: `app_check` and `wait --for checked` now
take the nearest clickable ancestor's state when the target has none of its
own, and a row with no state, like Display language, is still refused.
`docs/checks/compose-app.sh` holds all three against the app, and passes on
an Android 15 emulator and on the Pixel 8 Pro on Android 17.

### 179. `label=` found a Face ID button twice, and the remedy could not narrow it

**Found by:** answering the Face ID prompt by hand on an iPhone 17 Pro
simulator, while building `app_biometric`. `mobium tap "label=Try Face ID
Again"` was refused as ambiguous — two elements — with the remedy to append
`,role=button`; appended, it was refused again, because both are buttons.
The ref from `map` worked, since `map` merges what stacks on one rectangle.

LocalAuthentication's sheet reports each button as a button inside a button:
same name, same label, same frame, one accessible and one not. `text=`
already counted a node at exactly its ancestor's bounds as the ancestor —
React Native nests a Text in a Text the same way — and `label=` did not.
Now it does. `testid=` still does not: a test id is a name the app gave, and
two nodes carrying it stay two. The fixture is the sheet itself,
`ios26-faceid-not-recognized.xml`.

### 180. Two first drafts of the biometric prompt reading, corrected by the simulators

**Found by:** running `app_biometric` against both kinds of iOS simulator
before it was committed.

The first draft answered a Face ID face that did not match, shown to the Not
Recognized alert, with "tap Try Again". Tapped by coordinates, or through
WebDriverAgent's own element click, Try Again leaves that alert up on a
simulator, and a second failure changes nothing — so the remedy could never
work. It names what does: a match, which the alert still accepts, or Cancel.

The same draft took the LocalAuthentication alert to mean "not recognized",
which is true of Face ID, whose waiting prompt is an element named
`authentication_ui` and becomes an alert only after a failure. Touch ID's
prompt is that alert from the start, so on an iPhone SE simulator every
non-match was refused before it was sent. The prompt is now compared before
and after: a match closes it (accepted); a non-match changes it (not
recognized) or closes it (failed — Touch ID gives up on the third, and the
app hears `authentication_failed`). Touch ID's second failure changes
nothing on screen, and that answer says so rather than claiming a result.

And on the emulator, the first draft of enrollment read the screen straight
after `am start` for Settings, found MobiumApp still in front, and reported
that enrollment had left Settings — the rule about dispatch and foreground,
met again. It now waits for Settings to arrive, and only an app in front
after Settings has been is a walk that left.

### 181. mobium drove a WebDriverAgent it had not installed, believing it its own

**Found by:** another tool's session on the same simulator, during the work
on [the hit test](guides/autowait.md#what-it-does-not-see). A
WebDriverAgent built from source elsewhere is installed under the bundle id
every build of it shares, `com.facebook.WebDriverAgentRunner.xctrunner`. On
a simulator mobium checked only that an app with that id was installed, and
went on driving that 16.12.11 build while it pins 16.12.8 — the drift the pin
exists to prevent, with nothing to say it had happened. The two could not be
told apart by version either: both runners' Info.plist says "1.0".

A simulator's app container is a folder on the Mac, so mobium now compares
the installed test bundle, `WebDriverAgentRunner.xctest`, with its verified
copy, and replaces a runner that is not its own. Measured: the first session
after the other build said "replacing a WebDriverAgent that is not mobium's
16.12.8", the installed bundle then hashed to mobium's, and the next session
left it alone. Two tools on one simulator will go on replacing each other's
runner; mobium now notices.

### 182. A remedy that could not work, for a device whose UiAutomation was taken

**Found by:** another automation tool's session on the same Pixel 7 AVD.
Its on-device server, run from the shell as `app_process` and left running
after the tool exited, held UiAutomation — which one client may use at a
time — and mobium's UiAutomator2 session failed with "UiAutomation not
connected". The error ended "To run without the UiAutomator2 server, use
--driver uiautomator", and that cannot help: `uiautomator dump` needs
UiAutomation too, and was killed (exit 137). A remedy that is obeyed and
fails is worse than none.

Now, when that is the failure, mobium lists the shell's `app_process` and
`uiautomator` processes, and names the holder, its process id and the
`adb … shell kill` that frees it; with none found, it says another client
holds UiAutomation and to stop the other tool. Measured: with the holder
named, the one `kill` it gave was enough, and the next session started.

### 183. A ref pressed another element after the screen changed

**Found by:** a stale-reference case in a measurement driven over MCP. A ref
resolves by the locator `map` gave it, and nothing checked that the element
it now finds is the one mapped. MobiumApp's home button "Login Demo" and
the Login screen's "Log In" button share a test id, so a ref taken for the
first, used after navigating, pressed the second and reported success — on
an iOS simulator and an Android emulator alike.

Each ref now keeps what it said and where it was when mapped, and one whose
locator finds something that differs in both is refused, "the screen has
changed; run app_map again", which is a remedy that works. Either alone is
allowed: a button's words change as it counts down, and a list scrolls.
Measured on the simulator: the stale ref refused and Log In not pressed;
and a ref taken before a swipe still tapped its button, moved.

### 184. Reading a label under the keyboard was refused as if it were a tap

**Found by:** MobiumApp's OTP Demo on an iPhone 17 Pro simulator, typing a
code into the six boxes and reading back the line that says what they hold.
The one-time-code number pad was up over it, and `app_text` answered "failed
check receivesEvents: the keyboard is over it". Nothing was being touched:
iOS keeps what the keyboard covers in the tree, with its text, and the text
was right there — `app_find` printed it in the same breath.

`app_text` resolved its target with the same check a tap uses, and a tap
must refuse a dialog or the keyboard over its target, since that is where
the touch would land. A read needs only that the locator find one element.
It now resolves through `pickToRead`, which keeps that and the ref check of
183, and leaves the rest to actions. Waiting keeps treating what a dialog
covers as not on screen, as decided in 145.

Android is the other half of it: there the keyboard is a window of its own
and what it covers is not in the tree at all, so the same read was a plain
miss, "run app_map again" — a remedy that cannot find it. A read that misses
while the keyboard is up now says so, and to hide it; measured on the Pixel
7 AVD, hiding it brought back "entered: 950359".

### 185. label=Allow Paste found two buttons, and nothing could say which

**Found by:** answering iOS's paste prompt on the same simulator. Its buttons
are "Allow Paste" and "Don’t Allow Paste", `label=` and `text=` match a part
of a label as well as the whole, and the tap was refused as ambiguous — with
the remedy to append `,role=button`, which both are, and no syntax to ask for
the whole label. `testid=` has long preferred the one element whose id is
the whole value; `text=` and `label=` now do the same, after nested copies
of one element are merged (179): the one whose whole text or label is the
value, ignoring case, wins, and a part that is nobody's whole stays
ambiguous. Measured: the paste prompt answered by `label=Allow Paste`, and
the app read the 14 characters put on the clipboard.

### 186. The emulator console answered nothing, and the error said nothing

**Found by:** `app_shake` on the Pixel 7 AVD, which had passed its check two
days before, failing with "the emulator did not say where its accelerometer
rests:" — and nothing after the colon. Every `adb emu` command printed
nothing and exited 0, while the same commands typed into the console's own
port answered at once.

Another automation tool had rewritten `~/.emulator_console_auth_token`
empty, a documented step of its own, to skip the console's authentication.
With the file empty, `adb emu` sent each command without authenticating and
read no answer; restarting the emulator with the file still empty changed
nothing. The emulator writes a fresh token only when the file is missing at
its start. The console always answers "OK" or "KO", so silence now fails as
`device_not_ready`, and with the token file present and empty the refusal
names it and the fix. Measured both ways: with the file emptied and the
emulator restarted, the refusal named the file; following it — delete the
file, restart the emulator — gave a 16-byte token and a working shake.

### 187. A tap at (50000, 50000) was reported done on iOS

**Found by:** a sweep of requests that cannot succeed, each expected to fail.
On an iPhone 17 Pro simulator `app_tap` with x and y far off the screen
answered "tapped (50000, 50000)"; on Android UiAutomator2 happened to refuse
it, with a message about W3C actions. A coordinate tap now checks the point
against the screen's bounds first and refuses one outside them, on both
platforms, naming the screen's size; a point on screen is tapped as before.

### 188. With the home screen in front, a page behind was attached

**Found by:** exercising `app_cookies` on an iPhone 17 Pro simulator in a
session that had just closed MobiumApp. Safari's page, left open by an
earlier step, was attached although Safari was not in front, and its cookies
were read. With MobiumApp in front the same switch was refused, as 138 has
it.

Whether a page is behind is WebKit's answer, from each application's active
flag, and that answer needs some application named active to compare with.
On the home screen none is — SpringBoard publishes no pages — so every page
counted as not behind. When WebKit names no application in front,
`app_context` now asks the native side which app is, and refuses a page
whose app is not it. Where WebKit does name one it is still the answer,
since an in-app browser's pages belong to another process than the app that
hosts them. Measured: home screen in front, Safari's page refused naming
SpringBoard; Safari brought forward, attached; MobiumApp's own page with
MobiumApp in front, attached.

### 189. Another tool removed Mobium's runner from the iPhone, and Mobium put it back in silence

**Found by:** comparing another tool's session on the iPhone 15 Plus,
2026-09-30. Before installing its own WebDriverAgent, that tool
uninstalls every user app whose `CFBundleName` is `WebDriverAgentRunner-Runner`
except its own, whatever the bundle id. Mobium's runner is built from
WebDriverAgent's source and carried that name, so it went, along with runners
other people had built. Mobium's next session started one anyway:
`xcodebuild test-without-building` installs the runner if it is missing, and
says nothing, so nobody could tell the phone had lost an app or why the start
took longer.

The phone build is now named `MobiumWDA`, so the app on the phone is
`MobiumWDA-Runner`. Only the runner target is renamed: a `PRODUCT_NAME` on the
command line applies to every target, so it looks up a setting keyed by the
target's name and falls back to the name itself, and `WebDriverAgentLib`
keeps its own. Before starting a runner, a session lists the phone's apps; a
missing runner is announced, and one this Mac installed before is announced
as removed. Measured on the iPhone: the rebuilt runner read back from the
phone as `MobiumWDA-Runner` under the same bundle id; after it was uninstalled
between sessions, the next one said "Mobium's WebDriverAgent … is no longer
installed on iPhone 15 Plus — something removed it since the last session" and
started; and the session after that said nothing.

The sweep was then run on the phone: the other tool's own lookup and
removal, called directly, without the install of its own runner that
follows it. The phone held no runner but Mobium's. As a positive
control, Mobium's runner was first built and installed under the old name;
the sweep listed it and removed it. A Mobium session then announced it
missing and installed `MobiumWDA-Runner`, and the same sweep listed nothing
under `WebDriverAgentRunner-Runner` and removed nothing, and `devicectl` read
the runner back from the phone afterward.

### 190. A tap on an element below the screen was reported done

**Found by:** a survey, on the iPhone 15 Plus, of where a full read would act
on an element iOS calls hidden — to see whether `pickOne`, which does not
consult visibility, ever acted on one it should not. On seventeen real
screens the hidden elements on screen were all covered ones, which the
cover rule answers, and one of them, the Obstruction Demo's pass-through
target, really is reached: refusing "hidden" outright would have refused a
tap that works. The defect was elsewhere. Wikipedia's feed held 1,108
hidden elements a locator resolved uniquely, every one with its center
below the screen, and `tap label=computer printers` answered "tapped … at
(615, 2833)" on a screen 2796 pixels tall. Nothing around the link scrolls
in the tree, so the check that an element sits inside its scroll container
had nothing to check it against. CHALLENGES 187 had fixed the same thing
for a tap by coordinates.

Scrolling for it was tried first and was wrong: the link was the hidden
rest of an article extract its card clips, which no scroll brings into
view, and swiping for it ran the call out of time. So an element whose
center is off the screen, with nothing around it that scrolls, is refused
as not visible, saying where it is, and nothing is touched; one inside a
scroll view is brought into view as before. A unit test holds that, and
fails with the refusal taken out.

### 191. `role=button` found an iOS link, which `map` printed as `(link)`

**Found by:** driving NetNewsWire, the second third-party app on iOS, on an
iPhone 17 Pro simulator. An article's page carries a link named after its
feed, "NetNewsWire Blog", and the back button above it has the same label.
`map` printed one `(button)` and one `(link)`, and `label=NetNewsWire
Blog,role=button` was refused as matching two. The touchable-means-button
fallback from CHALLENGES 136 gave the link the button role too, though `map`
names a link before anything else. CHALLENGES 136's rule is that a locator
finds a node by the role `map` printed; it also has to not find it by a
role `map` did not. An iOS link no longer takes the fallback. A test holds
every mapped link on the captured article to it.

### 192. An unlabeled list printed as `XCUIElementTypeTable`

**Found by:** the same app. A container with no name falls back to its
class, which on Android is `RecyclerView` and on iOS was XCUITest's whole
type name, `XCUIElementTypeTable (list)`, on NetNewsWire's Settings and its
article's scroll view, and on every captured system alert. The prefix is
dropped from the short class name, which is all `map` and `formflux` use;
a `class=` locator matches the full name as before, and now the short one
too.

### 193. Section headers mapped as buttons

**Found by:** NetNewsWire's Settings, where "Accounts", "Feeds" and
"Timeline" printed as `(button)`. A section header is accessible, so
VoiceOver can land on it, and is typed Other, which says nothing; Mobium
took accessible-and-not-content as a control. WebDriverAgent's `traits`
said `Header`, and nothing read them. A node whose traits say Header, and
not Button, is no longer clickable. Across every captured hierarchy that
removed four entries, all of them headers.

### 194. Rows named after a disclosure arrow: "On My iPhone chevron"

**Found by:** the same Settings screen. A row with no label of its own is
named from the text inside it, and NetNewsWire's rows carry their
disclosure arrow as a disabled button labeled "chevron", with
`accessible="false"`. VoiceOver never reads it; `map` did. A leaf iOS marks
not accessible is now left out of a composed label. Only leaves: a link is
not accessible and the text inside it is, as Wikipedia's are (CHALLENGES
78). No other label in the captured hierarchies changed.

### 195. Which tab or segment is chosen never reached `map`

**Found by:** NetNewsWire's search, whose scope control offers Here and All
Articles with no sign of which is chosen. `Node.Selected` existed, on the
wire for external drivers too, and was never set on iOS: WebDriverAgent
reports selection only in `traits` (`"Selected, Button"`) and the parser
read the `selected` attribute, which it leaves out. Nor did `map` show it
on either platform. It is read from the traits now, shown as `(button,
selected)`, sent as `selected` in the JSON and every client's element, and
`map --diff` reports a selection that moved. Wikipedia's onboarding
(CHALLENGES 77) now shows its chosen option as selected, which is what that
entry wanted shown.

### 196. A miss under other words was blamed on the keyboard

**Found by:** NetNewsWire's Add Feed sheet. Its URL field maps as `URL
(input)`; that is its placeholder, which is the field's text, and the field
has no label. `label=URL` found nothing and, with the keyboard up, the
refusal said the keyboard "may be covering it" — of a field at the top of
the screen. Hiding the keyboard could not help, and a remedy that cannot
work is obeyed anyway. A `text=`, `label=` or `testid=` locator that finds
nothing is now tried under the other two; when one finds exactly one
element the refusal names it (`no element matches label=URL, but text=URL
does`), and the miss is not scrolled for or put down to the keyboard.

### 197. A row behind iOS 26's toolbar was tapped through the toolbar

**Found by:** the NetNewsWire check failing on its second run. The feed
list grew by a row, which put "NetNewsWire Blog" at y 781–832 points, and
iOS 26 draws the toolbar over the list from 733 down: the list runs the
full height of the screen behind its bars. `tap text=NetNewsWire Blog`
answered "tapped … at (622, 2419)", which touched the toolbar between its
buttons, and nothing happened. The row was inside its scroll container's
bounds, so it counted as in view, and the cover rule leaves a full-width
container that is not a control unreported, rightly, since both platforms
lay transparent full-screen layers over everything. What was missing is
that the part of a list in view is its bounds less the bars drawn over its
edges. `uitree.Viewport` takes off any later node outside the list, as wide
as it, at most a third of its height, across its top or bottom edge, with a
control in it; every "is it in view" question now asks the viewport, and
the measured nudge from CHALLENGES 114 measures against it, so the row is
scrolled out from under the bar and then tapped. Across the captured
hierarchies it found NetNewsWire's toolbar on two screens, iOS Settings'
floating search field, a notification banner over SpringBoard and the
Android launcher's dock, under which no icon lies. On the iPhone 15 Plus
the same thing happened to an article row, its center at y 2553 pixels
under a toolbar from 2538: the build before this answered "tapped … at
(645, 2553)" and opened nothing, and this one scrolled the row out, tapped
it at y 1669, and the article opened. The check taps the lowest article row
on screen on both devices for that reason.

### 198. A simulator typed into once believed a hardware keyboard was attached

**Found by:** `docs/checks/keyboard.sh` failing its first step — "focusing a
field did not bring the keyboard up" — after one run in two on an iPhone
17 Pro simulator, on `main` as on a branch, and passing on a freshly booted
one. The field had focus, empty and ready; the keyboard was in the tree
with its top at y 952 on a screen 874 points tall, below the edge, with
its input-assistant bar under it: the state iOS keeps it in when a
hardware keyboard is attached (CHALLENGES 107). Simulator's own Connect
Hardware Keyboard setting was off throughout.

What put it there is XCTest's typing, now and then. After a reboot, one
`fill` or `type` left it there in some runs and not others, with the same
requests in the same order, traced; WebDriverAgent's value and keys
requests sent by hand did not, five times out of five; a full run of the
check left it there four times in four. Once there, relaunching the app
did not bring the keyboard back, nor did restarting
`com.apple.TextInput.kbd`, nor anything `simctl` offers. A reboot did.

Mobium's answer in that state is the true one: the keyboard is not shown,
and a target under it is not covered. What was wrong was the check, which
assumed a simulator nobody had typed into since it booted. On a simulator
it now reboots once, saying so, when a focused field leaves the keyboard
held below the screen, and judges the step after that; four runs in a row
passed, each with its reboot. A real iPhone does not do it: on the iPhone
15 Plus three runs in a row passed with the keyboard up each time, so the
reboot is a simulator's only, as the state is.

### 199. An interrupted test run left its daemons running

**Found by:** `docs/checks/test-ui.sh`, which failed on its first draft
partway through a run on an emulator and a simulator; its cleanup killed
`mobium test --ui`, and two daemons, `mobium-<run>-1` and `-2`, were still
running afterwards with their sockets left. A run gives each project a
`mobium pipe` and a daemon of its own and stops them when it ends — in a
deferred cleanup, which a killed process never reaches. They would have
stopped themselves after their thirty-minute idle timeout, holding their
devices' sessions until then. Failing runs that never reached a device did
not show it; only a run that was driving devices when it was stopped did.

Plain `mobium test` had the same gap on Ctrl-C, and the first fix half
closed it: closing the connections made the run's remaining calls fail at
once, so the command printed its summary and exited before the daemons it
was stopping had stopped, and one was left. Now both commands remember every
project connection a run opens, and on an interrupt or SIGTERM close each one
still open — ending its session and stopping its daemon — while the run's
own path waits for that and does not exit first. Killed mid-run with SIGTERM,
`--ui` left no daemon; interrupted mid-run three times, `mobium test` left
none.

### 200. A refused tap in Chrome's web app said the app had navigated away

**Found by:** `docs/checks/pwa.sh`, written on 2026-10-01 to hold a
progressive web app the way APP-TYPES described it, on its first runs on
the Pixel 7 AVD. Squoosh, opened from its home-screen icon, mapped, read
and evaluated, and a tap on a ref in its page was refused: "no WebView is
visible in the native hierarchy — the app may have navigated away; switch
back with `app_context NATIVE_APP`". The app had not moved: Squoosh was in
front, in Chrome's `WebappActivity`. Chrome had stopped putting its
`android.webkit.WebView` in the accessibility tree, leaving a
`FrameLayout` described as "Web View" with no children, and nothing native
said where the page was.

Measured by reading the hierarchy every second or two after a launch from
the icon: opened while Chrome was already running, the WebView was there
for about five seconds and then gone for good, three launches out of three;
opened after Chrome's process had ended, it stayed for the half minute
sampled, two out of two. Once gone, Chrome's tabs lost theirs too, until
Chrome was restarted. The "Web View" frame is not a stand-in: when the
WebView is present the frame is not, and the one sample of it started 147
pixels higher than the WebView had.

The refusal itself was right — a tap with no host to measure from would
land wherever the arithmetic put it. Its remedy was not: it sent the caller
to `NATIVE_APP`, where Chrome's tree is just as empty, and a remedy that
cannot work is obeyed anyway. Now it says the app is in front, names both
causes, gives the remedy only for the one that has it, and says reading the
page still works. `pwa.sh` launches the app with Chrome running, as an
install leaves it, and holds Mobium to landing the tap or refusing it with
that explanation, with the page counting nothing; `chrome.sh` starts Chrome
fresh. What would let a tap be placed again is open (ROADMAP).

On the Pixel 8 Pro, Android 17, Chrome 154, it did not happen. With Play
services Chrome installs a web app as a WebAPK, a package of its own, not a
launcher shortcut; launched with Chrome running, its WebView stayed in the
tree for the thirty seconds sampled and a tap in its page landed. So this is
the emulator's legacy shortcut, Chrome's `WebappActivity`; `pwa.sh` accepts
the refusal there and requires the tap to land on a phone.

### 201. On iOS an async expression came back as `{}`

**Found by:** the same check, asking a home-screen web app's page whether a
service worker was registered: `navigator.serviceWorker.getRegistrations()`
is a promise, and on the iPhone 17 Pro simulator `app_eval` answered `{}`
where the same expression on Android answered `true`. Any promise did:
`Promise.resolve(42)` was `{}` too.

Both transports send `awaitPromise: true` with `Runtime.evaluate`. CDP
honors it. WebKit's `Runtime.evaluate` has no such parameter, ignores it,
and with `returnByValue` serialized the pending promise itself. Nothing
failed — the call succeeded with a value — so a script that awaited
anything on iOS was answered before it ran. Now the expression is evaluated
to a handle; a promise is awaited with WebKit's own `Runtime.awaitPromise`,
anything else is read back by value, and the handles are released. The
simulator test asks for a resolved promise, an async function, a delayed
promise, an object and a rejection: against the old code the four promise
cases failed, against the new one all pass, and `ios-webview.sh`,
`pwa.sh`, `web-storage.sh`, `web-type.sh` and `web-actionability.sh` passed
on the simulator after it.

### 202. Launching an installed web app reported the browser in front

**Found by:** `docs/checks/pwa.sh` on the Pixel 8 Pro, the first run with
Play services. Chrome installed Squoosh as a WebAPK,
`org.chromium.webapk.<hash>`, and `app_launch` of that package opened the web
app and answered "launched org.chromium.webapk…, but com.android.chrome is
in the foreground" — after waiting out the whole foreground budget for a
package that was never going to be the one in front. A WebAPK's pages are
shown by the browser: the top activity is Chrome's `SameTaskWebApkActivity`,
in a task whose root is the WebAPK's own splash activity, so the hierarchy,
and every read of what is in front, names Chrome.

The general answer — the launched app owns the task in front — would also
call a permission prompt over a just-launched app "launched", and that
warning exists for the prompt. So the rule is the narrow one Android states:
the task in front is the launched package's and the activity on top of it is
a browser's `WebApkActivity`. `app_launch` then says it launched an installed
web app, shown by Chrome, whose page is a `WEBVIEW_com.android.chrome`
context. The fixture is the five lines of that task, nothing else from the
phone's activity list, and the permission-prompt capture is the negative
control.

### 203. A tab whose renderer had gone held a context switch for minutes

**Found by:** first on the Pixel 7 AVD on 2026-10-01, switching into a
long-backgrounded Chrome tab, which hung until the client gave up; not
reproduced on the Pixel 8 Pro, where a tab twenty minutes in the background
attached in 0.45s. Reproduced on the emulator by killing a background tab's
renderer, which is what Android does to a tab it unloads: the tab stays in
the DevTools listing, the debugger's connection is accepted, and nothing
ever answers. `app_context` took six minutes and the client's read timed out.

Each CDP round trip was meant to be bounded by thirty seconds, but only
when the call had no deadline of its own, and every tool call has one —
four minutes — so the bound never applied; WebKit's transport bounds every
round trip regardless, and did not have this. And the switch ignored its
console install failing, so it would have reported success over a page that
answers nothing. Now each round trip is bounded either way, a timeout is
reported as `timeout`, and a switch asks the page for `1` first, refusing
within ten seconds with what happened and a remedy that works: choosing the
tab in Chrome's tab switcher reloaded it, and the same switch then attached
and read it. A unit test with a server that accepts and never answers held
the old code for its whole one-minute deadline.

### 204. Back said "pressed back" whether it went back or closed the app

**Found by:** Lana, on the Pixel 8 Pro: in MobiumApp a swipe back from any
demo closed the app. Measuring it on five devices (docs/BACK.md) showed the
app was at fault — it handled no back — and that Mobium would never have
said so: `press back` answered "pressed back" whether the app went to its
home screen or closed, its result claimed nothing, and a test using it saw
the same answer for the bug and for the fix. Back was reported as "sent"
on the reasoning that what it does is the app's business — true, and the
reason to report what the app did rather than nothing.

Now back reads what is in front before and after, says when it left the
app, and on iOS gives the navigation bar's title; it has its own wait,
since the launch wait it first borrowed held every back inside an app for
2.4s. Asked for, it is the gesture rather than the key — on iOS the only
back there is — and refused where an edge swipe is not back. The check
that holds it is `docs/checks/back.sh`.

### 205. A PWA from the Play Store was reported as Chrome

**Found by:** installing OYO Lite, a Trusted Web Activity from the Play
Store, on the Pixel 8 Pro to see whether Mobium could drive a store-listed
PWA. It could — the page attached, ran standalone and took taps — but
`app_launch com.oyo.consumerlite` answered "but com.android.chrome is in the
foreground", and a back out of it said "it left com.android.chrome", which
reads as leaving the browser. A TWA's screen is Chrome's `CustomTabActivity`
on top of the TWA's own task, so every read of the app in front names
Chrome. 202 had recognized the same shape for a WebAPK, by
`WebApkActivity`, and nothing else.

Now one reading serves launch and back: a browser's `WebApkActivity` or
`CustomTabActivity` on top of a task rooted in another package names that
package, as an installed web app, a Trusted Web Activity — rooted in
Google's `androidbrowserhelper.trusted` launcher — or an app in a custom
tab. Launch says which; back names the app it stayed in or left, while
waiting on the window's package as before. The fixture is the five lines of
OYO Lite's task. `docs/checks/twa.sh` holds it.

### 206. A Flutter app, first look: five things wrong

**Found by:** MobiumApp's `flutter/` demo, built on 2026-10-01 to test what
APP-TYPES had only reasoned — that Flutter paints and needs a driver. It
did not: UiAutomator2 and XCUITest read Flutter's semantics tree, and
`map` listed every control. The first run, judged by what the app said it
got, found five things wrong.

- **Typing on Android went nowhere and was reported done.** `type` said
  `typed "lana"`, the tree read the field back as holding it, and the app
  said "Signed in as  with 0 characters". Flutter offers a field's set-text
  action only while it has focus; UiAutomator2 sets text without focusing.
  Now an unfocused *virtual* field — `drawing-order="0"`, made up by an
  accessibility provider, as Flutter's and Compose's are; MobiumApp's real
  React Native fields read 10 and 12 — is clicked first, as a person taps
  before typing, and a keyboard the click raised is put away again: left
  up, it covered the next button in `login.sh`. Real views are not clicked.
  The first version clicked every field, and on the Pixel 8 Pro each login
  then ended in an offer to save the password — Google Password Manager's
  sheet, or the system's own dialog, which `alert` does not see — because a
  click starts an autofill session. Seal's Compose field took text with the
  click, on screen.
- **An empty field mapped as "EditText".** Flutter reports a field's label
  as its hint with no text, where a native field puts the hint in the text.
  `map` names an empty field by its hint now, and `label=` finds it by the
  same name — a name `map` prints that no locator takes is a dead end.
- **On iOS `testid=signIn` was refused as covered by "Sign In".** Flutter
  turns `Semantics(identifier:)` into an element of its own, followed by the
  button in exactly its frame; the button is the control the identifier
  names. A sibling in the target's very frame, over a target that is no
  control, is no longer a cover.
- **On iOS a password was reported dropped, and printed.** Flutter's
  obscured field is a plain TextField until it holds something, so the field
  was resolved as no password, the bullets it read back did not equal the
  text, and the error said `typed "secret12" and the field holds
  "••••••••"`. A read-back of nothing but bullets now marks the field a
  password, confirmed by length and never echoed.
- **On iOS `scroll-to` stopped after one swipe.** Flutter's scroll view is
  an empty element and the rows it scrolls are its siblings, so reading the
  container found nothing that moved. A childless container is read through
  its parent.

## Findings that were not defects

Worth recording because each one closed off an approach that looked obvious.

- **Back skips a page `app_eval` navigated to.** In OYO Lite, a link tapped
  and then back returned to the page before; `location.assign` run through
  `app_eval` and then back left the app. Chrome skips, on back, history
  entries made without a user's gesture, and a script run over the
  debugging protocol has none. It is Chrome keeping a page from trapping
  back, not Mobium; a test that wants back to return must reach the page
  with a tap.

- **A button behind a sheet makes a locator ambiguous, safely.** On
  NetNewsWire's Add Feed sheet `label=Add,role=button` matched two: the
  sheet's Add, and the feed list's Add behind it, still in the tree as
  `visible="false"`. The refusal is right — it touches nothing and its
  remedy, a ref from `map`, works — and choosing the visible one would
  make visibility decide resolution, which CHALLENGES 190's survey found
  wrong for a pass-through target. Left as it is.

- **A reinstall resets a phone app's permissions — an install over it does
  not.** A note from 2026-09-28 said the iPhone kept a notification denial
  through a reinstall, which would have left a phone no way to reset
  permissions at all. Measured on the iPhone 15 Plus, iOS 26.6.2, with
  MobiumApp's notification, location and camera permissions denied: an
  uninstall and an install brought back all three prompts and emptied the
  data container (65 entries to 9); an install over the app, with no
  uninstall, kept all three denials. The second is an update, and is most
  likely what the earlier note saw. `app_clear_data` on a phone uninstalls
  first for that reason.

- **A biometric prompt, seen from outside, on each virtual device.** On an
  Android emulator the fingerprint service counts every touch it looks at —
  accepted, rejected, locked out — in `dumpsys fingerprint`, so a finger
  presented to no prompt moves nothing and can be told from one a prompt
  refused; five refusals lock the sensor out, which the service reports live
  as `timedLockout=true`. An emulator enrolls no fingerprint without a
  screen lock, and clearing the lock removes every print with it. On an iOS
  simulator nothing counts: the prompt is read from SpringBoard's tree
  instead, and while Face ID waits for a face the app's own tree reads as
  one bare scroll view. `app_alert dismiss` does not close Face ID's Not
  Recognized alert — WebDriverAgent's endpoint returns and the alert stays,
  which `app_alert` reports — while a tap on its Cancel does. Enrollment on a
  simulator is a notification's state, `com.apple.BiometricKit.enrollmentChanged`,
  and a running app hears the change within a second.

- **An emulator's fingerprint failures carry over, and nothing outside
  resets them but a restart.** On a freshly booted Pixel 7 AVD a prompt
  locked out on the fifth stranger. After that, failures from earlier prompts
  counted: the lockout came on the second or third, whatever came between —
  a finger accepted, the PIN at the lock screen, thirty seconds' wait. Twenty
  in all made it permanent, which the PIN at the lock screen cleared, and the
  next stranger locked it again. The touch that finds the sensor locked is
  counted as a lockout, so a match can be answered "locked out", and
  `app_biometric` says the failures carried over. `docs/checks/biometric.sh`
  asserts no count for that reason: each stranger is not recognized until one
  is locked out, the app hears lockout, and the finger signs in once it ends.

- **The emulator console's network throttling reads back and does
  nothing.** On emulator 37.1.11 with the Pixel 7 AVD (API 35), `network
  speed` and `network delay` were accepted, and `network status` read them
  back as set; 2MB from the Mac at a 1000 kbit/s limit still took 1.1s
  where 16s was due, a 500ms delay left a ping to the Mac at 1ms and a TCP
  connect to the internet at 490ms, over Wi-Fi and over emulated mobile data
  alike. Given at launch, `-netdelay` did slow outside traffic, and then the
  console could take the delay away but not put one back. So `app_network`
  does not use it: an emulator's Google APIs image is a userdebug build,
  and `tc` under `su` shapes every packet — a netem delay, and token buckets
  for upload and, through ifb0, download — measured by the traffic, not the
  settings. Two more things the traffic showed before this shipped: airplane
  mode removes the filter that feeds the download limit while the limit
  stays, so a read-back from the limit alone reported one nothing applied;
  and back from airplane mode the emulator is online on eth0 and moves to
  wlan0 seconds later, so a delay put on the default interface of that
  moment was on one nothing used. And on a phone with wireless debugging on,
  the network coming back raises "Allow wireless debugging on this
  network?" — a prompt only the owner should answer.

- **WebDriverAgent's `hittable` cannot see an overlay hidden from
  accessibility either.** The page source never emits it (see the step 4
  entry), but one element can be asked for it, and XCTest computes it with a
  hit test. On the Obstruction Demo, simulator: true for the target with a
  clear center, false under a pressable cover, a scrim and a plain view —
  and **true** under the overlay hidden from accessibility, where a tap
  lands on the overlay, and **false** for the pass-through target, where a
  tap reaches it. Its hit test is an accessibility hit test, so it misses
  what accessibility cannot see and refuses what a finger reaches. It
  closes nothing in CHALLENGES 115's blind spot and would break the case
  that works, so nothing consults it.

- **An iOS app read as in front all the way through being backgrounded.**
  While WebDriverAgent's `deactivateApp` held Settings away for four
  seconds, `apps/state` was asked once a second and answered 4 — running in
  the foreground — seven times in seven. Nothing was wrong with the
  backgrounding: WebDriverAgent answers one request at a time, so each
  reading waited for the deactivation to end. A `simctl` screenshot, which
  does not go through it, showed the home screen halfway through. To watch a
  WebDriverAgent action from the outside, use something that is not
  WebDriverAgent.

- **A tap on Log In above the keyboard reached the app, and the app moved the
  button.** On a headless Android emulator, with the soft keyboard up over
  MobiumApp's Login Demo, `tap testid=loginBtn` was reported as tapped and
  the login never ran, in some runs and not others. The layout differed
  between runs: when the password field's "meets the requirements" notice
  was not yet showing, it appeared on the field's blur — which the tap
  itself causes — and pushed Log In below the keyboard while the press was
  still down, and React Native cancels a press whose target slides out from
  under it. A finger does the same. Blurring the field first, the tap
  reached the app five runs in five. It is the app's layout shift, and a
  useful control for one.

- **Four things the Dialog Demo measured that are the platforms' own.**
  Android's share API resolves "shared" however its sheet closed — Back gave
  "shared" — so the app says only "closed" there. iOS 26's share sheet has
  no close button in the tree at all, only its targets; a swipe down closes
  it. An iOS notification prompt comes once per install, because simctl's
  privacy reset does not cover notifications; reinstalling brings it back.
  And a button's caption is its `text` on Android and its `label` on iOS:
  `map` prints it the same on both, but no one locator kind matches it on
  both, which the declared handlers will have to answer.
- **An app's own overlay hides what is under it from nothing.** The Dialog
  Demo's pinned button, drawn over the end of the list, covered the camera
  button; WebDriverAgent reported both `visible="true"`, and the tap landed
  on the overlay. A dialog and the keyboard are recognizable; an app's own
  view over another is only geometry and drawing order, and Mobium does not
  compute either. Fixed in the app by putting the button below the list;
  open in Mobium.

- **A simulator app's preferences outlive their file.** The obvious way to
  clear an iOS simulator app's data is to empty its data container. It leaves
  the preferences: a key written with `defaults` was not yet on disk at all,
  and after its plist was deleted cfprefsd went on serving it. Restarting the
  daemon did not clear it either — it came back under a new pid, still
  serving the value, most likely because app preferences sit with the
  per-user agent rather than the daemon launchctl names. `defaults delete`
  goes through cfprefsd, so it is what `app_clear_data` uses, first, and the
  preferences are read back through the same daemon afterwards.
- **Reinstalling an app moves its data container.** The same app's container
  was at one path before a rebuild installed over it and another after, so
  nothing may keep the path: it is asked of simctl every time.
- **`pm clear` revokes runtime permissions too.** A location grant flagged
  `USER_SET` read `granted=false` afterwards, on API 35. It is a reset to the
  defaults rather than a revocation, which the Pixel 8 Pro (Android 17)
  showed: `ACCESS_LOCAL_NETWORK` stayed granted through `pm clear`, flagged
  `REVOKE_WHEN_REQUESTED` and not `USER_SET` — a permission the platform
  grants by default, and one API 35 does not have. So `app_clear_data` reads
  the permissions back and reports the ones still granted rather than
  promising which survive. It also stops
  the app, and prints `Failed` with exit 1 for a package that is not
  installed as much as for one it would not clear, so installation is asked
  first. MobiumApp is a Release build and not debuggable, so `run-as` could
  not see its data; the external `Android/data/<pkg>` directory, which the
  shell may list without root, is emptied too, and is the part read back.

- **A recording's frame count is not how long it ran, and differs by
  platform.** Android's `screenrecord` writes a frame only when the screen
  changes: four still seconds were one frame of 0.00s. The simulator's
  recorder also wrote one frame for a still screen, and called it 3.92s. Both
  finish their file only on SIGINT; Android's, killed with SIGKILL, left
  3,232 bytes with no header, which nothing can play. So `app_record` stops
  with SIGINT, judges the saved file by its own header, reports frames,
  duration and wall time side by side, and says a single frame means the
  screen did not change rather than calling it a failure.

- **Android's keyboard cannot type at a cursor.** UiAutomator2's keys
  endpoint, and an element's value endpoint, replace the focused field's
  contents whatever they are asked: "ab" then "cd" left "cd", and so did
  `replace: false`, on API 35. So `app_keyboard` reads the field and writes
  back what it held plus the text — the end of the field, not the cursor,
  and it says "the end". A password field reads back as one bullet per
  character, so one that already holds something is refused rather than
  silently replaced.
- **An empty field reports its placeholder as its value, on both
  platforms**, with the same string as its `hint` on Android and its
  `placeholderValue` on iOS. Taken at its word it made every append look
  like a dropped keystroke, and on iOS the retry for one then wrote the
  placeholder into the field — `usernamemob ü"q's` — which the check now
  guards against.
- **An iPhone keyboard has no key that hides it.** WebDriverAgent says so —
  "Did not know how to dismiss the keyboard" — and pressing return, which a
  single-line React Native field answers by giving up focus, is what works.
  The refusal names it, and the check follows it.

- **An ANR's trace is available without root or a bug report.** The note
  [logcat vs bugreport](https://github.com/lana-20/android-crash-anr-logcat-bugreport)
  says a `bugreport.zip` is what to reach for when an ANR report is needed
  without root, because the trace in `/data/anr` is root-only. On a Pixel 8
  Pro (Android 17, a production build) the dropbox entry `app_crashes` reads
  carried the whole thread dump — the stuck main thread's stack included —
  so `mobium crashes <id>` answers that question in a second. A full ANR
  read now leads with its reason and the main thread, which in that entry
  was at line 73 of 1,582.

- **Android stops recording an app's crashes if it crashes often enough.**
  Running `crashes.sh` repeatedly on the emulator crashed MobiumApp ten
  times in nine minutes, all recorded; the next three crashes — the app
  visibly died each time, and Android showed "MobiumApp keeps stopping" —
  produced no record at all, and `app_crashes` honestly reported nothing
  new. The first record Android wrote afterwards, 7½ minutes after the last
  one before the gap, carried **`Dropped-Count: 3`**: exactly those three.
  That header is now read, and a listing says "(and 3 crashes before it
  Android did not record)". The exact limit is not measured here; the
  effect is. `crashes.sh` names it when the crash happened and no record
  appeared, since running the check repeatedly is how to reach it.

- **A frozen app's ANR has no stacks, and that is the platform's answer.**
  The controlled ANR — MobiumApp stopped with SIGSTOP, then touched — is
  Android's documented kind, input dispatching timed out after 5 seconds,
  and its dropbox entry has no thread dump: collecting one means asking the
  process, and a stopped process cannot answer, so Android lists each
  thread's wait channel (`do_signal_stop`) instead. The read says so rather
  than leading with nothing. The two real shapes, with and without stacks,
  are both fixtures.

- **On a phone, a JavaScript crash's cause is in the log, not the report.**
  MobiumApp's Crash Demo throws an unhandled error; the iPhone's report says
  `EXC_CRASH (SIGABRT)` and `abort() called` and nothing of the error, while
  the app's log has `Unhandled JS Exception: Error: mobium-crash-control …`.
  Android's report is the opposite: the same error, on a Pixel 7 AVD, is a
  `data_app_crash` whose text is the `JavascriptException` and its message.
  And a Release build sends `console.error` to the device log but not
  `console.warn` — a warn marker was simply absent while the app's other
  lines were there, which read as a filtering bug until the level was
  changed. Both are why `docs/checks/crashes.sh` reads the log and the
  report together on a phone.

- **A real iPhone keeps no log to ask for.** Its log is lockdown's
  `syslog_relay`, a live stream with no history, so it is captured from the
  session's start into a bounded buffer: on the iPhone 15 Plus, about 740
  lines a second at rest, NUL-separated, anything outside ASCII escaped with
  BSD `vis`. The phone stamps lines in whole seconds with no year, which
  cannot order two lines in one second, so Mobium stamps each as it arrives
  and says so. Its crash reports are files over AFC,
  `crashreportcopymobile`: 112 at the top level, 35 of them crashes — the
  rest resource reports, jetsam events and spindumps, told apart by the
  header's `bug_type` — and reading every header took 0.24s.

- **A simulator's crash report takes up to half a minute to exist.** Settings
  on an iPhone 17 Pro simulator, killed with SIGABRT from outside, produced no
  report after 6 seconds and none after 16 — which read as "an outside signal
  is not a crash", and would have gone into a comment as that. The report was
  written **34 seconds** after the kill; an abort inside the process, from a
  library injected at launch, after one second. So the answer was "not yet",
  not "no", and `docs/checks/crashes.sh` waits up to a minute for a new
  report. Nothing in a report's header names the simulator; its
  `coalitionName`, `com.apple.CoreSimulator.SimDevice.<UDID>`, does, and is
  the only way to keep two simulators' crashes of one app apart.

- **Dropbox, not logcat's crash buffer, is Android's crash record.** The
  buffer is a ring any noisy process can overrun; dropbox keeps each entry
  under its own id, `tag@epoch-millis`, and on the API 35 AVD still held
  crashes from three days and several boots earlier. Its search terms combine
  with AND, so each tag is its own search, in one shell round trip. It also
  held Mobium's own UiAutomator2 server crashing on disconnect, 30-odd times
  on that AVD — recorded here at first as the server's problem, and in fact
  Mobium's: see defect 93.

- **A shipped app's WebView stays shut on a real phone too.** With a phone's
  WebViews reachable, Wikipedia's App Store build still published no page:
  it does not set `isInspectable`. That had been measured on a simulator
  with another app; this is the same answer from a shipped app on
  real hardware, and why the Wikipedia check reads articles through the
  native tree.

- **A tooltip can hide a whole screen from accessibility, and `map` is right to
  follow it.** Wikipedia's first search raises an "Add languages" tooltip, and
  while it is up every search result reports `visible="false"` — on screen,
  under the finger, and not mapped. It is the platform's answer, not a
  WebDriverAgent failure: the tooltip is modal for accessibility, so VoiceOver
  cannot reach the results either. Closing it brought all nine back.

- **Reading a long article takes about 30 seconds on a phone.** Wikipedia's
  Bacteria article: 33s for every read, against 1.4s for Clock. WebDriverAgent
  asks each of a long page's elements whether it is visible, and a phone
  answers slowly. Not a defect, and not fixed: it is why the Wikipedia check
  allows two minutes for an article.

- **An unanswered permission prompt does not block WebDriverAgent on a phone.**
  On a simulator it does — the runner cannot launch behind one, and the error
  blames the install (62). The iPhone 15 Plus started the runner with
  MobiumApp's location prompt on screen, and `alert` and `map` both read it.
  62's remedy is a simulator's; a phone does not need one.

- **On a phone, an unanswered permission request outlives the app.** A run
  that died with MobiumApp's location prompt up left a request that iOS showed
  again every time the app came forward — **after an uninstall and a fresh
  install too**, over a home screen that had asked for nothing. Answering it
  cleared it. A phone has no `simctl privacy reset`, so reinstalling is how the
  check gets an unanswered permission for its interruption section, and the
  prompt has to be answered first or reinstalling does not help.

- **A WKWebView is unreachable unless the app opted in, and no amount of
  driving changes that.** On iOS 16.4+ WebKit only publishes a target for a
  `WKWebView` whose owner set `isInspectable`. Measured on TheApp
  v1.12.0, a third-party demo app — a real React Native hybrid app, installed and running on iOS 26.5,
  page loaded and rendering — which produces zero attachable contexts. Writing
  `WebKitDeveloperExtrasEnabledPreferenceKey` into the *app's own* defaults
  domain does nothing; that key is Safari's. So this is not something a better
  debugger client could solve, and it is not a simulator limitation either: a
  shipped App Store app that has not opted in is just as unreachable on real
  hardware. It is why the app under test had to be one we control
  ([MobiumApp](https://github.com/mobiumdev/mobium-app)).

  The measurement took three attempts to become valid, each failing the same
  way this project keeps documenting: the first read "no contexts" on a screen
  with no WebView, the second on a screen whose WebView had not loaded, and
  only the third — confirmed by screenshot that content was on screen — meant
  anything. The zero was correct all three times and only earned that the once.

- **React Native repeats an accessibility identifier across the views it
  renders for one component.** `label=Webview Demo` matched **14** nodes on a
  single screen and `testid=navigateBtn` matched 5. Nothing is broken: the
  locators `map` derives are still unique, which was checked rather than
  assumed, and mobium refuses the ambiguous hand-written ones rather than
  tapping the first. But it means a locator typed by hand is unreliable
  against any RN app, and the ref from `map` is the only durable way in. Worth
  knowing before writing a rule tuned on Settings, where one label is one node.

- **`map` labels an empty text input with its placeholder.** A URL field
  containing nothing printed as its placeholder URL followed by `(input)`, which reads
  exactly like a field already filled in. It cost two wrong turns here — a
  "Go" button was tapped twice against an empty field — before a screenshot
  showed the text was gray. Not wrong, since a placeholder is the best label an
  empty field has, and no locator is derived from it. But the map cannot
  currently be read to tell "empty, hinting X" from "contains X".

- **The platforms' own `scrollIntoView` does not take Mobium's locators.**
  UiAutomator2 offers one through the `-android uiautomator` selector strategy
  and WebDriverAgent through `/wda/element/{id}/scroll`, and either would be a
  single round trip instead of a swipe loop. But `UiSelector` covers text,
  description, resource-id and class and not `role` or `path`, and the WDA
  endpoint needs the element id that is being looked for. Mapping what fits and
  falling back for the rest would make scrolling behave differently depending
  on how the caller happened to name the element, so the loop is the whole
  implementation on every backend.
- **A canceled call cannot be resynchronized on one pipe.** The Go client
  takes a `context.Context`, as a Go caller expects. But replies are told apart
  only by id, so a call abandoned half-way would have its answer read as the
  answer to the next one. Canceling therefore ends the connection, and the
  next call says so plainly rather than quietly returning the wrong thing.
- **A JSON library would have been the wrong dependency.** The Java client
  carries a hand-written JSON reader rather than Jackson or Gson. Java is the
  ecosystem where a version collision on the classpath hurts most, and a test
  harness that forces one on the application under test has made the user's
  problem worse to save the author an afternoon. The reader is tested on the
  things that actually break — control characters, whole numbers staying
  whole so a coordinate does not arrive as `540.0`, and the non-breaking
  hyphen in Android's own "Wi‑Fi" surviving a round trip. Its tests have no
  framework either — a main method the Maven build runs in its `test` phase —
  so not even a test-scope dependency is needed, and the build's enforcer
  fails on any dependency at all.
- **iOS WebViews were written off too early.** The note that WKWebView needs
  "a third protocol, reached through `com.apple.webinspectord_sim`" was
  recorded without anyone checking what that is, and the phrase did the work
  of a decision for months. It is a Unix socket. Ten minutes of probing turned
  a wall into a specification — see
  [SETUP, "WebViews and Safari"](SETUP.md#webviews-and-safari). The lesson is
  the same one as the rest of this document, applied to an assumption rather
  than to code: **a name written down is not a finding until someone has run
  it.**
- **Chrome's page content is not in the native hierarchy.** Two different pages
  produced byte-identical hierarchies. This is defect 12 from a different
  angle, and it is why Chrome cannot be used to test anything that watches for
  a screen change.

---



## Environment traps

None of these is a Mobium defect; each cost real time and no error named
its cause.

- **`platform-tools` must be installed into the SDK root** with `sdkmanager`,
  not only as the Homebrew cask. The emulator validates the SDK root by looking
  for that directory and dies with `Broken AVD system path` — while `adb` is on
  `PATH` and working perfectly, which sends you looking in entirely the wrong
  place.
- **`avdmanager` prints `Could not load devices from .../devices.xml`** while
  creating an AVD. Harmless; the device profile is applied anyway. Check
  `hw.lcd.width` in the AVD's `config.ini` rather than believing the error.
- **A running daemon serves the old binary after a rebuild.** `mobium daemon
  stop` after `make build`, or debug code you are not running.
- **`xcodes install` is broken upstream as of 2026-09-11.** Apple removed the
  `/olympus/v1/app/config` endpoint xcodes uses to fetch its service key before
  login, so it 404s straight after the Apple ID prompt
  ([xcodes#490](https://github.com/XcodesOrg/xcodes/issues/490); fastlane hit
  the same thing the same day). This blocked iOS for a day; Xcode came from the
  Mac App Store instead and iOS is verified.
  Workarounds: install Xcode from the Mac App Store, which does not use that
  endpoint, or download the `.xip` from developer.apple.com and run
  `xcodes install 26.6 --path ~/Downloads/Xcode_26.6_Apple_silicon.xip`, which
  skips Apple auth entirely.
- **golangci-lint's own `install.sh` fails a checksum on a genuine download.**
  It greps the checksums file for the archive name, which also matches the
  `.sbom.json` line, so it compares the SBOM's hash against the tarball's and
  reports a mismatch. The download was fine — verified by hand against the
  release's published checksums. `make lint-install` does the same job with
  the match anchored to the end of the line. An integrity check that fails for
  the wrong reason is no better than one that passes for the wrong reason,
  because the next person learns to ignore it.
- **`brew install --cask dotnet-sdk` exits 0 after failing.** It needs `sudo`
  to run the `.pkg` installer, there is no TTY in an agent session, and the
  cask purges itself and returns success. Nothing is installed and nothing
  says so. The official `dotnet-install.sh` installs to `~/.dotnet` with no
  administrator rights and is what `clients/dotnet/README.md` points at. Same
  shape as the Xcode trap above, and the same shape as every device tool here:
  **the exit code is not the result.**

---
