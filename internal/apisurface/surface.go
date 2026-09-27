// Package apisurface sweeps every public entry point Mobium has and reports
// what each one covers.
//
// It exists because "one tool layer under every front door" is the claim the
// whole architecture rests on, and a claim like that decays silently: a tool
// added to the MCP schema and not to the CLI, or to three clients and not the
// fourth, is invisible until somebody reaches for it. Nothing here needs a
// device, so the drift check runs in CI on every commit.
//
// Everything is read from source rather than from a running binary, so the
// sweep cannot disagree with the code that would be built.
package apisurface

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mobiumdev/mobium/internal/agent"
)

// Surface is the whole public API, one entry per tool.
type Surface struct {
	Tools   []ToolEntry   `json:"tools"`
	Clients []ClientEntry `json:"clients"`
	Counts  Counts        `json:"counts"`
	Extra   ExtraCommands `json:"cliWithoutTool"`
}

// ToolEntry is one tool and everything that reaches it.
type ToolEntry struct {
	Number int    `json:"n"`
	Name   string `json:"tool"`
	// CLI is the command that dispatches this tool, empty if none does.
	CLI string `json:"cli,omitempty"`
	// Methods maps a client language to the method that calls this tool.
	Methods map[string]string `json:"clients"`
	// Missing lists the surfaces that do not reach this tool at all.
	Missing []string `json:"missing,omitempty"`
}

// ClientEntry describes one client library's coverage.
type ClientEntry struct {
	Language string   `json:"language"`
	File     string   `json:"file"`
	Covers   int      `json:"covers"`
	Missing  []string `json:"missing,omitempty"`
}

// Counts are the headline numbers, so a claim about them can be checked
// rather than remembered. The two command counts differ on purpose: a reader
// of `mobium --help` sees the top-level ones, while the constructor count
// includes `daemon start`, `stop` and `status`.
type Counts struct {
	Tools int `json:"tools"`
	// CLIRegistered is every command added to the root, hidden ones included.
	CLIRegistered int `json:"cliRegistered"`
	// CLIVisible is what `mobium --help` shows of them. The two differ by the
	// hidden ones — `pipe`, which clients spawn and nobody types — and
	// neither counts cobra's own `help` and `completion`.
	CLIVisible int `json:"cliVisible"`
	// CLIHidden names them, so the arithmetic can be checked rather than
	// taken on trust. Two counts that differ by an unexplained number is how
	// this project has miscounted its own surface twice.
	CLIHidden []string `json:"cliHidden"`
	// CLIConstructors counts every command built in the source, subcommands
	// included.
	CLIConstructors int `json:"cliConstructors"`
	Clients         int `json:"clients"`
}

// ExtraCommands are CLI commands that dispatch no tool. They are not drift —
// `daemon`, `mcp` and `pipe` are the process itself rather than device work —
// but they are listed so the count adds up and a genuinely orphaned command
// cannot hide among them.
type ExtraCommands []string

// toolLiteral finds a tool name written as a string, in either quoting style.
//
// Deliberately not tied to the helper that dispatches it. Each client has
// several — `data`, `act`, `call`, `text` and more — and an earlier version of
// this matched one helper per client and reported that every client was
// missing `app_tap`, which every client has. Matching the *name* cannot drift
// out of date the way a list of helper names can.
var toolLiteral = regexp.MustCompile(`["']((?:app_[a-z_]+))["']`)

// commentPrefixes are how each language opens a comment, so documentation that
// names a tool in prose is not mistaken for a call to it. Every client's docs
// do exactly that.
var commentPrefixes = map[string][]string{
	"go":         {"//"},
	"python":     {"#", `"""`, "'''"},
	"javascript": {"//", "*", "/*"},
	"dotnet":     {"//", "*", "/*", "///"},
	"java":       {"//", "*", "/*"},
}

