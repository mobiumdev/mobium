package claim

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/trace"
)

// recording is a real Mobium trace, made by the recorder that `mobium trace`
// uses: a tap on Accept terms that ticked it, then a wait that read it back.
func recording(t *testing.T) string {
	t.Helper()
	r := trace.New(trace.Options{Name: "form", Device: "emulator-5554", Platform: "android", Version: "test"})
	id := r.Before("app_tap", "tap testid=termsCheck", map[string]interface{}{"target": "testid=termsCheck"})
	r.Input(id, 120, 400, &trace.Box{Ref: "@e3", Label: "Accept terms", Role: "checkbox", X1: 20, Y1: 380, X2: 300, Y2: 420})
	r.After(id, nil, "tapped testid=termsCheck at (120, 400)")
	r.Frame(id, []byte("\xff\xd8jpeg-one"), 360, 800, []trace.Box{
		{Ref: "@e3", Label: "Accept terms", Role: "checkbox, checked", X1: 20, Y1: 380, X2: 300, Y2: 420},
	})
	id2 := r.Before("app_wait_for", "wait testid=status", map[string]interface{}{"target": "testid=status"})
	r.After(id2, os.ErrDeadlineExceeded, "")
	b, err := r.Zip()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "form.zip")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestATraceIsReadAsCallsAndScreens(t *testing.T) {
	tr, err := Open(context.Background(), recording(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if len(tr.Calls) != 2 || tr.Calls[0].Tool != "app_tap" || tr.Calls[1].Error == "" {
		t.Fatalf("calls: %+v", tr.Calls)
	}
	c := tr.Calls[0]
	if c.Touched == nil || c.Touched.X != 120 || !strings.Contains(c.Result, "tapped") {
		t.Errorf("tap: %+v", c)
	}
	if len(c.Screen) != 1 || c.Screen[0].Title != "@e3 Accept terms (checkbox, checked)" || !strings.HasPrefix(c.Screen[0].Box, "left ") {
		t.Errorf("screen after the tap: %+v", c.Screen)
	}
	img, err := tr.Image(c.ID)
	if err != nil || !bytes.HasPrefix(img, []byte("\xff\xd8")) {
		t.Errorf("screenshot: %q %v", img, err)
	}
	if img, _ := tr.Image(tr.Calls[1].ID); img != nil {
		t.Error("a screenshot for a call that kept none")
	}
}

func TestAnUnsafeArchiveIsRefused(t *testing.T) {
	for name, entries := range map[string][]string{
		"no trace":  {"other.txt"},
		"traversal": {"trace.trace", "../evil"},
	} {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, e := range entries {
			w, _ := zw.Create(e)
			w.Write([]byte(`{"type":"context-options","version":8}` + "\n"))
		}
		zw.Close()
		p := filepath.Join(t.TempDir(), "x.zip")
		os.WriteFile(p, buf.Bytes(), 0o600)
		if _, err := Open(context.Background(), p); err == nil {
			t.Errorf("%s: opened", name)
		}
	}
}

func TestTheToolsAnswerFromTheRecording(t *testing.T) {
	tr, _ := Open(context.Background(), recording(t))
	defer tr.Close()
	id := tr.Calls[0].ID
	ask := func(name string, in interface{}) Observation {
		b, _ := json.Marshal(in)
		return tr.Execute(context.Background(), name, b)
	}
	if o := ask("trace_summary", map[string]interface{}{}); !strings.Contains(o.Text, `"calls":2`) || !strings.Contains(o.Text, "app_wait_for") {
		t.Errorf("summary: %s", o.Text)
	}
	if o := ask("trace_screen", map[string]interface{}{"id": id}); !strings.Contains(o.Text, "Accept terms (checkbox, checked)") {
		t.Errorf("screen: %s", o.Text)
	}
	if o := ask("trace_screenshot", map[string]interface{}{"id": id}); o.JPEG == nil {
		t.Error("no image")
	}
	if o := ask("trace_search", map[string]interface{}{"query": "ACCEPT TERMS"}); !strings.Contains(o.Text, id) {
		t.Errorf("search: %s", o.Text)
	}
	if o := ask("trace_inspect_call", map[string]interface{}{"id": "call@99"}); !o.IsError {
		t.Error("an unknown id was answered")
	}
}

// script is a stand-in model: it makes the calls it was given, in order.
type script struct {
	steps   [][]ToolCall
	results [][]Result
}

func (s *script) Begin(context.Context, string, string, []Tool) (Step, error) { return s.next(), nil }
func (s *script) Next(_ context.Context, r []Result) (Step, error) {
	s.results = append(s.results, r)
	return s.next(), nil
}
func (s *script) next() Step {
	if len(s.steps) == 0 {
		return Step{}
	}
	st := s.steps[0]
	s.steps = s.steps[1:]
	return Step{Calls: st}
}

func call(name, input string) ToolCall {
	return ToolCall{ID: name, Name: name, Input: json.RawMessage(input)}
}

func TestAVerdictComesFromTheModelAndRestsOnEvidence(t *testing.T) {
	tr, _ := Open(context.Background(), recording(t))
	defer tr.Close()
	id := tr.Calls[0].ID
	s := &script{steps: [][]ToolCall{
		{call("trace_screen", `{"id":"`+id+`"}`)},
		{call(verdictTool, `{"status":"passed","summary":"ticked","evidence":[]}`)}, // refused: no evidence
		{call(verdictTool, `{"status":"passed","summary":"Accept terms is checked after the tap","evidence":[{"type":"observation","summary":"`+id+` screen: Accept terms (checkbox, checked)"}]}`)},
	}}
	v, err := Check(context.Background(), tr, "the terms box ends up ticked", s)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "passed" || len(v.Evidence) != 1 || v.Steps != 3 {
		t.Errorf("verdict: %+v", v)
	}
	if got := s.results[1][0]; !got.IsError || !strings.Contains(got.Text, "needs observable evidence") {
		t.Errorf("an evidence-less pass was not sent back: %+v", got)
	}
}

func TestTheLoopEndsInconclusive(t *testing.T) {
	tr, _ := Open(context.Background(), recording(t))
	defer tr.Close()
	many := make([][]ToolCall, maxSteps+5)
	for i := range many {
		many[i] = []ToolCall{call("trace_summary", `{}`)}
	}
	for name, s := range map[string]Session{
		"limit":      &script{steps: many},
		"no verdict": &script{steps: [][]ToolCall{{call("trace_summary", `{}`)}}},
		"malformed": &script{steps: [][]ToolCall{
			{call(verdictTool, `{"status":"maybe"}`)}, {call(verdictTool, `{"status":"maybe"}`)}, {call(verdictTool, `{"status":"maybe"}`)},
		}},
	} {
		v, err := Check(context.Background(), tr, "anything", s)
		if err != nil || v.Status != "inconclusive" {
			t.Errorf("%s: %+v %v", name, v, err)
		}
	}
	if _, err := Check(context.Background(), tr, "  ", &script{}); err == nil {
		t.Error("an empty claim was accepted")
	}
}

func TestAVerdictMustCiteCallsTheRecordingHolds(t *testing.T) {
	tr, _ := Open(context.Background(), recording(t))
	defer tr.Close()
	id := tr.Calls[0].ID
	for name, c := range map[string]struct {
		input string
		ok    bool
	}{
		"cites a real call":   {`{"status":"passed","summary":"s","evidence":[{"type":"observation","summary":"` + id + ` shows it checked"}]}`, true},
		"cites no call":       {`{"status":"failed","summary":"s","evidence":[{"type":"observation","summary":"it looked unchecked"}]}`, false},
		"cites a call absent": {`{"status":"passed","summary":"s","evidence":[{"type":"observation","summary":"call@99 shows it"}]}`, false},
		"inconclusive, none":  {`{"status":"inconclusive","summary":"cannot tell","evidence":[]}`, true},
		// mistral-small's shape: the id in a key of its own, which is not read
		"cites in a key": {`{"status":"passed","summary":"s","evidence":[{"type":"observation","call":"` + id + `","summary":"` + id + ` shows it"}]}`, false},
	} {
		_, err := Validate(tr, "the box is ticked", json.RawMessage(c.input))
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// Every turn of the model is reported before it is asked: the CLI's read
// gives up after two minutes of silence, and a local model's check ran past.
func TestACheckReportsEachTurn(t *testing.T) {
	tr, _ := Open(context.Background(), recording(t))
	defer tr.Close()
	id := tr.Calls[0].ID
	s := &script{steps: [][]ToolCall{
		{call("trace_screen", `{"id":"`+id+`"}`)},
		{call("trace_screen", `{"id":"`+id+`"}`)},
		{call(verdictTool, `{"status":"inconclusive","summary":"cannot tell","evidence":[]}`)},
	}}
	var heard []string
	ctx := WithProgress(context.Background(), func(m string) { heard = append(heard, m) })
	if _, err := Check(ctx, tr, "the terms box ends up ticked", s); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 3 || !strings.Contains(heard[2], "2 reads") {
		t.Errorf("progress: %q", heard)
	}
	if _, err := Check(context.Background(), tr, "x", &script{}); err != nil {
		t.Errorf("no progress callback: %v", err)
	}
}
