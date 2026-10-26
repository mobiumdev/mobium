// Command outputschema writes internal/agent/outputschema_gen.go: each tool's
// MCP outputSchema, the shape of the structuredContent a successful call
// returns, worked out from the code by Go's own type checker.
//
// A client may validate every result against a tool's outputSchema and
// refuse one that does not match, so a wrong schema is worse than none. A
// tool gets one only when every way its handler can succeed returns the same
// concrete type: dispatch names the handler; each return is followed through
// Result(text, v), which gives v's static type, and through the handlers'
// own methods it delegates to. Anything else — TextResult, a result built
// elsewhere, two types — leaves the tool without a schema, and the run says
// why for each. The schema follows encoding/json: field names from json tags,
// omitempty fields optional and the rest required, pointers, slices, maps and
// interfaces nullable, and a type with its own MarshalJSON anything.
//
//	go run ./internal/apisurface/outputschema .   (make api runs it)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	dir := filepath.Join(root, "internal", "agent")
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go") && fi.Name() != "outputschema_gen.go"
	}, parser.ParseComments)
	check(err)
	var files []*ast.File
	for _, f := range pkgs["agent"].Files {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return fset.File(files[i].Pos()).Name() < fset.File(files[j].Pos()).Name() })
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	// The file this writes is left out, so the names it defines are the one
	// thing the package may be missing; any other type error is real.
	var typeErrs []string
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil), Error: func(e error) {
		if msg := e.Error(); !strings.Contains(msg, "undefined: outputSchemas") && !strings.Contains(msg, "undefined: outputZeros") {
			typeErrs = append(typeErrs, msg)
		}
	}}
	_, _ = conf.Check("github.com/mobiumdev/mobium/internal/agent", fset, files, info)
	if len(typeErrs) > 0 {
		check(fmt.Errorf("internal/agent does not type-check:\n%s", strings.Join(typeErrs, "\n")))
	}

	methods := map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Body != nil && recvIsHandlers(fd) {
				methods[fd.Name.Name] = fd
			}
		}
	}
	tools := dispatchTable(methods["dispatch"])

	a := &analysis{info: info, methods: methods, memo: map[string]outcome{}}
	schemas := map[string]json.RawMessage{}
	zeros := map[string]string{}
	imports := map[string]bool{}
	var report []string
	names := make([]string, 0, len(tools))
	for n := range tools {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, tool := range names {
		o := a.method(tools[tool])
		switch {
		case o.why != "":
			report = append(report, fmt.Sprintf("%-24s no schema: %s", tool, o.why))
		case len(o.types) != 1:
			var ts []string
			for _, t := range o.types {
				ts = append(ts, t.String())
			}
			sort.Strings(ts)
			report = append(report, fmt.Sprintf("%-24s no schema: %d result types (%s)", tool, len(ts), strings.Join(ts, ", ")))
		default:
			s := schemaOf(o.types[0], map[types.Type]bool{})
			if s["type"] != "object" {
				report = append(report, fmt.Sprintf("%-24s no schema: its result is not an object", tool))
				continue
			}
			raw, err := json.Marshal(s)
			check(err)
			schemas[tool] = raw
			if n, ok := types.Unalias(o.types[0]).(*types.Named); ok && n.Obj().Pkg() != nil {
				if pkg := n.Obj().Pkg(); pkg.Name() == "agent" {
					zeros[tool] = n.Obj().Name()
				} else if strings.HasPrefix(pkg.Path(), "github.com/mobiumdev/mobium/") {
					zeros[tool] = pkg.Name() + "." + n.Obj().Name()
					imports[pkg.Path()] = true
				}
			}
			report = append(report, fmt.Sprintf("%-24s %s", tool, o.types[0]))
		}
	}
	write(filepath.Join(dir, "outputschema_gen.go"), schemas, zeros, imports)
	fmt.Println(strings.Join(report, "\n"))
	fmt.Printf("%d of %d tools have an output schema\n", len(schemas), len(names))
}

