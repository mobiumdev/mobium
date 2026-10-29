package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// assistant is app_assistant: put a request to the platform's assistant as
// typed text, and answer with what came back and what is in front afterward.
//
// It is for a task-based test of an app through the assistant (ACCESSIBILITY.md,
// step 6): a person who cannot use the screen asks the assistant to do
// something in the app. The test is whether the app did it, which the app
// shows — never that the assistant said so — so the answer names the app in
// front, and with `app` says whether that is the app the caller expected.
// The request is typed, never spoken, and the result says so.
//
// Measured: Google Assistant on the Android 15 emulator, signed out, takes a
// typed request through its Keyboard button and the keyboard's enter, and
// "Open Clock" brought Clock forward in about two seconds, while "Open
// MobiumApp" became a web search; Gemini on the Pixel 8 Pro takes it through
// its "Ask Gemini" field and Send (CHALLENGES 350). Siri on the iPhone takes
// it through WebDriverAgent (CHALLENGES 349).
func (h *Handlers) assistant(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	request := strings.TrimSpace(stringArg(args, "request"))
	if request == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_assistant needs a request, such as \"Open Clock\"")
	}
	app := strings.TrimSpace(stringArg(args, "app"))
	if verb := actsForSomeone(request); verb != "" && app == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"%q asks the assistant to %s on the device owner's behalf; name the app it is for with app, and Mobium "+
				"reports whether that app came forward — it never asks for a call, a message or a purchase blind", request, verb)
	}
	wait := assistantWait
	if v, ok := args["timeout"].(float64); ok && v > 0 {
		wait = time.Duration(v * float64(time.Second))
	}
	var view *AssistantView
	switch s.backend {
	case BackendUIA2, BackendDump:
		view, err = h.askAndroidAssistant(ctx, s, request, app, wait)
	default:
		view, err = h.askSiri(ctx, s, request, app, wait)
	}
	if err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)
	return Result(view.text(), view), nil
}

// assistantWait is how long an answer or an app is waited for by default:
// Gemini's answer to a sum took about seven seconds on the Pixel.
const assistantWait = 15 * time.Second

// AssistantView is the result of app_assistant.
type AssistantView struct {
	Device    string `json:"device"`
	Assistant string `json:"assistant"`
	Request   string `json:"request"`
	// Typed is always true: the request was typed, not spoken.
	Typed bool `json:"typed"`
	// Answer is the text the assistant showed, in screen order. Empty when
	// it showed none a tree can read: Google Assistant's calculator card
	// has no text at all.
	Answer []string `json:"answer"`
	// Front is the app in front once the assistant was done.
	Front string `json:"front"`
	// App is the app the caller named, and Reached whether it came forward.
	App     string `json:"app,omitempty"`
	Reached *bool  `json:"reached,omitempty"`
	// Closed says the assistant's panel was closed afterward, so the next
	// call reads the app.
	Closed bool `json:"closed"`
}

func (v *AssistantView) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "asked %s (typed, not spoken): %q\n", v.Assistant, v.Request)
	if len(v.Answer) > 0 {
		shown := make([]string, len(v.Answer))
		for i, a := range v.Answer {
			shown[i] = uitree.OneLine(a)
		}
		fmt.Fprintf(&b, "it showed: %s\n", strings.Join(shown, " | "))
	} else {
		b.WriteString("it showed no text a screen read can see\n")
	}
	fmt.Fprintf(&b, "in front: %s", v.Front)
	if v.Closed {
		b.WriteString(" (the assistant's panel closed)")
	}
	if v.Reached != nil {
		if *v.Reached {
			fmt.Fprintf(&b, "\n%s came forward", v.App)
		} else {
			fmt.Fprintf(&b, "\n%s did not come forward: the assistant did not hand the request to it", v.App)
		}
	}
	return b.String()
}

// sideEffects are requests that act for the device's owner: placing a
// call, sending something, paying. Matched as the request's first verb,
// after a greeting.
var sideEffects = regexp.MustCompile(`(?i)^(?:(?:hey|ok|okay)\s+(?:google|siri|gemini)[,\s]*)?(?:please\s+)?` +
	`(call|dial|text|message|send|email|mail|text|pay|buy|order|purchase|book|post|tweet|share|delete|transfer|venmo)\b`)

