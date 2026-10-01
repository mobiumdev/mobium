# 0006 — A test runner: `mobium test`, over JSON test files

**2026-09-28. Iteration 1 built the same day** — see "What iteration 1
showed", at the end. Mobium gets a test runner in
the shape of Playwright's — `mobium test`, projects, workers, retries,
reporters, a report to open — whose tests are **JSON files of the steps
`app_batch` already runs**, executed by the Go binary itself.

## Why now

Every tool a test needs exists: actions that wait and refuse, `app_wait_for`
for every state an assertion checks (visible, hidden, text, value, enabled,
checked, focused), `app_batch` to run checked sequences, sessions that put the
device back, and four kinds of device verified. What does not exist is the
thing a CI job runs and publishes. The competitor notes say it plainly:
"Mobium produces no artifact a CI job can publish. This is the difference
between a tool and a testing framework, and most buyers want the framework"
(`landscape/mobilewright.md`, gap 2). Until now the tests of Mobium itself
have been the shell scripts in `docs/checks/`, which is the same gap from the
inside.

## What was decided

**Declarative test files, run by Mobium.** Not tests in the client languages
run by pytest, Jest or JUnit: that is five runners, five report formats and
five sets of fixtures to keep in step, and none of them works without a
language toolchain. A declarative file keeps the property this project rests
on — one static binary, nothing else installed — and it is what an agent
writes most reliably. Tests in the client languages stay possible later; they
are programs that call Mobium, as they are today.

**JSON, not YAML.** It was YAML until asked why, and the answer held up:

- The standard library reads it; YAML would be a new module.
- Everything else in Mobium is JSON — `app_batch` steps, MCP arguments, the
  tool schemas, `--json`, `docs/api/*.json` — so a test step is **exactly** a
  batch step, checked by the same code.
- YAML reads an unquoted `off` as a boolean under some parsers, and Mobium
  has real values like `bold_text off`. A test that quietly means something
  else is the failure this project exists to refuse.
- It is not a one-way door: every JSON file is valid YAML, so YAML can be
  added later as a second reader of the same schema without changing a test.
  The loss is comments; `description` on a test and on a step covers most of
  it.

**Steps are `app_batch` steps; assertions are `app_wait_for`** (more in
"Assertions" below). Mutatis
mutandis: no second vocabulary. A step is `{"name": "app_tap", "arguments":
{...}}`; an expectation is an `app_wait_for` step, which already retries until
its timeout and fails saying what the screen showed — Playwright's
auto-retrying `expect`. A dedicated `expect` shorthand is a later nicety, not
a new mechanism.

**The runner is a client of the tools, not a tool.** It lives in
`internal/testrun` and calls the daemon as the clients do: one `app_batch`
per test, so a test fails with its step's own error code and number. The
rule that the CLI implements no behavior is about tools — what a tool does,
MCP must get too. A runner reads files and writes reports around tools; it
is the clients' kind of code. If agents need to run a test file over MCP, an
`app_test` tool can wrap the same package later.

## Assertions

Three layers, each the existing mechanism with what it lacks added, never a
parallel one.

**On screen: `app_wait_for`.** Playwright's `expect(locator)` is a check that
retries until its timeout, and a failure that says what it found. That is
what `app_wait_for` already is — "timed out after 10s waiting for
testid=username to hold \"mo\" — its value is \"mob\"". Against
Playwright's matchers:

| Playwright | `app_wait_for` | Gap |
| --- | --- | --- |
| `toBeVisible`, `toBeHidden` | `visible`, `hidden` | — |
| `toBeEnabled`, `toBeDisabled` | `enabled`, `disabled` | — |
| `toBeChecked`, `not.toBeChecked` | `checked`, `unchecked` | — |
| `toBeFocused` | `focused` | its negation |
| `toHaveValue` | `value`, exact; a password field is refused | its negation |
| `toContainText` | `text`, a substring | its negation |
| `toHaveText`, exact or a pattern | — | missing |
| `toHaveCount` | — | missing |
| `.not` in general | only where a pair exists | missing |

