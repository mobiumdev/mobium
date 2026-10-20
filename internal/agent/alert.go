package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
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
// accept and dismiss mean yes and no, and are answered through the W3C alert
// endpoints, so they need no knowledge of what the buttons say: a check that
// taps "While using the app" works until the device is in Japanese, and this
// project pins apps to `ja-JP` on purpose. They are only defined where one
// button is yes and one is no — a dialog of one or two buttons. With more,
// more than one may mean yes (iOS's location prompt has Allow Once and Allow
// While Using App), and iOS moves the cancel button to the bottom, where
// WebDriverAgent's accept pressed it: accept denied the permission and pressed
// Cancel on a three-button alert (CHALLENGES 63, 106). So those are refused,
// naming the buttons, and `button` presses one by its caption or position.
// Every answer says which button it pressed. CHALLENGES 305.
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
	button, pressing := args["button"].(string)
	if _, given := args["button"]; given && !pressing {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "button must be a string: a caption, or a position from 1")
	}
	if pressing && strings.TrimSpace(button) == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "button is empty: give a caption, or a position from 1")
	}
	answering := action == "accept" || action == "dismiss"
	if pressing && answering {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "give action or button, not both: button presses the "+
			"one it names, and %s the one the dialog marks %s", action, yesNo(action == "accept"))
	}
	reading := !pressing && !answering
	// What the call would have done, for a message saying it could not.
	verb := action
	if pressing {
		verb = "press a button on"
	}

	text, err := ctrl.AlertText(ctx)
	if errors.Is(err, mobiumdriver.ErrNoAlert) {
		// A dialog the platform's endpoint does not know — an app's own,
		// such as a Jetpack Compose dialog (CHALLENGES 177), or Android's
		// autofill offer to save a password. It can be read, and a button on
		// it pressed by caption; it cannot be accepted or dismissed, because
		// those press the buttons the platform marks and this one has none.
		if tree, serr := s.driver.Snapshot(ctx); serr == nil {
			if own := appDialogText(tree); own != "" {
				title := strings.TrimSpace(strings.SplitN(own, "\n", 2)[0])
				view := AlertView{Text: own, Present: true, Serial: s.dev.Serial, Buttons: captionsOf(dialogButtons(tree))}
				if pressing {
					return h.pressAlertButton(ctx, s, tree, own, button, view)
				}
				// A dialog whose own ids say which button is yes and which
				// is no — Wear OS's — is answered by pressing that one.
				if answering {
					if marked := markedBy(dialogButtons(tree), action == "accept"); marked != "" {
						view.Answered = action
						return h.pressAlertButton(ctx, s, tree, own, marked, view)
					}
				}
				// Wear OS's: its ids say which is yes and which is no.
				marked := markedBy(dialogButtons(tree), true) != "" && markedBy(dialogButtons(tree), false) != ""
				if reading && marked {
					return Result(fmt.Sprintf("a dialog is on screen: %q%s", own, buttonsLine(view.Buttons)), view), nil
				}
				if reading {
					return Result(fmt.Sprintf("a dialog is on screen: %q%s\n(not one the platform's alert endpoint "+
						"answers: press one of its buttons with app_alert button, or declare an answer with "+
						"app_dialogs)", own, buttonsLine(view.Buttons)), view), nil
				}
				return nil, mobiumerr.New(mobiumerr.Unsupported, "%q is not a dialog the platform's alert endpoint "+
					"answers, so there is no button for %s to press", title, action).
					WithRemedy("press one of its buttons with app_alert button, or declare an answer with app_dialogs").
					WithDetail("dialog", title)
			}
			if note := popoverNote(tree); note != "" {
				if reading {
					return Result("no dialog is on screen, but "+note, AlertView{Serial: s.dev.Serial}), nil
				}
				return nil, mobiumerr.New(mobiumerr.NoSuchAlert, "there is no dialog to %s, but %s", verb, note).
					WithRemedy("app_tap outside the popover to close it")
			}
		}
		// Not a failure. Nothing asking the user anything is a perfectly good
		// state, and reporting it as an error would make "the screen is calm"
		// look like "the device is broken".
		if reading {
			return Result("no dialog is on screen", AlertView{Serial: s.dev.Serial}), nil
		}
		return nil, mobiumerr.New(mobiumerr.NoSuchAlert, "there is no dialog to %s — app_alert with no action "+
			"reports whether one is up", verb)
	}
	if err != nil {
		return nil, err
	}

	view := AlertView{Text: text, Present: true, Serial: s.dev.Serial}
	// The dialog's buttons, from the screen. A read that fails leaves them
	// unknown, which a read can live with and an answer cannot: accept on a
	// dialog whose buttons cannot be counted might be accept on three.
	tree, treeErr := s.driver.Snapshot(ctx)
	var buttons []uitree.Entry
	if treeErr == nil {
		buttons = dialogButtons(tree)
		view.Buttons = captionsOf(buttons)
	}

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
		if reading {
			return Result(fmt.Sprintf("typed %q into the dialog %q", typed, text), view), nil
		}
	}

	if reading {
		return Result(fmt.Sprintf("a dialog is on screen: %q%s", text, buttonsLine(view.Buttons)), view), nil
	}
	if treeErr != nil {
		return nil, treeErr
	}
	// A device driven with its remote answers by focus, whichever way was
	// asked. An Apple TV's three-button dialog was refused naming button,
	// which taps, and a TV refuses every tap: the remedy sent the caller to
	// a second refusal. Asked here, before accept, dismiss or a button is
	// chosen, so every answer on a TV says the same thing (CHALLENGES 313).
	if err := mobiumdriver.AlertAnsweredByRemote(s.driver); err != nil {
		if e, ok := mobiumerr.As(err); ok {
			title := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
			return nil, e.WithDetail("dialog", title).WithDetail("buttons", view.Buttons)
		}
		return nil, err
	}
	if pressing {
		return h.pressAlertButton(ctx, s, tree, text, button, view)
	}

	title := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	if len(buttons) > 2 {
		// One yes and one no is what accept and dismiss mean, and a dialog
		// of three does not have that shape: two of the location prompt's
		// three buttons grant it, and on iOS the one WebDriverAgent's accept
		// presses is the cancel button, moved to the bottom (CHALLENGES 305).
		return nil, mobiumerr.New(mobiumerr.Unsupported, "%q has %d buttons — %s — so %s does not say which: "+
			"accept and dismiss answer a dialog with one yes and one no, and with more than two, more than one "+
			"may mean yes", title, len(buttons), quoteAll(view.Buttons), action).
			WithRemedy("app_alert with button set to one of them, by its caption or its position from 1").
			WithDetail("dialog", title).WithDetail("buttons", view.Buttons)
	}
	pressed := pressedBy(sessionPlatform(s), buttons, action == "accept")

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
				"ones the alert endpoint can press — press one with app_alert button, or tap one by the ref "+
				"app_map gives it: %w", text, err)
		}
		return nil, err
	}
	if err := waitAlertGone(ctx, ctrl, text, action+"ed the dialog"); err != nil {
		return nil, err
	}

	// Whatever was behind the dialog is back, and every ref from before it
	// named a screen that had a dialog over it.
	delete(h.refs, s.dev.Serial)

	view.Answered, view.Pressed = action, pressed
	if pressed == "" {
		// Android marks its buttons positive and negative by id, and a dialog
		// whose ids say neither leaves what was pressed the platform's; a
		// caption here would be a guess.
		view.Note = fmt.Sprintf("pressed the button the platform marks %s; which caption that is could not be read",
			map[bool]string{true: "positive", false: "negative"}[action == "accept"])
		return Result(fmt.Sprintf("%sed the dialog %q — %s", action, text, view.Note), view), nil
	}
	return Result(fmt.Sprintf("%sed the dialog %q: pressed %q", action, text, pressed), view), nil
}

