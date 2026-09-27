# Developing Mobium

For contributors and collaborators: from a fresh clone to a change that passes
CI. If you only want to use Mobium — install it and drive a device — you want
the [quick start](quickstart/README.md) instead.

Every command here was run from a fresh clone of `main`: `make build`,
`make lint-install`, `make ci` and the five-client device check all passed.

## Contents

- [1. Toolchains](#1-toolchains)
- [2. Clone and build](#2-clone-and-build)
- [3. Run what CI runs](#3-run-what-ci-runs)
- [4. Drive a device](#4-drive-a-device)
- [5. Work on a client](#5-work-on-a-client)
- [6. Regenerate what is generated](#6-regenerate-what-is-generated)
- [7. Send a pull request](#7-send-a-pull-request)
- [Before you write a test](#before-you-write-a-test)

## 1. Toolchains

| For | You need | CI uses |
| --- | --- | --- |
| the binary and the Go client | Go 1.24 or later | the version in `go.mod` |
| linting | golangci-lint 2.13.2 — `make lint-install` fetches it and checks its checksum | the same |
| the JavaScript client | Node.js 18 or later | 22 |
| the Python client | Python 3.9 or later | the runner's |
| the Java client | a JDK, 17 or later; the Maven wrapper fetches Maven | Temurin 17 |
| the .NET client | the .NET 10 SDK (its tests target `net10.0`) | 10.0.x |
| Android devices | the Android SDK: platform-tools, and the emulator | — |
| iOS devices | Xcode, on macOS | — |

`make java` and `make dotnet` skip with a note when their toolchain is
missing, so you can work on the Go without either. CI has all of them, and a
change that touches a client is only checked once its toolchain is installed.

## 2. Clone and build

```sh
git clone https://github.com/mobiumdev/mobium.git
cd mobium
make build          # -> bin/mobium
bin/mobium --version
make lint-install   # once; installs golangci-lint into $(go env GOPATH)/bin
```

## 3. Run what CI runs

```sh
make ci
```

It is exactly what the CI workflow runs, so a green local run means a green
build. On a fresh clone it printed, among its other lines:

```
gofmt clean
clients parse
javascript errors: 14 codes mapped, all checks passed
javascript connection: 23 checks passed
javascript types: 20 exports and 71 Device methods declared
python errors: 14 codes mapped, all checks passed
python connection: 26 checks passed
143 checks, 0 failed
license copies match
docs: spelling, anchors and quick-start pages clean
164 checks, 0 failed
```

The `143` is the Java client's suite and the second `164` the .NET client's.
It also covers `go vet`, the linter, both Go modules' tests, cross-compilation
for six targets, and the checks that the generated docs are current.

`make test` alone is faster and skips most of that. `clients/go` is its own
module, so `go test ./...` from the root does not reach it — use `make test`.

**After rebuilding, run `bin/mobium daemon stop`.** A daemon already running
keeps serving the old binary, and you will debug code you are not running.

## 4. Drive a device

The Go suite is hermetic — fake servers and captured fixtures, no device.
Behavior on a device is checked by the scripts in [checks/](checks/), each of
which drives a real emulator, simulator or phone and asserts on what happened
rather than on an exit code.

Start one device ([SETUP.md](SETUP.md) has every kind, and the traps), then
run the check for what you changed. The five clients' end-to-end flows are
one script:

```sh
sh docs/checks/clients.sh emulator-5554
```

```
    python passed
    javascript passed
    go passed
    java passed
    dotnet passed
```

[checks/README.md](checks/README.md) lists every script and what it proves;
[RELEASE-CHECKLIST.md](RELEASE-CHECKLIST.md) is the set to run before a
release. When you are done, stop cleanly — the daemon before the device:

```sh
sh docs/checks/clean-stop.sh --quit
```

## 5. Work on a client

Each client's tests start a fake `mobium` and need no device:

| Client | Its tests | Run a program against it from this clone |
| --- | --- | --- |
| Python | `make clients` | `pip install ./clients/python` in a virtualenv |
| JavaScript | `make clients` | `npm install ./clients/javascript` |
| Go | `make test` | `go mod edit -replace=github.com/mobiumdev/mobium/clients/go=<path to this clone>/clients/go` in your module |
| Java | `make java` | `cd clients/java && ./mvnw install`, then depend on `dev.mobium:mobium:0.1.0-SNAPSHOT` |
| .NET | `make dotnet` | `dotnet pack clients/dotnet/Mobium -o ./nupkgs`, then `dotnet add package Mobium --source ./nupkgs` |

The examples in [quickstart/examples/](quickstart/examples/) are a ready-made
program for each, and every client's end-to-end flow is in
[checks/clients.sh](checks/clients.sh).

Adding a tool, a flag or an error code touches every client at once; the
build refuses a gap. [CONTRIBUTING.md](../CONTRIBUTING.md) has the procedure.

## 6. Regenerate what is generated

Some documents are built from the code, and `make ci` fails when one is
stale:

| When you change | Run | It writes |
| --- | --- | --- |
| a tool, or which surfaces reach it | `make api` | [API.md](API.md) |
| a tool's arguments or a command's flags | `make flags` | [FLAGS.md](FLAGS.md) |
| a quick-start example or its captured output | `make quickstart` | the [quick start](quickstart/README.md) pages |
| the root `LICENSE` | `for d in python javascript go; do cp LICENSE clients/$d/; done` | the clients' copies |

The checks compare against the git index, so `git add` the regenerated files
before running `make ci`.

## 7. Send a pull request

`main` is protected. A change reaches it through a pull request that has been
reviewed and whose **Build, vet and test** check passes; a second job runs the
.NET client's tests on Windows. History stays linear: pull requests are
squash-merged, merge commits are off, force-pushes to `main` are refused, and a
merged branch is deleted.

1. Branch from an up-to-date `main`.
2. Make the change, with tests, and run `make ci`.
3. Run the device check for what you touched, and say in the pull request
   which device and which result.
4. Open the pull request against `main`.

## Before you write a test

Read [CHALLENGES.md](CHALLENGES.md). Most of the defects in it were found
only by running against a real device, and several of the rest by a test that
could not fail. A fixture cannot tell you what a platform does, and a
measurement that comes back "zero" is not a result until it has been shown it
can come back non-zero.

The design rule behind all of it is in [PHILOSOPHY.md](PHILOSOPHY.md).
