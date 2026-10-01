# 0007 — An inspector: `mobium inspect`

**2026-09-29. Iteration 1 built the same day** — see "What iteration 1
showed", at the end. A page on this machine
that shows a device's screen with every element `map` finds drawn over it,
says for the one clicked what `map` says of it — its ref, role, state and
the locator to write — acts on it, and keeps what was done as a test file.

## Why

Every other surface asks the reader to already know the vocabulary: which
locator names this button, whether `text=` or `label=` finds it on this
platform, whether it is one element or three. The answer is on the device,
and until now reaching it meant running `map` and reading coordinates. An
inspector is how someone learns the locator vocabulary without reading
anything, and often how they first get anything working at all.

It is also the missing half of `mobium test`: decision 0006 left recording a
test from what a person does as "later", and the daemon already sees every
call. An inspector that acts through the tools can keep those calls.

## What was decided

**A client of the tools, not a tool.** Like the test runner and the five
clients, it calls the tools the CLI calls — `app_screenshot`, `app_map`,
`app_find`, and the actions — through the same daemon, so a terminal and
the inspector drive the one session side by side, and an action from the
page is the action a command takes, waiting and refusing the same way.
Nothing here implements behavior: what the page shows is what the tools
answered. It lives in `internal/inspect`; `mobium inspect` serves it.

**What the page does, in iteration 1:**

- the screen, from `app_screenshot` with no path (the PNG comes inline), and
  over it a box for every entry `app_map` returns, in the same device pixels
  the screenshot is in, so no unit is converted;
- the element clicked: its ref, label, role, checked state, bounds, and the
  locator `map` derived for it — the durable one, not the ref, which does
  not survive the next screen;
- a locator typed in, resolved with `app_find`, every match outlined and the
  count said — so "matches 3 elements" is seen, not read about;
- actions on the element clicked — tap, double tap, long press, type, fill,
  check, wait for it — and on the screen — back, home, swipe; each refreshes
  the picture after;
- **what was done, recorded** as `*.test.json` steps in the step shorthand,
  targeted by locator, and downloaded as a test file `mobium test` runs.

**Local, and only to the page it printed.** It listens on 127.0.0.1 only, as
`mobium grid ui` does. That is not enough for a page that taps a device: any
web page open in the same browser can send a request to 127.0.0.1. So every
request carries a random token that is in the URL the command prints, and
nowhere else, and its `Host` must be the address it listens on, which
refuses a DNS-rebinding page too. A request without both is refused before
any tool is called.

**Only the tools it needs.** The page can ask for a fixed list of actions; it
cannot call `app_uninstall`, `app_clear_data` or `app_eval` by naming them.

## What it does not decide

- **A live stream.** The picture is a screenshot after each action and on
  request, not video. Streaming is its own question, platform by platform.
- **The raw hierarchy.** `app_source` is one call away and can be added as a
  view; iteration 1 shows what `map` shows, which is what a test acts on.
- **Phones.** A phone's screen is somebody's; the page shows it only to the
  person who started the command, on their own machine, and saves nothing
  unless they download a test.

## What iteration 1 showed

Built as planned, in `internal/inspect` with `mobium inspect` serving it, and
driven on MobiumApp's Login Demo on an Android 15 emulator: the screen with
`map`'s five elements outlined; the username field clicked, in a real
browser, showing `testid=username`; `role=button` tried and both buttons
outlined as two matches; a request without the token refused, an action by
GET refused, and `app_uninstall` refused by name. Recorded from a fresh
app — tap Login Demo, fill both fields, tap Log In, wait for Welcome — the
downloaded file ran under `mobium test` and passed.

One thing the first recording changed: **it has to start where a test
starts.** Recorded from the Login Demo, the file began with filling a field
on a screen `mobium test`, which launches the app fresh, never reaches; the
tap that opened the demo had happened before recording began. The page now
has **Start from a fresh app**, which stops and launches the app in front as
the runner does, and starts the recording over.