func yesNo(yes bool) string {
	if yes {
		return "yes"
	}
	return "no"
}

// pressAlertButton presses the dialog's button named by caption or by
// position, and waits for the dialog to go. It taps the button where the
// screen shows it, as a declared rule does (app_dialogs), rather than asking
// WebDriverAgent to press it by name: that matches a caption exactly, and
// Apple writes Don’t Allow with a curly apostrophe, so the "Don't Allow" a
// caller types was refused (measured on iOS 26.5).
func (h *Handlers) pressAlertButton(ctx context.Context, s *session, tree *uitree.Tree, text, button string, view AlertView) (*ToolsCallResult, error) {
	title := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	buttons := dialogButtons(tree)
	target, ok := pickAlertButton(buttons, button)
	if !ok {
		return nil, mobiumerr.New(mobiumerr.NoSuchElement, "%q has no button %q: its buttons are %s", title, button,
			quoteAll(captionsOf(buttons))).
			WithRemedy("app_alert with button set to one of them, by its caption or its position from 1").
			WithDetail("dialog", title).WithDetail("buttons", captionsOf(buttons))
	}
	x, y := target.Bounds.Center()
	if err := h.touch(ctx, s, x, y, strings.TrimSpace(target.Label)); err != nil {
		return nil, err
	}
	pressed := strings.TrimSpace(target.Label)
	if err := h.waitDialogChanged(ctx, s, text, fmt.Sprintf("pressed %q on the dialog", pressed)); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)
	view.Pressed = pressed
	if view.Answered != "" {
		return Result(fmt.Sprintf("%sed the dialog %q: pressed %q", view.Answered, text, pressed), view), nil
	}
	return Result(fmt.Sprintf("pressed %q on the dialog %q", pressed, text), view), nil
}

