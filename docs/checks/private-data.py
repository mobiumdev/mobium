#!/usr/bin/env python3
"""Refuse a real device's or person's identifiers in tracked files.

This repository is public. A real iPhone's UDID, a phone's serial in an adb
mdns name, a home network's address and a home directory's path all reached
it from captures and test fixtures (CHALLENGES 407). This check knows no real
value: it knows the shapes, and the made-up values that may stand in for one.
A value of a flagged shape that is not on the list fails `make ci`.

What it prints names the file, the line and the kind, never the value: a CI
log of a public repository is public too.

    docs/checks/private-data.py scan <file>...
    docs/checks/private-data.py selftest
"""
import re
import sys

# Made-up values a test or a page may use. Add one here, never a real one.
ALLOWED = {
    "udid": {
        "00008120-0000000000000001",
        "00008120-0000000000000002",
        "00008120-0001234567890ABC",
    },
    # 10.0.2.2 is the emulator's own name for the host. The rest are fixtures
    # that stand for any address; new ones belong in 192.0.2.0/24,
    # 198.51.100.0/24 or 203.0.113.0/24, kept for documentation and never
    # flagged.
    "lan-ip": {"10.0.2.2", "10.0.2.15", "10.0.0.1", "10.0.0.5", "192.168.1.4", "192.168.1.5"},
    "home-path": {"/home/runner", "/Users/x", "/Users/you", "/home/you", "C:\\Users\\you"},
    "mdns-name": set(),
}

RULES = [
    ("udid", re.compile(r"(?<![0-9A-Fa-f])0000[0-9]{4}-[0-9A-F]{16}(?![0-9A-Fa-f])")),
    ("mdns-name", re.compile(r"\badb-(?!X{4,})[0-9A-Z]{4,}(?:\.\.\.)?-[A-Za-z0-9]{6}\b")),
    ("lan-ip", re.compile(
        r"(?<![0-9.])(?:10\.\d{1,3}|192\.168|172\.(?:1[6-9]|2\d|3[01]))\.\d{1,3}\.\d{1,3}(?![0-9])")),
    ("home-path", re.compile(r"/Users/[A-Za-z][A-Za-z0-9._-]*|/home/[A-Za-z][A-Za-z0-9._-]*|"
                             r"[A-Za-z]:\\\\?Users\\\\?[A-Za-z][A-Za-z0-9._-]*")),
]

# Placeholders a document writes in place of a name.
PLACEHOLDER_HOME = re.compile(r"/(?:Users|home)/(?:<|\$|USER|you|me|name|runner)", re.I)
# One letter stands for anyone: C:\Users\a in a test of two homes.
ONE_LETTER_HOME = re.compile(r"(?:Users|home)\\{1,2}[A-Za-z]$|(?:Users|home)/[A-Za-z]$")


def bytecode(path):
    return path.endswith(".pyc") or "__pycache__/" in path


def findings(path, text):
    """(line, kind) for each value of a flagged shape not on the list."""
    out = []
    if bytecode(path):
        out.append((0, "bytecode"))
    for n, line in enumerate(text.splitlines(), 1):
        for kind, rule in RULES:
            for m in rule.finditer(line):
                v = m.group(0)
                if v in ALLOWED[kind]:
                    continue
                if kind == "home-path" and (PLACEHOLDER_HOME.match(v) or ONE_LETTER_HOME.search(v)):
                    continue
                out.append((n, kind))
    return out


def scan(paths):
    bad = 0
    for p in paths:
        if p == "docs/checks/private-data.py":
            continue
        try:
            with open(p, "rb") as f:
                text = f.read().decode("utf-8", "replace")
        except (IsADirectoryError, FileNotFoundError):
            continue
        for n, kind in findings(p, text):
            bad += 1
            where = p if n == 0 else "%s:%d" % (p, n)
            hint = {
                "bytecode": "Python bytecode records the absolute path it was built in: untrack it",
                "udid": "an iPhone's UDID: use one on the made-up list",
                "mdns-name": "an adb mdns name holds the phone's serial: write adb-XXXXXXXX-abcdef",
                "lan-ip": "a home network's address: use 192.0.2.x",
                "home-path": "a home directory's path: write ~ or /Users/you",
            }[kind]
            print("%s: %s — %s" % (where, kind, hint))
    return bad


def selftest():
    # Each rule must be seen to fire, or a pass means nothing.
    cases = [
        ("a.md", "UDID 00008120-00AB34CD56EF7890 here", ["udid"]),
        ("a.md", "UDID 00008120-0000000000000001 here", []),
        ("a.md", "# adb-1A2B3C4D...-QwErTy  _adb-tls-connect._tcp", ["mdns-name"]),
        ("a.md", "# adb-XXXXXXXX-abcdef  _adb-tls-connect._tcp", []),
        ("a.md", "adb connect 10.0.0.42:5555", ["lan-ip"]),
        ("a.md", "adb connect 192.168.4.20:5555 and 172.20.1.1", ["lan-ip", "lan-ip"]),
        ("a.md", "adb connect 192.0.2.10:5555; host 10.0.2.2", []),
        ("a.md", "not an address: 110.0.0.1 or 1.10.0.0.12", []),
        ("a.md", "built in /Users/someone/project", ["home-path"]),
        ("a.md", "under /home/someone and C:\\Users\\someone", ["home-path", "home-path"]),
        ("a.md", "~/mobium, /Users/you/x, /Users/<you>, /home/runner/work, $HOME", []),
        ("a.go", "pipeName(`C:\\Users\\a\\.mobium`) and /Users/b/x", []),
        ("a.md", "C:\\Users\\someone\\AppData", ["home-path"]),
        ("scripts/__pycache__/x.cpython-314.pyc", "", ["bytecode"]),
    ]
    failed = 0
    for path, text, want in cases:
        got = [k for _, k in findings(path, text)]
        if got != want:
            failed += 1
            print("FAIL %r: got %s, want %s" % (text or path, got, want))
    # What it prints never holds the value it flags.
    import io
    import contextlib
    import os
    import tempfile
    secret = "10.0.0.42"
    with tempfile.TemporaryDirectory() as d:
        p = os.path.join(d, "leak.md")
        with open(p, "w") as f:
            f.write("adb connect %s:5555\n" % secret)
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            n = scan([p])
        if n != 1 or secret in buf.getvalue():
            failed += 1
            print("FAIL the report should flag one and print no value: %r" % buf.getvalue())
    if failed:
        return 1
    print("private-data: all ok")
    return 0


if __name__ == "__main__":
    if len(sys.argv) >= 2 and sys.argv[1] == "selftest":
        sys.exit(selftest())
    if len(sys.argv) >= 2 and sys.argv[1] == "scan":
        sys.exit(1 if scan(sys.argv[2:]) else 0)
    print(__doc__, file=sys.stderr)
    sys.exit(2)
