package apisurface

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/agent"
)

// The JavaScript client's declarations are written by hand, and a parameter
// that takes one of a tool's enum values is declared as a union of string
// literals — press(button: 'back' | 'home' | ...). When the tool's enum grows,
// nothing else notices: the code passes any string through, so only a
// TypeScript user finds out, as a type error on a value the tool accepts.
// press stayed at its first five buttons after the D-pad and media keys were
// added for the TV.

// toolEnums is every string enum in the tool schemas, by tool.argument.
func toolEnums() map[string][]string {
	out := map[string][]string{}
	for _, t := range agent.GetToolSchemas() {
		props, _ := t.InputSchema["properties"].(map[string]interface{})
		for name, spec := range props {
			m, _ := spec.(map[string]interface{})
			// Built in Go the enum is a []string; read back from JSON it
			// would be []interface{}. Either way it is the same list.
			var vals []string
			switch raw := m["enum"].(type) {
			case []string:
				vals = raw
			case []interface{}:
				for _, v := range raw {
					if s, ok := v.(string); ok {
						vals = append(vals, s)
					}
				}
			}
			if len(vals) > 1 {
				out[t.Name+"."+name] = vals
			}
		}
	}
	return out
}

var literalUnion = regexp.MustCompile(`'[^'\n]+'(?:\s*\|\s*'[^'\n]+')+`)

// staleUnions names every string-literal union in dts that shares at least
// two values with a tool's enum without listing exactly that enum. Two shared
// values is what says the union is meant for the enum; a union for something
// else shares none.
func staleUnions(dts string, enums map[string][]string) []string {
	var problems []string
	for _, u := range literalUnion.FindAllString(dts, -1) {
		var lits []string
		for _, part := range strings.Split(u, "|") {
			lits = append(lits, strings.Trim(strings.TrimSpace(part), "'"))
		}
		have := map[string]bool{}
		for _, l := range lits {
			have[l] = true
		}
		for arg, vals := range enums {
			shared := 0
			for _, v := range vals {
				if have[v] {
					shared++
				}
			}
			if shared < 2 {
				continue
			}
			want := map[string]bool{}
			for _, v := range vals {
				want[v] = true
			}
			var missing, extra []string
			for _, v := range vals {
				if !have[v] {
					missing = append(missing, v)
				}
			}
			for _, l := range lits {
				if !want[l] {
					extra = append(extra, l)
				}
			}
			if len(missing)+len(extra) > 0 {
				sort.Strings(missing)
				sort.Strings(extra)
				problems = append(problems, arg+": the union "+u+" lacks ["+strings.Join(missing, ", ")+
					"] and has ["+strings.Join(extra, ", ")+"] that the tool does not")
			}
		}
	}
	return problems
}

// TestJavaScriptUnionsMatchTheToolEnums holds index.d.ts to the schemas.
func TestJavaScriptUnionsMatchTheToolEnums(t *testing.T) {
	dts, err := os.ReadFile(repoRoot + "/clients/javascript/index.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	enums := toolEnums()
	if len(enums) == 0 {
		t.Fatal("no string enums found in the tool schemas: the check would pass on nothing")
	}
	for _, p := range staleUnions(string(dts), enums) {
		t.Error(p)
	}
}

// TestTheUnionCheckCanFail: a declaration with press's first five buttons,
// against the schema as it is now, must be reported.
func TestTheUnionCheckCanFail(t *testing.T) {
	stale := `press(button: 'back' | 'home' | 'recents' | 'volume-up' | 'volume-down'): Promise<void>`
	got := staleUnions(stale, toolEnums())
	if len(got) == 0 {
		t.Fatal("a union missing the D-pad and media keys was not reported")
	}
	if !strings.Contains(got[0], "dpad-down") {
		t.Errorf("the report does not name what is missing: %s", got[0])
	}
}
