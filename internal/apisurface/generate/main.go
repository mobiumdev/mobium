// Command generate writes the API sweep to docs/api/surface.json and the
// numbered table in docs/API.md.
//
// Generated rather than maintained, because a hand-written table of 35 tools
// across five surfaces is a table that is wrong within a week — and this
// repository has already corrected its own counts three times.
//
//	go run ./internal/apisurface/generate
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mobiumdev/mobium/internal/apisurface"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	s, err := apisurface.Collect(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sweep:", err)
		os.Exit(1)
	}
	if problems := apisurface.Check(s); len(problems) > 0 {
		// Generating a document that records drift as though it were the
		// intended state is worse than not generating one.
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "drift:", p)
		}
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Join(root, "docs", "api"), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	jsonPath := filepath.Join(root, "docs", "api", "surface.json")
	if err := os.WriteFile(jsonPath, append(body, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	docPath := filepath.Join(root, "docs", "API.md")
	if err := os.WriteFile(docPath, []byte(render(s)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// The flag surface, the same procedure one level down: what each tool
	// accepts, and which command sets it.
	args, cmds, err := apisurface.CollectFlags(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flag sweep:", err)
		os.Exit(1)
	}
	if problems := apisurface.CheckFlags(args, cmds); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "flag drift:", p)
		}
		os.Exit(1)
	}

	flagBody, err := json.MarshalIndent(map[string]interface{}{
		"arguments": args, "commands": cmds,
	}, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	flagJSON := filepath.Join(root, "docs", "api", "flags.json")
	if err := os.WriteFile(flagJSON, append(flagBody, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	flagDoc := filepath.Join(root, "docs", "FLAGS.md")
	if err := os.WriteFile(flagDoc, []byte(renderFlags(args, cmds)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s and %s (%d tools, %d surfaces)\n",
		docPath, jsonPath, s.Counts.Tools, s.Counts.Clients+1)
	fmt.Printf("wrote %s and %s (%d arguments, %d commands)\n",
		flagDoc, flagJSON, len(args), len(cmds))
}

// renderFlags writes the flag surface: every argument a tool accepts and the
// command that sets it, and every command's flags.
func renderFlags(args []apisurface.ArgEntry, cmds []apisurface.CommandFlags) string {
	var b strings.Builder
	b.WriteString(`# The flag surface

**Generated — do not edit.** Run ` + "`make flags`" + ` to rebuild this and
[api/flags.json](api/flags.json) from the source.

[API.md](API.md) answers whether every front door reaches every tool. This
answers the question underneath it: **whether the CLI and the schema agree
about what each tool accepts.**

That is worth enforcing separately because the two ends fail in opposite
directions and neither one shouts. Every tool's schema is
` + "`additionalProperties: false`" + `, so an MCP client passing an undeclared key
is refused outright; the CLI hands a plain map to the same handler, and a
handler reads the keys it knows by name. A command writing
` + "`call[\"speeed\"]`" + ` is therefore rejected loudly on one surface and
silently ignored on the other — the one people use.

` + "`--device`" + ` and ` + "`--driver`" + ` are persistent flags that
` + "`daemonCall`" + ` attaches to every call, so they are marked global and no
command mentions them.

## Arguments, by tool

| Tool | Argument | Type | Set by |
| --- | --- | --- | --- |
`)
	for _, a := range args {
		setBy := strings.Join(a.SetBy, ", ")
		if a.Global {
			setBy = "_global_ " + setBy
		}
		if setBy == "" {
			setBy = "—"
		}
		typ := a.Type
		if typ == "" {
			typ = "—"
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s |\n", a.Tool, a.Name, typ, setBy)
	}

	b.WriteString(`
## Commands, and what they send

A command with no flags of its own is not unusual: most take positional
arguments and the two global flags.

| Command | Tools | Flags | Sends |
| --- | --- | --- | --- |
`)
	for _, c := range cmds {
		flags := strings.Join(c.Flags, " ")
		if flags == "" {
			flags = "—"
		}
		sends := strings.Join(c.Sends, ", ")
		if sends == "" {
			sends = "—"
		}
		tools := strings.Join(c.Tools, ", ")
		if tools == "" {
			tools = "—"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", c.Command, tools, flags, sends)
	}
	return b.String()
}

func render(s *apisurface.Surface) string {
	var b strings.Builder

	b.WriteString(`# The API surface

**Generated — do not edit.** Run ` + "`make api`" + ` to rebuild this and
[api/surface.json](api/surface.json) from the source.

Mobium's architecture rests on one claim: every front door reaches the same
tool layer, so a CLI command and an MCP tool cannot answer differently. This is
that claim, enumerated, and a drift check keeps it true —
` + "`internal/apisurface`" + ` fails the build if any tool stops being reachable
from any surface without the gap being written down.

One level down, [FLAGS.md](FLAGS.md) asks whether the CLI and the schema agree
about what each tool *accepts*. Same procedure, same package, same kind of
exemption list — and a separate check, because the two questions fail
differently.

`)

	fmt.Fprintf(&b, `## The numbers

| | |
| --- | --- |
| Tools | **%d** |
| CLI commands registered | %d |
| …visible in `+"`mobium --help`"+` | %d |
| …hidden | %d (%s) |
| Command constructors in source | %d (includes `+"`daemon start`"+`, `+"`stop`"+`, `+"`status`"+`) |
| Client libraries | %d |

`, s.Counts.Tools, s.Counts.CLIRegistered, s.Counts.CLIVisible,
		len(s.Counts.CLIHidden), strings.Join(s.Counts.CLIHidden, ", "),
		s.Counts.CLIConstructors, s.Counts.Clients)

	b.WriteString(`Those three command counts differ on purpose, and the arithmetic is asserted
by a test: registered = visible + hidden, and the constructor count is higher
again because ` + "`daemon`" + ` has subcommands. Reported as one number, they have
been wrong twice.

## Every tool, and what reaches it

`)

	b.WriteString("| # | Tool | CLI | Go | Python | JavaScript | Java |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, t := range s.Tools {
		fmt.Fprintf(&b, "| %d | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` |\n",
			t.Number, t.Name, cell(t.CLI),
			cell(t.Methods["go"]), cell(t.Methods["python"]),
			cell(t.Methods["javascript"]), cell(t.Methods["java"]))
	}

	b.WriteString(`
## Client coverage

`)
	b.WriteString("| Client | Source | Tools reached |\n| --- | --- | --- |\n")
	for _, c := range s.Clients {
		fmt.Fprintf(&b, "| %s | [%s](../%s) | %d / %d |\n",
			c.Language, c.File, c.File, c.Covers, s.Counts.Tools)
	}

	fmt.Fprintf(&b, `
## Commands that dispatch no tool

%s

These run the process rather than the device — the daemon's own lifecycle, and
the two stdio servers. They are listed because a command that reaches no tool
is either one of these or an orphan, and from outside the two look identical; a
test names them so a new orphan fails rather than blending in.

## How drift is prevented

`+"`internal/apisurface`"+` sweeps the source — not a running binary, so it
cannot disagree with what would be built — and asserts:

1. **Every tool is reachable from the CLI.** Both dispatch paths count: most
   commands go through the daemon, while `+"`doctor`"+` calls the tool layer
   in-process so it still works with no daemon, no device and nothing on PATH.
2. **Every client reaches every tool.** Per client, so a failure names the
   client rather than a list of tools.
3. **Gaps may exist, but only declared ones.** An undeclared gap fails; so does
   an exemption for a gap that has since been closed, so the list cannot rot
   into a blanket excuse. It is currently empty.
4. **The sweep can see each surface.** A pattern that stopped matching would
   report a client as covering nothing; that is asserted against, because a
   drift check that fails open is worse than none.

The first thing this found, on the day it was written, was real: `+"`doctor`"+`
existed in the Java client and in none of the other three. Nothing had compared
them before.
`, bulletList(s.Extra))

	return b.String()
}

func cell(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

func bulletList(items []string) string {
	var b strings.Builder
	for _, s := range items {
		fmt.Fprintf(&b, "- `%s`\n", s)
	}
	return b.String()
}
