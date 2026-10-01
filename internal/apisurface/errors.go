package apisurface

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Error codes are public API exactly as tools are (docs/guides/cli.md), so
// they get the same guard: every code the server can send has an exception in
// every client, under the name mobiumerr.Name gives it, mapped to the right
// wire string — and no client carries a code the server no longer sends.
//
// A client missing a code is not broken — it raises the base exception — but
// a caller writing `except UnsupportedError` against that client catches
// nothing, silently. That is the failure worth a red build.

// errorSources says, per client, where its exceptions live and how one reads:
// the regexp captures the name (without its suffix) and the wire code.
var errorSources = []struct {
	Language string
	Glob     string
	decl     *regexp.Regexp
}{
	{
		// A sentinel names a Code constant; the constant carries the string.
		// Both are read, and joined, below.
		Language: "go", Glob: "clients/go/errors.go",
		decl: regexp.MustCompile(`(?m)^\s*Err(\w+)\s*=\s*&Error\{Code:\s*(Code\w+)\}`),
	},
	{
		Language: "python", Glob: "clients/python/mobium/_errors.py",
		decl: regexp.MustCompile(`(?ms)^class (\w+)Error\(MobiumError\):.*?^    code = "(\w+)"`),
	},
	{
		Language: "javascript", Glob: "clients/javascript/index.js",
		decl: regexp.MustCompile(`(?m)^export class (\w+)Error extends MobiumError \{\r?\n  static code = '(\w+)'`),
	},
	{
		Language: "java", Glob: "clients/java/src/main/java/dev/mobium/*Exception.java",
		decl: regexp.MustCompile(`(?ms)^public final class (\w+)Exception extends MobiumException \{.*?CODE = "(\w+)"`),
	},
	{
		Language: "dotnet", Glob: "clients/dotnet/Mobium/MobiumException.cs",
		decl: regexp.MustCompile(`(?ms)public sealed class (\w+)Exception : MobiumException\s*\{.*?ErrorCode = "(\w+)"`),
	},
}

var goCodeConst = regexp.MustCompile(`(?m)^\s*(Code\w+)\s+Code\s*=\s*"(\w+)"`)

// ErrorCoverage returns, per language, what is wrong with its exceptions:
// codes missing, a name mapped to the wrong code, or codes the server does not
// send. An empty map means every client is complete.
func ErrorCoverage(root string) (map[string][]string, error) {
	sources := map[string]string{}
	for _, s := range errorSources {
		files, err := filepath.Glob(filepath.Join(root, s.Glob))
		if err != nil || len(files) == 0 {
			return nil, fmt.Errorf("no files for %s at %s", s.Language, s.Glob)
		}
		sort.Strings(files)
		var text string
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			text += string(b) + "\n"
		}
		sources[s.Language] = text
	}
	return errorProblems(sources), nil
}

// errorProblems is ErrorCoverage on text already read, so a test can hand it a
// client with a code removed and watch it fail.
func errorProblems(sources map[string]string) map[string][]string {
	want := map[string]mobiumerr.Code{} // name -> code
	for _, c := range mobiumerr.Codes {
		if n := mobiumerr.Name(c); n != "" {
			want[n] = c
		}
	}
	problems := map[string][]string{}
	for _, s := range errorSources {
		src := sources[s.Language]
		consts := map[string]string{}
		for _, m := range goCodeConst.FindAllStringSubmatch(src, -1) {
			consts[m[1]] = m[2]
		}
		have := map[string]string{} // name -> wire code
		for _, m := range s.decl.FindAllStringSubmatch(src, -1) {
			code := m[2]
			if s.Language == "go" {
				code = consts[m[2]]
			}
			have[m[1]] = code
		}
		var out []string
		for name, code := range want {
			got, ok := have[name]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("missing %s (%s)", name, code))
			case got != string(code):
				out = append(out, fmt.Sprintf("%s is mapped to %q, want %q", name, got, code))
			}
		}
		for name := range have {
			if _, ok := want[name]; !ok {
				out = append(out, fmt.Sprintf("%s is not a code the server sends", name))
			}
		}
		if len(out) > 0 {
			sort.Strings(out)
			problems[s.Language] = out
		}
	}
	return problems
}

// UnclassifiedErrors lists every place under internal/ that creates an error
// with no code: a fmt.Errorf or errors.New that does not wrap another error
// with %w. A wrapping one is fine — mobiumerr.CodeOf sees through it to its
// cause's code — but a new error made without mobiumerr.New reaches a client
// as the bare code "error", which is exactly what error codes exist to
// end. mobiumerr itself and this package are exempt; tests are not scanned.
func UnclassifiedErrors(root string) ([]string, error) {
	var out []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "mobiumerr" || n == "apisurface" || n == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			isErrorf := pkg.Name == "fmt" && sel.Sel.Name == "Errorf"
			isNew := pkg.Name == "errors" && sel.Sel.Name == "New"
			if !isErrorf && !isNew {
				return true
			}
			if isErrorf && len(call.Args) > 0 && strings.Contains(literalText(call.Args[0]), "%w") {
				return true
			}
			rel, _ := filepath.Rel(root, fset.Position(call.Pos()).Filename)
			out = append(out, fmt.Sprintf("%s:%d", rel, fset.Position(call.Pos()).Line))
			return true
		})
		return nil
	})
	return out, err
}

// literalText joins the string literals of a format argument, so a format
// split across lines with + is read whole.
func literalText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Value
	case *ast.BinaryExpr:
		return literalText(v.X) + literalText(v.Y)
	case *ast.ParenExpr:
		return literalText(v.X)
	}
	return ""
}