Iteration 1 adds three arguments to `app_wait_for` — `exact` for `text`, a
`count` condition, and `not`, which inverts any condition — so the CLI, MCP
and all five clients get them with the runner.

**Off screen: `expect` on a read-only tool.** A test checks more than the
screen: that the app is in front (`app_state`), a dialog is up
(`app_alert`), the device is offline (`app_network`), a notification
arrived. Every tool already answers with structured data, so one step shape
covers them all:

```json
{"expect": {"tool": "app_state", "arguments": {"app": "dev.mobium.mobiumapp"},
            "field": "state", "equals": "foreground", "timeout_ms": 5000}}
```

The runner calls the tool, compares the one field, and retries until the
timeout by the same polling rule as `app_wait_for`; a failure prints the
value last seen. **Only a read-only tool is accepted**, or a retrying
assertion could tap five times. Nothing marks a tool read-only yet, so
iteration 1 adds MCP's own `readOnlyHint` annotation to every tool's schema —
useful to MCP clients in its own right — and `internal/apisurface` fails the
build on a tool that does not declare it either way.

**For every assertion:**

- **A password is never compared.** `value` already refuses a password
  field; an `expect` on a field carrying device text goes through the same
  redaction as `map` (CHALLENGES 43).
- **A failure leaves evidence**: the screenshot and `map` of the moment it
  failed, in the report. On a real phone the report says it holds a
  screenshot.
- **Each assertion has its own timeout**, ten seconds by default as
  `app_wait_for`'s, inside the test's.
- **An assertion is shown it can fail.** The suite has, for each kind, a
  test that must fail, and counts as passing only when it does.

**Later: soft assertions**, which record a failure and carry on. A test is one
`app_batch`, which stops at the first failure; soft steps mean splitting a
test into batches at each one, which iteration 2 can do.

**Not planned: screenshot comparison.** Pixels differ between devices, OS
versions, dark mode and a clock in the status bar; `toHaveScreenshot` would
be a source of flaky failures, not of evidence.

## The files

A test file, `*.test.json`:

```json
{
  "description": "The login demo's negative paths",
  "app": "dev.mobium.mobiumapp",
  "beforeEach": [
    {"name": "app_tap", "arguments": {"target": "label=Login Demo"}}
  ],
  "tests": [
    {
      "name": "wrong password",
      "steps": [
        {"name": "app_fill", "arguments": {"target": "testid=username", "text": "mobium"}},
        {"name": "app_fill", "arguments": {"target": "testid=password", "text": "wrongpass1"}},
        {"name": "app_tap", "arguments": {"target": "testid=loginBtn"}},
        {"name": "app_wait_for", "arguments": {"target": "testid=loginError",
          "condition": "text", "text": "Incorrect username or password."}}
      ]
    }
  ]
}
```

Each test starts from a fresh app: `app` is terminated and launched before
`beforeEach`, and the session's end puts back anything a test changed —
network, accessibility, as sessions already do. Tests in a file run in order
on one device; files are what workers share out.

A config, `mobium.config.json`, found by walking up from the working
directory:

```json
{
  "testDir": "tests",
  "timeout": 60000,
  "retries": 0,
  "projects": [
    {"name": "android", "device": "emulator-5554"},
    {"name": "ios", "device": "457C7DC2-C706-45D9-8D68-1D26953E28B1", "driver": "wda"}
  ]
}
```

A project is a device — Playwright's browsers, mapped onto what Mobium drives.
A device serial in a checked-in file is fine for an emulator and a simulator;
a real phone's identifier is personal and belongs in a local override or the
command line, never the repository (the UDID scrub of 2026-09-23 is why).

## The commands, against Playwright's

