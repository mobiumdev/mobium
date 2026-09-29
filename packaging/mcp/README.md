# MCP packaging

What puts Mobium's MCP server — `mobium mcp` — into the
[MCP Registry](https://registry.modelcontextprotocol.io) and into clients
that install MCP bundles, such as Claude Desktop.

| File | What it is |
| --- | --- |
| [manifest.json](manifest.json) | The MCPB manifest, a template: the version and the tool list are filled in at build time |
| [launcher.sh](launcher.sh) | The bundle's entry point, `server/mobium`: a bundle names one command per operating system and none per architecture, so this picks the build for the machine and runs it |
| [server.json](server.json) | The registry entry, a template: the version and the bundle's SHA-256 are filled in at build time |
| [build.py](build.py) | Builds both from the release archives: `make dist && make mcpb` |

`make mcpb` writes `dist/mobium-<version>.mcpb` — a zip of the macOS and
Linux builds for both architectures, the launcher, the manifest and the
icon — and `dist/server.json`, and adds the bundle to `SHA256SUMS`. The
manifest's tools are read from the release's own binary, asked `tools/list`
over MCP, so they are exactly what ships. The same inputs build the same
bundle, byte for byte.

Windows is left out of the bundle while Windows is unsupported; it joins
when [WINDOWS.md](../../docs/WINDOWS.md) says it passes.

The [release workflow](../../.github/workflows/release.yml) builds both on a
tag, and on any pull request that touches this directory, and checks them
with the tools that will read them: the manifest with the MCPB CLI, the
registry entry with `mcp-publisher validate` against the live registry, and
the bundle by running it, through its launcher, as an MCP server. Publishing
the entry is a person's step: [RELEASE-CHECKLIST.md](../../docs/RELEASE-CHECKLIST.md),
"The MCP Registry".