// actsForSomeone names the verb when request acts for the owner, or "".
func actsForSomeone(request string) string {
	if m := sideEffects.FindStringSubmatch(strings.TrimSpace(request)); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// askAndroidAssistant opens Gemini or Google Assistant with the assist key,
// types the request into its field, sends it, waits for an app or an
// answer, and closes the panel with back if it is still up.
func (h *Handlers) askAndroidAssistant(ctx context.Context, s *session, request, app string, wait time.Duration) (*AssistantView, error) {
	adb, err := h.adbFor(ctx, s)
	if err != nil {
		return nil, err
	}
	before, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	was := before.Package()
	if _, err := adb.Shell(ctx, "input", "keyevent", "KEYCODE_ASSIST"); err != nil {
		return nil, err
	}
	var tree *uitree.Tree
	name := ""
	deadline := time.Now().Add(5 * time.Second)
	for name == "" {
		if time.Now().After(deadline) {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "the assist key brought no assistant forward — this device "+
				"has no Gemini or Google Assistant, or it is not set as the default digital assistant")
		}
		if err := sleepCtx(ctx, 300*time.Millisecond); err != nil {
			return nil, err
		}
		if tree, err = s.driver.Snapshot(ctx); err == nil {
			name = uitree.AssistantOverlay(tree)
		}
	}
	view := &AssistantView{Device: s.dev.Serial, Assistant: name, Request: request, Typed: true, Answer: []string{}, App: app}

	// Google Assistant opens listening, with a Keyboard button; Gemini opens
	// with its field.
	field := assistantField(tree)
	if field == nil {
		if kb := findByLabel(tree, "Keyboard"); kb != nil {
			if _, err := h.tapOn(ctx, s, map[string]interface{}{"target": "label=Keyboard"}); err != nil {
				return nil, err
			}
			for i := 0; i < 10 && field == nil; i++ {
				if err := sleepCtx(ctx, 300*time.Millisecond); err != nil {
					return nil, err
				}
				if tree, err = s.driver.Snapshot(ctx); err == nil {
					field = assistantField(tree)
				}
			}
		}
	}
	if field == nil {
		_, _ = h.pressOn(ctx, s, map[string]interface{}{"button": "back"})
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "%s opened with no field to type a request into", name)
	}
	if _, err := h.typeTextOn(ctx, s, map[string]interface{}{"target": "testid=" + field.ShortTestID(), "text": request}, true); err != nil {
		return nil, err
	}
	typed, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	// Gemini sends with its Send button — the keyboard's enter closes its
	// overlay with the request unsent — and Google Assistant with enter.
	if send := findByLabel(typed, "Send"); send != nil {
		_, err = h.tapOn(ctx, s, map[string]interface{}{"target": "label=Send"})
	} else {
		_, err = h.keyboardOn(ctx, s, map[string]interface{}{"key": "enter"})
	}
	if err != nil {
		return nil, err
	}

	handedOff := false
	seen := textsOf(typed, assistantPackage)
	seen[strings.ToLower(request)] = true
	last, still := "", 0
	end := time.Now().Add(wait)
	for time.Now().Before(end) {
		if err := sleepCtx(ctx, 700*time.Millisecond); err != nil {
			return nil, err
		}
		t, err := s.driver.Snapshot(ctx)
		if err != nil {
			continue
		}
		front := t.Package()
		if front != assistantPackage && uitree.AssistantOverlay(t) == "" {
			view.Front = front
			handedOff = true
			break
		}
		answer := newTexts(t, assistantPackage, seen)
		view.Answer = answer
		if key := strings.Join(answer, "\x00"); key != "" && key == last {
			still++
			if still >= 2 {
				break
			}
		} else {
			last, still = key, 0
		}
	}
	if view.Front == "" {
		// Still the assistant: close it, so the next call reads the app.
		if _, err := h.pressOn(ctx, s, map[string]interface{}{"button": "back"}); err == nil {
			view.Closed = true
		}
		if t, err := s.driver.Snapshot(ctx); err == nil {
			view.Front = t.Package()
			if uitree.AssistantOverlay(t) != "" {
				view.Closed = false
			}
		}
	}
	if view.Front == "" {
		view.Front = was
	}
	view.setReached(app, handedOff)
	return view, nil
}

// setReached says whether the app the caller named came forward because the
// assistant handed the request to it. The app in front once the panel is
// closed proves nothing: closing it uncovers whatever was under it, and
// "Open MobiumApp" — a web search, signed out — read as MobiumApp coming
// forward when MobiumApp had been under the panel all along.
func (v *AssistantView) setReached(app string, handedOff bool) {
	if app == "" {
		return
	}
	reached := handedOff && v.Front == app
	v.Reached = &reached
}

// assistantPackage is the Google app, which draws both Gemini and Google
// Assistant.
const assistantPackage = "com.google.android.googlequicksearchbox"

// assistantField is the assistant's text field, by class and package.
func assistantField(t *uitree.Tree) *uitree.Node {
	var out *uitree.Node
	t.WalkFront(func(n *uitree.Node) bool {
		if n.Displayed && n.Package == assistantPackage && strings.HasSuffix(n.Class, "EditText") && n.ShortTestID() != "" {
			out = n
		}
		return out == nil
	})
	return out
}

func findByLabel(t *uitree.Tree, label string) *uitree.Node {
	var out *uitree.Node
	t.WalkFront(func(n *uitree.Node) bool {
		if n.Displayed && strings.EqualFold(strings.TrimSpace(n.Label), label) {
			out = n
		}
		return out == nil
	})
	return out
}