| Playwright | `mobium` | Iteration |
| --- | --- | --- |
| `test` | `mobium test` — every `*.test.json` under `testDir` | 1 |
| `test <file>` | `mobium test tests/login.test.json` | 1 |
| `test -g "login"` | `mobium test -g login` — tests whose name matches | 1 |
| `test --last-failed` | `mobium test --last-failed`, from `.mobium-test/last-run.json` | 1 |
| `test --project=chromium` | `mobium test --project=android` | 1 |
| `test --workers=4` | `--workers`: one per device, projects in parallel; a device holds one session, so never two workers on one | 1 |
| `test --retries=2` | `--retries=2`, and a pass after a retry is reported **flaky**, not passed | 1 |
| `test --timeout=30000` | `--timeout=30000`, per test | 1 |
| `test --reporter=html` | `--reporter=list` (default), `json`, `junit`, `html` | 1 |
| `show-report` | `mobium show-report` — opens the last HTML report | 1 |
| `--version`, `test --help` | exist already | — |
| `test --headed` | a simulator is headless until `open -a Simulator`; an emulator decides at launch | later |
| `test --debug` | `--debug`: stop before each step, show it and the map; Enter steps, `c` continues, `q` quits | 2 |
| `test --ui` | an interactive mode | later |
| `test --trace on` | `--trace on` or `retain-on-failure`: each step's screenshot and map, a filmstrip in the HTML report | 2 |
| `show-trace <file>` | the trace is in the HTML report, so `show-report` opens it | 2 |
| `codegen <url>` | record the tool calls a person or agent makes as a test file — the daemon already sees every call | later |
| `install`, `install --with-deps` | `mobium doctor` checks; the device-side agents install themselves, pinned and checksummed | mostly exists |

What a failure leaves: the step that failed, its error code and message, and
a screenshot and a `map` taken at the moment of failure, in the report. `map`
is already redacted; a **screenshot of a real phone can show a person's
data**, so on a phone the report says it holds one, and a flag turns them
off.

Exit status: 0 when every test passed (flaky counts as passed, and is
listed), 1 when any failed, and the existing codes when the run could not
start — no device is `no_device`, a bad file `invalid_argument` — so CI can
tell "the app is broken" from "the test run is".

## How iteration 1 will be shown to work

By the rule the rest of this project follows — a result is not a result until
it has been seen to come back the other way:

- A `tests/` directory of MobiumApp flows, ported from `docs/checks/`
  (login, OTP, wait states), run on the emulator and the simulator, and then
  the phones.
- **A test that must fail**, kept in the suite behind a flag, to show a
  failure is reported with its step, code, screenshot and map, and exits 1.
- **A test that fails once and then passes**, to show `--retries` reports it
  flaky rather than passed.
- The JUnit file read by a consumer that is not ours (a JUnit schema check
  in CI), because a report nobody can parse is not an artifact.
- `--workers 2` across two devices, timed against one, and each report line
  naming the device it ran on.
- `--last-failed` after a run with one failure runs exactly that one.

## What it does not decide

- A shorthand for steps (`{"tap": "label=Login Demo"}`). Worth doing once the
  long form has been written by hand enough to know which steps are common.
- Test-level parameters and data (the same test on several inputs).
- Sharding across machines — the grid already lends devices; how a run
  splits across it is iteration 2 at the earliest.

## What iteration 1 showed

Built as planned: `internal/testrun`, `mobium test` and `mobium show-report`,
the three `app_wait_for` arguments, and `readOnlyHint` on every tool with
`IsReadCall` per call. The suite is `tests/`, and `docs/checks/test-runner.sh`
holds the runner to its controls. On the Pixel 7 AVD and the iPhone 17 Pro
simulator together, the check passed its first run: an unset device refused
before any test; 14 tests passing on both platforms in 95 seconds for 189
seconds of work; all six must-fail tests failing with their step, code and a
screenshot, and Python's XML parser agreeing with the JUnit counts;
`--last-failed` running those six and no others; and the flaky control failed
without a retry and reported flaky, on attempt 2, with one.

Three things the first real runs changed:

- **A project's device is settled before any test runs.** With a phone and a
  simulator both attached, "the iOS device" was ambiguous, and the first run
  reported that seven times, once per test, as seven failures. It is now one
  refusal before anything runs — a run that cannot start is not a test that
  failed.
- **A project's device can come from the environment**, as
  `"${MOBIUM_IOS_DEVICE}"`, and an unset variable is refused by name. A
  simulator's id is one Mac's and a phone's is somebody's; neither belongs in
  a checked-in config.
