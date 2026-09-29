# Quick start: `mobium test`

From an empty directory to a test suite that runs on a device, fails with
evidence, retries, and leaves a report CI can publish — the same shape as
Playwright's runner, for native apps. The design and its reasons are
[decisions/0006](../decisions/0006-a-test-runner.md).

Every command and every line of output below is what `mobium test` printed on
2026-09-28, against [MobiumApp](../decisions/0004-an-app-under-test-of-our-own.md)
on an Android 15 emulator. Where a path to a report is shortened, it says so.

Before this guide: [the quick start](../quickstart/README.md) — mobium
installed and a device running — and MobiumApp installed
(`mobiumdev/mobium-app`). Nothing else: the runner is in the `mobium` binary.

## Contents

- [1. A config and a test file](#1-a-config-and-a-test-file)
- [2. Run it](#2-run-it)
- [3. A failure, and what it leaves](#3-a-failure-and-what-it-leaves)
- [4. Run only what failed](#4-run-only-what-failed)
- [5. Retries, and flaky](#5-retries-and-flaky)
- [6. More than one device](#6-more-than-one-device)
- [7. In CI](#7-in-ci)
- [Writing tests](#writing-tests)
- [The commands](#the-commands)

## 1. A config and a test file

```
login-tests/
  mobium.config.json
  tests/
    login.test.json
```

`mobium.config.json` says where the tests are and which devices they run on.
A **project** is a device, as a Playwright project is a browser:

```json
{
  "testDir": "tests",
  "projects": [
    {"name": "android", "device": "emulator-5554"}
  ]
}
```

`tests/login.test.json` is two tests of MobiumApp's Login Demo. Each step is
exactly what [`app_batch`](../API.md) takes — a tool and its arguments — and
each assertion is an `app_wait_for` step or an `expect`:

```json
{
  "app": "dev.mobium.mobiumapp",
  "beforeEach": [
    {"name": "app_tap", "arguments": {"target": "label=Login Demo"}}
  ],
  "tests": [
    {
      "name": "a wrong password is refused",
      "steps": [
        {"name": "app_fill", "arguments": {"target": "testid=username", "text": "mobium"}},
        {"name": "app_fill", "arguments": {"target": "testid=password", "text": "wrongpass1"}},
        {"name": "app_tap", "arguments": {"target": "testid=loginBtn"}},
        {"name": "app_wait_for", "arguments": {"target": "testid=loginError", "condition": "text",
          "text": "Incorrect username or password.", "exact": true}}
      ]
    },
    {
      "name": "the demo account signs in",
      "steps": [
        {"name": "app_fill", "arguments": {"target": "testid=username", "text": "mobium"}},
        {"name": "app_fill", "arguments": {"target": "testid=password", "text": "hunter2"}},
        {"name": "app_tap", "arguments": {"target": "testid=loginBtn"}},
        {"name": "app_wait_for", "arguments": {"target": "testid=welcomeText"}},
        {"expect": {"tool": "app_state", "arguments": {"app": "dev.mobium.mobiumapp"},
          "field": "state", "equals": "foreground"}}
      ]
    }
  ]
}
```

Every test starts from a fresh app: `app` is stopped and launched, then
`beforeEach` runs, then the test's steps. The file is checked before anything
runs — a misspelled tool, an argument its tool does not take, a key the
format does not have — and `--list` shows what a run would cover without a
device:

```
$ mobium test --list
  login.test.json › a wrong password is refused
  login.test.json › the demo account signs in
2 tests in 1 file
```

## 2. Run it

From the directory with the config:

```
$ mobium test
  ok    [android] login.test.json › a wrong password is refused (7.7s)
  ok    [android] login.test.json › the demo account signs in (7.8s)
2 passed (15.5s)
```

Exit status 0. Nothing in the test sleeps: every action waits for its own
target ([auto-wait](autowait.md)), and every assertion retries until it holds
or its timeout — ten seconds unless the step says otherwise.

## 3. A failure, and what it leaves

Change the first test to expect a message the app never shows — `"text":
"Wrong password."`, with `"timeout_ms": 3000` — and run just that test, with
all three reports:

```
$ mobium test -g "wrong password" --reporter list,junit,html
  FAIL  [android] login.test.json › a wrong password is refused (9.5s)
        step 4 (app_wait_for): [timeout] step 4 of 4 (app_wait_for) failed: timed out after 3.012s waiting for testid=loginError to say exactly "Wrong password." — its text is "Incorrect username or password."; steps 1-3 ran before it, and nothing after
0 passed, 1 failed (9.5s)
junit report: …/login-tests/mobium-report/junit.xml
html report: …/login-tests/mobium-report/index.html
error: 1 of 1 tests failed
```

(The report paths are shortened here.) Exit status 1. The failure names the
step, its error code, and what the screen said instead of what was expected.
`mobium show-report` opens the HTML report, which keeps, for each failure, a
screenshot and the screen's map at the moment it failed:

```
mobium-report/
  index.html
  junit.xml
  artifacts/
    android-login.test.json-a-wrong-password-is-refused-1.png
```

On a real phone that screenshot is a picture of somebody's screen, and the
report says so; `--no-screenshots` keeps none.

## 4. Run only what failed

```
$ mobium test --last-failed
  FAIL  [android] login.test.json › a wrong password is refused (8.7s)
        step 4 (app_wait_for): [timeout] step 4 of 4 (app_wait_for) failed: timed out after 3.018s waiting for testid=loginError to say exactly "Wrong password." — its text is "Incorrect username or password."; steps 1-3 ran before it, and nothing after
0 passed, 1 failed (8.7s)
error: 1 of 1 tests failed
```

The previous run's failures are kept in `mobium-report/.last-run.json`.

## 5. Retries, and flaky

`--retries N` runs a failed test again, from a fresh app, up to N times. A
test that fails and then passes is **flaky**: the run passes, and it says so.
This is the runner's own control for it, from `tests/controls/` in this
repository — it saves a counter, and passes only once the counter reaches 2,
which the retry's relaunch makes it do:

```
$ mobium test tests/storage.test.json --retries 1
  flaky [android] storage.test.json › passes on its second attempt (7.3s) — passed on attempt 2
0 passed, 1 flaky (7.3s)
```

Exit status 0. In JUnit, a flaky test is a pass with a `flaky` property, and
in the HTML report it opens on the failure it had first.

## 6. More than one device

Add a project per device. An iOS simulator's id is one Mac's, and a phone's is
somebody's, so a project can take its device from the environment, and an
unset variable is refused by name before any test runs:

```json
{
  "testDir": "tests",
  "projects": [
    {"name": "android", "device": "emulator-5554"},
    {"name": "ios", "device": "${MOBIUM_IOS_DEVICE}", "driver": "wda"}
  ]
}
```

```sh
MOBIUM_IOS_DEVICE=<simulator-udid> mobium test          # both, at once
mobium test --project android                           # one
```

Projects run at once, one worker to a device — a device holds one session,
so two workers never share one — and `--workers 1` runs them one after
another. This repository's own suite, [tests/](../../tests/README.md), ran its
14 tests on an Android emulator and an iOS simulator together in 95 seconds,
for 189 seconds of work.

## 7. In CI

The command a CI job runs, and what it publishes:

```sh
mobium test --reporter list,junit,html
# junit:  mobium-report/junit.xml   — for the CI's test report
# html:   mobium-report/            — as a build artifact
```

The exit status is 0 when every test passed or was flaky, 1 when any failed,
and one of mobium's own codes when the run could not start — 2 for a bad file
or an unset device, 3 for no device — so a job can tell "the app is broken"
from "the run is". The JUnit file has been read by Python's XML parser in
[`docs/checks/test-runner.sh`](../checks/test-runner.sh), counts matching.
A CI job also needs a device running before the tests start; for Android on a
Linux runner, [SETUP.md](../SETUP.md#on-linux) has the emulator's steps.

## Writing tests

- **Find targets with `map` first.** `mobium map` on the screen you are
  testing gives each element's locator; `testid=` is the one that survives a
  translation and a redesign.
- **Assert with `app_wait_for`**, never a sleep. `--for` in the CLI is
  `condition` in a step: `visible`, `hidden`, `text` (with `exact`), `value`,
  `enabled`, `disabled`, `checked`, `unchecked`, `focused`, `count`, and
  `"not": true` for the opposite. The [auto-wait guide](autowait.md#5-waiting-on-purpose-mobium-wait)
  has the table.
- **Assert on anything else with `expect`**: a tool that only reads, a field
  of its answer, and the value — `app_state`'s `state`, `app_network`'s
  `online`, `app_accessibility`'s `settings.bold_text`. A call that changes
  something is refused, because an `expect` repeats its call until it
  matches.
- **Answer the dialogs a platform raises** with a rule in `beforeEach`:
  `{"name": "app_dialogs", "arguments": {"when": "Save Password", "press":
  "Not Now"}}` answers iOS's offer to save a password, and never fires on
  Android, where it never comes.
- **Keep platform differences out of the steps.** Pressing enter in
  MobiumApp's password field submits the form on iOS and not on Android; a
  test that pressed it signed in twice on one platform. Tap the button.
- **A test is a unit.** Tests in a file run in order on one device, but each
  starts from a fresh app, so none may depend on another.

## The commands

| Command | What it does |
| --- | --- |
| `mobium test` | runs every `*.test.json` under the config's `testDir`, on every project |
| `mobium test tests/login.test.json` | runs one file, or every file under a directory |
| `mobium test -g login` | runs the tests whose title — "file › name" — matches a regular expression |
| `mobium test --last-failed` | runs only the tests that failed last time |
| `mobium test --project android` | runs on the named projects only |
| `mobium test --workers 4` | how many devices run at once — at most one per device |
| `mobium test --retries 2` | runs a failed test again, up to twice; a pass after a failure is flaky |
| `mobium test --timeout 30s` | the time for each test; `timeout` in the config is milliseconds |
| `mobium test --reporter list,json,junit,html` | which reports to write, comma-separated |
| `mobium test --list` | lists the tests a run would cover, and runs nothing |
| `mobium test --no-screenshots` | keeps no screenshot of a failure |
| `mobium show-report` | opens the last HTML report |

Not yet: a visible device, stepping through a test, an interactive mode, a
trace for each test, and recording a test from what you do —
[decisions/0006](../decisions/0006-a-test-runner.md) says what each will be.
