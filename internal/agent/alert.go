package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// alertGone is how long to wait for a dialog to actually leave after being
// answered. A dialog animates out, and the window it was covering takes a
// moment to come back.
const alertGone = 3 * time.Second

// alert is app_alert: read a system dialog, or answer it.
//
// A permission prompt is not part of the app. It is another process's window —
// `com.google.android.permissioncontroller` on Android, SpringBoard on iOS,
// both measured — and `app_current` reports that other process while one is up,
// which is the clearest signal available that the screen is not the app's.
//
// It is answered through the W3C alert endpoints rather than by tapping a
// button, and that is the whole point of having this tool. Both device-side
// servers implement them, so one code path covers both platforms — and more
// importantly, **accepting does not require knowing what the button says**. A
// check that taps "While using the app" works until the device is in Japanese,
// and this project pins apps to `ja-JP` on purpose.
func (h *Handlers) alert(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.alertOn(ctx, s, args)
}

// alertOn is app_alert once the device is resolved.
func (h *Handlers) alertOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsAlerts(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapAlerts, "read or answer system dialogs")
	}

	action := strings.ToLower(stringArg(args, "action"))
	switch action {
	case "", "read", "accept", "dismiss":
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "action must be \"accept\", \"dismiss\" or omitted to read, got %q", action)
	}

	text, err := ctrl.AlertText(ctx)
	if errors.Is(err, mobiumdriver.ErrNoAlert) {
		// A dialog the platform's endpoint does not know — an app's own,
		// such as a Jetpack Compose dialog (CHALLENGES 177), or Android's
		// autofill offer to save a password. It can be read; it cannot be
		// accepted or dismissed, because those press the buttons the
		// platform picks and this one has none it picks.
		if tree, serr := s.driver.Snapshot(ctx); serr == nil {
			if own := appDialogText(tree); own != "" {
				title := strings.TrimSpace(strings.SplitN(own, "\n", 2)[0])
				if action == "" || action == "read" {
					return Result(fmt.Sprintf("a dialog is on screen: %q\n(not one the platform's alert endpoint "+
						"answers: tap one of its buttons from app_map, or declare an answer with app_dialogs)", own),
						AlertView{Text: own, Present: true, Serial: s.dev.Serial}), nil
				}
				return nil, mobiumerr.New(mobiumerr.Unsupported, "%q is not a dialog the platform's alert endpoint "+
					"answers, so there is no button for %s to press", title, action).
					WithRemedy("tap one of its buttons from app_map, or declare an answer with app_dialogs").
					WithDetail("dialog", title)
			}
			if note := popoverNote(tree); note != "" {
				if action == "" || action == "read" {
					return Result("no dialog is on screen, but "+note, AlertView{Serial: s.dev.Serial}), nil
				}
				return nil, mobiumerr.New(mobiumerr.NoSuchAlert, "there is no dialog to %s, but %s", action, note).
					WithRemedy("app_tap outside the popover to close it")
			}
		}
		// Not a failure. Nothing asking the user anything is a perfectly good
		// state, and reporting it as an error would make "the screen is calm"
		// look like "the device is broken".
		if action == "" || action == "read" {
			return Result("no dialog is on screen", AlertView{Serial: s.dev.Serial}), nil
		}
		return nil, mobiumerr.New(mobiumerr.NoSuchAlert, "there is no dialog to %s — app_alert with no action "+
			"reports whether one is up", action)
	}
	if err != nil {
		return nil, err
	}

	view := AlertView{Text: text, Present: true, Serial: s.dev.Serial}

	// Typing comes before answering, so one call can fill a prompt and confirm
	// it — which is the whole shape of a prompt and awkward to do in two.
	if raw, ok := args["text"]; ok {
		typed, isString := raw.(string)
		if !isString {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "text must be a string")
		}
		if err := ctrl.SendAlertText(ctx, typed); err != nil {
			// Two different causes, and the caller should not have to guess.
			// On Android the server does not implement alert text entry at
			// all — measured, it answers "unknown command" — and React
			// Native has no prompt there either, `Alert.prompt` being
			// iOS-only. On iOS the endpoint exists and a plain alert simply
			// has no field to type into.
			// The server's W3C code, kept in details, rather than its wording.
			if e, ok := mobiumerr.As(err); ok && e.Details["w3c"] == "unknown command" {
				return nil, mobiumerr.New(mobiumerr.Unsupported, "this backend cannot type into a dialog: the "+
					"device-side server does not implement it. Android has no prompt "+
					"alert to type into either — `Alert.prompt` is iOS-only — so there "+
					"is nothing here to reach: %w", err)
			}
			return nil, fmt.Errorf("typing into the dialog failed, which on iOS means it "+
				"has no text field — a plain alert has nothing to type into: %w", err)
		}
		view.Typed = typed
		if action == "" || action == "read" {
			return Result(fmt.Sprintf("typed %q into the dialog %q", typed, text), view), nil
		}
	}

	if action == "" || action == "read" {
		return Result(fmt.Sprintf("a dialog is on screen: %q", text), view), nil
	}

	if err := ctrl.AnswerAlert(ctx, action == "accept"); err != nil {
		// Detecting a dialog and pressing its buttons go through different
		// endpoints, and not every dialog that can be read can be answered:
		// Android's "isn't responding" dialog is read fine, and both accept
		// and dismiss failed with UiAutomator2's "The expected button cannot
		// be detected on the alert" — measured on the emulator, with no
		// remedy in it. Tapping a button by its map ref works; that dialog
		// closed its app when "Close app" was tapped.
		if e, ok := mobiumerr.As(err); ok && e.Details["w3c"] == "invalid element state" {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "the dialog %q is on screen but its buttons are not "+
				"ones the alert endpoint can press — tap one by the ref app_map gives it instead: %w", text, err)
		}
		return nil, err
	}

	// Verify by outcome. "The request was accepted" is not "the dialog is
	// gone", and this project has a rule about the difference — a command that
	// starts something is not finished when it returns.
	deadline := time.Now().Add(alertGone)
	for {
		_, err := ctrl.AlertText(ctx)
		if errors.Is(err, mobiumdriver.ErrNoAlert) {
			break
		}
		if time.Now().After(deadline) {
			return nil, mobiumerr.New(mobiumerr.NotConfirmed, "%sed the dialog and it is still on screen after %s",
				action, alertGone)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}

	// Whatever was behind the dialog is back, and every ref from before it
	// named a screen that had a dialog over it.
	delete(h.refs, s.dev.Serial)

	view.Answered, view.Text = action, text
	// Report what was done and **not** what it meant, because on a permission
	// prompt those come apart — and on iOS they come apart backwards.
	//
	// Measured on iOS 26.5, from a fresh reset each time: `accept` left the
	// permission **denied** and `dismiss` left it **granted**. That is not a
	// bug to paper over, it is what the words mean. W3C accept presses the
	// affirmative button and Apple puts "Don't Allow" last, so the mapping
	// that is right for an OK/Cancel alert inverts on a three-button prompt.
	// Inverting it here would fix the permission case and break every ordinary
	// alert, which is the definition of approximating.
	//
	// So: these answer a dialog. They do not choose an outcome. A caller who
	// needs a specific outcome taps the button, which `map` returns like any
	// other. CHALLENGES 63.
	return Result(fmt.Sprintf("%sed the dialog %q — this answered it and did not "+
		"choose an outcome: on a permission prompt accept and dismiss do not mean "+
		"grant and deny, and on iOS they are the other way round. Tap the button by "+
		"ref if you need a particular answer", action, text), view), nil
}

// AlertView is the result of app_alert.
type AlertView struct {
	// Text is what the dialog said. Empty when none was up.
	Text string `json:"text,omitempty"`
	// Present is whether a dialog was on screen when asked.
	Present bool `json:"present"`
	// Answered is "accept" or "dismiss" when this call answered one.
	Answered string `json:"answered,omitempty"`
	// Typed is what was sent into a prompt's field, when anything was.
	Typed  string `json:"typed,omitempty"`
	Serial string `json:"device"`
}
