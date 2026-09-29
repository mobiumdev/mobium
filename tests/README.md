# Tests

Mobium's own tests of MobiumApp, run by `mobium test` — and the suite that
holds the runner to its word. The format and the reasons are in
[decisions/0006](../docs/decisions/0006-a-test-runner.md).

```sh
cd tests
mobium test --project android                       # emulator-5554
MOBIUM_IOS_DEVICE=<simulator-udid> mobium test      # and an iOS simulator
mobium test --reporter list,junit,html && mobium show-report
```

- `mobiumapp/` is the suite: the Login and Form Demos — the Form in the
  step shorthand, the Login in the long form — each test from a
  freshly launched app.
- `controls/` are for the runner, not the app, and are not run by default:
  `must-fail.test.json` must fail, every test of it, and `flaky.test.json`
  must pass only on a retry, from cleared app data, and `soft.test.json`
  must fail with two soft failures and still reach its last step.
  `docs/checks/test-runner.sh` runs all three and says whether they did.
- `mobium.config.json` names the projects. The iOS one takes its device from
  `MOBIUM_IOS_DEVICE`: a simulator's id is one Mac's, and a phone's is
  somebody's, so neither is written here.
