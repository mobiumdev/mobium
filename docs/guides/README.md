# Guides

Past [the quick start](../quickstart/README.md): one task each, walked on a
real device, with every command and line of output what mobium printed.

| Guide | For |
| --- | --- |
| [The command line](cli.md) | how the commands fit together: sessions, the map-act-map loop, locators and refs, `--json`, exit statuses, `batch`, and the daemon |
| [MCP](mcp.md) | Mobium as an agent's tools: connecting a client, an agent driving a device, what a client sees, answers, failures and sessions |
| [Auto-wait](autowait.md) | what an action waits for before it touches anything, what it refuses and why, and waiting on purpose with `mobium wait` |
| [Test runner](test-runner.md) | `mobium test`: a config, a test file, a failure with its evidence, retries and flaky, several devices, CI — and the step shorthand, soft assertions, a trace and a debugger, one test over several cases with `each`, and a page to run tests from with `--ui` |
| [Inspector](inspector.md) | `mobium inspect`: the screen with every element outlined, the locator for any of them, trying a locator, and recording what you do as a test |
| [Network conditions](network.md) | taking an Android device offline or slowing it down, reading back what it really has, and doing it inside a test |
| [Another machine's devices, and a grid](grid.md) | `--remote` to one node over SSH, and `MOBIUM_GRID` to share several nodes' devices between runs |