// clientSources are the five client libraries: where each lives, and how a
// method definition looks in it — a package- or module-level function and a
// builder's method included, which is where start lives in four of them.
var clientSources = []struct {
	Language string
	File     string
	method   *regexp.Regexp
}{
	{
		Language: "go", File: "clients/go/mobium.go",
		method: regexp.MustCompile(`^func (?:\(d \*Device\) )?([A-Z][A-Za-z]*)\(`),
	},
	{
		Language: "python", File: "clients/python/mobium/_device.py",
		method: regexp.MustCompile(`^(?:    )?def ([a-z_]+)\(`),
	},
	{
		Language: "javascript", File: "clients/javascript/index.js",
		method: regexp.MustCompile(`^(?:  (?:async )?|export (?:async )?function )([a-zA-Z]+)\(`),
	},
	{
		Language: "java", File: "clients/java/src/main/java/dev/mobium/Mobium.java",
		method: regexp.MustCompile(`^ {4}(?: {4})?public [A-Za-z<>, \[\]]+ ([a-zA-Z]+)\(`),
	},
	{
		Language: "dotnet", File: "clients/dotnet/Mobium/Device.cs",
		method: regexp.MustCompile(`^        public (?:static )?[A-Za-z<>, \[\]?]+ ([A-Za-z]+)\(`),
	},
}

// Collect sweeps the repository rooted at root.
func Collect(root string) (*Surface, error) {
	tools := agent.ToolNames()
	sort.Strings(tools)

	cliByTool, extra, cliCount, err := collectCLI(filepath.Join(root, "cmd", "mobium"))
	if err != nil {
		return nil, err
	}

	byClient := map[string]map[string]string{}
	var clients []ClientEntry
	for _, c := range clientSources {
		methods, err := collectClient(filepath.Join(root, c.File), c.method, commentPrefixes[c.Language])
		if err != nil {
			return nil, err
		}
		if len(methods) == 0 {
			// Failing open here would report perfect coverage for a client
			// whose source had merely been reformatted.
			return nil, fmt.Errorf("found no tool calls in %s — its pattern has stopped "+
				"matching, and a drift check that cannot see a surface is worse than none",
				c.File)
		}
		byClient[c.Language] = methods
		clients = append(clients, ClientEntry{Language: c.Language, File: c.File})
	}

	registered, err := collectTopLevel(filepath.Join(root, "cmd", "mobium", "main.go"))
	if err != nil {
		return nil, err
	}
	hidden, err := collectHidden(filepath.Join(root, "cmd", "mobium"))
	if err != nil {
		return nil, err
	}

	s := &Surface{
		Counts: Counts{
			Tools:           len(tools),
			CLIRegistered:   registered,
			CLIVisible:      registered - len(hidden),
			CLIHidden:       hidden,
			CLIConstructors: cliCount,
			Clients:         len(clientSources),
		},
		Extra: extra,
	}
	for i, name := range tools {
		e := ToolEntry{Number: i + 1, Name: name, CLI: cliByTool[name], Methods: map[string]string{}}
		if e.CLI == "" {
			e.Missing = append(e.Missing, "cli")
		}
		for _, c := range clientSources {
			if m, ok := byClient[c.Language][name]; ok {
				e.Methods[c.Language] = m
			} else {
				e.Missing = append(e.Missing, c.Language)
			}
		}
		s.Tools = append(s.Tools, e)
	}

	for i := range clients {
		lang := clients[i].Language
		for _, e := range s.Tools {
			if _, ok := e.Methods[lang]; ok {
				clients[i].Covers++
			} else {
				clients[i].Missing = append(clients[i].Missing, e.Name)
			}
		}
	}
	s.Clients = clients
	return s, nil
}

