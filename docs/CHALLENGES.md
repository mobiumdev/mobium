# Challenges

Platform behaviors found while building Mobium, what each one broke, how it
was caught and how it is handled now. Code comments cite entries here by
number — "CHALLENGES 105" — so each entry is the long form of a decision in
the source.

The pattern across them is the reason the document exists: **almost nothing
here was found by reading code or by a test written from imagination.** Of
117 defects, 98 were found only by running against a real device. The other
nineteen — 4, 5, 6, 14, 23, 24, 32, 35, 36, 39, 44, 50, 53, 54, 57, 66, 89, 99
and 100 — came from reading code, the compiler, a test, a linter,
cross-checking a computed number against a screenshot, using the tooling on
itself, and typing a negative number at a command line.

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
| An element is on screen but a locator says it is not | It matched more than once and the message said "no element matches" — 33; or it resolved but sits outside the container being judged — 33 |
| A tap lands slightly wrong | The element was still moving; actions now wait for stable bounds — the settling section, and 17 for iOS points against pixels |
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
| A third-party driver's answer is ignored | Only advertised capabilities are wired up; `mobium doctor` lists drivers found, and the refusal names the backend — [decisions/0003](decisions/0003-drivers-are-processes-not-plugins.md) |

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
attaching to the device — a second Mobium, or Appium — breaks every subsequent
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
driver's own sentence, including its advice to use `--backend uiautomator2`,
reached the user verbatim with the driver's name in front of it, which is what
the pass-through rule in [decisions/0003](decisions/0003-drivers-are-processes-not-plugins.md)
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
  [decisions/0003](decisions/0003-drivers-are-processes-not-plugins.md) the
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
is on the *reading* app. Appium works around it with a separate helper
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
serial with `--backend webdriveragent` — failed the same way, as did a real
iPhone's UDID.

A missing device for a named serial is now checked against the other
platform before it is reported, and the error names the backend to use,
spelled for both front doors: `pass backend "webdriveragent" (on the CLI,
--backend webdriveragent)`. It is `invalid_argument`, not `no_device`,
because the device is there and the combination is what is wrong. A serial
neither platform knows keeps the old answer, which is right for it.
Choosing the backend from the serial automatically would remove the error
altogether, and is a larger change to what an unset backend means.

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
incomplete one. Nothing had caught it: decision 0005's measurements went
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
ends it with no record — Appium stops it the same way. Measured after the
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
webdriveragent backend cannot record the screen" — true of the phone, false
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
mismatch, clears and retries. A password field is exempt from the read-back
— it reads as bullets and could only ever disagree (defect 61's rule) — so
nothing noticed. It is now cleared before typing, which makes `app_type`
replace a field's contents on iOS as UiAutomator2 does on Android, password
or not; the test fails if the clear is removed.

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
the center, as EarlGrey does; and something over the point that is not a
control is tapped through and reported in the result, `cover` in the
structured half. Measured again after the change, every row went the way
the table says it should, on both platforms, except one: on iOS the overlay
hidden from accessibility is still tapped, because WebDriverAgent's tree does
not contain it. That is recorded as the rule's blind spot, and a test
asserts it so it stays known.

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

## Findings that were not defects

Worth recording because each one closed off an approach that looked obvious.

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
  it does not set `isInspectable`. decisions/0004 said so from a simulator
  measurement of another app; this is the same answer from a shipped app on
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
  `WKWebView` whose owner set `isInspectable`. Measured on Appium's TheApp
  v1.12.0 — a real React Native hybrid app, installed and running on iOS 26.5,
  page loaded and rendering — which produces zero attachable contexts. Writing
  `WebKitDeveloperExtrasEnabledPreferenceKey` into the *app's own* defaults
  domain does nothing; that key is Safari's. So this is not something a better
  debugger client could solve, and it is not a simulator limitation either: a
  shipped App Store app that has not opted in is just as unreachable on real
  hardware. It is why the app under test had to be one we control
  ([decisions/0004](decisions/0004-an-app-under-test-of-our-own.md)).

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
  containing nothing printed as `https://appiumpro.com (input)`, which reads
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
- **A canceled call cannot be resynchronised on one pipe.** The Go client
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
  framework either, so the client is checkable with `javac` alone.
- **iOS WebViews were written off too early.** The note that WKWebView needs
  "a third protocol, reached through `com.apple.webinspectord_sim`" was
  recorded without anyone checking what that is, and the phrase did the work
  of a decision for months. It is a Unix socket. Ten minutes of probing turned
  a wall into a specification — see
  [decisions/0002](decisions/0002-ios-webviews-are-reachable.md). The lesson is
  the same one as the rest of this document, applied to an assumption rather
  than to code: **a name written down is not a finding until someone has run
  it.**
- **Chrome's page content is not in the native hierarchy.** Two different pages
  produced byte-identical hierarchies. This is defect 12 from a different
  angle, and it is why Chrome cannot be used to test anything that watches for
  a screen change.

---



## Environment traps

Neither is Mobium's fault; both cost real time and neither error names its
cause.

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
