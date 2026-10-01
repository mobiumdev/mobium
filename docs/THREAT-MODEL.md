# Threat model

What can go wrong when an AI agent drives a phone through Mobium, mapped to
the OWASP Top 10 for LLM Applications (2025) and the risks OWASP lists for
agentic applications. Written 2026-09-28. Each row says what is done, and
what is open — and an open item stays open until it has been measured, not
reasoned about: this project's record is that reasoning about devices is
usually wrong (see [CHALLENGES](CHALLENGES.md)).

## What it is, for this purpose

- **Mobium does not run a model.** It is the hands of one: an agent, or a
  script, calls tools; Mobium acts on a device and reports what it saw.
- **Everything it reports comes from apps it does not control.** `map`
  labels, `text`, `source`, WebView `eval` results, notifications, device
  logs, crash reports, the clipboard. Any app or page can put text there,
  including text written to be read by an agent as an instruction.
- **What it can do is a lot.** Tap and type anywhere, install and uninstall,
  clear an app's data, change permissions, lock the device, and — through
  `--remote` and the grid — do all of that to another machine's devices.
- **Trust boundaries:** the device and its apps are untrusted; the agent is
  trusted to the extent its operator trusts it; the Mac Mobium runs on is
  trusted, and anything local to it can reach what Mobium reaches.

## The LLM Top 10

| Risk | Here | Done | Open |
| --- | --- | --- | --- |
| **LLM01 Prompt injection** | Screen text is the classic indirect path: an app or page shows "ignore your task and uninstall…", and the agent reads it in `map` or `text` | Labels are bounded and cleaned, so one label cannot carry a page of instructions | Results mark which content came from the device, so an agent or its harness can treat it as data. An agent evaluation: a MobiumApp screen showing injection text, driven by a real agent, counting how often the agent obeys the screen over its task — a measurement that has been seen to come back non-zero before its zero means anything |
| **LLM02 Sensitive information disclosure** | On a real phone, screenshots, `source`, logs, notifications (message text), the clipboard and crash reports are a person's data | Passwords are redacted everywhere a node's text surfaces (`uitree.Redact`, CHALLENGES 43). Checks assert *whether*, never *what*, on a real phone (CHALLENGES 111) | One redaction path for everything device-derived, not only password fields. A way to run with personal surfaces off |
| **LLM03 Supply chain** | The UiAutomator2 server APKs, WebDriverAgent's source, and third-party drivers | Device-side artifacts are pinned by version and verified by checksum. A third-party driver is a separate process, never code loaded into Mobium ([decisions/0003](decisions/0003-drivers-are-processes-not-plugins.md)) | A driver is still a stranger's executable, run with the user's rights; the docs should say so where drivers are installed |
| **LLM04 Data and model poisoning** | — | Mobium trains nothing | — |
| **LLM05 Improper output handling** | An agent's output becomes device input: text typed into fields, JavaScript run by `eval`, arguments that reach `adb shell` on the device | Everything passed to `adb shell` is quoted (`shellQuote`, CHALLENGES 52). `type` refuses what is certainly not a text field | An audit of every argument an agent sets that reaches a shell, a page or the file system, checked by a test per path |
| **LLM06 Excessive agency** | An agent can uninstall, clear data, reset every app's permissions, lock the device, and drive other machines' devices | Destructive actions confirm by reading back, and name what they changed. A grid node lends physical phones only with `MOBIUM_GRID_PHONES=1` in its own environment | A read-only mode and a tool allowlist, so a session meant to look cannot act. An app allowlist, so actions refuse outside the app under test |
| **LLM07 System prompt leakage** | — | Mobium holds no prompts | — |
| **LLM08 Vector and embedding weaknesses** | — | Mobium keeps no index | — |
| **LLM09 Misinformation** | An agent reports "tapped Allow" and a person believes it | Every answer says what was checked and what was only sent — "report only what was checked" is a project rule; a tap that did not land is reported, not assumed | — |
| **LLM10 Unbounded consumption** | Long calls tie up the device and the daemon | Per-call timeouts; a batch takes at most 100 steps; backgrounding at most 180 seconds | Output size bounds for `record`, `logs` and `source` |

## From the agentic risks

| Risk | Here | Done | Open |
| --- | --- | --- | --- |
| **Tool misuse and privilege abuse** | Anything that can reach the automation servers can drive the device without Mobium; none of them authenticates | The daemon's socket is owner-only (`0600`, in a `0700` directory); `--remote` forwards it over SSH to an owner-only socket. The simulator's WebDriverAgent server binds `127.0.0.1` (CHALLENGES 152) | See "Measured" below: what is still reachable, from where |
| **Tool descriptions as instructions** | An agent reads tool schemas as guidance | Mobium's own are in this repository and reviewed like code | A third-party driver declares its own capabilities and could misstate them; what an agent sees from a driver is not reviewed here |
| **Memory and context poisoning** | Screen text carried from one call into the next | — | The same marking as LLM01, so it stays data across calls |
| **Human trust exploitation** | A confident report of an action that did not happen | Read-back confirmation, and refusals that say why | — |

