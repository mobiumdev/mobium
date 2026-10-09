"""Builds the MCP bundle and the registry entry for a release.

    python3 packaging/mcp/build.py <version> [dist]

Reads the release archives `make dist` wrote, so the bundle carries exactly
the binaries being released, and writes into the same directory:

- mobium-<version>.mcpb: an MCPB bundle (a zip) holding the macOS and Linux
  builds for both architectures, the launcher that picks one, the manifest
  and the icon. Windows joins when Windows is supported.
- server.json: the MCP Registry entry, pointing at the bundle's release URL
  with its SHA-256, ready for `mcp-publisher publish`.

The manifest's tool list comes from the release's own binary — the build
for the machine this runs on, asked `tools/list` over MCP — so it names
exactly the tools that ship, described as they describe themselves.
"""

import hashlib
import json
import pathlib
import platform
import subprocess
import sys
import tarfile
import tempfile
import zipfile

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parent.parent
BUILDS = ["darwin_arm64", "darwin_amd64", "linux_amd64", "linux_arm64"]
# One fixed timestamp for every entry, so the same inputs give the same bundle.
STAMP = (2026, 1, 1, 0, 0, 0)


def binary(dist, version, build):
    archive = dist / f"mobium_{version}_{build}.tar.gz"
    with tarfile.open(archive) as t:
        return t.extractfile(t.getmember(f"mobium_{version}_{build}/mobium")).read()


def host_build():
    system = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system())
    machine = {"x86_64": "amd64", "AMD64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(platform.machine())
    if not system or not machine:
        sys.exit(f"build.py runs on macOS or Linux, not {platform.system()} {platform.machine()}")
    return f"{system}_{machine}"


def list_tools(exe):
    """The tools the binary serves, from `mobium mcp`'s own tools/list."""
    p = subprocess.Popen([str(exe), "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                         stderr=subprocess.DEVNULL, text=True)
    def request(i, method, params):
        p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": i, "method": method, "params": params}) + "\n")
        p.stdin.flush()
        while True:
            line = p.stdout.readline()
            if not line:
                sys.exit(f"{exe} mcp ended before answering {method}")
            answer = json.loads(line)
            if answer.get("id") == i:
                return answer["result"]
    request(1, "initialize", {"protocolVersion": "2024-11-05", "capabilities": {},
                              "clientInfo": {"name": "mcpb-build", "version": "1"}})
    tools = request(2, "tools/list", {})["tools"]
    p.stdin.close()
    p.wait(timeout=30)
    return tools


def first_sentence(text):
    end = text.find(". ")
    return text if end < 0 else text[: end + 1]


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: build.py <version> [dist]")
    version = sys.argv[1]
    dist = pathlib.Path(sys.argv[2] if len(sys.argv) > 2 else ROOT / "dist")

    with tempfile.TemporaryDirectory() as tmp:
        exe = pathlib.Path(tmp) / "mobium"
        exe.write_bytes(binary(dist, version, host_build()))
        exe.chmod(0o755)
        served = list_tools(exe)
    tools = [{"name": t["name"], "description": first_sentence(t["description"])} for t in served]

    manifest = json.loads((HERE / "manifest.json").read_text())
    manifest["version"] = version
    manifest["tools"] = tools

    bundle = dist / f"mobium-{version}.mcpb"
    with zipfile.ZipFile(bundle, "w", zipfile.ZIP_DEFLATED) as z:
        def add(name, data, mode=0o644):
            info = zipfile.ZipInfo(name, STAMP)
            info.external_attr = (0o100000 | mode) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, data)

        add("manifest.json", json.dumps(manifest, indent=2) + "\n")
        add("icon.png", (ROOT / "assets/branding/mobium-icon-512.png").read_bytes())
        add("LICENSE", (ROOT / "LICENSE").read_bytes())
        add("NOTICE", (ROOT / "NOTICE").read_bytes())
        add("server/mobium", (HERE / "launcher.sh").read_bytes(), 0o755)
        for build in BUILDS:
            add(f"server/mobium-{build.replace('_', '-')}", binary(dist, version, build), 0o755)

    digest = hashlib.sha256(bundle.read_bytes()).hexdigest()
    sums = dist / "SHA256SUMS"
    lines = [l for l in (sums.read_text().splitlines() if sums.exists() else []) if not l.endswith(".mcpb")]
    sums.write_text("\n".join(lines + [f"{digest}  {bundle.name}"]) + "\n")
    server = (HERE / "server.json").read_text().replace("@VERSION@", version).replace("@SHA256@", digest)
    (dist / "server.json").write_text(server)
    print(f"  {bundle.name}  {bundle.stat().st_size} bytes, {len(tools)} tools, sha256 {digest}")
    print(f"  server.json")


if __name__ == "__main__":
    main()