func recvIsHandlers(fd *ast.FuncDecl) bool {
	if len(fd.Recv.List) != 1 {
		return false
	}
	star, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == "Handlers"
}

// dispatchTable maps each tool to the method its dispatch case returns.
func dispatchTable(fd *ast.FuncDecl) map[string]string {
	out := map[string]string{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok || len(cc.Body) != 1 {
			return true
		}
		ret, ok := cc.Body[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) == 0 {
			return true
		}
		m := handlerCall(ret.Results[0])
		for _, e := range cc.List {
			if lit, ok := e.(*ast.BasicLit); ok && m != "" {
				out[strings.Trim(lit.Value, `"`)] = m
			}
		}
		return true
	})
	return out
}

// handlerCall is the method name of a call h.M(...), or "".
func handlerCall(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if id, ok := sel.X.(*ast.Ident); ok && id.Name == "h" {
		return sel.Sel.Name
	}
	return ""
}

type outcome struct {
	types []types.Type
	why   string
}

type analysis struct {
	info    *types.Info
	methods map[string]*ast.FuncDecl
	memo    map[string]outcome
}

func (a *analysis) method(name string) outcome {
	if o, ok := a.memo[name]; ok {
		return o
	}
	a.memo[name] = outcome{why: "it delegates back to itself"} // a cycle stops here
	fd := a.methods[name]
	if fd == nil {
		return outcome{why: "its handler is not a method of Handlers"}
	}
	var o outcome
	add := func(t types.Type) {
		for _, have := range o.types {
			if types.Identical(have, t) {
				return
			}
		}
		o.types = append(o.types, t)
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if o.why != "" {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false // a closure's returns are not the handler's
		}
		// A return is (result, err) or one call yielding both.
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) == 0 || len(ret.Results) > 2 {
			return true
		}
		first := ret.Results[0]
		if id, ok := first.(*ast.Ident); ok && id.Name == "nil" {
			return true // the error path: a failed call is isError, not this schema's
		}
		if call, ok := first.(*ast.CallExpr); ok {
			if fn, ok := call.Fun.(*ast.Ident); ok {
				switch fn.Name {
				case "Result":
					if len(call.Args) == 2 {
						if id, ok := call.Args[1].(*ast.Ident); ok && id.Name == "nil" {
							o.why = "a return gives no structured content"
							return false
						}
						t := a.info.TypeOf(call.Args[1])
						if t == nil {
							o.why = "a result's type could not be read"
							return false
						}
						add(t)
						return false
					}
				case "TextResult":
					o.why = "a return gives text only"
					return false
				}
			}
			if m := handlerCall(call); m != "" {
				sub := a.method(m)
				if sub.why != "" {
					o.why = "it delegates to " + m + ", where " + sub.why
					return false
				}
				for _, t := range sub.types {
					add(t)
				}
				return false
			}
		}
		o.why = "a return's result is built elsewhere (" + exprString(first) + ")"
		return false
	})
	a.memo[name] = o
	return o
}

func exprString(e ast.Expr) string {
	var b bytes.Buffer
	_ = format.Node(&b, token.NewFileSet(), e)
	s := b.String()
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return s
}

var marshaler = types.NewInterfaceType([]*types.Func{
	types.NewFunc(token.NoPos, nil, "MarshalJSON", types.NewSignatureType(nil, nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "", types.NewSlice(types.Typ[types.Byte])),
			types.NewVar(token.NoPos, nil, "", types.Universe.Lookup("error").Type())), false)),
}, nil).Complete()