// waitAlertGone waits for the platform's dialog to leave. Verify by outcome:
// "the request was accepted" is not "the dialog is gone", and a command that
// starts something is not finished when it returns.
func waitAlertGone(ctx context.Context, ctrl mobiumdriver.Alerts, text, done string) error {
	deadline := time.Now().Add(alertGone)
	for {
		_, err := ctrl.AlertText(ctx)
		if errors.Is(err, mobiumdriver.ErrNoAlert) {
			return nil
		}
		if time.Now().After(deadline) {
			return mobiumerr.New(mobiumerr.NotConfirmed, "%s and it is still on screen after %s", done, alertGone)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// waitDialogChanged waits for any dialog, the app's own included, to go or
// to be replaced: a button may raise the next dialog.
func (h *Handlers) waitDialogChanged(ctx context.Context, s *session, text, done string) error {
	deadline := time.Now().Add(alertGone)
	for {
		now, err := h.dialogText(ctx, s)
		if err == nil && now != text {
			return nil
		}
		if time.Now().After(deadline) {
			return mobiumerr.New(mobiumerr.NotConfirmed, "%s and it is still on screen after %s", done, alertGone)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// dialogButtons is the dialog's buttons in the order a person reads them:
// top to bottom, and along a row left to right. The dialog is the tree's
// Alert or Sheet on iOS, where the app beneath stays in the tree; elsewhere
// the tree is the dialog's window alone. Only buttons: the location prompt's
// Precise and Approximate on Android are radio buttons, a choice made before
// answering, not answers.
func dialogButtons(tree *uitree.Tree) []uitree.Entry {
	if tree == nil || tree.Root == nil {
		return nil
	}
	scope := tree.Dialog()
	if scope == nil {
		scope = tree.Root
	}
	var all []uitree.Entry
	for _, e := range tree.Map() {
		if e.Role == "button" && e.Node.Within(scope) && strings.TrimSpace(e.Label) != "" {
			all = append(all, e)
		}
	}
	// Not a panel that holds them: the camera prompt's whole panel is
	// clickable, and map names it a button by the title inside it.
	var bs []uitree.Entry
	for _, e := range all {
		holds := false
		for _, o := range all {
			if o.Node != e.Node && o.Node.Within(e.Node) {
				holds = true
				break
			}
		}
		if !holds {
			bs = append(bs, e)
		}
	}
	nodes := make([]*uitree.Node, len(bs))
	byNode := map[*uitree.Node]uitree.Entry{}
	for i, e := range bs {
		nodes[i], byNode[e.Node] = e.Node, e
	}
	uitree.ReadingOrder(nodes)
	for i, n := range nodes {
		bs[i] = byNode[n]
	}
	return bs
}

func captionsOf(bs []uitree.Entry) []string {
	var cs []string
	for _, b := range bs {
		cs = append(cs, strings.TrimSpace(b.Label))
	}
	return cs
}

// buttonsLine is a read's line naming the buttons, or "" with none known.
func buttonsLine(captions []string) string {
	if len(captions) == 0 {
		return ""
	}
	return "\nbuttons: " + quoteAll(captions)
}

// pickAlertButton finds the button a caller named: by caption, in any case
// and with straight or curly quotes alike — Apple writes Don’t Allow, and a
// caller types Don't Allow — or, when no caption is the word given, by its
// position from 1 in the order the dialog shows them.
func pickAlertButton(bs []uitree.Entry, button string) (uitree.Entry, bool) {
	want := plainCaption(button)
	for _, b := range bs {
		if plainCaption(b.Label) == want || plainCaption(b.Node.Text) == want || plainCaption(b.Node.Label) == want {
			return b, true
		}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(button)); err == nil && n >= 1 && n <= len(bs) {
		return bs[n-1], true
	}
	return uitree.Entry{}, false
}

var captionQuotes = strings.NewReplacer("’", "'", "‘", "'", "“", "\"", "”", "\"")

func plainCaption(s string) string {
	return strings.ToLower(strings.TrimSpace(captionQuotes.Replace(s)))
}

// pressedBy is the caption of the button accept or dismiss presses on a
// dialog of one or two, or "" when that cannot be told.
//
// iOS: WebDriverAgent presses an alert's last button for accept and its first
// for dismiss, and with two buttons iOS puts the cancel button first — side
// by side, and stacked when the captions are long (App Tracking
// Transparency). Measured on iOS 26.5 on two-button alerts and the
// notification, camera and tracking prompts. Android: UiAutomator2 presses
// the buttons the dialog marks positive and negative, which its ids name —
// android:id/button1 and button2 on an alert, permission_allow_button and
// permission_deny_button on a permission prompt — and not by position: the
// notification prompt stacks Allow above Don't allow. Measured on Android 15.
// Wear OS's own dialog is no alert to UiAutomator2 and is answered by
// pressing its marked button (CHALLENGES 317).
func pressedBy(platform string, bs []uitree.Entry, accept bool) string {
	switch {
	case len(bs) == 1:
		return strings.TrimSpace(bs[0].Label)
	case len(bs) != 2:
		return ""
	case platform == "ios" && accept:
		return strings.TrimSpace(bs[1].Label)
	case platform == "ios":
		return strings.TrimSpace(bs[0].Label)
	}
	return markedBy(bs, accept)
}

// markedBy is the caption of the button whose id marks it positive, or
// negative, on a dialog of two — Android's alert and permission ids, and
// Wear OS's own alert, whose ✓ and ✕ are wear_alertdialog_positive_button
// and negative_button — or "" when none does.
func markedBy(bs []uitree.Entry, accept bool) string {
	if len(bs) != 2 {
		return ""
	}
	for _, b := range bs {
		id := b.Node.TestID
		if accept && (strings.HasSuffix(id, ":id/button1") || strings.Contains(id, "permission_allow") ||
			strings.HasSuffix(id, ":id/wear_alertdialog_positive_button")) ||
			!accept && (strings.HasSuffix(id, ":id/button2") || strings.Contains(id, "permission_deny") ||
				strings.HasSuffix(id, ":id/wear_alertdialog_negative_button")) {
			return strings.TrimSpace(b.Label)
		}
	}
	return ""
}

// AlertView is the result of app_alert.
type AlertView struct {
	// Text is what the dialog said. Empty when none was up.
	Text string `json:"text,omitempty"`
	// Present is whether a dialog was on screen when asked.
	Present bool `json:"present"`
	// Buttons are the dialog's buttons, in the order it shows them, when
	// they could be read.
	Buttons []string `json:"buttons,omitempty"`
	// Answered is "accept" or "dismiss" when this call answered one.
	Answered string `json:"answered,omitempty"`
	// Pressed is the caption of the button this call pressed: an answer
	// the caller does not hear about is a click they did not make.
	Pressed string `json:"pressed,omitempty"`
	// Note says what could not be told about an answer.
	Note string `json:"note,omitempty"`
	// Typed is what was sent into a prompt's field, when anything was.
	Typed  string `json:"typed,omitempty"`
	Serial string `json:"device"`
}
