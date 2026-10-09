# Security policy

## Reporting a vulnerability

Report it privately through GitHub: on this repository's **Security** tab,
choose **Report a vulnerability**. The report reaches the maintainer only, and
the fix can be worked out in a private fork before anything is public.

Please do not open a public issue, pull request or discussion for a
vulnerability.

A useful report says what Mobium was asked to do, on which device and
platform, what happened, and what should have happened. A command line and its
output usually say all of that.

## What is in scope

Mobium acts on devices for an agent or a script, so the questions that matter
most are the ones [docs/THREAT-MODEL.md](docs/THREAT-MODEL.md) maps:

- text from an app or a web page that Mobium passes to an agent as anything
  but data,
- a password, or another value an app marks secret, appearing in output,
- something written to a device and left there,
- `--remote` or the grid reaching a machine or a device it was not pointed at,
- a downloaded device-side component used without its pinned checksum.

Vulnerabilities in the device-side servers Mobium installs, the UiAutomator2
server and WebDriverAgent, belong to their own projects; a report here is
still welcome when Mobium's use of them makes it worse.

## Supported versions

Mobium has no tagged release yet. Fixes land on `main`, and from the first
release on, in the latest release.
