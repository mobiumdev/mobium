"""Builds the per-client quick-start pages from their examples.

    python3 docs/quickstart/build.py          # write the pages
    python3 docs/quickstart/build.py --check  # fail if a page is out of date

Each page embeds its example file and the output that example printed on a
real Android emulator and a real iOS simulator (docs/quickstart/output/), so a
page cannot show code that is not the code that ran, or output it did not
print. make quickstart writes them; make docs-check runs --check. When an
example changes, run it on both platforms, save what it printed into
output/<client>-<platform>.txt, and rebuild.
"""

import pathlib
import sys

HERE = pathlib.Path(__file__).resolve().parent

INSTALL_FROM_CLONE = (
    "No client is on a package registry yet. Until the first release, install it "
    "from your clone of the repository — the path below assumes the clone is at "
    "`~/mobium`."
)

CLIENTS = [
    {
        "slug": "cli", "name": "Command line", "file": "cli/quickstart.sh", "lang": "sh",
        "needs": "Nothing beyond `mobium` itself on your `PATH`.",
        "install": None,
        "run": {"dir": "", "cmd": "MOBIUM_PLATFORM={p} sh quickstart.sh"},
        "start": "`mobium session start --platform android --app com.android.settings`",
        "quit": "`mobium session end`",
        "notes": [
            "Every command after `session start` uses that session, so none of them "
            "needs `--driver`. With more than one device attached, pass the same "
            "`--device` to each.",
            "`mobium session status` lists the sessions open. `mobium daemon stop` "
            "ends all of them at once.",
        ],
    },
    {
        "slug": "python", "name": "Python", "file": "python/quickstart.py", "lang": "python",
        "needs": "Python 3.9 or later.",
        "install": "python3 -m venv .venv\n.venv/bin/pip install ~/mobium/clients/python",
        "run": {"dir": "", "cmd": "MOBIUM_PLATFORM={p} .venv/bin/python quickstart.py"},
        "start": "`start(platform=..., app=...)`",
        "quit": "`device.quit()`, or leaving the `with` block",
        "notes": [
            "`with start(...) as device:` quits when the block ends, even on an "
            "exception. Without `with`, call `device.quit()` yourself — in a "
            "`finally`.",
            "`connect()` still exists: it opens a connection without touching the "
            "device, and its `close()` leaves the session open for whoever started it.",
        ],
    },
    {
        "slug": "javascript", "name": "JavaScript", "file": "javascript/quickstart.mjs", "lang": "js",
        "needs": "Node.js 18 or later.",
        "install": "npm init -y\nnpm install ~/mobium/clients/javascript",
        "run": {"dir": "", "cmd": "MOBIUM_PLATFORM={p} node quickstart.mjs"},
        "start": "`await start({ platform, app })`",
        "quit": "`await device.quit()`",
        "notes": [
            "Put `quit()` in a `finally`, as the example does, so an error part-way "
            "still ends the session. A second `quit()` does nothing.",
            "`connect()` still exists: it opens a connection without touching the "
            "device, and its `close()` leaves the session open.",
        ],
    },
    {
        "slug": "go", "name": "Go", "file": "go/main.go", "lang": "go",
        "needs": "Go 1.24 or later.",
        "install": "go mod init quickstart\ngo get github.com/mobiumdev/mobium/clients/go",
        "run": {"dir": "", "cmd": "MOBIUM_PLATFORM={p} go run ."},
        "start": "`mobium.Start(ctx, mobium.WithPlatform(...), mobium.WithApp(...))`",
        "quit": "`device.Quit(ctx)`",
        "notes": [
            "`defer device.Quit(ctx)` right after a successful `Start` is the usual "
            "shape. A second `Quit` does nothing, so an explicit one before it is fine.",
            "The example in the repository builds against the client beside it, "
            "through a `replace` line in its `go.mod`; your own module uses `go get`.",
            "`mobium.Connect` still exists: it opens a connection without touching "
            "the device, and `Close` leaves the session open.",
        ],
    },
    {
        "slug": "java", "name": "Java", "file": "java/Quickstart.java", "lang": "java",
        "needs": "Java 17 or later.",
        "install": "cd ~/mobium/clients/java && ./mvnw install -DskipTests && cd -",
        "run": {
            "dir": "",
            "cmd": "MOBIUM_PLATFORM={p} java -cp ~/.m2/repository/dev/mobium/mobium/0.1.0-SNAPSHOT/mobium-0.1.0-SNAPSHOT.jar Quickstart.java",
        },
        "start": "`Mobium.builder().platform(...).app(...).start()`",
        "quit": "`device.quit()`, or leaving try-with-resources",
        "notes": [
            "`./mvnw install` puts the jar in your local Maven repository. In a Maven "
            "project, depend on it with:\n\n"
            "  ```xml\n  <dependency>\n    <groupId>dev.mobium</groupId>\n"
            "    <artifactId>mobium</artifactId>\n    <version>0.1.0-SNAPSHOT</version>\n"
            "  </dependency>\n  ```\n\n"
            "  That is the scope this example runs with, from `src/main/java`; in a test "
            "suite, add `<scope>test</scope>` (Gradle: `testImplementation`).\n\n"
            "  In Gradle — verified with Gradle 9.8, running this example on both "
            "platforms — `build.gradle.kts`:\n\n"
            "  ```kotlin\n  plugins { application }\n\n"
            "  repositories {\n      mavenLocal()     // until dev.mobium:mobium is on Maven Central\n"
            "      mavenCentral()\n  }\n\n"
            "  dependencies { implementation(\"dev.mobium:mobium:0.1.0-SNAPSHOT\") }\n\n"
            "  application { mainClass = \"Quickstart\" }\n  ```\n\n"
            "  with `Quickstart.java` in `src/main/java/`, then `gradle run`.",
            "try-with-resources around `start()` quits when it ends. Around "
            "`connect()` it only closes the connection, leaving the session open.",
        ],
    },
    {
        "slug": "dotnet", "name": ".NET", "file": "dotnet/Program.cs", "lang": "csharp",
        "needs": "The .NET SDK 8 or later. The package targets netstandard2.0, so it "
                 "also runs on .NET Framework 4.6.1+.",
        "install": ("dotnet pack ~/mobium/clients/dotnet/Mobium -o ~/mobium-packages\n"
                    "dotnet new console -o quickstart && cd quickstart\n"
                    "dotnet add package Mobium --version 0.1.0 --source ~/mobium-packages"),
        "run": {"dir": "", "cmd": "MOBIUM_PLATFORM={p} dotnet run"},
        "start": "`Device.Builder().Platform(...).App(...).Start()`",
        "quit": "`device.Quit()`, or leaving the `using` block",
        "notes": [
            "Replace the generated `Program.cs` with the example below.",
            "A `using` block around `Start()` quits when it ends, even on an "
            "exception. Around `Connect()` it only closes the connection.",
        ],
    },
]

