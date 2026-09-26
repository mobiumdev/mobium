package main

import (
	"encoding/json"
	"testing"

	"github.com/mobiumdev/mobium/internal/agent"
)

// handlePipeMessage answers the handshake locally and forwards only tool
// calls, so the protocol half is testable without a device.

func decode(t *testing.T, msg string) *agent.Response {
	t.Helper()
	return handlePipeMessage([]byte(msg))
}

func TestPipeAnswersInitialize(t *testing.T) {
	resp := decode(t, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if resp == nil || resp.Error != nil {
		t.Fatalf("initialize failed: %+v", resp)
	}
	res, ok := resp.Result.(agent.InitializeResult)
	if !ok {
		t.Fatalf("result is %T", resp.Result)
	}
	if res.ServerInfo.Name != "mobium" || res.ProtocolVersion != agent.ProtocolVersion {
		t.Errorf("serverInfo = %+v, protocol = %q", res.ServerInfo, res.ProtocolVersion)
	}
}

func TestPipeListsTheSameToolsAsMCP(t *testing.T) {
	// The clients read this list; if it diverged from the MCP server's, a
	// client and an agent would see different tools.
	resp := decode(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	res, ok := resp.Result.(agent.ToolsListResult)
	if !ok {
		t.Fatalf("result is %T", resp.Result)
	}
	if len(res.Tools) != len(agent.GetToolSchemas()) {
		t.Errorf("pipe lists %d tools, the tool layer declares %d",
			len(res.Tools), len(agent.GetToolSchemas()))
	}
}

func TestPipeIgnoresNotifications(t *testing.T) {
	// No id means no reply; answering would desynchronise the client.
	if resp := decode(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); resp != nil {
		t.Errorf("a notification produced a response: %+v", resp)
	}
}

func TestPipeReportsParseErrors(t *testing.T) {
	resp := decode(t, `{not json`)
	if resp == nil || resp.Error == nil || resp.Error.Code != agent.ParseError {
		t.Errorf("expected a parse error, got %+v", resp)
	}
}

func TestPipeRejectsUnknownMethods(t *testing.T) {
	resp := decode(t, `{"jsonrpc":"2.0","id":1,"method":"nope"}`)
	if resp == nil || resp.Error == nil || resp.Error.Code != agent.MethodNotFound {
		t.Errorf("expected method-not-found, got %+v", resp)
	}
}

func TestPipeRejectsMalformedToolParams(t *testing.T) {
	resp := decode(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"nope"}`)
	if resp == nil || resp.Error == nil || resp.Error.Code != agent.InvalidParams {
		t.Errorf("expected invalid-params, got %+v", resp)
	}
}

func TestOrEmptyNeverReturnsNil(t *testing.T) {
	// A nil argument map reaches the daemon as JSON null, which the tool
	// layer then cannot index.
	if got := orEmpty(nil); got == nil {
		t.Error("orEmpty(nil) returned nil")
	}
	original := map[string]interface{}{"a": 1}
	if got := orEmpty(original); len(got) != 1 {
		t.Errorf("orEmpty dropped arguments: %v", got)
	}
}

func TestPipeResponsesAreValidJSON(t *testing.T) {
	// Everything the pipe emits is read by a client's JSON parser.
	for _, msg := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"nope"}`,
	} {
		resp := decode(t, msg)
		if resp == nil {
			continue
		}
		if _, err := json.Marshal(resp); err != nil {
			t.Errorf("response to %s does not marshal: %v", msg, err)
		}
	}
}
