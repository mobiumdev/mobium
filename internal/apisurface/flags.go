package apisurface

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/mobiumdev/mobium/internal/agent"
)

// The flag surface.
//
// The tool sweep next door answers "can every front door reach every tool".
// This answers the narrower question underneath it: **does what the CLI sends
// match what the tool declares it accepts.**
//
// That question has teeth because the two ends fail differently. Every tool's
// schema is `additionalProperties: false`, so an MCP client passing an
// undeclared key is refused outright. The CLI passes a plain map to the same
// handler, and a handler reads the keys it knows by name — so a CLI flag
// writing `call["speeed"]` is rejected loudly on one surface and does nothing
// at all on the other. Same binary, same tool, opposite behavior, no error
// either way on the path people actually use.
//
// The reverse gap is quieter still: a property declared in the schema that no
// command can set is a feature an MCP client has and a CLI user does not,
// which is the kind of asymmetry this project exists to not have.

// ArgEntry is one tool argument and what reaches it.
type ArgEntry struct {
	Tool string `json:"tool"`
	Name string `json:"arg"`
	Type string `json:"type,omitempty"`
	// SetBy names the commands that send this argument, or is empty when
	// nothing does.
	SetBy []string `json:"setBy,omitempty"`
	// Global marks the two arguments every command sends without declaring
	// them: --device and --backend are persistent flags that `daemonCall`
	// attaches to every call, so no per-command code mentions them.
	Global bool `json:"global,omitempty"`
}

// CommandFlags is one CLI command and the flags it registers.
type CommandFlags struct {
	Command string   `json:"command"`
	Tools   []string `json:"tools,omitempty"`
	Flags   []string `json:"flags,omitempty"`
	// Sends are the tool-argument keys this command writes.
	Sends []string `json:"sends,omitempty"`
	// Unknown are keys it sends that no tool it dispatches declares. These
	// are the defects this sweep exists to catch.
	Unknown []string `json:"unknown,omitempty"`
}

// globalArgs are attached by daemonCall from persistent flags rather than by
// any command, so they are reachable everywhere and mentioned nowhere.
var globalArgs = map[string]string{
	"device":  "--device",
	"backend": "--backend",
}

// CollectFlags reads the declared arguments from the schema and the sent
// arguments from the CLI source.
//
// The schema side needs no parsing: agent.GetToolSchemas() is the same
// function the MCP server answers with, so this compares against the thing
// itself rather than against a description of it.
func CollectFlags(root string) ([]ArgEntry, []CommandFlags, error) {
	declared := map[string]map[string]string{} // tool -> arg -> type
	for _, t := range agent.GetToolSchemas() {
		props := map[string]string{}
		if raw, ok := t.InputSchema["properties"].(map[string]interface{}); ok {
			for name, spec := range raw {
				typ := ""
				if m, ok := spec.(map[string]interface{}); ok {
					typ, _ = m["type"].(string)
				}
				props[name] = typ
			}
		}
		declared[t.Name] = props
	}

	cmds, err := collectCommandFlags(root + "/cmd/mobium")
	if err != nil {
		return nil, nil, err
	}

	// Attribute every sent key to the tools its command dispatches.
	setBy := map[string]map[string][]string{} // tool -> arg -> commands
	for i := range cmds {
		c := &cmds[i]
		for _, key := range c.Sends {
			known := false
			for _, tool := range c.Tools {
				if _, ok := declared[tool][key]; ok {
					known = true
					if setBy[tool] == nil {
						setBy[tool] = map[string][]string{}
					}
					setBy[tool][key] = append(setBy[tool][key], c.Command)
				}
			}
			// A key no dispatched tool declares. Recorded against the command
			// rather than against a tool, because there is no tool it belongs
			// to — that is the whole problem with it.
			if !known && len(c.Tools) > 0 {
				c.Unknown = append(c.Unknown, key)
			}
		}
		sort.Strings(c.Unknown)
	}

	var args []ArgEntry
	for tool, props := range declared {
		for name, typ := range props {
			e := ArgEntry{Tool: tool, Name: name, Type: typ}
			if flag, ok := globalArgs[name]; ok {
				e.Global = true
				e.SetBy = []string{flag}
			} else if who := setBy[tool][name]; len(who) > 0 {
				sort.Strings(who)
				e.SetBy = dedupe(who)
			}
			args = append(args, e)
		}
	}
	sort.Slice(args, func(i, j int) bool {
		if args[i].Tool != args[j].Tool {
			return args[i].Tool < args[j].Tool
		}
		return args[i].Name < args[j].Name
	})
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Command < cmds[j].Command })
	return args, cmds, nil
}

