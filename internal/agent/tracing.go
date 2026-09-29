package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // screenshots arrive as PNG
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/trace"
)

// app_trace: a session recorded as Vibium records one, into a zip that
// trace.playwright.dev and player.vibium.dev open. See internal/trace for
// the format. The recording itself happens in Call, which every front door
// goes through: while a device has a trace running, each call on it is a
// before/after pair, and after it the screen is kept with its map.

// Version is this binary's version, set by main, for a trace's header.
var Version = "dev"

// traceCapture bounds the screenshot and map taken after each call, so a
// device that stops answering does not hold the call's answer.
const traceCapture = 10 * time.Second

// sessionTrace is a trace in progress on one device.
type sessionTrace struct {
	rec         *trace.Recorder
	screenshots bool
	maps        bool
	started     time.Time
}

// TraceView is the result of app_trace.
type TraceView struct {
	Device  string `json:"device"`
	Tracing bool   `json:"tracing"`
	Calls   int    `json:"calls"`
	Elapsed string `json:"elapsed,omitempty"`
	Path    string `json:"path,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
	// Data is the zip, base64, on stop without a path — for a daemon on
	// another machine; the CLI and pipe ask for it and save it themselves.
	Data string `json:"data,omitempty"`
}

func (h *Handlers) traceTool(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	view := TraceView{Device: s.dev.Serial}
	switch action := stringArg(args, "action"); action {
	case "", "status":
		if s.trace == nil {
			return Result("not tracing", view), nil
		}
		view.Tracing, view.Calls = true, s.trace.rec.Calls()
		view.Elapsed = time.Since(s.trace.started).Round(time.Millisecond).String()
		return Result(fmt.Sprintf("tracing for %s, %d calls so far", view.Elapsed, view.Calls), view), nil

	case "start":
		if s.trace != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "a trace is already running on %s — stop it first "+
				"(app_trace with stop)", s.dev.Serial)
		}
		t := &sessionTrace{screenshots: true, maps: true, started: time.Now()}
		if v, ok := args["screenshots"].(bool); ok {
			t.screenshots = v
		}
		if v, ok := args["maps"].(bool); ok {
			t.maps = v
		}
		t.rec = trace.New(trace.Options{Name: stringArg(args, "name"), Device: s.dev.Serial,
			Platform: platformOf(s), Version: Version})
		s.trace = t
		view.Tracing = true
		return Result("tracing: every call on "+s.dev.Serial+" is recorded until app_trace stop", view), nil

	case "stop":
		if s.trace == nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "no trace is running on %s — start one with "+
				"app_trace start", s.dev.Serial)
		}
		path := stringArg(args, "path")
		if path == "" && !boolArg(args, "return_data") {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_trace stop needs a path to save the trace to, "+
				"a .zip")
		}
		t := s.trace
		s.trace = nil
		raw, err := t.rec.Zip()
		if err != nil {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "writing the trace: %w", err)
		}
		view.Calls, view.Bytes = t.rec.Calls(), len(raw)
		if path == "" {
			view.Data = base64.StdEncoding.EncodeToString(raw)
			return Result(fmt.Sprintf("trace of %d calls (%d bytes, in this answer)", view.Calls, view.Bytes), view), nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(abs, raw, 0o600); err != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "cannot write the trace to %s: %w", abs, err)
		}
		view.Path = abs
		return Result(TraceSavedMessage(abs, view), view), nil
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "action must be start, stop or omitted, got %q", action)
	}
}

// TraceSavedMessage is app_trace stop's answer once the zip is saved, the
// same whether the daemon saved it or a remote caller did.
func TraceSavedMessage(path string, v TraceView) string {
	return fmt.Sprintf("saved a trace of %d calls to %s (%d bytes) — open it at https://trace.playwright.dev "+
		"or https://player.vibium.dev", v.Calls, path, v.Bytes)
}

func platformOf(s *session) string {
	if s.backend == BackendWDA {
		return "ios"
	}
	return "android"
}

// tracedSession is the session a call is recorded on: the device it names,
// or the one the daemon is pinned to, or — when it names none — the one
// device with a trace running. None when that is ambiguous.
func (h *Handlers) tracedSession(args map[string]interface{}) *session {
	serial := stringArg(args, "device")
	if serial == "" {
		serial = h.defaultDevice
	}
	var found *session
	for _, s := range h.sessions {
		if s.trace == nil || (serial != "" && s.dev.Serial != serial) {
			continue
		}
		if found != nil && found != s {
			return nil
		}
		found = s
	}
	return found
}

// traceBefore opens a call in the trace. Typed text is never recorded: a
// trace is a file made to be passed around, and the field may have been a
// password (CHALLENGES 43); its length is kept.
func traceBefore(t *sessionTrace, name string, args map[string]interface{}) string {
	params := map[string]interface{}{}
	for k, v := range args {
		params[k] = v
	}
	switch name {
	case "app_type", "app_fill", "app_alert":
		if text, ok := params["text"].(string); ok {
			params["text"] = fmt.Sprintf("(%d characters, not recorded)", len([]rune(text)))
		}
	}
	title := strings.TrimPrefix(name, "app_")
	for _, k := range []string{"target", "action", "app", "url", "key", "name"} {
		if v, ok := args[k].(string); ok && v != "" {
			title += " " + v
			break
		}
	}
	return t.rec.Before(name, title, params)
}

// traceAfter closes a call and keeps the screen as it is after it.
func (h *Handlers) traceAfter(s *session, t *sessionTrace, id string, res *ToolsCallResult, err error) {
	result := ""
	if res != nil && len(res.Content) > 0 {
		result = res.Content[0].Text
	}
	if res != nil && res.StructuredContent != nil {
		var at struct {
			Action string `json:"action"`
			X, Y   *int
		}
		if raw, merr := json.Marshal(res.StructuredContent); merr == nil && json.Unmarshal(raw, &at) == nil &&
			at.Action != "" && at.X != nil && at.Y != nil && (*at.X != 0 || *at.Y != 0) {
			// Only a point the action touched: typing reports none, and a
			// dot at the corner would say it tapped there.
			t.rec.Input(id, *at.X, *at.Y, nil)
		}
	}
	t.rec.After(id, err, result)
	if !t.screenshots {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), traceCapture)
	defer cancel()
	png, serr := s.driver.Screenshot(ctx)
	if serr != nil || len(png) == 0 {
		return
	}
	img, _, derr := image.Decode(bytes.NewReader(png))
	if derr != nil {
		return
	}
	var buf bytes.Buffer
	if jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}) != nil {
		return
	}
	var boxes []trace.Box
	if t.maps {
		if tree, terr := s.driver.Snapshot(ctx); terr == nil {
			for _, e := range tree.Map() {
				boxes = append(boxes, trace.Box{Ref: e.Ref, Label: e.Label, Role: e.Role,
					X1: e.Bounds.X1, Y1: e.Bounds.Y1, X2: e.Bounds.X2, Y2: e.Bounds.Y2})
			}
		}
	}
	b := img.Bounds()
	t.rec.Frame(id, buf.Bytes(), b.Dx(), b.Dy(), boxes)
}
