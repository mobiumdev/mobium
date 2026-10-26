package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/claim"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/trace"
)

// verdictOnly is a stand-in model that reads one screen and passes.
type verdictOnly struct{ id string }

func (v *verdictOnly) Begin(context.Context, string, string, []claim.Tool) (claim.Step, error) {
	return claim.Step{Calls: []claim.ToolCall{{ID: "1", Name: "trace_screen", Input: json.RawMessage(`{"id":"` + v.id + `"}`)}}}, nil
}

func (v *verdictOnly) Next(context.Context, []claim.Result) (claim.Step, error) {
	return claim.Step{Calls: []claim.ToolCall{{ID: "2", Name: "return_verdict", Input: json.RawMessage(
		`{"status":"passed","summary":"Accept terms is checked","evidence":[{"type":"observation","summary":"` + v.id + ` shows Accept terms checked"}]}`)}}}, nil
}

func TestACheckJudgesARecording(t *testing.T) {
	r := trace.New(trace.Options{Name: "form", Version: "test"})
	id := r.Before("app_set", "set testid=termsCheck", map[string]interface{}{"target": "testid=termsCheck"})
	r.After(id, nil, "testid=termsCheck is now checked")
	r.Frame(id, []byte("\xff\xd8x"), 360, 800, []trace.Box{{Ref: "@e3", Label: "Accept terms", Role: "checkbox, checked", X2: 10, Y2: 10}})
	b, err := r.Zip()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "form.zip")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	was := newClaimSession
	defer func() { newClaimSession = was }()
	newClaimSession = func(string) (claim.Session, error) { return &verdictOnly{id: id}, nil }

	h := &Handlers{}
	res, err := h.claimCheck(context.Background(), map[string]interface{}{"claim": "the terms box was ticked", "record": p})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(CheckView)
	if v.Status != "passed" || len(v.Evidence) != 1 || v.Model != claim.DefaultModel || v.Steps != 2 {
		t.Errorf("view: %+v", v)
	}
	if text := res.Content[0].Text; !strings.HasPrefix(text, "PASSED: ") || !strings.Contains(text, "not proof") {
		t.Errorf("text: %q", text)
	}

	_, err = h.claimCheck(context.Background(), map[string]interface{}{"claim": "x"})
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument || !strings.Contains(mobiumerr.Text(err), "app_trace") {
		t.Errorf("no record: %v", err)
	}
}

// A stateless tool runs beside a call that holds the handlers' lock: a check
// that thinks for minutes held it, and every other call waited behind.
func TestAStatelessCallDoesNotWaitForTheLock(t *testing.T) {
	r := trace.New(trace.Options{Name: "x", Version: "test"})
	id := r.Before("app_map", "map", nil)
	r.After(id, nil, "")
	b, _ := r.Zip()
	p := filepath.Join(t.TempDir(), "x.zip")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{}
	h.mu.Lock() // a device call in progress
	defer h.mu.Unlock()
	done := make(chan *ToolsCallResult, 1)
	params, _ := json.Marshal(map[string]interface{}{"name": "app_recording",
		"arguments": map[string]interface{}{"record": p, "read": "summary"}})
	go func() {
		res, _ := CallTool(params, h)
		done <- res.(*ToolsCallResult)
	}()
	select {
	case res := <-done:
		if !strings.Contains(res.Content[0].Text, `"calls":1`) {
			t.Errorf("summary: %q", res.Content[0].Text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app_recording waited for the lock a device call holds")
	}
	if !StatelessRequest(Request{Method: "tools/call", Params: params}) {
		t.Error("app_recording is not taken for stateless")
	}
	tap, _ := json.Marshal(map[string]interface{}{"name": "app_tap", "arguments": map[string]interface{}{}})
	if StatelessRequest(Request{Method: "tools/call", Params: tap}) {
		t.Error("app_tap was taken for stateless")
	}
}
