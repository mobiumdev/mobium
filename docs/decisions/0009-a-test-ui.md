# 0009 — A test UI: `mobium test --ui`

**2026-10-01. Iteration 1.** The interactive mode decision 0006 left for
later: a page on this machine that lists the
suite, runs any part of it on any of its projects, shows every step with the
screen after it as it happens, and runs again what failed.

## Why

`mobium test` answers once a run is over, in a terminal and a report. The loop
a person is in while writing a test is shorter than that: change a step, run
that one test, look at the screen it stopped on, change it again. `--debug`
serves one test at a time in a terminal; a page that runs any test and
shows its steps as they happen serves the whole loop.

## What was decided

**The runner, not a second one.** A run from the page is `testrun.Run` with
the options `mobium test` builds from the same flags and config — the same
projects, a `mobium pipe` and a daemon of its own per project, the same
retries and timeout — so a test that passes on the page passes in CI. What
the page adds is which tests: `Options.Only`, by the same IDs
`--last-failed` keeps, so "run this one" and "re-run what failed" are exact,
never a pattern.

**A client of the tools, as the inspector is** (decision 0007). Served on
127.0.0.1, every request refused unless its path carries a token only the
printed URL holds and its Host names the server: a page that taps a device
must not be reachable from any other page open in the same browser.

**Live, step by step.** The page runs every test traced, so after each step
the runner keeps a screenshot and the map; `Options.StepDone` hands each one
over as it is taken, beside `Progress`, which hands over each result. The
page polls for both. A screenshot is served from the run's output directory
and from nowhere else.

**The files are read again for every list and every run**, so an edited test
is the test that runs, without restarting anything — the part of
a watch mode that matters while writing one.

**When a run ends** its HTML and JSON reports are written as `--reporter
html,json` writes them, and `--last-failed` sees its failures.

## What it does not do, in iteration 1

- Stop a run midway. A test cannot be abandoned between two steps without
  leaving its device somewhere nobody chose; a run is a few tests at a time
  on this page, and ends.
- Pick a locator, or record. That is `mobium inspect`.
- Run on a timer when a file changes. Running is a click.
- `--debug` from the page.

## What iteration 1 showed

Built the same day: `internal/testui`, `mobium test --ui` (`--ui-port`,
`--open`), and `Options.Only` and `Options.StepDone` in the runner. Driven
in a browser by [checks/test-ui.sh](../checks/test-ui.sh) — the server's
tests cannot reach the page's own script — on an Android emulator and an
iPhone simulator at once:

- the suite's nine tests listed on both projects, the three cases of the
  parameterized Login test among them;
- ▶ on one test ran it on both; its first step was on the page while the
  run was still going, and all fourteen step screens loaded, under a policy
  that lets the page load images from itself and nothing else;
- with the page still open, the test file was changed on disk to expect a
  message the app does not show: the next run was the changed test, failed
  on both, and the failure quoted both messages;
- the file put back, "Re-run failed" ran exactly those two, and they passed,
  and the run's HTML and JSON reports were written.

The check's first draft looked for a step six seconds after the click and
found none: each run starts a daemon and a driver per project, as
`mobium test` does, and the first step waits for them. It now waits for a
step while the run is still going, which is the claim. That failed draft
also found a defect: killed mid-run, `mobium test --ui` left the run's
daemons running, and Ctrl-C did the same to plain `mobium test`
(CHALLENGES 199). Both now close whatever a run left open before exiting.