func dedupe(in []string) []string {
	out := in[:0:0]
	seen := map[string]bool{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// collectCommandFlags walks the CLI source for each command's flags and the
// argument keys it sends.
func collectCommandFlags(dir string) ([]CommandFlags, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, err
	}
	// Every function in the package, so a command that builds its arguments in
	// a helper is followed rather than reported as sending nothing. `grant`
	// and `revoke` do exactly that, through permissionArgs.
	local := map[string]*ast.FuncDecl{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
					local[fn.Name.Name] = fn
				}
			}
		}
	}

	var out []CommandFlags
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
				c := CommandFlags{Command: use, Tools: tools}
				c.Flags, c.Sends = inspectFlagsAndKeys(fn, local, map[string]bool{})
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// inspectFlagsAndKeys finds the flags a command registers and the map keys it
// writes.
//
// Keys are taken from two shapes and no others, because a looser rule would
// collect every string literal in the function and report half of them as
// undeclared arguments: an index into a map variable, `call["speed"] = x`, and
// the keys of a map literal, `map[string]interface{}{"app": args[0]}`.
func inspectFlagsAndKeys(fn *ast.FuncDecl, local map[string]*ast.FuncDecl,
	visited map[string]bool) (flags, keys []string) {
	seenFlag := map[string]bool{}
	seenKey := map[string]bool{}
	add := func(s string) {
		if s != "" && !seenKey[s] {
			seenKey[s] = true
			keys = append(keys, s)
		}
	}

	ast.Inspect(fn, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.RangeStmt:
			// `for i, key := range []string{"x1","y1","x2","y2"}` with the
			// loop variable used as a map index. swipe builds its coordinates
			// this way, and the keys are literals in a slice rather than in
			// the index, so nothing below would see them.
			for _, s := range rangeKeys(v) {
				add(s)
			}
		case *ast.CallExpr:
			// A helper that builds the arguments, followed only where its
			// value is *passed as* the arguments: `runTool("app_grant",
			// permissionArgs(args))`. grant and revoke build theirs that way,
			// outside the command entirely.
			//
			// Following every local call instead would walk into runTool,
			// emit and printJSON, and collect "result" — an output key — as
			// an argument of every tool in the program. Narrowing by position
			// rather than by a list of plumbing function names to skip, since
			// the list is the thing that would rot.
			if isToolDispatch(v.Fun) {
				for _, a := range v.Args {
					call, ok := a.(*ast.CallExpr)
					if !ok {
						continue
					}
					id, ok := call.Fun.(*ast.Ident)
					if !ok || visited[id.Name] {
						continue
					}
					helper, known := local[id.Name]
					if !known {
						continue
					}
					visited[id.Name] = true
					_, hk := inspectFlagsAndKeys(helper, local, visited)
					for _, k := range hk {
						add(k)
					}
				}
				return true
			}
			// cmd.Flags().StringVar(&x, "name", ...) and every sibling. The
			// flag name is the first string literal argument.
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok || !isFlagRegistration(sel.Sel.Name) {
				return true
			}
			for _, a := range v.Args {
				if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil && s != "" {
						if !seenFlag[s] {
							seenFlag[s] = true
							flags = append(flags, "--"+s)
						}
					}
					break
				}
			}
		case *ast.IndexExpr:
			if lit, ok := v.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					add(s)
				}
			}
		case *ast.CompositeLit:
			if !isStringKeyedMap(v.Type) {
				return true
			}
			for _, elt := range v.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if lit, ok := kv.Key.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						add(s)
					}
				}
			}
		}
		return true
	})
	sort.Strings(flags)
	sort.Strings(keys)
	return flags, keys
}

// isFlagRegistration covers both pflag spellings, which is not a detail: the
// Var forms bind a variable, the plain ones return a pointer, and this
// repository uses both — `location` registers with StringVar and `swipe` with
// Duration. Listing only the Var forms reported swipe as having no flags at
// all, which it plainly does, and made a sweep that could not fail.
//
// The name is the first *string literal* argument either way: the Var forms
// put a pointer first, which is not one.
func isFlagRegistration(name string) bool {
	switch name {
	case "String", "StringP", "Bool", "BoolP",
		"Int", "IntP", "Int64", "Int64P", "Float64", "Float64P",
		"StringSlice", "StringSliceP", "Duration", "DurationP",
		"StringVar", "StringVarP", "BoolVar", "BoolVarP",
		"IntVar", "IntVarP", "Int64Var", "Int64VarP", "Float64Var", "Float64VarP",
		"StringSliceVar", "StringSliceVarP", "DurationVar", "DurationVarP":
		return true
	}
	return false
}

func isStringKeyedMap(t ast.Expr) bool {
	m, ok := t.(*ast.MapType)
	if !ok {
		return false
	}
	k, ok := m.Key.(*ast.Ident)
	return ok && k.Name == "string"
}

// rangeKeys reads the strings out of `for _, k := range []string{...}` when the
// loop variable is used to index something inside the body. Without the second
// condition this would collect every slice of strings in a command, including
// the lists of valid directions and roles, and report them as arguments.
func rangeKeys(r *ast.RangeStmt) []string {
	lit, ok := r.X.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	if arr, ok := lit.Type.(*ast.ArrayType); !ok {
		return nil
	} else if id, ok := arr.Elt.(*ast.Ident); !ok || id.Name != "string" {
		return nil
	}
	val, ok := r.Value.(*ast.Ident)
	if !ok {
		return nil
	}
	used := false
	ast.Inspect(r.Body, func(n ast.Node) bool {
		if ix, ok := n.(*ast.IndexExpr); ok {
			if id, ok := ix.Index.(*ast.Ident); ok && id.Name == val.Name {
				used = true
			}
		}
		return !used
	})
	if !used {
		return nil
	}
	var out []string
	for _, e := range lit.Elts {
		if b, ok := e.(*ast.BasicLit); ok && b.Kind == token.STRING {
			if s, err := strconv.Unquote(b.Value); err == nil {
				out = append(out, s)
			}
		}
	}
	return out
}
