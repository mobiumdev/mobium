package mobiumdriver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// VoiceOver, through WebDriverAgent.
//
// With VoiceOver on, a touch is not a tap: it moves VoiceOver's cursor to
// what it touches, as a person's single tap does, and activates nothing. A
// double tap anywhere activates what the cursor is on. Measured on the iPhone
// 15 Plus, iOS 27.0.1, and on an iPhone 18 Pro simulator, iOS 27.0, both with
// WebDriverAgent 16.12.8 built by Xcode 27.1: `tap` on MobiumApp's Pro radio
// answered "tapped" and left it unchecked, a check read back unchecked, an
// alert's button left the alert up and a WebView's checkbox stayed as it
// was — while the cursor moved onto each. A one-finger swipe moved the
// cursor too and scrolled nothing, and scroll-to blamed the end of the list.
// Typing reached no field, and was blamed on a dropped keystroke: the field
// never had focus, and VoiceOver said "Double tap to edit." With VoiceOver
// off, every one of them landed (CHALLENGES 348).
//
// WebDriverAgent's /wda/voiceOver routes reach XCUIAutomation's VoiceOver
// service, which a runner built by Xcode 27 has and one built earlier does
// not: those answer "requires an iOS 27+ runtime" on any device, an iOS 27
// phone included. Asking costs 5 to 22ms on the phone, so it is asked before
// every touch — VoiceOver can be turned on by hand, or by its shortcut, at
// any moment of a session.

// ScreenReader is implemented by a backend that can tell whether the
// device's screen reader is on, and touch as the screen reader's user does.
// Built in only: a driver process has no way to say any of it yet.
type ScreenReader interface {
	// ScreenReaderOn reports whether the screen reader is on. known is false
	// when the backend cannot tell, as a runner built before Xcode 27
	// cannot; such a device is treated as having it off, as before.
	ScreenReaderOn(ctx context.Context) (on, known bool)
	// FocusAt touches a point, in device pixels, which moves the screen
	// reader's cursor there, and returns what the screen reader then says.
	FocusAt(ctx context.Context, x, y int) (string, error)
	// ActivateFocused double-taps at a point, in device pixels, which
	// activates what the cursor is on, wherever the point is.
	ActivateFocused(ctx context.Context, x, y int) error
}

// AsScreenReader returns the driver's screen reader support, if it has any.
func AsScreenReader(d Driver) (ScreenReader, bool) {
	s, ok := d.(ScreenReader)
	return s, ok
}

type screenReaderAskedKey struct{}

// ScreenReaderAsked marks ctx as belonging to a touch whose caller has asked
// about the screen reader already and is touching as its answer requires,
// so the touch is sent without asking again.
func ScreenReaderAsked(ctx context.Context) context.Context {
	return context.WithValue(ctx, screenReaderAskedKey{}, true)
}

func screenReaderWasAsked(ctx context.Context) bool {
	asked, _ := ctx.Value(screenReaderAskedKey{}).(bool)
	return asked
}

// errVoiceOverOn refuses a touch VoiceOver would take for its own.
var errVoiceOverOn = mobiumerr.New(mobiumerr.DeviceNotReady, "VoiceOver is on, and with it on a touch moves "+
	"VoiceOver's cursor instead of reaching the app — a swipe moves the cursor too, and scrolls nothing. "+
	"app_tap, app_check, app_type and a dialog's button are made as a VoiceOver user makes them, a touch and "+
	"then a double tap; this gesture is not").
	WithRemedy("turn VoiceOver off on the device — Settings, Accessibility, VoiceOver, or the Accessibility " +
		"Shortcut — and call again, or use app_tap, which works with it on")

// voiceOverUnsupported is the start of what a runner built before Xcode 27
// answers every VoiceOver route with.
const voiceOverUnsupported = "does not support VoiceOver control"

