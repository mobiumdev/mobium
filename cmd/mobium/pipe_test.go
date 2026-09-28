package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/agent"
)

// The pipe answers the handshake locally and forwards only tool
// calls, so the protocol half is testable without a device.

func decode(t *testing.T, msg string) *agent.Response {
	t.Helper()
	return newPipe(daemonCall).handle([]byte(msg))
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

// fakeDaemon answers app_session as the daemon does, and records every call.
type fakeDaemon struct {
	calls []string
	open  map[string]bool
}

func (f *fakeDaemon) call(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
	device, _ := args["device"].(string)
	action, _ := args["action"].(string)
	f.calls = append(f.calls, tool+" "+action+" "+device)
	if tool != "app_session" {
		return agent.Result("ok", nil), nil
	}
	view := agent.SessionView{Action: action, Device: device}
	switch action {
	case "start":
		view.Reused = f.open[device]
		f.open[device] = true
	case "end":
		view.Ended = f.open[device]
		delete(f.open, device)
	}
	return agent.Result(action, view), nil
}

func (f *fakeDaemon) ended() []string {
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, "app_session end ") {
			out = append(out, strings.TrimPrefix(c, "app_session end "))
		}
	}
	return out
}

func startCall(device string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"app_session","arguments":{"action":"start","device":"` + device + `"}}}`
}

// A client that goes away without quit() — a crash, or a script that just
// returns — closes stdin exactly as close() does. The session it started
// used to stay open until the daemon went idle for 30 minutes.
func TestPipeEndsTheSessionsItsClientStarted(t *testing.T) {
	f := &fakeDaemon{open: map[string]bool{}}
	p := newPipe(f.call)
	p.handle([]byte(startCall("emulator-5554")))
	p.handle([]byte(startCall("SIM-1")))
	p.release()
	if got := f.ended(); strings.Join(got, ",") != "SIM-1,emulator-5554" {
		t.Errorf("ended %v, want both sessions the client started", got)
	}
	// Once: SIGTERM and the deferred release can both reach it.
	p.release()
	if got := f.ended(); len(got) != 2 {
		t.Errorf("a second release ended again: %v", got)
	}
}

func TestPipeLeavesWhatItDidNotStartOrWasToldToKeep(t *testing.T) {
	// A session already open when the client started it belongs to whoever
	// opened it — another client, or the CLI.
	f := &fakeDaemon{open: map[string]bool{"emulator-5554": true}}
	p := newPipe(f.call)
	p.handle([]byte(startCall("emulator-5554")))
	p.release()
	if got := f.ended(); len(got) != 0 {
		t.Errorf("ended a reused session: %v", got)
	}

	// One the client ended itself is not ended twice.
	f = &fakeDaemon{open: map[string]bool{}}
	p = newPipe(f.call)
	p.handle([]byte(startCall("emulator-5554")))
	p.handle([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"app_session","arguments":{"action":"end","device":"emulator-5554"}}}`))
	p.release()
	if got := f.ended(); len(got) != 1 {
		t.Errorf("ended %v, want only the client's own quit", got)
	}

	// close() detaches first: the documented "close leaves the session open".
	f = &fakeDaemon{open: map[string]bool{}}
	p = newPipe(f.call)
	p.handle([]byte(startCall("emulator-5554")))
	if resp := p.handle([]byte(`{"jsonrpc":"2.0","method":"` + DetachMethod + `"}`)); resp != nil {
		t.Errorf("detach, a notification, was answered: %+v", resp)
	}
	p.release()
	if got := f.ended(); len(got) != 0 {
		t.Errorf("a detached client's session was ended: %v", got)
	}
}