// collectCLI parses the cobra commands out of the CLI source.
//
// Parsed rather than run, because running would need a built binary and the
// point is to compare what is *written*. Each command is a `new…Cmd` function
// whose `Use:` opens with the command word and whose body calls runTool with
// the tool it dispatches.
func collectCLI(dir string) (map[string]string, ExtraCommands, int, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, nil, 0, err
	}

	byTool := map[string]string{}
	var extra ExtraCommands
	count := 0

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !strings.HasPrefix(fn.Name.Name, "new") ||
					!strings.HasSuffix(fn.Name.Name, "Cmd") {
					continue
				}
				use, tools := inspectCommand(fn)
				if use == "" {
					continue
				}
				count++
				if len(tools) == 0 {
					extra = append(extra, use)
					continue
				}
				for _, t := range tools {
					// A command that dispatches several tools is recorded
					// against each, so neither looks unreachable.
					if byTool[t] == "" {
						byTool[t] = use
					} else if byTool[t] != use {
						byTool[t] += ", " + use
					}
				}
			}
		}
	}
	sort.Strings(extra)
	return byTool, extra, count, nil
}

// collectTopLevel counts what `mobium --help` lists, by reading the
// AddCommand calls on the root command rather than by running the binary.
func collectTopLevel(path string) (int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return 0, err
	}
	count := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "AddCommand" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "root" {
			return true
		}
		count += len(call.Args)
		return true
	})
	if count == 0 {
		return 0, fmt.Errorf("found no commands added to the root command in %s", path)
	}
	return count, nil
}

// collectHidden names the commands registered but not shown in help.
func collectHidden(dir string) ([]string, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, err
	}
	var hidden []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !strings.HasPrefix(fn.Name.Name, "new") ||
					!strings.HasSuffix(fn.Name.Name, "Cmd") {
					continue
				}
				use, _ := inspectCommand(fn)
				if use != "" && isHidden(fn) {
					hidden = append(hidden, use)
				}
			}
		}
	}
	sort.Strings(hidden)
	return hidden, nil
}

// isHidden reports whether a constructor sets Hidden: true.
func isHidden(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Hidden" {
			if id, ok := kv.Value.(*ast.Ident); ok && id.Name == "true" {
				found = true
			}
		}
		return true
	})
	return found
}

// inspectCommand pulls the command word and the tools it dispatches out of one
// constructor.
func inspectCommand(fn *ast.FuncDecl) (string, []string) {
	var use string
	var tools []string
	ast.Inspect(fn, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.KeyValueExpr:
			key, ok := v.Key.(*ast.Ident)
			if !ok || key.Name != "Use" || use != "" {
				return true
			}
			if lit, ok := v.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				s, err := strconv.Unquote(lit.Value)
				if err == nil {
					use = strings.Fields(s)[0]
				}
			}
		case *ast.CallExpr:
			// Two dispatch paths, not one. Most commands go through runTool,
			// which talks to the daemon; `doctor` calls the tool layer
			// in-process, deliberately, so it still works with no daemon, no
			// device and nothing on PATH. Matching only runTool reported
			// doctor as unreachable from the CLI, which it plainly is not.
			if !isToolDispatch(v.Fun) || len(v.Args) == 0 {
				return true
			}
			if lit, ok := v.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil &&
					strings.HasPrefix(s, "app_") {
					tools = append(tools, s)
				}
			}
		}
		return true
	})
	return use, tools
}

// isToolDispatch reports whether a call sends a named tool to the tool layer.
func isToolDispatch(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name == "runTool"
	case *ast.SelectorExpr:
		return f.Sel.Name == "Call"
	}
	return false
}

// collectClient maps tool name to the method that calls it, by walking the
// source and attributing each call to the method it sits in.
func collectClient(path string, method *regexp.Regexp, comments []string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := method.FindStringSubmatch(line); m != nil {
			current = m[1]
		}
		// Every client's documentation names tools in prose — "app_logs reads
		// a page's console" — and counting those as calls would report
		// coverage a client does not have.
		trimmed := strings.TrimSpace(line)
		isComment := false
		for _, marker := range comments {
			if strings.HasPrefix(trimmed, marker) {
				isComment = true
				break
			}
		}
		if isComment || current == "" {
			continue
		}
		for _, c := range toolLiteral.FindAllStringSubmatch(line, -1) {
			// The first method to reach a tool wins, so a convenience wrapper
			// added later does not displace the primary name.
			if _, seen := out[c[1]]; !seen {
				out[c[1]] = current
			}
		}
	}
	return out, nil
}
