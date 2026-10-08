package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// CHALLENGES 286: `mobium mcp` saved a relative path where it was asked and
// answered with it unresolved.
func TestAbsolutePaths(t *testing.T) {
	wd, _ := os.Getwd()
	args := map[string]interface{}{"action": "stop", "path": "out/x.wav", "app": "dev.app"}
	AbsolutePaths("app_audio", args)
	if args["path"] != filepath.Join(wd, "out/x.wav") || args["app"] != "dev.app" {
		t.Errorf("app_audio: %v", args)
	}
	abs := filepath.Join(wd, "already.png")
	args = map[string]interface{}{"path": abs}
	AbsolutePaths("app_screenshot", args)
	if args["path"] != abs {
		t.Errorf("an absolute path changed: %v", args["path"])
	}
	args = map[string]interface{}{"target": "label=x"}
	AbsolutePaths("app_tap", args)
	if args["target"] != "label=x" {
		t.Errorf("a tool with no path argument changed: %v", args)
	}
	batch := map[string]interface{}{"steps": []interface{}{
		map[string]interface{}{"name": "app_screenshot", "arguments": map[string]interface{}{"path": "a.png"}},
		map[string]interface{}{"name": "app_tap", "arguments": map[string]interface{}{"target": "b.png"}},
	}}
	AbsolutePaths("app_batch", batch)
	steps := batch["steps"].([]interface{})
	if got := steps[0].(map[string]interface{})["arguments"].(map[string]interface{})["path"]; got != filepath.Join(wd, "a.png") {
		t.Errorf("a batch step's path: %v", got)
	}
	if got := steps[1].(map[string]interface{})["arguments"].(map[string]interface{})["target"]; got != "b.png" {
		t.Errorf("a batch step's other argument: %v", got)
	}
}

func TestMCPServerResolvesPaths(t *testing.T) {
	wd, _ := os.Getwd()
	raw := absoluteCallParams(json.RawMessage(`{"name":"app_audio","arguments":{"action":"stop","path":"out/x.wav"},"_meta":{"k":1}}`))
	var p ToolsCallParams
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.Arguments["path"] != filepath.Join(wd, "out/x.wav") || p.Meta["k"] == nil {
		t.Errorf("rewritten: %s", raw)
	}
	if got := absoluteCallParams(json.RawMessage(`{not json`)); string(got) != `{not json` {
		t.Errorf("unreadable params changed: %s", got)
	}
}
