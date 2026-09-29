# Quick start: another machine's devices, and a grid

Two steps, the second built on the first:

- **`--remote`** drives the devices plugged into one other machine, over
  SSH — with any command and any client, and no change to its code.
- **`MOBIUM_GRID`** spreads runs over several machines' devices, as
  Selenium Grid does, without a hub: each run gets the first free device that
  matches, and waits when there is none.

Every command and every line of output below is what mobium printed on
2026-09-28, with a Mac standing in for a node over SSH to itself, two Android
emulators and an iOS simulator on it. The node's device list is trimmed to
what the guide needs, and says where. The full reference is
[SETUP.md](../SETUP.md#driving-another-machines-devices).

## Contents

- [What a node needs](#what-a-node-needs)
- [1. One node: `--remote`](#1-one-node---remote)
- [2. A grid](#2-a-grid)
- [3. Runs that share it](#3-runs-that-share-it)
- [Phones on a grid](#phones-on-a-grid)
- [What stays local](#what-stays-local)

## What a node needs

A node is any Mac or Linux machine with devices and mobium on it.

| | |
| --- | --- |
| **SSH without a prompt** | key-based login from your machine to the node. mobium runs SSH with `BatchMode=yes`, since a password prompt would land in the stream a client speaks. `MOBIUM_SSH` replaces the `ssh` command, options and all — `ssh -i ~/.ssh/node_key` |
| **mobium on the node** | reachable by the node's non-interactive shell. `MOBIUM_REMOTE_BIN` is what that shell runs as mobium, and may set its environment when adb or Xcode's tools are not on its default `PATH` |
| **macOS or Linux here** | the forward ends in a Unix socket, so `--remote` refuses on Windows, and says so |

Nothing new listens on a network: SSH authenticates and encrypts, and the
node's daemon socket stays owner-only on the node. On a Mac, a node needs
Remote Login on (System Settings > General > Sharing).

For the runs below, the node was this Mac, reached with a key of its own and
with the Android tools put on the node's `PATH`:

```sh
export MOBIUM_SSH="ssh -i $HOME/.ssh/node_key"
export MOBIUM_REMOTE_BIN="PATH=/opt/homebrew/share/android-commandlinetools/platform-tools:/usr/bin:/bin:/usr/sbin:/sbin $HOME/bin/mobium"
```

## 1. One node: `--remote`

`--remote <node>`, or `MOBIUM_REMOTE=<node>`, sends every call to the node's
daemon, starting it there if it is not running. The node's devices:

```
$ mobium --remote localhost devices
emulator-5554                          device     (android emulator, model: sdk_gphone64_arm64)
emulator-5556                          device     (android emulator, model: sdk_gphone64_arm64)
457C7DC2-C706-45D9-8D68-1D26953E28B1   booted     (ios simulator, iPhone 17 Pro, iOS 26.5)
```

(Trimmed: the node also listed its shut-down simulators and a phone.) Every
command works the same way through it — `mobium --remote localhost map`,
`tap`, `screenshot -o shot.png`, which saves the picture here, not on the
node — and so does every client, by the environment variable alone:

```sh
MOBIUM_REMOTE=localhost python3 my_test.py
```

Files travel as content: an app to install and a GPX route go to the node,
and a screenshot or a recording comes back and lands where you asked.

## 2. A grid

`MOBIUM_GRID=node1,node2` names the nodes. At a run's first call, mobium asks
each node for its devices and which are held, takes the first free one that
matches what the run asked for, and connects to its node as `--remote` would.
`mobium grid status` shows the same answers routing reads:

```
$ MOBIUM_GRID=localhost mobium grid status
NODE       DEVICE                                PLATFORM  OS          MODEL                  STATE      HELD BY
localhost  emulator-5554                         android   Android 15  sdk_gphone64_arm64     device     free
localhost  emulator-5556                         android   Android 17  sdk_gphone64_arm64     device     free
localhost  457C7DC2-C706-45D9-8D68-1D26953E28B1  ios       iOS 26.5    iPhone 17 Pro          booted     free
localhost  <phone-udid>                          ios       iOS 26.6.2  iPhone 15 Plus         connected  not offered (a phone; MOBIUM_GRID_PHONES=1 on the node lends it)
```

(Trimmed of shut-down simulators, and the phone's id replaced.) `mobium grid
ui` serves the same as a page on `127.0.0.1`, refreshed every few seconds.

Narrow what a run will take with `MOBIUM_GRID_MODEL` — part of the model, in
any case — and `MOBIUM_GRID_OS` — `17` is Android 17, `iOS 26` an iOS 26
runtime. With nothing free that matches, a run waits, asking again every two
seconds, up to `MOBIUM_GRID_WAIT` (default `60s`), and then says what was
busy. A node that does not answer within five seconds is left out, and
routing goes on without it.

## 3. Runs that share it

A run keeps its device as long as it lives, so the way to hold one is a
client — each CLI command is a run of its own. This Python script,
[examples/grid_run.py](examples/grid_run.py), asks for an Android device,
holds it eight seconds, and says which it got:

```python
# Three of these at once, with two Android emulators on the grid: each asks
# for an Android device, holds it for a few seconds, and says which it got.
import sys, time
from mobium import start

name = sys.argv[1]
t0 = time.time()
with start(platform="android", app="com.android.settings") as device:
    got = time.time() - t0
    serial = device.sessions()[0]["device"]
    print(f"{name}: got {serial} after {got:.1f}s", flush=True)
    rows = len(device.map())
    time.sleep(8)
    print(f"{name}: mapped {rows} elements, releasing after {time.time() - t0:.1f}s", flush=True)
```

Three of them at once, with two emulators on the grid:

```sh
export MOBIUM_GRID=localhost
python3 grid_run.py run-a & python3 grid_run.py run-b & python3 grid_run.py run-c &
```

```
run-a: got emulator-5554 after 3.0s
run-b: got emulator-5556 after 3.6s
run-a: mapped 11 elements, releasing after 11.5s
run-b: mapped 12 elements, releasing after 11.7s
run-c: got emulator-5554 after 16.2s
run-c: mapped 11 elements, releasing after 24.8s
```

And `grid status` while the first two held their devices:

```
$ mobium grid status
NODE       DEVICE                                PLATFORM  OS          MODEL                   STATE      HELD BY
localhost  emulator-5554                         android   Android 15  sdk_gphone64_arm64      device     g0131f753 for 7s
localhost  emulator-5556                         android   Android 17  sdk_gphone64_arm64      device     gbfafeb50 for 7s
localhost  457C7DC2-C706-45D9-8D68-1D26953E28B1  ios       iOS 26.5    iPhone 17 Pro           booted     free

waiting:
  g44f1f659 wants an android device, waiting 7s (seen by localhost)
```

(Trimmed as above.) Two runs got the two emulators; the third waited, and got
one the moment it was released. Afterwards every device read `free` again,
with nothing left behind.

**The lease is the node's, and the node enforces it.** Each run gets a daemon
of its own on its node, and every daemon there refuses a device leased to
another — a plain `mobium --device emulator-5554` on the node, going around
the grid, is told the device belongs to a grid run until that run ends. A
lease is renewed every 20 seconds while its run lives, and free again 60
seconds after a run that died without releasing it.

## Phones on a grid

A phone plugged into a node is usually somebody's, and a grid run would
install on it, tap and change its settings. So the grid lends a physical
device only when **the node's own** environment sets `MOBIUM_GRID_PHONES=1`
— the node's choice, never the caller's. Until then a phone is listed as
not offered, as above, and a run that asks for it by serial is told so.
What a grid shows about a phone is its model, never its name, which is its
owner's ([CHALLENGES](../CHALLENGES.md) 161).

## What stays local

`daemon`, `doctor` and `mcp` are about this machine and ignore `--remote`.
One device still belongs to one run at a time, on a node as anywhere.
`mobium test` does not route through a grid yet — its projects name their
devices — which is on the runner's list for iteration 2
([decisions/0006](../decisions/0006-a-test-runner.md)).