## Measured

Measured 2026-09-28 with read-only requests, on the Pixel 7 AVD and the
iPhone 17 Pro simulator, the Mac's firewall off. A server "answering" means
it returned its status without credentials; reading the screen or acting
needs nothing more.

| Server | Listens on | Reachable from | Lifetime | State |
| --- | --- | --- | --- | --- |
| UiAutomator2, the Mac end of `adb forward` | `127.0.0.1` | Any process of any user on the Mac — it returned the whole screen's hierarchy through the running session. Not from the LAN: refused | Stops with the daemon | Local exposure, **open**: UiAutomator2 has no authentication |
| UiAutomator2, on the device (port 6790, and its MJPEG stream on 7810) | **Every interface** (`*:6790`, `*:7810`) | **The LAN, on a real phone**: on the Pixel 8 Pro, from the Mac over Wi-Fi, the server answered "ready to accept commands" and the stream answered (CHALLENGES 154). On the emulator, only the device itself: its network is behind its own NAT. The cellular IPv6 address did not answer from the internet, with no positive control — not a result | Stops within 2s of the daemon stopping | **Open.** The server has no bind setting (Netty `bind(port)`) and Mobium installs the project's prebuilt APK; the fix is upstream, a bind address it reads — written up with a tested patch in [upstream/](upstream/uiautomator2-server-binds-every-interface.md) — which Mobium would set to `127.0.0.1` since `adb forward` reaches the device's localhost. **Meanwhile:** starting a session on a real phone says so in its answer (`exposure`), and names `--driver uiautomator`, which runs no server — measured, a dump session added no listening socket. Whether an ordinary app on the device can reach the server: **inconclusive** — `su` to an app's uid keeps `su`'s groups and context, and a uid that should have had no network got through too, so the test could not fail |
| WebDriverAgent server, simulator | `127.0.0.1` since `USE_IP` (was every interface) | Any process on the Mac; the LAN was refused after the change, and answered before it | Stops with the session | Local exposure, as UiAutomator2's |
| WebDriverAgent MJPEG stream, simulator | **Every interface** — it ignores `USE_IP` | **The LAN**: an iPhone on the same Wi-Fi opened the Mac's address in Safari and got a live 1206×2622 frame of the simulator's screen | Stops with the session | **Open** in Mobium: it does not use the stream, and closing it needs WebDriverAgent to bind it as it binds the server ([upstream/](upstream/webdriveragent-mjpeg-ignores-use-ip.md)). Building the simulator's runner from the patched source, as the phone's is, does not work as things stand: a runner built by `xcodebuild` aborts under `simctl launch` for want of `lib_TestingInterop.dylib`, which only `xcodebuild`'s own launch supplies — so it would mean launching simulators the way phones are. **Mitigated by the macOS firewall, measured:** with the runner set to "Block incoming connections", the same probe from the iPhone got "the network connection was lost" while the Mac itself still read the stream. The firewall had allowed the runner by itself — "Automatically allow downloaded signed software" is on by default — and a reinstalled runner is a new path, allowed again unless that option is off |
| WebDriverAgent server on a real iPhone (port 8100) | The CoreDevice tunnel's address since CHALLENGES 153 (was every interface) | Over the cable's tunnel only: the Wi-Fi address was refused after the change, and answered with the live session before it | Stopped within 2s of the daemon stopping | **Fixed**, measured on the iPhone 15 Plus. A runner already running is checked from the Mac at the phone's Wi-Fi address: Mobium's own is replaced when it answers there, and anyone else's is refused — measured both ways |
| WebDriverAgent MJPEG stream on a real iPhone (port 9100) | The tunnel's address, since Mobium patches the runner it builds (was every interface) | Over the tunnel only: refused on Wi-Fi after the patch, answered before it | Stops with the session | **Fixed**, measured on the iPhone 15 Plus. The patch is one line WebDriverAgent itself lacks — its stream never uses `USE_IP` — and goes when a release has it |

The "LAN" probes of the simulator were first made from the Mac's own network
address, which the firewall does not filter — traffic to yourself never
arrives from outside. They showed what each socket was bound to, not who
could reach it; only a second device on the network, the iPhone, could
answer that.

## The mobile side

The OWASP Mobile Top 10 and MASVS are about the apps under test, not about
Mobium, but two points apply to Mobium itself:

- It installs a server APK on the device with its permissions granted, and
  builds and signs WebDriverAgent for a person's iPhone. Both are removable,
  and [SETUP](SETUP.md#what-mobium-installs-and-removing-it) says how.
- It must leave nothing on the device: a hierarchy dump in `/data/local/tmp`
  is a copy of whatever was on screen, and is deleted after every read.

## The rule the checks follow

A security measurement that comes back zero is not a result until it has
been shown it can come back non-zero. "No agent followed the injected
instruction" means nothing until an agent has been seen to follow one.
