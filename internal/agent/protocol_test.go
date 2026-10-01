package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContentAlwaysCarriesTextField(t *testing.T) {
	// An empty text block must still serialize a "text" key. With plain
	// omitempty it becomes {"type":"text"}, which MCP clients reject as an
	// invalid union — and empty results are routine here (an element with no
	// label, a screen with nothing readable).
	data, err := json.Marshal(Content{Type: "text", Text: ""})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"type":"text","text":""}` {
		t.Errorf("empty text block marshalled as %s", data)
	}
}

func TestContentImageOmitsText(t *testing.T) {
	data, err := json.Marshal(Content{Type: "image", Data: "AAAA", MimeType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "text") {
		t.Errorf("image block carried a text field: %s", got)
	}
	if !strings.Contains(got, `"mimeType":"image/png"`) {
		t.Errorf("image block missing mimeType: %s", got)
	}
}

func TestToolSchemasAreWellFormed(t *testing.T) {
	schemas := GetToolSchemas()
	if len(schemas) == 0 {
		t.Fatal("no tools declared")
	}
	seen := map[string]bool{}
	for _, tool := range schemas {
		if seen[tool.Name] {
			t.Errorf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true

		if !strings.HasPrefix(tool.Name, "app_") {
			t.Errorf("tool %q does not use the app_ prefix", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("tool %q has no description", tool.Name)
		}
		if tool.InputSchema["type"] != "object" {
			t.Errorf("tool %q schema is not an object", tool.Name)
		}
		if _, ok := tool.InputSchema["properties"].(map[string]interface{}); !ok {
			t.Errorf("tool %q schema has no properties map", tool.Name)
		}
		// The schema has to survive a round trip, or tools/list is unusable.
		if _, err := json.Marshal(tool); err != nil {
			t.Errorf("tool %q does not marshal: %v", tool.Name, err)
		}
	}
}

func TestEveryDeviceToolAcceptsDeviceArg(t *testing.T) {
	// The exceptions are tools that deliberately touch no device the call is
	// pinned to. Each is listed by name rather than the rule being relaxed,
	// so a new tool that forgets the device argument still fails here.
	deviceless := map[string]string{
		"app_devices":  "lists all devices; pinning one would be meaningless",
		"app_doctor":   "checks the environment, and must work when no device exists",
		"app_boot":     "starts a device by name; none is running to pin it to",
		"app_shutdown": "names its device by name, since a pipe pins every call to its own device",
	}
	for _, tool := range GetToolSchemas() {
		if _, skip := deviceless[tool.Name]; skip {
			continue
		}
		props := tool.InputSchema["properties"].(map[string]interface{})
		if _, ok := props["device"]; !ok {
			t.Errorf("tool %q cannot be pinned to a device", tool.Name)
		}
	}
}

func TestToolNamesMatchSchemas(t *testing.T) {
	names := ToolNames()
	if len(names) != len(GetToolSchemas()) {
		t.Fatalf("ToolNames returned %d names for %d schemas", len(names), len(GetToolSchemas()))
	}
}

func TestContextToolsAreDeclared(t *testing.T) {
	names := ToolNames()
	for _, want := range []string{"app_contexts", "app_context"} {
		if !containsName(names, want) {
			t.Errorf("%s is not declared (have %v)", want, names)
		}
	}
}

func containsName(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
