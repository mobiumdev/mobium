package agent

import "testing"

// Every tool is decided: read-only, read unless given a setting, or acting —
// exactly one — and every argument named as acting is one the tool takes.
func TestEveryToolIsReadOrActDecided(t *testing.T) {
	props := map[string]map[string]interface{}{}
	for _, tool := range GetToolSchemas() {
		p, _ := tool.InputSchema["properties"].(map[string]interface{})
		props[tool.Name] = p
		n := 0
		if readOnlyTools[tool.Name] {
			n++
		}
		if _, ok := readUnless[tool.Name]; ok {
			n++
		}
		if actingTools[tool.Name] {
			n++
		}
		if n != 1 {
			t.Errorf("%s is in %d of the read/act sets, want exactly 1 — decide it in readonly.go", tool.Name, n)
		}
		if tool.Annotations["readOnlyHint"] != readOnlyTools[tool.Name] {
			t.Errorf("%s's readOnlyHint is %v", tool.Name, tool.Annotations["readOnlyHint"])
		}
	}
	for name, acts := range readUnless {
		for _, a := range acts {
			if _, ok := props[name][a]; !ok {
				t.Errorf("%s has no argument %q, which readUnless says makes it act", name, a)
			}
		}
	}
	for _, sets := range []map[string]bool{readOnlyTools, actingTools} {
		for name := range sets {
			if _, ok := props[name]; !ok {
				t.Errorf("%s is classified and is not a tool", name)
			}
		}
	}
}

func TestIsReadCall(t *testing.T) {
	for _, c := range []struct {
		tool string
		args map[string]interface{}
		want bool
	}{
		{"app_state", map[string]interface{}{"app": "x"}, true},
		{"app_network", map[string]interface{}{}, true},
		{"app_network", map[string]interface{}{"offline": true}, false},
		{"app_accessibility", map[string]interface{}{"setting": "bold_text"}, true},
		{"app_accessibility", map[string]interface{}{"setting": "bold_text", "value": "on"}, false},
		{"app_tap", map[string]interface{}{"target": "@e1"}, false},
		{"app_nonsense", map[string]interface{}{}, false},
	} {
		if got := IsReadCall(c.tool, c.args); got != c.want {
			t.Errorf("IsReadCall(%s, %v) = %v", c.tool, c.args, got)
		}
	}
}