// textsOf is every text shown in pkg, lower case, as a set.
func textsOf(t *uitree.Tree, pkg string) map[string]bool {
	out := map[string]bool{}
	t.WalkFront(func(n *uitree.Node) bool {
		if n.Displayed && n.Package == pkg {
			for _, s := range []string{n.Text, n.Label} {
				if s = strings.TrimSpace(s); s != "" {
					out[strings.ToLower(s)] = true
				}
			}
		}
		return true
	})
	return out
}

// newTexts is the answer: text the assistant shows that was not there when
// the request was typed — not its buttons' names, which are its own chrome,
// and never a password field's.
func newTexts(t *uitree.Tree, pkg string, seen map[string]bool) []string {
	out := []string{}
	t.WalkFront(func(n *uitree.Node) bool {
		if !n.Displayed || n.Package != pkg || n.Clickable || n.Password {
			return true
		}
		if s := strings.TrimSpace(n.Text); s != "" && !seen[strings.ToLower(s)] && len(out) < 20 {
			out = append(out, clip(s, 200))
		}
		return true
	})
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// askSiri types the request to Siri on an iPhone, waits for an app to come
// forward or Siri's answer to settle, and closes Siri's panel with Home if
// it is still up — Home closes it and the app comes back (CHALLENGES 351).
func (h *Handlers) askSiri(ctx context.Context, s *session, request, app string, wait time.Duration) (*AssistantView, error) {
	asker, ok := mobiumdriver.AsSiriAsker(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapAssistant, "ask the platform's assistant")
	}
	before, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	was := before.Package()
	if err := asker.AskSiri(ctx, request); err != nil {
		return nil, err
	}
	view := &AssistantView{Device: s.dev.Serial, Assistant: "Siri", Request: request, Typed: true, Answer: []string{}, App: app}
	last, still := "", 0
	end := time.Now().Add(wait)
	panel, handedOff := false, false
	for time.Now().Before(end) {
		if err := sleepCtx(ctx, 700*time.Millisecond); err != nil {
			return nil, err
		}
		t, err := s.driver.Snapshot(ctx)
		if err != nil {
			continue
		}
		if !uitree.SiriPanel(t) {
			if done, front := siriSettled(t, was, panel); done {
				view.Front = front
				handedOff = front != was || !panel
				break
			}
			continue
		}
		panel = true
		answer := siriTexts(t, request)
		view.Answer = answer
		if key := strings.Join(answer, "\x00"); key != "" && key == last {
			still++
			if still >= 2 {
				break
			}
		} else {
			last, still = key, 0
		}
	}
	if view.Front == "" {
		if _, err := h.pressOn(ctx, s, map[string]interface{}{"button": "home"}); err == nil {
			view.Closed = true
		}
		if t, err := s.driver.Snapshot(ctx); err == nil {
			view.Front = t.Package()
			if uitree.SiriPanel(t) {
				view.Closed = false
			}
		}
	}
	view.setReached(app, handedOff)
	return view, nil
}

// siriSettled decides one read with no Siri panel in it: whether Siri is
// done, and the app in front once it is. SpringBoard in front is never where
// Siri handed off, unless the request was made from it: Siri runs in
// SpringBoard, and on the iPhone a read begun just after the request blocked
// until the hand-off and came back as SpringBoard with no Siri window — three
// times in three, the next read MobiumApp. Taken for the hand-off, it was
// reported as "not reached", one run in four of assistant.sh. A request whose
// answer leaves SpringBoard in front is still reported so when the wait ends.
func siriSettled(t *uitree.Tree, was string, panel bool) (done bool, front string) {
	pkg := t.Package()
	if pkg == springboardBundleID && was != springboardBundleID {
		return false, ""
	}
	if panel || pkg != was {
		return true, pkg
	}
	return false, ""
}

// siriChrome are the controls Siri's panel shows beside an answer, measured
// on the iPhone: not part of the answer.
var siriChrome = map[string]bool{"listen": true, "copy": true}

// siriTexts is what Siri's panel shows, in screen order, without its own
// controls or the request repeated.
func siriTexts(t *uitree.Tree, request string) []string {
	out := []string{}
	t.WalkFront(func(n *uitree.Node) bool {
		if !n.Displayed || !inSiriWindow(n) || n.Password {
			return true
		}
		s := strings.TrimSpace(n.Label)
		if s == "" {
			s = strings.TrimSpace(n.Text)
		}
		if s == "" || siriChrome[strings.ToLower(s)] || strings.EqualFold(s, request) || n.TestID == "siri-interface-window" {
			return true
		}
		if len(n.Children) == 0 && len(out) < 20 {
			out = append(out, clip(s, 200))
		}
		return true
	})
	return out
}

func inSiriWindow(n *uitree.Node) bool {
	for p := n; p != nil; p = p.Parent {
		if p.TestID == "siri-interface-window" {
			return true
		}
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
