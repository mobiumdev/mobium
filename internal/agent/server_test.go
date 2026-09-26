package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// call drives one JSON-RPC message through the shared route, the way both the
// MCP server and the daemon do.
func call(t *testing.T, h *Handlers, msg string) *Response {
	t.Helper()
	var req Request
	if err := json.Unmarshal([]byte(msg), &req); err != nil {
		t.Fatalf("bad test message: %v", err)
	}
	result, rpcErr := Route(req, h, "test", nil)
	if req.ID == nil {
		return nil
	}
	if rpcErr != nil {
		return &Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	}
	return &Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func TestInitialize(t *testing.T) {
	resp := call(t, NewHandlers(), `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	res, ok := resp.Result.(InitializeResult)
	if !ok {
		t.Fatalf("initialize returned %T", resp.Result)
	}
	if res.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocol version = %q", res.ProtocolVersion)
	}
	if res.ServerInfo.Name != "mobium" {
		t.Errorf("server name = %q", res.ServerInfo.Name)
	}
	if res.Capabilities.Tools == nil {
		t.Error("server did not advertise tools capability")
	}
}

func TestInitializedIsANotification(t *testing.T) {
	// No id means no response, even though the method is understood.
	if resp := call(t, NewHandlers(), `{"jsonrpc":"2.0","method":"notifications/initialized"}`); resp != nil {
		t.Errorf("notification produced a response: %+v", resp)
	}
}

func TestToolsList(t *testing.T) {
	resp := call(t, NewHandlers(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	res, ok := resp.Result.(ToolsListResult)
	if !ok {
		t.Fatalf("tools/list returned %T", resp.Result)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"app_devices", "app_map", "app_tap", "app_text", "app_screenshot"} {
		if !contains(names, want) {
			t.Errorf("tools/list is missing %s (has %v)", want, names)
		}
	}
}

func TestUnknownMethodIsAProtocolError(t *testing.T) {
	resp := call(t, NewHandlers(), `{"jsonrpc":"2.0","id":1,"method":"nope"}`)
	if resp.Error == nil {
		t.Fatal("expected a JSON-RPC error")
	}
	if resp.Error.Code != MethodNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, MethodNotFound)
	}
}

func TestUnknownToolIsAToolError(t *testing.T) {
	// A tool that cannot run is a successful call reporting a failure — the
	// agent needs to read why, and a JSON-RPC error would hide it.
	resp := call(t, NewHandlers(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"app_nope","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("unknown tool produced a protocol error: %+v", resp.Error)
	}
	res, ok := resp.Result.(ToolsCallResult)
	if !ok {
		t.Fatalf("tools/call returned %T", resp.Result)
	}
	if !res.IsError {
		t.Error("unknown tool did not set isError")
	}
	if !strings.Contains(res.Content[0].Text, "app_map") {
		t.Errorf("error does not list the available tools: %q", res.Content[0].Text)
	}
}

func TestMalformedToolParamsIsAProtocolError(t *testing.T) {
	resp := call(t, NewHandlers(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"not an object"}`)
	if resp.Error == nil || resp.Error.Code != InvalidParams {
		t.Errorf("expected InvalidParams, got %+v", resp.Error)
	}
}

func TestExtraRouteWins(t *testing.T) {
	// The daemon adds its own methods through the extra hook; they must take
	// precedence and everything else must still fall through.
	extra := func(req Request) (interface{}, *Error, bool) {
		if req.Method == "daemon/status" {
			return "ok", nil, true
		}
		return nil, nil, false
	}
	var req Request
	json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"method":"daemon/status"}`), &req)
	result, err := Route(req, NewHandlers(), "test", extra)
	if err != nil || result != "ok" {
		t.Errorf("extra route not used: result=%v err=%v", result, err)
	}

	json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), &req)
	if _, err := Route(req, NewHandlers(), "test", extra); err != nil {
		t.Errorf("shared route broken by extra hook: %v", err)
	}
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