PLATFORM_NAMES = {"android": "Android", "ios": "iOS"}


def page(c):
    code = (HERE / "examples" / c["file"]).read_text()
    fname = pathlib.Path(c["file"]).name
    lines = [
        f"# Quick start: {c['name']}",
        "",
        "<!-- Generated by docs/quickstart/build.py from examples/ and output/. "
        "Edit those, then run `make quickstart`. -->",
        "",
        f"Start a session on a device, launch Settings, tap a row, take a "
        f"screenshot, and quit — from {c['name'] if c['slug'] != 'cli' else 'the command line'}. "
        f"Starts with {c['start']}; ends with {c['quit']}.",
        "",
        "Before this page: [install mobium and prepare a device](README.md).",
        "",
        "## 1. What you need",
        "",
        c["needs"],
        "",
    ]
    if c["install"]:
        lines += [
            "## 2. Install the client",
            "",
            INSTALL_FROM_CLONE,
            "",
            "```sh",
            c["install"],
            "```",
            "",
        ]
        n = 3
    else:
        n = 2
    lines += [
        f"## {n}. The code",
        "",
        f"Save this as `{fname}`"
        + (" in the project folder" if c["install"] else "")
        + f" — it is [examples/{c['file']}](examples/{c['file']}).",
        "",
        f"```{c['lang']}",
        code.rstrip(),
        "```",
        "",
        "Settings is on every Android and iOS device with nothing to install. The "
        "row and the screen after it are the only things that differ by platform.",
        "",
        f"## {n + 1}. Run it",
        "",
    ]
    for p in ("android", "ios"):
        out = (HERE / "output" / f"{c['slug']}-{p}.txt").read_text().rstrip()
        lines += [
            f"### {PLATFORM_NAMES[p]}",
            "",
            "```sh",
            c["run"]["cmd"].format(p=p),
            "```",
            "",
            f"What it printed on {'an Android 15 emulator' if p == 'android' else 'an iOS 26.5 simulator'}:",
            "",
            "```",
            out,
            "```",
            "",
            f"| After start | After the tap |",
            "| --- | --- |",
            f"| ![Settings, as start left it](images/{p}-1-start.jpg) "
            f"| ![The screen the tap opened](images/{p}-2-tapped.jpg) |",
            "",
        ]
    lines += [
        "The first start on a device is slow: it installs the UiAutomator2 server "
        "on Android, and on a real iPhone builds WebDriverAgent. Later starts take "
        "seconds.",
        "",
        "## Notes",
        "",
    ]
    lines += [f"- {n_}" for n_ in c["notes"]]
    lines += [
        "- With more than one device attached, `start` refuses to guess and lists "
        "them. Name one with `MOBIUM_DEVICE=<serial or UDID>`, which the example "
        "passes on as the device.",
        "",
        "Next: [the rest of the tool surface](../API.md), and "
        "[setting up phones and simulators](../SETUP.md).",
        "",
    ]
    return "\n".join(lines)


def main():
    check = "--check" in sys.argv[1:]
    stale = []
    for c in CLIENTS:
        path = HERE / f"{c['slug']}.md"
        text = page(c)
        if check:
            if not path.exists() or path.read_text() != text:
                stale.append(path.name)
        else:
            path.write_text(text)
    if stale:
        print("quick-start pages out of date: " + ", ".join(stale) + " — run `make quickstart`",
              file=sys.stderr)
        sys.exit(1)
    print(f"quick start: {len(CLIENTS)} pages {'checked' if check else 'written'}")


if __name__ == "__main__":
    main()
