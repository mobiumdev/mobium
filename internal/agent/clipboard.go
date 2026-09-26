package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// clipboard is app_clipboard: read the device clipboard, or write it.
//
// The platforms are not symmetric and the tool says so. iOS reads and writes
// through simctl, so a write is confirmed by reading it back. Android writes
// through the UiAutomator2 server and **cannot read**: `get_clipboard` returns
// an empty string on Android 10 and later whatever the clipboard holds,
// because reading requires the requesting app to have focus and that server
// has no activity. Reporting "" there would say the clipboard is empty, which
// is a different claim and usually a false one, so the read refuses instead.
func (h *Handlers) clipboard(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.clipboardOn(ctx, s, args)
}

// clipboardOn is app_clipboard once the device is resolved.
func (h *Handlers) clipboardOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	reader, canRead := mobiumdriver.AsClipboardReader(s.driver)
	raw, writing := args["text"]

	if !writing {
		// On WebDriverAgent the read is missing only on a real iPhone, where
		// the write is missing too — so the Android explanation below, and its
		// advice to write instead, would both be wrong.
		if !canRead && s.backend == BackendWDA {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "a real iPhone's clipboard cannot be read or written "+
				"here: a simulator's is reached through simctl, and devicectl has no "+
				"equivalent. Use a simulator for clipboard checks")
		}
		if !canRead {
			return nil, mobiumerr.New(mobiumerr.Unsupported,
				"the %s backend cannot read the clipboard. On Android 10 and later only "+
					"an app with focus may read it, and the UiAutomator2 server has no "+
					"activity of its own — it answers with an empty string however full the "+
					"clipboard is, which would be reported as \"empty\" rather than as "+
					"\"unknown\". Pass text to write one; to check a write, paste into a "+
					"field and read that", s.backend)
		}
		text, err := reader.ClipboardText(ctx)
		if err != nil {
			return nil, err
		}
		if text == "" {
			return Result("the clipboard is empty",
				ClipboardView{Serial: s.dev.Serial, Known: true}), nil
		}
		return Result(fmt.Sprintf("the clipboard holds %q", text),
			ClipboardView{Text: text, Known: true, Serial: s.dev.Serial}), nil
	}

	ctrl, ok := mobiumdriver.AsClipboard(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapClipboard, "write the clipboard")
	}
	text, ok := raw.(string)
	if !ok {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "text must be a string")
	}
	if err := ctrl.SetClipboard(ctx, text); err != nil {
		return nil, err
	}

	view := ClipboardView{Text: text, Serial: s.dev.Serial}
	if !canRead {
		// Say which question was answered. The write is real — verified by
		// pasting into a field — but nothing here observed it.
		return Result(fmt.Sprintf("wrote %q to the clipboard — Android cannot read it "+
			"back, so this is the write reported as sent. Paste into a field to see it",
			text), view), nil
	}

	// Verify by outcome rather than by the command not erroring, which is the
	// rule the rest of this project runs on.
	got, err := reader.ClipboardText(ctx)
	if err != nil {
		return nil, err
	}
	if got != text {
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "wrote %q to the clipboard and it reads back as %q", text, got)
	}
	view.Known = true
	return Result(fmt.Sprintf("the clipboard holds %q, written and read back", text), view), nil
}

// ClipboardView is the result of app_clipboard.
type ClipboardView struct {
	Text string `json:"text,omitempty"`
	// Known is false when the platform cannot report the clipboard at all,
	// which is different from the clipboard being empty.
	Known  bool   `json:"known"`
	Serial string `json:"device"`
}