- **A test that presses enter behaves differently by platform.** Enter in
  MobiumApp's password field submits the form on iOS and not on Android, so
  a test that pressed it and then tapped Log In signed in twice on iOS and
  found no Log In to tap. The map kept with the failure showed `Log Out`,
  which is what decided it; the test now taps Log In and nothing else. That
  is the report's evidence doing its job on its first day.

Next, for iteration 2: the step shorthand, soft assertions, a trace per test
(the session-recording item), `--debug`, and runs on the real phones, where a
password needs the phone's keyboard to have its letters (CHALLENGES 159).

**Through a grid, 2026-09-28.** A grid route is one process's — the lease's
holder, the device, the forward to its node — so with `MOBIUM_GRID` set the
runner gives each project a `mobium pipe` of its own, as every client has
one, and its first call, a session start carrying the project's `platform`,
leases the device. Closing it releases the lease. The runner's own process
makes no call, which would lease a device nothing used. Every result names
the device its project got. `docs/checks/test-grid.sh` passed with two
emulators on one node: two projects on two devices at once, both leases gone
after, and a third project refused with exit 3 and nothing left held.

**A daemon and a session per project, 2026-09-28.** Off a grid too, each
project now gets its own `mobium pipe` and a daemon named for the run. A
test that went offline had left the emulator shaped after the run, because
the run never ended the CLI's shared session (CHALLENGES 163); now closing a
project's connection ends its session, which puts back what the tests
changed, and the daemon is stopped. Projects no longer queue behind one
daemon: the suite on two emulators took 40 seconds for 75 seconds of work.

## What iteration 2 showed

Built on 2026-09-29, and held to its controls in `docs/checks/test-runner.sh`
on the Pixel 7 AVD and the iPhone 17 Pro simulator:

- **The step shorthand.** `{"tap": "label=Login Demo"}` is a tool's name
  without `app_`, and a string goes to the argument the tool's schema makes
  its main one — `target` where there is one, else its one required string,
  else its only argument — or an object of its arguments. It is only a
  spelling: read into the long form, and checked as the long form is, so a
  misspelled key is still refused, and a tool with no one main argument says
  to name them (`{"swipe": "up"}` is refused; `{"swipe": {"direction":
  "up"}}` is not). Written by hand enough to know which steps are common, as
  "What it does not decide" asked: `tests/mobiumapp/form.test.json` is in it,
  and `login.test.json` keeps the long form, so the suite runs both.
- **Soft assertions**, as "Assertions" planned: `"soft": true` on a
  `wait_for` or an expect runs that step alone, outside the batch around it,
  and records its failure — with its own screenshot and map — and the test
  goes on. The test still fails, and every failure is in the result, the
  list line, JUnit and the report. Only an assertion can be soft: an action
  that failed leaves nothing after it worth checking. The control,
  `tests/controls/soft.test.json`, fails with both of its soft failures and
  reaches its last step, which is how it shows the test carried on.
- **A trace per test**, Playwright's names: `--trace on` or
  `retain-on-failure`, or `trace` in the config. Steps run one at a time
  rather than as a batch, and after each the runner keeps a screenshot and
  the map; the HTML report shows them as a filmstrip. A traced run is slower
  by a screenshot and a map a step, which is why it is off by default.
  `--no-screenshots` keeps the maps and no picture, for a phone. Since
  2026-10-01 each kept attempt is also a Playwright trace, `trace.zip` in
  its trace directory, as `mobium trace` writes one, linked from the report
  and named in `trace_file`: the runner starts a session trace before the
  app is launched and stops it after the last step, and sends its own
  screenshots and maps with `_meta` `dev.mobium/untraced`, which keeps them
  out of it.
- **`--debug`** stops before every step, prints it in the long form and the
  map, and waits: Enter runs the step, `m` maps again, `c` runs the rest of
  the test, `q` quits — the test is reported `stopped`, and nothing after it
  runs. The test's timeout is off while a person reads. One project only,
  since two would stop at once. With nobody answering — input closed — it
  runs on rather than hang.

Left for later: the phones, an interactive mode, recording a test from what a
person does (`codegen`), and test parameters.
