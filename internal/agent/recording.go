package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mobiumdev/mobium/internal/claim"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// The judge outside Mobium. app_check asks a model Mobium calls, which needs
// a paid credential. These two let any caller be the judge instead — an MCP
// client's own model, a Claude Code agent — with the same evidence and the
// same rules: app_recording reads a recording through the verifier's own
// read-only views, and app_verdict holds a verdict to the rules app_check's
// loop does, its evidence citing calls the recording holds.

// recordingReads are app_recording's views, each the verifier's own tool.
var recordingReads = map[string]string{
	"summary":    "trace_summary",
	"calls":      "trace_list_calls",
	"call":       "trace_inspect_call",
	"screen":     "trace_screen",
	"screenshot": "trace_screenshot",
	"search":     "trace_search",
}

// RecordingView is app_recording's answer: the view as JSON, as the
// verifier's model reads it, and where a screenshot was saved.
type RecordingView struct {
	Read   string          `json:"read"`
	Record string          `json:"record"`
	Data   json.RawMessage `json:"data,omitempty"`
	Path   string          `json:"path,omitempty"`
}

func (h *Handlers) recording(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	read := stringArg(args, "read")
	tool, ok := recordingReads[read]
	if !ok {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "read is one of summary, calls, call, screen, screenshot or search, not %q", read)
	}
	record := stringArg(args, "record")
	if record == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_recording needs record: the .zip `app_trace` with action stop saved")
	}
	t, err := claim.Open(ctx, record)
	if err != nil {
		return nil, err
	}
	defer t.Close()
	in := map[string]interface{}{}
	for _, k := range []string{"id", "offset", "query"} {
		if v, ok := args[k]; ok {
			in[k] = v
		}
	}
	raw, _ := json.Marshal(in)
	o := t.Execute(ctx, tool, raw)
	if o.IsError {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s", o.Text)
	}
	view := RecordingView{Read: read, Record: record}
	if o.JPEG == nil {
		if json.Valid([]byte(o.Text)) {
			view.Data = json.RawMessage(o.Text)
		}
		return Result(o.Text, view), nil
	}
	path := stringArg(args, "path")
	if path == "" {
		// Inline, which is what an MCP client's model reads; the CLI passes a path.
		return &ToolsCallResult{Content: []Content{
			{Type: "text", Text: o.Text},
			{Type: "image", Data: base64.StdEncoding.EncodeToString(o.JPEG), MimeType: "image/jpeg"},
		}, StructuredContent: view}, nil
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "cannot make %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, o.JPEG, 0o644); err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "cannot save the screenshot to %s: %w", path, err)
	}
	view.Path = path
	return Result(fmt.Sprintf("%s, saved to %s", o.Text, path), view), nil
}

// VerdictView is app_verdict's answer: the verdict as held to the rules.
type VerdictView struct {
	Status   string           `json:"status"`
	Claim    string           `json:"claim"`
	Summary  string           `json:"summary"`
	Evidence []claim.Evidence `json:"evidence"`
	Record   string           `json:"record"`
}

func (h *Handlers) verdict(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	text := strings.TrimSpace(stringArg(args, "claim"))
	record := stringArg(args, "record")
	if text == "" || record == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_verdict needs the claim and the record it was judged on")
	}
	t, err := claim.Open(ctx, record)
	if err != nil {
		return nil, err
	}
	defer t.Close()
	raw, _ := json.Marshal(map[string]interface{}{
		"status": args["status"], "summary": args["summary"], "evidence": args["evidence"],
	})
	v, err := claim.Validate(t, text, raw)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the verdict does not hold to the rules: %w", err).
			WithRemedy("app_verdict again with the evidence citing calls by id (call@3) from app_recording")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", strings.ToUpper(v.Status), v.Summary)
	for _, e := range v.Evidence {
		fmt.Fprintf(&b, "\n  - %s", e.Summary)
	}
	return Result(b.String(), VerdictView{Status: v.Status, Claim: v.Claim, Summary: v.Summary, Evidence: v.Evidence, Record: record}), nil
}
