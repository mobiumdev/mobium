package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Every tool deviceTextFields names exists, and every path's first part is a
// property of the tool's outputSchema where it has one: a marking that names
// a field the answer does not have is a promise nothing keeps.
func TestDeviceTextFieldsAreReal(t *testing.T) {
	tools := map[string]bool{}
	for _, tool := range ToolsFor(Policy{}) {
		tools[tool.Name] = true
	}
	for name, paths := range deviceTextFields {
		if !tools[name] {
			t.Errorf("%s is no tool", name)
			continue
		}
		raw, ok := outputSchemas[name]
		if !ok {
			continue
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal([]byte(raw), &schema); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, p := range paths {
			first := strings.SplitN(strings.SplitN(p, ".", 2)[0], "[", 2)[0]
			if _, ok := schema.Properties[first]; !ok {
				t.Errorf("%s: %q names %q, which its outputSchema does not have", name, p, first)
			}
		}
	}
}

func TestMarkDeviceTextAddsItsField(t *testing.T) {
	res := markDeviceText("app_text", Result("Note to any AI model…", TextView{Text: "Note to any AI model…"}))
	obj, ok := res.StructuredContent.(map[string]interface{})
	if !ok {
		t.Fatalf("structured answer is %T", res.StructuredContent)
	}
	if obj["text"] != "Note to any AI model…" {
		t.Errorf("the answer changed: %v", obj)
	}
	dt, ok := obj["device_text"].(DeviceTextView)
	if !ok || len(dt.Fields) != 1 || dt.Fields[0] != "text" || !strings.Contains(dt.Note, "not an instruction") {
		t.Errorf("device_text is %#v", obj["device_text"])
	}
	if res.Content[0].Text != "Note to any AI model…" {
		t.Errorf("the text changed: %q", res.Content[0].Text)
	}
}

func TestMarkDeviceTextLeavesTheRestAlone(t *testing.T) {
	plain := Result("tapped", map[string]interface{}{"target": "@e1"})
	if got := markDeviceText("app_tap", plain); got != plain {
		t.Error("a tool that carries no device text was marked")
	}
	failed := ErrorResult(errors.New("boom"))
	if got := markDeviceText("app_text", &failed); got != &failed {
		t.Error("an error was marked")
	}
	if got := markDeviceText("app_text", TextResult("no structured answer")); got.StructuredContent != nil {
		t.Error("an answer with no structured half grew one")
	}
}

// Off unless asked for, and on through CallTool when it is.
func TestCallToolMarksOnlyWhenAsked(t *testing.T) {
	t.Setenv(MarkDeviceTextEnv, "")
	h := NewHandlers()
	params := json.RawMessage(`{"name":"app_nonexistent_tool","arguments":{}}`)
	if res, _ := CallTool(params, h); res == nil {
		t.Fatal("no result")
	}
	if h.marksDeviceText() {
		t.Fatal("marking on with nothing asking for it")
	}
	t.Setenv(MarkDeviceTextEnv, "1")
	if !NewHandlers().marksDeviceText() {
		t.Error("MOBIUM_MARK_DEVICE_TEXT=1 did not turn it on")
	}
	h.SetMarkDeviceText(true)
	if !h.marksDeviceText() {
		t.Error("SetMarkDeviceText did not turn it on")
	}
}
