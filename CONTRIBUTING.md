# Contributing

Thanks for looking. Mobium is small enough that the rules below are most of
what there is to know; [docs/CHALLENGES.md](docs/CHALLENGES.md) is why each of
them exists.

## The design rule

***Mutatis mutandis*** — the same argument carried into a new domain, changing
only what must change. When something needs to work on a second platform,
through a second front door or via a third-party backend, the answer is the
existing thing with the necessary changes, not a parallel implementation. A
change that duplicates a layer is almost always the wrong one.
[docs/PHILOSOPHY.md](docs/PHILOSOPHY.md) has where the motto comes from — Poul
Anderson's "The Three-Cornered Wheel" — and the rule applied across the codebase.

- `internal/mobiumdriver` is the only platform-specific layer. Everything above
  it is written once. A third party adds a backend as a **driver process**
  ([the driver protocol](examples/drivers/PROTOCOL.md)),
  not by importing this package.
- `internal/device` finds, launches and readies the thing being automated.
- The CLI parses flags and calls a tool by name. Behavior lives in
  `internal/agent`, so the MCP surface and every client get it too.

## Building and testing

[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) is the step-by-step version: the
toolchains, a fresh clone to a passing `make ci`, driving a device, working on
a client, and sending a pull request. In short:

```sh
make build   # -> bin/mobium
make ci      # what CI runs: gofmt, vet, lint, tests, client checks,
             # cross-compilation, and the generated-doc and spelling checks
```

Run `make ci` before sending a change; a green local run means a green build.
`make test` alone skips formatting, the client checks and cross-compilation.
`clients/go` is its own module, so `go test ./...` from the root does not reach
it — use `make test`.

The Go suite is hermetic: fake HTTP servers and captured fixtures, no device
needed. Behavior on a device is checked by the scripts in
[docs/checks/](docs/checks/) and the list in
[docs/RELEASE-CHECKLIST.md](docs/RELEASE-CHECKLIST.md).

After rebuilding, run `mobium daemon stop`: a running daemon keeps serving the
old binary.

## Adding a tool, a flag or an error

- **A tool** goes everywhere at once: the CLI, the MCP schema and all five
  clients. `internal/apisurface` fails the build otherwise; a deliberate gap is
  declared there with a reason. Run `make api` to regenerate
  [docs/API.md](docs/API.md).
- **A flag or schema property** is the same rule one level down: every argument
  a tool declares must be settable from the CLI, and every key a command sends
  must be one the tool declares. Run `make flags` to regenerate
  [docs/FLAGS.md](docs/FLAGS.md).
- **Every error has a code.** Create one with `mobiumerr.New(mobiumerr.<Code>,
  ...)`, never a bare `fmt.Errorf` or `errors.New`. The codes are public API,
  the same in every client and in the CLI's exit status
  ([the codes](docs/guides/cli.md#6-when-a-command-fails)).
- A tool that fails returns `isError` with an explanation, never a JSON-RPC
  error. An error that suggests a remedy must suggest one that can work.

## Rules the platforms taught

- **Verify by outcome, never by exit code.** adb, `pm`, `uiautomator dump`,
  `simctl` and `devicectl` all report some failures while exiting 0. Where the
  platform can report state, read it back instead of believing the command.
- **A command that starts something is not finished when it returns.** `am
  start` and `simctl launch` report dispatch, not foreground; wait for the
  outcome.
- **Refuse rather than approximate.** A backend that cannot do something
  correctly says so and names the fix. Anything a platform genuinely lacks —
  iOS has no back button — is named, not imitated.
- **Every locator `map` hands out resolves to exactly one node**, and locators
  are re-resolved before acting. Never replay coordinates from an earlier map.
- **A fixture can prove logic self-consistent; it cannot establish platform
  semantics.** Anything of the form "elements of type X behave like Y" needs a
  device. A measurement that comes back zero is not a result until it has been
  shown it can come back non-zero.
- **Never print a password field's contents.** Every path that surfaces a
  node's text goes through `uitree.Redact`.
- **Leave nothing on the device.** Anything written to `/data/local/tmp` is
  deleted after use.
- **Three units, not interchangeable.** Pixels are what `map` bounds,
  screenshots and taps use on both platforms. Android's dp is `px*160/dpi`;
  iOS's point is `px/scale`.
- **Anything passed to `adb shell` is shell input on the device.** Quote it
  with `shellQuote`.

## Conventions

- **No runtime dependencies.** Mobium is one static binary. A feature that
  needs Node or Python on the user's machine is the wrong design. Device-side
  agents are pinned by version and verified by checksum.
- **American spelling** in code, comments and docs.
  `docs/checks/american-spelling.py scan` checks it, and `make ci` runs it.
- **Never name a Go file `*_ios.go` or `*_android.go`**: Go reads the suffix as
  a build constraint and silently drops the file from other builds.
- A linter that is switched off is documented in `.golangci.yml` with the
  reason, never silenced with a bare `//nolint`.
- Unix socket paths are capped at about 104 bytes by the OS; keep
  `MOBIUM_HOME` and session names short.

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE).
