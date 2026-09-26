package mobiumdriver

import "context"

// The clipboard is one capability on iOS and half of one on Android, so the
// backends implement what each can honestly do. See the Clipboard and
// ClipboardReader docs for why the read is missing on one side.

// SetClipboard writes the device clipboard through the UiAutomator2 server.
func (u *UIA2) SetClipboard(ctx context.Context, text string) error {
	return u.w3c.setClipboard(ctx, text)
}

// SetClipboard writes the simulator's clipboard.
func (w *WDA) SetClipboard(ctx context.Context, text string) error {
	if err := w.simOnly(CapClipboard); err != nil {
		return err
	}
	return w.sim.SetPasteboard(ctx, text)
}

// ClipboardText reads the simulator's clipboard.
//
// UIA2 deliberately has no counterpart: UiAutomator2's get_clipboard returns
// an empty string on Android 10 and later however full the clipboard is,
// because reading requires the requesting app to have focus and the server has
// no activity of its own. Implementing it here would turn "cannot read" into
// "is empty".
func (w *WDA) ClipboardText(ctx context.Context) (string, error) {
	if err := w.simOnly(CapClipboardRead); err != nil {
		return "", err
	}
	return w.sim.Pasteboard(ctx)
}
