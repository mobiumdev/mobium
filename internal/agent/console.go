package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"

	"github.com/mobiumdev/mobium/internal/webview"
)

// consoleLogs is app_logs: what the page has written to its console.
//
// The one thing in this area Mobium could not do, and the one an agent
// debugging a hybrid app wants most: a JavaScript error is usually the whole
// explanation for a screen that renders wrong, and nothing in the native
// hierarchy shows it.
func (h *Handlers) consoleLogs(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.consoleLogsOn(ctx, s, args)
}

func (h *Handlers) consoleLogsOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	// Which log is read when none is named follows the context: the page's
	// in a WebView, the device's on the native shell. Both are "the log of
	// what I am looking at", and before the device log existed the native
	// case was only a refusal.
	switch source := stringArg(args, "source"); source {
	case "device":
		return h.deviceLogsOn(ctx, s, args)
	case "":
		if s.web == nil {
			return h.deviceLogsOn(ctx, s, args)
		}
	case "webview":
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown log source %q (want \"webview\" or \"device\")", source)
	}
	if stringArg(args, "app") != "" || args["lines"] != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app and lines narrow the device log; a WebView's "+
			"console is already one page's — pass source \"device\" to read the device log instead")
	}
	if s.web == nil {
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "a page's console needs a WebView — "+
			"this session is on the native shell. Use app_contexts to see what is "+
			"attachable, then app_context to switch into one; or omit source to read the device log")
	}

	level := strings.ToLower(strings.TrimSpace(stringArg(args, "level")))
	switch level {
	case "", "log", "info", "warn", "error", "debug":
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown level %q (want \"log\", \"info\", \"warn\", "+
			"\"error\" or \"debug\")", level)
	}

	// Filtered in the page, so entries at other levels remain buffered for a
	// later read rather than being drained and thrown away.
	entries, err := webview.Console(ctx, s.web, level)
	if err != nil {
		return nil, err
	}

	if entries == nil {
		entries = []webview.ConsoleEntry{}
	}
	view := ConsoleView{Entries: entries, Context: s.webCtx, Device: s.dev.Serial}
	if len(entries) == 0 {
		// Said as "since the last read" rather than "nothing was logged",
		// because the buffer is drained on every read and the difference
		// matters to anyone assembling an assertion.
		msg := "nothing logged since the last read"
		if level != "" {
			msg = fmt.Sprintf("nothing at level %q since the last read", level)
		}
		return Result(msg, view), nil
	}

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("[%s] %s", e.Level, e.Text))
	}
	return Result(strings.Join(lines, "\n"), view), nil
}

// ConsoleView is the result of app_logs.
type ConsoleView struct {
	Entries []webview.ConsoleEntry `json:"entries"`
	Context string                 `json:"context"`
	Device  string                 `json:"device"`
}

// evalPage is app_eval: run an expression in the current WebView.
//
// On the roadmap as a small thing for a while, and pulled forward because the
// console capture could not be debugged without it: the shim is installed with
// its error discarded, so a failure to install was invisible from outside.
// A tool that can ask the page a question directly is the difference between
// guessing and knowing.
func (h *Handlers) evalPage(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	if s.web == nil {
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "app_eval runs an expression in a page, so it needs a "+
			"WebView — this session is on the native shell. Switch with app_context first")
	}
	expr := stringArg(args, "expression")
	if strings.TrimSpace(expr) == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_eval needs an expression, e.g. \"document.title\"")
	}
	value, err := s.web.Evaluate(ctx, expr)
	if err != nil {
		return nil, err
	}
	return Result(value, EvalView{Expression: expr, Value: value,
		Context: s.webCtx, Device: s.dev.Serial}), nil
}

// EvalView is the result of app_eval.
type EvalView struct {
	Expression string `json:"expression"`
	Value      string `json:"value"`
	Context    string `json:"context"`
	Device     string `json:"device"`
}
