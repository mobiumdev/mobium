package apisurface

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Wire types.
//
// The Go client decodes each tool's structured result into a struct of its
// own, and nothing tied those structs to the daemon's. When the daemon's
// screen result renamed width to width_px, the client went on reading "width"
// and reported every screen as 0x0 — silently, since a JSON key that is never
// sent decodes as a zero. This pairs every Go client type that has JSON tags
// with the daemon type it decodes, and fails when the client reads a key the
// daemon does not send.
//
// The other four clients read results as maps, keyed where they are used, so
// they have no struct to drift; the tool-coverage checks above cover them.

// GoWireTypes pairs each Go client type with the daemon type it decodes, as
// "directory under the repository root" and type name.
var GoWireTypes = map[string][2]string{
	"App":            {"internal/agent", "AppEntry"},
	"Bounds":         {"internal/agent", "BoundsView"},
	"ClearedData":    {"internal/agent", "ClearDataView"},
	"ConsoleEntry":   {"internal/webview", "ConsoleEntry"},
	"Cookie":         {"internal/webview", "Cookie"},
	"CrashReport":    {"internal/device", "CrashReport"},
	"DeviceInfo":     {"internal/agent", "DeviceView"},
	"DeviceLogEntry": {"internal/device", "LogEntry"},
	"DialogRule":     {"internal/agent", "dialogRule"},
	"Element":        {"internal/agent", "ElementView"},
	"Finding":        {"internal/agent", "FindingView"},
	"KeyboardField":  {"internal/agent", "FocusedView"},
	"KeyboardState":  {"internal/agent", "KeyboardView"},
	"Location":       {"internal/agent", "LocationView"},
	"Locator":        {"internal/agent", "LocatorView"},
	"Notification":   {"internal/device", "Notification"},
	"OriginStorage":  {"internal/webview", "OriginStorage"},
	"PageSource":     {"internal/agent", "SourceView"},
	"Recording":      {"internal/agent", "RecordView"},
	"Screen":         {"internal/agent", "ScreenView"},
	"Session":        {"internal/agent", "SessionView"},
	"StorageItem":    {"internal/webview", "StorageItem"},
	"StorageState":   {"internal/agent", "StorageState"},
}

// GoWireTypeExemptions are Go client types with JSON tags that decode no
// daemon result, with the reason.
var GoWireTypeExemptions = map[string]string{}

// jsonKeys reads every struct with JSON tags in the non-test Go files of dir,
// as type name to the keys its fields decode. On the client's side only
// exported types count: the unexported ones are the JSON-RPC envelope the
// connection itself reads, not a tool's result.
func jsonKeys(dir string, exportedOnly bool) (map[string][]string, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			var keys []string
			for _, field := range st.Fields.List {
				if field.Tag == nil {
					continue
				}
				tag, err := strconv.Unquote(field.Tag.Value)
				if err != nil {
					continue
				}
				key := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
				if key != "" && key != "-" {
					keys = append(keys, key)
				}
			}
			// Only exported types are results; the unexported ones are the
			// JSON-RPC envelope the connection itself reads.
			if len(keys) > 0 && (!exportedOnly || ast.IsExported(spec.Name.Name)) {
				out[spec.Name.Name] = keys
			}
			return true
		})
	}
	return out, nil
}

// CheckGoWireTypes reports every Go client type that reads a key its daemon
// type does not send, every Go client type with JSON tags that is not paired,
// and every pairing that names a type which no longer exists.
func CheckGoWireTypes(root string) ([]string, error) {
	client, err := jsonKeys(filepath.Join(root, "clients", "go"), true)
	if err != nil {
		return nil, err
	}
	daemons := map[string]map[string][]string{}
	var problems []string
	for name, keys := range client {
		pair, ok := GoWireTypes[name]
		if !ok {
			if _, exempt := GoWireTypeExemptions[name]; !exempt {
				problems = append(problems, fmt.Sprintf("Go client type %s has JSON tags and no daemon type in apisurface.GoWireTypes", name))
			}
			continue
		}
		if daemons[pair[0]] == nil {
			if daemons[pair[0]], err = jsonKeys(filepath.Join(root, pair[0]), false); err != nil {
				return nil, err
			}
		}
		sent, ok := daemons[pair[0]][pair[1]]
		if !ok {
			problems = append(problems, fmt.Sprintf("GoWireTypes pairs %s with %s.%s, which does not exist", name, pair[0], pair[1]))
			continue
		}
		have := map[string]bool{}
		for _, k := range sent {
			have[k] = true
		}
		for _, k := range keys {
			if !have[k] {
				problems = append(problems, fmt.Sprintf("Go client %s reads %q, which %s.%s never sends", name, k, pair[0], pair[1]))
			}
		}
	}
	for name := range GoWireTypes {
		if _, ok := client[name]; !ok {
			problems = append(problems, fmt.Sprintf("GoWireTypes names %s, which the Go client no longer has", name))
		}
	}
	sort.Strings(problems)
	return problems, nil
}
