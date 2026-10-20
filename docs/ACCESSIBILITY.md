# Assistive technology

What Mobium can do with the tools disabled people use to operate a phone or
a TV: screen readers, speech that drives the screen, and voice assistants
that do a task without it. What was measured, what it found, and the plan
to build on it, in the order it should be built. Each step names the control
that shows it can fail before its result counts.

**Status on 2026-10-20.** TalkBack stays on through a session (CHALLENGES 208)
and the accessibility settings are a tool (`mobium accessibility`). VoiceOver
was measured on the iPhone 15 Plus on iOS 27.0.1 the same day: it can be
turned on, walked and heard, and **with it on, a Mobium tap moves VoiceOver's
cursor and reports the tap done** — the first step below. Apple's audit and
Siri were measured on 2026-09-23 and are not tools. Nothing has been measured
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

Not measured: a simulator (it needs an iOS 27 runtime, not installed here); `in` and
`out`; a screen that scrolls under the cursor; a WebView; a system alert;
iPadOS and tvOS, where the routes exist too.

### TalkBack — 2026-10-05

Android's test-automation connection silenced every other accessibility
service while it was open, so a read of the screen turned TalkBack off for as
long as it ran. Both drivers now leave it running (CHALLENGES 208), and with
TalkBack on, taps, scrolls and typing land, because Mobium's touches are
injected rather than explored — the opposite of VoiceOver above.
`docs/checks/screen-reader.sh` holds it. What TalkBack *says* is not read by
anything.

### Apple's accessibility audit — 2026-09-23

WebDriverAgent's `/wda/performAccessibilityAudit` runs XCUITest's audit on
the app in front: 2.4 seconds on the simulator's Settings, 5 findings — a
hit area too small (an 18×22pt Dictate button), a label that repeats its
traits, three contrast failures. On MobiumApp eleven screens came back clean
and the Wide Viewport screen gave three hit-region findings, its links 38×10pt
— the positive control that makes the zeros answers. The phone gave the same.
A tool would have to say that contrast findings name no element, that the
audit's rectangles are points where `map`'s are pixels, and that it audits
only what is on screen.

### Siri — 2026-09-23

WebDriverAgent's `/wda/siri/activate` with `{"text": …}` hands Siri typed
text and answers in 0.4 seconds on the phone. The answer is SpringBoard's —
`map` read "Current Time, 3:44 AM" and "2 + 2 =, 4" — and the panel closes by
itself after 7–8 seconds. "Open Clock" brought Clock forward with a clean
first read. Not measured: anything with side effects, Siri turned off, the
simulator, an app's own App Shortcut.

## The plan

Built in this order. Each step is one PR, with its CHALLENGES entry where it
fixes something, and is done when its check passes on the devices named.

### 1. A tap with VoiceOver on lands, or is refused

The defect above, fixed before anything is built on VoiceOver, since every
later step drives a screen with it on.

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

### 2. Hear the screen reader: `app_speech`

A new tool — the CLI, MCP and all five clients — that returns what the
screen reader says: the element in focus, or a walk of the screen in its
order (`forward` until it repeats or a limit), each step's utterance beside
the `map` entry it lands on.

- **iOS 27 and later**, phones first. Anything earlier is refused, naming
  the version.
- It answers questions `map` cannot: the focus order (2.4.3), whether a
  heading or a status line is spoken (1.3.1, 4.1.3), what a control's hint
  says, and every element spoken that no ref reaches.
- **Android needs measuring first.** TalkBack's speech is not in the
  accessibility tree. Candidates: TalkBack's developer setting that shows
  speech output on screen, and its log. Neither is measured, and the tool
  refuses Android until one is.
- **Control:** a MobiumApp screen built to be spoken wrong — an unlabeled
  icon button, a status line that is not live, a focus order against reading
  order — gives each finding, and the Form Demo gives none.

### 3. The platform's own audit: `app_audit`

Reads only. Apple's audit on iOS, with the three caveats above stated in
each answer, its rectangles converted to pixels so a finding can be matched
to a ref, and contrast findings reported without one.

- **Android needs measuring first.** Google's Accessibility Test Framework
  is a library run inside a test, not a service on the device. Whether
  Mobium can run its checks on the hierarchy it already reads — touch target
  size in dp, missing labels, duplicate descriptions — is the question;
  formflux's units (FORMFLUX.md) are the place it would land.
- **Control:** MobiumApp's Wide Viewport screen and its eleven clean screens,
  on both platforms once Android has an answer.

### 4. Label in Name, from `map`

What speech that drives the screen depends on (2.5.3): a person says what
they see, so the spoken name must contain the visible words. On Android the
two are separate — a node's `text` and its `content-desc` — so a mismatch is
visible in the tree Mobium already reads, and `app_audit` reports it. On iOS
there is usually one `label`, so the visible text cannot be told apart
without reading the pixels; refuse there until something can.

- **Control:** a MobiumApp button whose description says something other
  than its caption.

### 5. A screen reader as a setting

`mobium accessibility screen_reader on` — TalkBack on Android, as
`screen-reader.sh` sets it, and VoiceOver on iOS 27 through the routes
above — confirmed by reading back, and put back when the session ends, as
the other settings are. It makes "run this test with the screen reader on" a
line in a test file.

- On a phone, the session's restore is the only safe way to turn it on: a
  phone left with VoiceOver on answers touch differently for its owner.
  Turning it off reads back off, or the session says it could not.

### 6. Voice assistants: `app_assistant`

A new tool that asks the platform's assistant in text and answers with what
came back and what is in front afterward, for the task-based test above. Its
result always says the request was typed, not spoken.

- **Siri**: measured, to be built as above, on simulators and phones once the
  simulator is measured. Requests with side effects (a message, a call, a
  purchase) are refused unless the caller names the app they are for, and
  nothing is asked that reads the owner's data aloud into a log.
- **Gemini and Google Assistant**: unmeasured. Whether an adb intent opens
  the assistant, whether it takes typed text, and whether its answer is in
  the hierarchy are each a device's answer, on the emulator and on the
  Pixel.
- **Alexa on a Fire TV**: unmeasured, and possibly unreachable — it listens
  through the remote's microphone, and nothing known injects a request. A
  refusal that says so is a result.
- **Control:** MobiumApp publishing one App Shortcut and one App Action whose
  screen shows the request it received; a phrasing for a task it does not
  offer must not be reported as done.

### 7. Speech that drives the screen: Voice Control and Voice Access

Unmeasured, and last, because both take speech, which Mobium does not send.
The question to measure first: whether either can be given a command
without a microphone — Voice Control's command list on iOS, Voice Access's
on Android — and whether an emulator's virtual microphone carrying generated
speech is enough. Until then step 4 is what Mobium checks for these users.

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
