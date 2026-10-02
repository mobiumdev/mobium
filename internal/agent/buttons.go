package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// press is app_press: a hardware button.
//
// On Android **back is primary navigation** — an app that opens a detail
// screen expects it — and no amount of tapping what `map` shows substitutes.
func (h *Handlers) press(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.pressOn(ctx, s, args)
}

func (h *Handlers) pressOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsButtons(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapButtons, "press hardware buttons")
	}

	button := strings.ToLower(strings.TrimSpace(stringArg(args, "button")))
	if button == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_press needs a button — this device has %s",
			mobiumdriver.ButtonNames(ctrl.SupportedButtons()))
	}
	// Two checks, deliberately. A name that is not in the vocabulary at all is
	// a typo and is caught here, before any device is touched. A name that is
	// real but absent on this platform is passed to the driver, because only
	// the driver can say *why* — that iOS has no back button and what to do
	// instead is worth far more than a list of what it does have.
	known := false
	for _, b := range mobiumdriver.AllButtons() {
		if b == button {
			known = true
			break
		}
	}
	if !known {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "there is no %q button — mobium knows %s",
			button, mobiumdriver.ButtonNames(mobiumdriver.AllButtons()))
	}

	// Home and back have outcomes worth checking. Home brings the launcher
	// forward. Back either leaves the app or keeps it in front, and which
	// one is the difference between going back a screen and closing the
	// app — MobiumApp closed on every back from a demo and Mobium said only
	// "pressed back" (docs/BACK.md). The others have no platform-wide signal
	// — volume moved nothing measurable on a device with no audio playing —
	// so they are reported as sent rather than as confirmed.
	gesture := boolArg(args, "gesture")
	if gesture && button != mobiumdriver.ButtonBack {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "only back has a gesture; press %s without one", button)
	}
	var before, beforeScreen, beforeTitle string
	var beforeTree *uitree.Tree
	if button == mobiumdriver.ButtonHome || button == mobiumdriver.ButtonBack {
		if tree, err := s.driver.Snapshot(ctx); err == nil && tree != nil {
			beforeTree, before, beforeScreen = tree, tree.Package(), fingerprint(tree.Root)
			beforeTitle = navigationTitle(tree)
		}
	}

	// A D-pad press moves focus, and focus is the one thing a TV reports
	// about where the user is — measured on a Fire TV, where `focused`
	// followed every press — so that is what it is read back by.
	dpad := false
	for _, b := range mobiumdriver.DpadButtons() {
		dpad = dpad || b == button
	}
	var beforeFocus *uitree.Node
	if dpad {
		if tree, err := s.driver.Snapshot(ctx); err == nil {
			beforeFocus = focusedNode(tree)
		}
	}

	// On Android the app in front is named by the task, not the window,
	// where a browser draws it: a back out of a Trusted Web Activity was
	// reported as leaving Chrome. CHALLENGES 205.
	beforeName := before
	if button == mobiumdriver.ButtonBack {
		beforeName = h.taskOwner(ctx, s, before)
	}

	how := "pressed " + button
	if gesture {
		from, err := h.gestureBack(ctx, s, beforeTree)
		if err != nil {
			return nil, err
		}
		how = "swiped back from " + from
	} else if err := ctrl.Press(ctx, button); err != nil {
		return nil, err
	}

	// Any press can move the screen, so refs from the last map are gone.
	delete(h.refs, s.dev.Serial)

	view := PressView{Button: button, Gesture: gesture, Device: s.dev.Serial}
	if button == mobiumdriver.ButtonBack {
		// Without a read from before, there is nothing to compare: say so
		// rather than wait for a change that cannot be seen.
		var after string
		if beforeTree != nil {
			after = h.taskOwner(ctx, s, h.awaitBack(ctx, s, before, beforeScreen))
		}
		var afterTitle string
		if s.backend == BackendWDA {
			if tree, err := s.driver.Snapshot(ctx); err == nil && tree != nil {
				afterTitle = navigationTitle(tree)
			}
		}
		msg, ok := backOutcome(how, beforeName, after, beforeTitle, afterTitle)
		view.Confirmed, view.Foreground, view.Title = ok, after, afterTitle
		if ok && beforeName != "" && after != beforeName {
			view.Left = beforeName
		}
		return Result(msg, view), nil
	}
	if dpad {
		msg, focus, ok := h.awaitFocus(ctx, s, button, beforeFocus)
		view.Confirmed, view.Focus = ok, focus
		return Result(msg, view), nil
	}
	if button == mobiumdriver.ButtonHome {
		if after, ok := h.awaitLauncher(ctx, s, before); ok {
			view.Confirmed, view.Foreground = true, after
			return Result(fmt.Sprintf("pressed home — %s is in the foreground", after), view), nil
		}
	}
	return Result(fmt.Sprintf("pressed %s", button), view), nil
}