// ScreenReaderOn asks WebDriverAgent whether VoiceOver is on. A runner that
// cannot tell says so once and is not asked again.
func (w *WDA) ScreenReaderOn(ctx context.Context) (on, known bool) {
	if w.tv() {
		return false, false
	}
	w.mu.Lock()
	unsupported := w.voiceOverUnsupported
	w.mu.Unlock()
	if unsupported {
		return false, false
	}
	var resp struct {
		Value struct {
			Enabled bool `json:"enabled"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, "/wda/voiceOver/enabled", nil, &resp); err != nil {
		if strings.Contains(err.Error(), voiceOverUnsupported) {
			w.mu.Lock()
			w.voiceOverUnsupported = true
			w.mu.Unlock()
		}
		return false, false
	}
	return resp.Value.Enabled, true
}

// voiceOverGuard refuses a touch while VoiceOver is on, unless its caller
// asked first and is touching as VoiceOver requires.
func (w *WDA) voiceOverGuard(ctx context.Context) error {
	if screenReaderWasAsked(ctx) {
		return nil
	}
	if on, _ := w.ScreenReaderOn(ctx); on {
		return errVoiceOverOn
	}
	return nil
}

// FocusAt touches a point, moving VoiceOver's cursor to what is there, and
// returns the sentence VoiceOver then speaks.
func (w *WDA) FocusAt(ctx context.Context, x, y int) (string, error) {
	ctx = ScreenReaderAsked(ctx)
	if err := w.w3c.pointerSequence(ctx, tapActions(w.toPoints(x), w.toPoints(y))); err != nil {
		return "", err
	}
	return w.voiceOverSpeech(ctx)
}

// ActivateFocused double-taps, which with VoiceOver on activates what its
// cursor is on. WebDriverAgent's own double tap, since a chain's pause is
// dropped while the pointer is up (see DoubleTap).
func (w *WDA) ActivateFocused(ctx context.Context, x, y int) error {
	return w.w3c.wdaDoubleTap(ScreenReaderAsked(ctx), w.toPoints(x), w.toPoints(y))
}

// ScreenReaderSpeech is implemented by a backend that can say what the
// screen reader says and move its cursor through the screen in its own
// order. Built in only, as ScreenReader is.
type ScreenReaderSpeech interface {
	// Speech is what the screen reader says about the element its cursor
	// is on.
	Speech(ctx context.Context) (string, error)
	// MoveCursor moves the cursor to the next element, or the one before,
	// and returns what the screen reader says there. At either end it does
	// not move, and says the same again.
	MoveCursor(ctx context.Context, forward bool) (string, error)
}

// AsScreenReaderSpeech returns the driver's screen reader speech, if it has
// any.
func AsScreenReaderSpeech(d Driver) (ScreenReaderSpeech, bool) {
	s, ok := d.(ScreenReaderSpeech)
	return s, ok
}

// Speech is what VoiceOver says about the element its cursor is on.
func (w *WDA) Speech(ctx context.Context) (string, error) { return w.voiceOverSpeech(ctx) }

// MoveCursor is a VoiceOver swipe right or left, through XCUIAutomation's
// VoiceOver service: 0.58 seconds a move on the iPhone 15 Plus, the list
// scrolling to keep the cursor in view. Past the last element it stays and
// says the same again, after about 2.4 seconds.
func (w *WDA) MoveCursor(ctx context.Context, forward bool) (string, error) {
	direction := "backward"
	if forward {
		direction = "forward"
	}
	var resp struct {
		Value struct {
			Utterance string `json:"utterance"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodPost, "/wda/voiceOver/move", map[string]interface{}{"direction": direction},
		&resp); err != nil {
		return "", err
	}
	return resp.Value.Utterance, nil
}

// voiceOverSpeech is what VoiceOver says about the element its cursor is on.
func (w *WDA) voiceOverSpeech(ctx context.Context) (string, error) {
	var resp struct {
		Value struct {
			Utterance string `json:"utterance"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, "/wda/voiceOver/currentSpeech", nil, &resp); err != nil {
		return "", err
	}
	return resp.Value.Utterance, nil
}

// focusFieldForVoiceOver gives a text field focus as a VoiceOver user does,
// when VoiceOver is on: WebDriverAgent's own typing touches the field, which
// with VoiceOver on only moves the cursor, so nothing typed reached it and
// the failure read as a dropped keystroke.
func (w *WDA) focusFieldForVoiceOver(ctx context.Context, n *uitree.Node) error {
	if on, _ := w.ScreenReaderOn(ctx); !on || n == nil || n.Bounds.Empty() {
		return nil
	}
	x, y := n.Bounds.Center()
	if _, err := w.FocusAt(ctx, x, y); err != nil {
		return err
	}
	if err := w.ActivateFocused(ctx, x, y); err != nil {
		return err
	}
	// The keyboard comes up after the double tap; typing into it before it
	// has is the race CHALLENGES 61 is about.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(voiceOverFocusSettle):
	}
	return nil
}

// voiceOverFocusSettle is how long a field is given to take focus after
// VoiceOver's double tap. Measured: the keyboard was up and the field
// editing within a second on the iPhone 15 Plus.
const voiceOverFocusSettle = 700 * time.Millisecond
