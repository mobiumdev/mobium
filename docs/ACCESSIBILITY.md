# Assistive technology

What Mobium can do with the tools disabled people use to operate a phone or
a TV: screen readers, speech that drives the screen, and voice assistants
that do a task without it. What was measured, what it found, and the plan
to build on it, in the order it should be built. Each step names the control
that shows it can fail before its result counts.

**Status on 2026-10-20.** TalkBack stays on through a session (CHALLENGES 208),
the accessibility settings are a tool (`mobium accessibility`), and Apple's
audit is one (`mobium audit`, #80). VoiceOver was measured on the iPhone 15
Plus on iOS 27.0.1 and an iOS 27 simulator: it can be turned on, walked and
heard, and **with it on, a Mobium tap moved VoiceOver's cursor and was
reported done** (CHALLENGES 348). Step 1 below fixes that, in #243; with
TalkBack on the same paths already landed, on the emulator and the Pixel.
Siri was measured on 2026-09-23 and is not a tool. Nothing has been measured
of Gemini, Alexa, Voice Control or Voice Access.

## Who it is for

A test of assistive technology makes a claim about a person, not a feature,
so the plan is organized by what a person cannot do, as EN 301 549's
functional performance statements are (clause 4.2), and each kind of tool is
held to the success criteria of WCAG 2.2 it is the user of. EN 301 549 applies
those criteria to native software in its chapter 11, which is how a mobile
app is assessed against them.

| A person who… | uses | to | so a test claims | WCAG 2.2 |
| --- | --- | --- | --- | --- |
| cannot see the screen (4.2.1, 4.2.2) | a screen reader: VoiceOver, TalkBack, VoiceView on a Fire TV | hear each element and act on the one in focus | every control is reached, in a sensible order, and spoken with its name, role and state; status changes are announced | 1.3.1, 1.3.2, 2.4.3, 4.1.2, 4.1.3 |
| cannot touch, or not precisely (4.2.7, 4.2.8) | speech that drives the screen: Voice Control on iOS, Voice Access on Android | say "tap Pro" — the visible label | the name spoken for a control contains the words shown on it; every action has a target to name | 2.5.3, 4.1.2 |
| cannot touch, or cannot use the screen at all (4.2.7, 4.2.8, 4.2.1) | a voice assistant: Siri, Gemini, Alexa | ask for the outcome — "add milk to my list" — instead of operating the app | the task the app offers to the assistant is reached and completed, with its result said back | — (see below) |
| sees or reads less, or is affected by motion (4.2.2, 4.2.9, 4.2.10) | the platform's settings: text size, Reduce Motion, contrast, bold text | change how every app looks | the screen still works at the setting: nothing clipped, overlapping, unreachable or animated against it | 1.4.3, 1.4.4, 2.3.3 |
| cannot speak (4.2.6) | anything but speech | — | **no task needs speech**: whatever a voice reaches, a touch or a screen reader reaches too | — |

### Voice assistants are assistive technology when the test is a task

Siri, Gemini and Alexa are not built as accessibility features, and they do
not operate an app's screen. They count because a person who cannot use the
screen uses them in its place: they reach an app through what it publishes to
the assistant — App Intents and App Shortcuts on iOS, App Actions on Android,
skills for Alexa — and answer by voice. So a voice assistant test is
**task-based**: name a task a person would ask for, ask, and check two
things the app is responsible for — that the task happened (read back from
the app, never from the assistant's reply) and that its answer says what was
done. And its counterpart: the same task can be done without speech (4.2.6).

Three limits keep the claim honest:

- **Mobium sends text, not speech.** Siri is given typed text (below), so a
  test reaches the assistant's understanding and the app's intent, never
  speech recognition — accents, dysarthria, a noisy room. Say so in every
  result. Speech itself is a microphone's question; an emulator's virtual
  microphone can take the host's audio, which is unmeasured here.
- **The assistant is the platform's.** Whether Siri understood a phrasing is
  Apple's to fix; whether the app published the task and did it is the
  app's. A test reports the first as an observation and asserts the second.
- **A voice assistant is not Voice Control.** Asking for an outcome is not
  operating the screen by voice; the second is the row above, and needs
  every control's spoken name to match its visible label (2.5.3).

## What was measured

### VoiceOver on a real iPhone — 2026-10-20

On the iPhone 15 Plus, iOS 27.0.1, through WebDriverAgent 16.12.8 rebuilt
with Xcode 27.1, called directly over HTTP. VoiceOver was off before, and
read back off after every run.

- **The runner has to be built with Xcode 27.** WebDriverAgent's
  `/wda/voiceOver/enable`, `/disable`, `/enabled`, `/move` and
  `/currentSpeech` reach XCUIAutomation's VoiceOver service, which Xcode
  27's has and 26.6's does not. The phone's runner had been built with Xcode
  26.6 on 2026-09-30 and kept through the update, so every route answered
  "This API requires an iOS 27+ runtime" on an iOS 27 phone. With the build
  set aside, Mobium built a new one (27 seconds, launch included) and all
  five routes answered. A cached runner is not rebuilt when Xcode changes —
  step 1 below.
- **It speaks what a VoiceOver user hears.** Each move answers the utterance,
  in 0.53–0.54 seconds. MobiumApp's home screen read "WebViews Button",
  "Login Demo Button" … "Accessibility Demo Button", the same fourteen
  buttons in the same order as `map`. The Form Demo read "Email me checkbox,
  unchecked", "Free radio button, checked", "Dark mode Switch button off
  Double tap to toggle setting." — with the hint, which `map` has no field
  for — and three things `map` leaves out because they are not targets: the
  "Form" heading, the caption under the switch, and the app's status line
  "notify=false terms=false plan=free theme=light". Directions are
  `forward`, `backward`, `in` and `out`; any other is refused, naming them.
- **With VoiceOver on, a tap is not a tap.** A touch moves VoiceOver's
  cursor, as a person's single tap does, and activates nothing. `check
  "label=Email me"` read the box back and refused, rightly; `tap
  "label=Pro"` answered `tapped label=Pro` and the radio stayed unchecked —
  a tap reported done that the app never received. The control: with
  VoiceOver off, the same two calls checked the box and moved the radio.
  And a **double tap**, VoiceOver's activation, with the cursor on Pro
  checked Pro.
- **Reading the screen moves the cursor.** After the double tap and a `map`,
  the cursor was on "Accept terms", not where the double tap had left it.
  Which call moved it is not measured.

- **The same on a simulator.** On an iPhone 18 Pro simulator, iOS 27.0, with
  a runner built by Xcode 27 by hand — Mobium's simulator runner is
  WebDriverAgent's prebuilt release, built against the iOS 18.4 SDK, which
  answers as 26.6's did — a touch on Settings' General moved the cursor to
  "General Button" and opened nothing, and a double tap opened it. That
  runtime crashed MobiumApp at launch on this Mac (macOS 26.6), in
  libdispatch, so Settings stood in.
- **Everything else, on the phone.** With VoiceOver on, a one-finger swipe
  moved the cursor and scrolled nothing, and `scroll-to` blamed the end of
  the list; typing reached no field and was blamed on a dropped keystroke,
  while VoiceOver said "Double tap to edit." — a touch and a double tap on
  the field, then typing, landed. An alert's button and a WebView's checkbox
  behaved as the tap did. Moving the cursor forward scrolls the list to keep
  it in view.
- **A VoiceOver touch is slow.** XCTest's touch takes about 5.5 seconds with
  VoiceOver on — by W3C actions or WebDriverAgent's own tap, with its idle
  and animation waits at zero or not — and its double tap 0.7. Asking
  whether VoiceOver is on takes 5 to 22ms.

Not measured: `in` and `out`; iPadOS and tvOS, where the routes exist too.

### TalkBack — 2026-10-05

Android's test-automation connection silenced every other accessibility
service while it was open, so a read of the screen turned TalkBack off for as
long as it ran. Both drivers now leave it running (CHALLENGES 208), and with
TalkBack on, taps, scrolls and typing land, because Mobium's touches are
injected rather than explored — the opposite of VoiceOver above.
`docs/checks/screen-reader.sh` holds it. What TalkBack *says* is read by
`app_speech`, from its log (step 2).

On 2026-10-20 the rest of step 1's paths were driven with TalkBack on, on the
Android 15 emulator and the Pixel 8 Pro on Android 17: a radio, `check`,
`scroll-to`, an alert's button and a WebView's checkbox landed too, at their
usual speed. TalkBack's own notification prompt, raised as it comes on, lost
the first press of "Don't allow" three times in three on the Pixel — refused,
never reported done; why is not measured. And turning TalkBack off, the
system wrote `accessibility_enabled` back after the phone's value had been
put back, which `screen-reader.sh` now waits out.

### Apple's accessibility audit — 2026-09-23, a tool since #80

WebDriverAgent's `/wda/performAccessibilityAudit` runs XCUITest's audit on
the app in front: 2.4 seconds on the simulator's Settings, 5 findings — a
hit area too small (an 18×22pt Dictate button), a label that repeats its
traits, three contrast failures. On MobiumApp eleven screens came back clean
and the Wide Viewport screen gave three hit-region findings, its links 38×10pt
— the positive control that makes the zeros answers. The phone gave the same.
`mobium audit` (`app_audit`) runs it on a simulator or an iPhone, in pixels
as `map` is, keeping a contrast finding that names no element. Android's is
Mobium's own, on the hierarchy (step 3). `docs/checks/audit.sh` holds both.

### Gemini and Google Assistant — 2026-10-20

The assist key (`input keyevent KEYCODE_ASSIST`) opens the device's
assistant; both measured are the Google app's, version 17.66 on the Pixel 8
Pro (Android 17, signed in), where it is **Gemini**, and 15.22 on the Android
15 emulator (signed out), where it is **Google Assistant**. Neither device
has a Gemini app of its own.

- **Typed requests reach both.** Google Assistant's panel has a Keyboard
  button and then a field; Gemini's overlay has an "Ask Gemini" field and a
  Send button. Enter closes Gemini's overlay, Send keeps it open.
- **Gemini's answer is readable; Google Assistant's calculator card is
  not.** "What is 15 times 3": Gemini's 45 was in the tree's text about seven
  seconds after Send; Google Assistant drew "15 * 3 = 45" in a card whose
  nodes carry no text, with either driver — the screenshot shows it, the tree
  does not. Its web results are readable.
- **"Open MobiumApp" brought MobiumApp forward through Gemini in five
  seconds**, the task-based case working; signed out, Google Assistant ran it
  as a web search and offered to sign in.
- **Its overlay stays until closed, and back closes it.** A tap behind it
  found nothing and said to map again until it was refused naming the
  assistant (CHALLENGES 350, `assistant-overlay.sh`).

Requests to Gemini on a phone are saved to the account's Gemini activity:
ask nothing personal, and say what was asked.

### Siri — 2026-09-23, and on iOS 27 since 2026-10-20

On iOS 27.0.1 it is the same, with two differences: a sum's answer is plain
text ("15 × 3 =", "45", with copy and Listen), which `text` reads and `map`
does not list, and a tap behind Siri's panel found nothing until Mobium
learned to wait for it (CHALLENGES 349, `siri.sh`). The panel stays 8.2 to
8.3 seconds; "Open MobiumApp" brought the app forward in about a second.

WebDriverAgent's `/wda/siri/activate` with `{"text": …}` hands Siri typed
text and answers in 0.4 seconds on the phone. The answer is SpringBoard's —
`map` read "Current Time, 3:44 AM" and "2 + 2 =, 4" — and the panel closes by
itself after 7–8 seconds. "Open Clock" brought Clock forward with a clean
first read. Not measured: anything with side effects, Siri turned off, the
simulator, an app's own App Shortcut.

## The plan

Built in this order. Each step is one PR, with its CHALLENGES entry where it
fixes something, and is done when its check passes on the devices named.

### 1. A tap with VoiceOver on lands, or is refused — built, #243

The defect above, fixed before anything is built on VoiceOver, since every
later step drives a screen with it on. As built: VoiceOver is asked about
before every touch, since it costs milliseconds and a person can turn it on
mid-session; tap, `check`, an alert's button and a dialog rule touch, read the
cursor back, and double-tap; typing focuses the field that way; every other
touch is refused naming VoiceOver; a phone's runner built for another SDK is
rebuilt. `voiceover.sh` passes on the iPhone and fails with `main`'s binary.
Left from it: Mobium's simulator runner, scrolling with VoiceOver on through
its cursor's moves, and the 5.5-second touch. The plan as written:

- Before an iOS action, know whether VoiceOver is on — `/wda/voiceOver/enabled`,
  once per session and again after anything that can change it, never per
  tap. A runner that cannot answer is an iOS 26 one, where the question
  stays open; say so rather than assume off.
- With it on, act as a VoiceOver user does — **mutatis mutandis**, as a
  dialog rule is answered with the remote on a TV (CHALLENGES 334): touch the
  target to move the cursor there, read the cursor back with
  `/currentSpeech` and match it against the target's name, then double tap,
  and read the outcome back. A cursor that went elsewhere is refused, naming
  where it went. Swipes, drags and typing need the same question asked;
  measure each before deciding.
- Rebuild a phone's runner when the Xcode that built it is not the one
  installed, as a new WebDriverAgent version is rebuilt now. The build
  records its `DTXcode`.
- **Control:** `tap "label=Pro"` with VoiceOver on fails on today's binary
  and passes after; with VoiceOver off nothing changes. **Check:**
  `docs/checks/voiceover.sh`, on the iPhone, turning VoiceOver on and reading
  it back off at exit, registered in an `at_exit` before it is turned on.

### 2. Hear the screen reader: `app_speech` — built

`mobium speech` and `app_speech` in all five clients: what the screen reader
says about the element its cursor is on; with a target, the cursor put on
it first; with `walk`, a move forward at a time in the screen reader's own
order until it goes no further, comes back round, or `limit` moves are made.
Each sentence is beside the element it was said about, its locator, and
whether `map` lists it — a heading, a caption and a status line are spoken
and not listed. The screen reader must be on (`accessibility screen_reader
on`, step 5). `speech.sh` passes on the Android 15 emulator, the Pixel 8 Pro
(Android 17, TalkBack 17.1) and the iPhone 15 Plus (iOS 27.0.1), and
refuses the simulator.

- **VoiceOver**, through WebDriverAgent's `/wda/voiceOver/move` and
  `/currentSpeech`: 0.58 seconds a move on the iPhone, the list scrolling to
  keep the cursor in view. Past the last element it stays and says the same
  again, after about 2.4 seconds — the walk's end, told from a neighbor
  spoken alike by that wait. A target is touched, as step 1 touches. Which
  element a sentence is about is found by its name: the element whose name
  the sentence starts with, the longest. Reading the screen did not move the
  cursor (`map`, a screenshot, `text`). A simulator's runner, WebDriverAgent's
  prebuilt release, cannot reach VoiceOver and is refused.
- **VoiceOver drops a label's own kind.** A button named "Save button" is
  spoken "Save Button", exactly as its twin named "Save" — so the redundant
  name the Android audit finds cannot be heard on iOS, and the first
  version paired both sentences with the redundant button. A shorter name
  followed only by a kind is now a rival, and rivals name nobody. VoiceOver
  also says a back button as the screen it goes to and "Back" ("MobiumApp
  Back Button"), which no element is named.
- **TalkBack's cursor is moved with a keyboard.** An injected touch passes
  through TalkBack as a click, `input keycombination` Alt+Right reaches no
  TalkBack shortcut, and writing to the touchscreen with `sendevent` is
  refused by SELinux without root. Android's `hid` command makes a USB
  keyboard over `/dev/uhid`, which the shell may open on a user build — the
  emulator and the Pixel both — and TalkBack takes its Alt+Right as the next
  item. It registers in a tenth of a second and is gone once the call ends.
  On the Pixel, TalkBack 17.1 answers a keyboard's arrival with "New keymap
  available"; the call answers it, Got it, which switches nothing, and says
  so.
- **TalkBack's sentence is read from its log**, which holds it only with
  TalkBack's Developer settings' Log output level at VERBOSE: `Speaking
  fragment text="…"`, the one a move causes marked
  `TYPE_VIEW_ACCESSIBILITY_FOCUSED`, about 70ms after the press. An
  utterance is told from the last time's by when it was logged: TalkBack
  numbers them from 1 again each time it is turned on, in the same process.
  Mobium does not set the level — it is TalkBack's preference, set in its
  own settings — and refuses, naming it, while nothing is logged.
  `speech.sh` sets it there and back as found. Verbose logging puts
  everything TalkBack says in the device log while it is on.
- **The element on Android is TalkBack's own cursor**, UiAutomator2's
  `a11y-focused`, taken only when the sentence holds its name: on the Pixel
  the tree's cursor lagged, and a move that said "Back. Button" read the
  heading it had left. A target is reached by moving forward until the
  cursor is on it, found again in each read — connecting the keyboard puts
  the on-screen one away and the fields move. A walk comes back round at
  the first element. TalkBack said its sentence in the order its "Element
  description order" setting gives, state first by default ("not checked.
  Email me. Check box").
- **Never a password.** A sentence about a password field, or one in which
  VoiceOver names its secure text field, is replaced: TalkBack's "Speak
  passwords" is on by default. Measured, TalkBack said a typed field's
  length, "16 characters", and VoiceOver a bullet a character — neither the
  value; the replacement is the rule anyway.
- **On the Screen Reader Demo** both platforms speak the icon with no name
  as a bare "Button" and Send as "Submit form"; TalkBack says "Save button.
  Button".
- **Listening: what an action makes it say.** `speech --listen start`, the
  action, then `--listen stop`, which answers with every sentence said in
  between. It starts before the action because a live region's change was
  said 0.4 seconds after the tap, before a later call could arrive. On
  Android it is TalkBack's log after a mark, each sentence with its cause —
  `focus`, `announcement` or `change` — from the event TalkBack logs. A stop
  waits until TalkBack has stopped talking, not until its log goes quiet: a
  sentence is logged when its turn comes, and a polite live region's "Live
  cart: 1" waited about five seconds behind a paragraph TalkBack was still
  reading; TalkBack logs each word as it begins it, and a new word is talk.
  On an iPhone, which keeps no log, VoiceOver is asked what it is saying
  every quarter second: through a wrong sign-in it gave "Log In Button",
  "Signing in… dimmed" and the app's announcement "Incorrect username or
  password." in turn. WebDriverAgent does not say why, so iOS steps have no
  cause, and a sentence over in less than a poll can fall between two.
- **What listening heard on MobiumApp.** A wrong sign-in's announcement
  (`announceForAccessibility`) on both platforms. On Android "Live cart: 1",
  a change on screen, while "Add quietly"'s count was never said — though
  its re-render made TalkBack say the unchanged "Live cart: 0" again. On an
  iPhone neither cart is said: React Native's `accessibilityLiveRegion` is
  Android's alone, so MobiumApp's live cart is silent there — the app's gap,
  reported as heard.

Not measured: TalkBack's other keymap, VoiceOver's `in` and `out`, a walk
backward, and the speech of a WebView's page on either platform.

### 3. The platform's own audit on Android — built

`app_audit` is built for iOS (#80), and since this step runs on Android too.
Android has no audit a tool outside the app can run — Google's
Accessibility Test Framework is a library inside an instrumented test — but
most of its checks need nothing but the hierarchy Mobium already reads, so
they run there under the framework's names (`formflux.AuditAndroid`):

- `TouchTargetSize`, under 48dp either way, in dp from the device's density,
  not judged when there is none;
- `SpeakableTextPresent`, a control with no text, description or hint in it
  or anything inside it — a resource id is not spoken, which is where it
  differs from `screen --inspect`'s "unlabeled";
- `DuplicateSpeakableText`, controls that say the same thing, one finding
  per name with the count;
- `DuplicateClickableBounds`, two controls on one rectangle;
- `RedundantDescription`, a description ending with the control's own kind
  ("Save button" on a button — not "One-button alert", which flagged on the
  first version);
- `EditableContentDesc`, a text field with a description.

Look-alike findings on repeated rows are one, with a count: the Table Demo
was 46 findings row by row. Contrast needs the pixels and is not checked;
the result names it in `unchecked`. On real captures: Aegis's Previous and
Next icon buttons have nothing to say, Settings' Display screen has two
"Make smaller", and F-Droid's list and Seal's look screen have nothing.

- **Control:** MobiumApp's Screen Reader Demo — a nameless icon and a
  "Save button", each beside its twin built right — gives exactly those
  two, and the home screen none (`audit.sh`, on the emulator, both
  drivers).

### 4. Label in Name — built, in `app_audit`

What speech that drives the screen depends on (2.5.3): a person says what
they see, so the spoken name must contain the visible words. On Android the
two are separate — a node's `text`, or a text view inside it, and its
`content-desc` — so a mismatch is visible in the tree Mobium already reads,
and `app_audit` reports it as `LabelInName`. On iOS there is usually one
`label`, so the visible text cannot be told apart without reading the
pixels; Apple's audit stands there, and Mobium adds nothing.

As built: a control with a description, and exactly one caption — its own
text or the one text inside it that is not a control of its own — is
reported when the description does not hold the caption's words, in order,
whatever their case and punctuation ("Resend" does not hold "Send"). Left
alone: a caption with no letter or digit (a glyph, "×" named "multiply"),
a card's several lines, a switch's own "ON" or "OFF", a text field, and a
control with no description, which is named by its text. **A caption the
app draws is not seen**: Google's Calculator draws its keys, so its "AC"
named "all clear" has no text in the tree to compare. Ten apps on the
emulator (Settings, Clock, Calculator, Contacts, Phone, Wikipedia,
F-Droid, Seal, Files, Chrome) and every captured Android hierarchy gave
none.

- **Control:** MobiumApp's Screen Reader Demo — "Submit form" on a button
  showing Send, beside "Send message" — gives the first alone (`audit.sh`).

### 5. A screen reader as a setting — built

`mobium accessibility screen_reader on` — TalkBack on Android and
VoiceOver on an iPhone on iOS 27 — confirmed by reading back, and put back
when the session ends, as the other settings are. It makes "run this test
with the screen reader on" a line in a test file.

As built:
- **Android:** TalkBack's service added to the enabled ones, the others
  kept, and `accessibility_enabled` on. Confirmed by Android binding
  TalkBack with touch exploration on — 0.55 seconds on the emulator. The
  undo puts both keys back exactly, unset included, and puts
  `accessibility_enabled` back again once TalkBack has gone, since the
  system rewrites it while TalkBack unbinds (CHALLENGES 348). A device
  without Google's TalkBack is refused, naming VoiceView on a Fire TV and
  Samsung's own TalkBack, which Mobium does not set yet.
- **iPhone:** WebDriverAgent's `/wda/voiceOver/enable` and `disable`,
  confirmed by its `enabled`, and set back as found.
- **Simulator and Apple TV:** refused, naming why — the simulator's runner
  is WebDriverAgent's prebuilt one, built before Xcode 27 — and the
  simulator's Settings switch instead.

`screen-reader-setting.sh` holds it: on the emulator, on, a tap that lands
with it on, and both keys as found three seconds after the session (also
from a device with nothing enabled, by hand); on a simulator, the refusal.
On the phones, with `ALLOW_PHONE=1`, on 2026-10-21: the iPhone 15 Plus
turned VoiceOver on, read it back, took a tap with it on and was off after
the session; the Pixel 8 Pro bound TalkBack with touch exploration, took the
tap, and had both keys as found after the session. While a UiAutomator2
session is open Android sets `accessibility_enabled` to 1 — the Pixel's is
0 — so the keys as found are read before any session (CHALLENGES 353).

- On a phone, the session's restore is the only safe way to turn it on: a
  phone left with VoiceOver on answers touch differently for its owner.
  Turning it off reads back off, or the session says it could not.

### 6. Voice assistants: `app_assistant` — built for Android and the iPhone

A tool that asks the platform's assistant in text and answers with what
came back and what is in front afterward, for the task-based test above.
Its result always says the request was typed, not spoken (`typed`).

As built (`mobium assistant "Open Calculator" --app com.google.android.calculator`):
- **Gemini and Google Assistant**: the assist key opens them; Google
  Assistant's Keyboard button, or Gemini's field, takes the request, and
  Gemini's Send or the keyboard's enter sends it — Enter closes Gemini's
  overlay unsent, measured. It waits up to 15 seconds for an app to come
  forward or the answer to settle, and closes a panel still up with back.
- **Siri on an iPhone**: WebDriverAgent's typed request; a panel still up
  afterward is closed with Home.
- **`app` and `reached`**: `reached` is true only when the assistant handed
  the request to the app named. The app in front once the panel closes
  proves nothing — "Open MobiumApp", a web search signed out, read as
  MobiumApp coming forward when it had been under the panel all along, in
  the first version.
- **The answer** is new text the assistant shows that is not one of its own
  controls. Google Assistant's calculator card has none, so a sum asked
  there answers with no text, and says so.
- **Refused**: a request that acts for the owner (call, text, message,
  send, email, pay, buy, order, book, post, share, delete, transfer) unless
  `app` names the app it is for; Siri on a simulator, which answers nothing
  (CHALLENGES 351); a TV's assistant.

`assistant.sh` passes on the emulator (Google Assistant, signed out) and
refuses the simulator. On the phones, with `ALLOW_PHONE=1`, on 2026-10-21 —
each request goes to the owner's account: on the Pixel 8 Pro, Gemini's
"Open Calculator" brought Calculator forward and the control was not
reached; on the iPhone 15 Plus, Siri's "Open MobiumApp" reached MobiumApp
three times in four. The first request of the run left the home screen in
front — Siri handed nothing to the app within the wait, and its panel was
closed with Home — and a request by hand and two whole runs after it passed.
A first request failing is what the simulator's Siri (CHALLENGES 351) and
the emulator's Google Assistant did too; whether Siri was slow or answered
without opening the app was not seen.

Still to do:
- **Alexa on a Fire TV**: measured, and unreachable — it listens only
  through the remote's microphone, which opens only for the physical
  button, and it opens only apps in Amazon's catalog. Not refused by name
  yet: the assist key brings nothing forward there, and the refusal says
  the device has no Gemini or Google Assistant. Unmeasured through the tool.
- **Control:** MobiumApp publishing one App Shortcut and one App Action
  whose screen shows the request it received; a phrasing for a task it
  does not offer must not be reported as done. Signed out, Google Assistant
  could not open MobiumApp at all.
- Requests are never asked that read the owner's data aloud into a log.

### 7. Speech that drives the screen: Voice Control and Voice Access — measured, not built

Both take speech, which Mobium does not send. The question was whether
either can be given a command without a microphone. Measured on 2026-10-20:

- **An emulator can be spoken to with no microphone and no room.** The
  emulator's control port — the one `mobium audio` captures from — has
  `injectAudio`, which plays PCM into the virtual microphone. Speech made
  by macOS's `say` (16 kHz, mono) and injected while Google Assistant
  listened was heard: "Open Calculator" opened Calculator 3 times in 3,
  and "Open Clock" 4 times in 4 once the injection waited for the
  assistant's "Tap to stop listening" control. Sent a fixed 1.5 seconds
  after the assist key it opened Clock 4 times in 6 — speech that arrives
  before listening begins is lost, so wait for it. Two seconds of silence,
  the control, opened nothing.
  `docs/probes/emulator-inject-audio.go` is the probe.
- **It crashes the emulator, sometimes.** Emulator 37.1.11 died twice in
  seventeen injections — SIGSEGV in `audio_forwarder_enable`, under
  `injectAudio` — and each next boot sat behind a crash-report consent
  dialog until the pending report was moved aside. A tool built on it has
  to say so and recover the device, or wait for a fixed emulator.
- **Voice Access is not on the emulator image.** The image has the Play
  Store and no Google account, so installing it needs a sign-in, which is
  the owner's call. The Pixel was not reachable when this was measured.
  Unmeasured: Voice Access hearing injected speech, and whether its
  command list can be driven any other way.
- **iOS has no such route.** Voice Control takes speech only; a simulator
  listens through the Mac's own input device, with nothing to inject into
  short of a virtual audio device installed on the Mac; a phone hears the
  room. Unmeasured.

What this means for the plan: on an emulator, "say this" is possible as a
tool — `injectAudio` with speech the caller supplies, since `say` is the
Mac's and no runtime dependency may be assumed — once the crash is either
fixed upstream or survived. Until then step 4, Label in Name, is what Mobium
checks for these users, and the probe is the way to try a spoken command by
hand.

## Rules for this work

- **A phone is somebody's.** Every check that turns a screen reader on
  registers turning it off before turning it on, and reads it back off at
  exit; a phone left with VoiceOver on cannot be used by its owner the
  ordinary way.
- **Read the app, not the assistant.** A task is done when the app shows it,
  never because an assistant said so.
- **Say which question was answered.** An utterance is what the screen
  reader said, not that it said it well; a typed request is not speech; an
  audit's zero on one screen is about that screen.
- **Never print what an assistant read from the device** — a time is fine, a
  message or a contact is the owner's.
