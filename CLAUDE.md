# CLAUDE.md

Mobile app automation for AI agents and humans: native apps on Android
emulators and phones, iOS simulators and iPhones, WebViews included — one Go
binary, driven from a CLI, an MCP server, or five language clients.

## Key Docs

- CONTRIBUTING.md — the rules, and why each exists. Read it before changing code
- docs/DEVELOPMENT.md — from a fresh clone to a passing `make ci`, and driving a device
- docs/ARCHITECTURE.md — every layer, and one call traced end to end
- docs/API.md and docs/FLAGS.md — every tool and argument, generated
- docs/CHALLENGES.md — every defect and what it taught. Read before asserting platform behavior
- docs/guides/ — how to use each surface; examples/drivers/PROTOCOL.md — the driver protocol
- docs/ROADMAP.md — what is next

## Tech Stack

- Go: the `mobium` binary, no runtime dependencies
- The UiAutomator2 server (Android) and WebDriverAgent (iOS), driven directly over HTTP — no Node
- CDP (Android WebViews) and WebKit's Remote Web Inspector (iOS WebViews)
- MCP server on stdio (`mobium mcp`)
- Clients: Go, Python, JavaScript, Java, .NET — each spawns `mobium pipe`

## Design Philosophy

*Mutatis mutandis*: the existing thing with the necessary changes, never a
parallel implementation. And the device is the authority, not the reasoning
about it:

- **Measure, don't reason.** Most of the defects in docs/CHALLENGES.md were
  found only by running on a device. A test built from an assumption passes
  with the assumption
- **A zero is not a result until it has been seen to come back non-zero.**
  Give every check a positive control
- **Refuse rather than approximate**, and **report only what was checked**
- **A remedy in an error must be one that works** — it will be followed

## Rules

- Run `make ci` before committing. After adding a tool, a flag or a schema property, run `make api flags` and add it everywhere — CLI, MCP schema, all five clients (CONTRIBUTING.md, "Adding a tool, a flag or an error")
- Every error has a code: `mobiumerr.New(mobiumerr.<Code>, ...)`, never a bare `fmt.Errorf`
- The CLI implements no behavior; it calls a tool. Behavior lives in `internal/agent`
- After rebuilding, `mobium daemon stop` — a running daemon serves the old binary. Stop the daemon before shutting a device down, never after (docs/SHUTDOWN.md)
- Clients spawn `mobium pipe`, never `mobium mcp`
- A defect gets an entry in docs/CHALLENGES.md — how it was found, what it broke, how it is handled — and the counts at its top and in docs/RELEASE-CHECKLIST.md move with it
- Claims about a device are checked on a device: a `docs/checks/` script, or a measurement you can show. A guide's commands and output are ones that actually ran
- **A real phone is somebody's.** Never print its private data — location, notifications, network names, the device's name — assert whether, not what. Read a check for device-wide changes before running it on a phone, and put back what it changes
- American spelling everywhere; `make ci` enforces it