// schemaOf is t's JSON Schema, as encoding/json would write a value of it.
func schemaOf(t types.Type, seen map[types.Type]bool) map[string]interface{} {
	if types.Implements(t, marshaler) || types.Implements(types.NewPointer(t), marshaler) {
		return map[string]interface{}{} // its own encoding: anything
	}
	switch u := t.Underlying().(type) {
	case *types.Pointer:
		return nullable(schemaOf(u.Elem(), seen))
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return map[string]interface{}{"type": "boolean"}
		case u.Info()&types.IsInteger != 0:
			return map[string]interface{}{"type": "integer"}
		case u.Info()&types.IsFloat != 0:
			return map[string]interface{}{"type": "number"}
		case u.Info()&types.IsString != 0:
			return map[string]interface{}{"type": "string"}
		}
		return map[string]interface{}{}
	case *types.Slice:
		if b, ok := u.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Byte {
			return nullable(map[string]interface{}{"type": "string"}) // base64
		}
		return nullable(map[string]interface{}{"type": "array", "items": schemaOf(u.Elem(), seen)})
	case *types.Array:
		return map[string]interface{}{"type": "array", "items": schemaOf(u.Elem(), seen)}
	case *types.Map:
		return nullable(map[string]interface{}{"type": "object", "additionalProperties": schemaOf(u.Elem(), seen)})
	case *types.Interface:
		return map[string]interface{}{}
	case *types.Struct:
		if seen[t] {
			return map[string]interface{}{"type": "object"}
		}
		seen[t] = true
		defer delete(seen, t)
		props := map[string]interface{}{}
		var required []string
		fields(u, seen, props, &required)
		s := map[string]interface{}{"type": "object", "properties": props}
		if len(required) > 0 {
			sort.Strings(required)
			s["required"] = required
		}
		return s
	}
	return map[string]interface{}{}
}

// fields adds a struct's JSON fields, embedded structs' among them.
func fields(st *types.Struct, seen map[types.Type]bool, props map[string]interface{}, required *[]string) {
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		tag := reflect.StructTag(st.Tag(i)).Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Embedded() && name == "" {
			if inner, ok := f.Type().Underlying().(*types.Struct); ok {
				fields(inner, seen, props, required)
				continue
			}
		}
		if !f.Exported() {
			continue
		}
		if name == "" {
			name = f.Name()
		}
		props[name] = schemaOf(f.Type(), seen)
		if !strings.Contains(opts, "omitempty") {
			*required = append(*required, name)
		}
	}
}

func nullable(s map[string]interface{}) map[string]interface{} {
	if t, ok := s["type"].(string); ok {
		s["type"] = []interface{}{t, "null"}
	}
	return s
}

func write(path string, schemas map[string]json.RawMessage, zeros map[string]string, imports map[string]bool) {
	var b bytes.Buffer
	b.WriteString("// Code generated by internal/apisurface/outputschema; DO NOT EDIT.\n\n")
	b.WriteString("package agent\n\n")
	if len(imports) > 0 {
		var paths []string
		for ip := range imports {
			paths = append(paths, ip)
		}
		sort.Strings(paths)
		b.WriteString("import (\n")
		for _, ip := range paths {
			fmt.Fprintf(&b, "\t%q\n", ip)
		}
		b.WriteString(")\n\n")
	}
	b.WriteString("// outputSchemas are the tools' MCP outputSchema, each the shape of the\n")
	b.WriteString("// structuredContent every successful call of the tool returns.\n")
	b.WriteString("var outputSchemas = map[string]string{\n")
	names := make([]string, 0, len(schemas))
	for n := range schemas {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "\t%q: %q,\n", n, string(schemas[n]))
	}
	b.WriteString("}\n\n")
	b.WriteString("// outputZeros make each schema's type, for the test that holds a value of\n")
	b.WriteString("// it, as encoding/json writes it, to the schema.\n")
	b.WriteString("var outputZeros = map[string]func() interface{}{\n")
	for _, n := range names {
		if z, ok := zeros[n]; ok {
			fmt.Fprintf(&b, "\t%q: func() interface{} { return %s{} },\n", n, z)
		}
	}
	b.WriteString("}\n")
	out, err := format.Source(b.Bytes())
	check(err)
	check(os.WriteFile(path, out, 0o644))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