// awaitLauncher waits for the home screen to come forward after a home press.
func (h *Handlers) awaitLauncher(ctx context.Context, s *session, before string) (string, bool) {
	deadline := time.Now().Add(3 * time.Second)
	for {
		app, _ := h.screenNow(ctx, s)
		if app != "" && app != before {
			return app, true
		}
		if time.Now().After(deadline) {
			return "", false
		}
		select {
		case <-ctx.Done():
			return "", false
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// focusedNode is the innermost node that has input focus, or nil.
func focusedNode(t *uitree.Tree) *uitree.Node {
	var f *uitree.Node
	t.Walk(func(n *uitree.Node) bool {
		if n.Focused {
			f = n
		}
		return true
	})
	return f
}

// sameNode is whether two reads' nodes are the same element: where it is
// and what it says, since a read hands out new nodes every time.
func sameNode(a, b *uitree.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Bounds == b.Bounds && uitree.Describe(a) == uitree.Describe(b)
}

// awaitFocus reads where focus went after a D-pad press. Focus moving is
// confirmed; focus that stays put — at the edge of a row, say — is reported
// as not moving, and a screen where nothing reports focus (an app that
// draws to one surface) as unreadable rather than as a success.
func (h *Handlers) awaitFocus(ctx context.Context, s *session, button string, before *uitree.Node) (msg, focus string, ok bool) {
	var now *uitree.Node
	read := false
	_ = pollUntil(ctx, 2*time.Second, func(ctx context.Context) (bool, error) {
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			return false, nil
		}
		now, read = focusedNode(tree), true
		return now != nil && !sameNode(before, now), nil
	})
	switch {
	case read && now != nil && !sameNode(before, now):
		focus = uitree.Describe(now)
		return fmt.Sprintf("pressed %s — focus moved to %s", button, focus), focus, true
	case now != nil:
		focus = uitree.Describe(now)
		return fmt.Sprintf("pressed %s — focus did not move from %s", button, focus), focus, false
	default:
		return fmt.Sprintf("pressed %s — nothing on screen reports focus, so where it went "+
			"cannot be read", button), "", false
	}
}

// PressView is the result of app_press.
type PressView struct {
	Button string `json:"button"`
	// Confirmed distinguishes a press whose effect was checked from one that
	// was merely sent: home, back, and a D-pad press that moved focus.
	Confirmed  bool   `json:"confirmed"`
	Foreground string `json:"foreground,omitempty"`
	// Focus is what has focus after a D-pad press, labeled as map labels it.
	Focus string `json:"focus,omitempty"`
	// Gesture is a back given as a swipe in from the edge, not the key.
	Gesture bool `json:"gesture,omitempty"`
	// Left is the app a back took the user out of. Empty when the app
	// stayed in front: back went somewhere inside it.
	Left string `json:"left,omitempty"`
	// Title is what an iOS navigation bar says after a back.
	Title  string `json:"title,omitempty"`
	Device string `json:"device"`
}

// lock is app_lock: read whether the screen is locked, or lock and unlock it.
//
// A state rather than a power-button press, because power is a toggle: asking
// twice leaves the device where it started and the caller cannot tell which
// way it went.
func (h *Handlers) lock(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.lockOn(ctx, s, args)
}

func (h *Handlers) lockOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsScreenLock(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapScreenLock, "lock or unlock the screen")
	}

	want := strings.ToLower(strings.TrimSpace(stringArg(args, "state")))
	switch want {
	case "", "lock", "locked", "unlock", "unlocked":
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown state %q (want \"lock\" or \"unlock\", "+
			"or omit it to read)", want)
	}

	before, err := ctrl.ScreenLocked(ctx)
	if err != nil {
		return nil, err
	}
	if want == "" {
		return Result(lockWord(before), LockView{Locked: before, Device: s.dev.Serial}), nil
	}

	locked := want == "lock" || want == "locked"
	if before == locked {
		return Result(fmt.Sprintf("already %s", lockWord(locked)),
			LockView{Locked: locked, Device: s.dev.Serial}), nil
	}
	if err := ctrl.SetScreenLocked(ctx, locked); err != nil {
		return nil, err
	}
	// The screen behind a lock is not the screen that comes back.
	delete(h.refs, s.dev.Serial)

	return Result(fmt.Sprintf("%s (was %s)", lockWord(locked), lockWord(before)),
		LockView{Locked: locked, Previous: &before, Device: s.dev.Serial}), nil
}

func lockWord(locked bool) string {
	if locked {
		return "locked"
	}
	return "unlocked"
}

// LockView is the result of app_lock.
type LockView struct {
	Locked   bool   `json:"locked"`
	Previous *bool  `json:"previous,omitempty"`
	Device   string `json:"device"`
}
