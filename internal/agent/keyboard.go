package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// keyboard is app_keyboard: whether the soft keyboard is up and which field
// it types into, typing at that field's cursor, a named key, or hiding it.
//
// app_type finds a field and types into it. This is the other half, for the
// field nothing names — a search box that took focus by itself, a field in a
// WebView-backed form, the next field after enter — and for the keyboard
// itself, which covers the lower half of the screen until something hides it.
func (h *Handlers) keyboard(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.keyboardOn(ctx, s, args)
}

// keyboardOn is app_keyboard once the device is resolved.
func (h *Handlers) keyboardOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	kb, ok := mobiumdriver.AsKeyboard(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapKeyboard, "drive the keyboard")
	}

	text, hasText := args["text"].(string)
	key := strings.ToLower(stringArg(args, "key"))
	hide := boolArg(args, "hide")
	if key != "" && !slices.Contains(mobiumdriver.KeyboardKeys, key) {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown key %q (want %s)", key,
			strings.Join(mobiumdriver.KeyboardKeys, ", "))
	}
	if hide && (hasText || key != "") {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "hide cannot be combined with text or key — "+
			"type first, then hide in a second call")
	}
	view := KeyboardView{Device: s.dev.Serial}

	if hide {
		up, err := kb.KeyboardShown(ctx)
		if err != nil {
			return nil, err
		}
		if !up {
			return Result("the keyboard is already hidden", view), nil
		}
		wasApp, _ := h.screenNow(ctx, s)
		if err := kb.HideKeyboard(ctx); err != nil {
			return nil, err
		}
		delete(h.refs, s.dev.Serial)
		view.Hidden = true
		// Hiding goes through a key the keyboard consumes first; if the
		// keyboard went by itself in between, that key reached the app.
		if now, _ := h.screenNow(ctx, s); wasApp != "" && now != "" && now != wasApp {
			return Result(fmt.Sprintf("hid the keyboard, and %s is now in front instead of %s", now, wasApp), view), nil
		}
		return Result("hid the keyboard", view), nil
	}

	if hasText {
		f, err := kb.TypeIntoFocus(ctx, text)
		if err != nil {
			return nil, err
		}
		view.Focused = fieldView(f)
		// Never echoed into a password field: the result reaches every
		// transcript and log, and the rule is not to print a password.
		if f == nil || !f.Password {
			view.Typed = text
		}
		delete(h.refs, s.dev.Serial)
	}
	if key != "" {
		if err := kb.PressKeyboardKey(ctx, key); err != nil {
			return nil, err
		}
		view.Key = key
		delete(h.refs, s.dev.Serial)
	}

	up, err := kb.KeyboardShown(ctx)
	if err != nil {
		return nil, err
	}
	view.Shown = up
	if view.Focused == nil {
		f, err := kb.FocusedField(ctx)
		if err != nil && mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
			return nil, err
		}
		view.Focused = fieldView(f)
	}

	var b strings.Builder
	switch {
	case hasText && view.Focused != nil && view.Focused.Password:
		fmt.Fprintf(&b, "typed %d characters into %s, a password field — not echoed; it now holds %s",
			len([]rune(text)), view.Focused.name(), view.Focused.shown())
	case hasText:
		fmt.Fprintf(&b, "typed %q into %s, which now holds %s", text, view.Focused.name(), view.Focused.shown())
	case key != "":
		// Reported as sent: what a key does is the app's to decide.
		fmt.Fprintf(&b, "pressed %s", key)
	default:
		if up {
			b.WriteString("the keyboard is up")
		} else {
			b.WriteString("the keyboard is hidden")
		}
	}
	if !hasText {
		if view.Focused != nil {
			fmt.Fprintf(&b, "; focus is on %s, holding %s", view.Focused.name(), view.Focused.shown())
		} else if key != "" || up {
			b.WriteString("; no field has focus")
		}
	}
	if key != "" {
		fmt.Fprintf(&b, "; the keyboard is %s", map[bool]string{true: "up", false: "hidden"}[up])
	}
	return Result(b.String(), view), nil
}

// fieldView is the focused field as reported, its value redacted when it is
// a password — the rule every path that surfaces text follows.
func fieldView(f *mobiumdriver.FocusedField) *FocusedView {
	if f == nil {
		return nil
	}
	v := &FocusedView{ID: f.ID, Kind: f.Kind, Password: f.Password, Value: f.Value}
	if f.Value == "" {
		v.Empty = true
	}
	if f.Password {
		v.Value = fmt.Sprintf("%s (%d characters, hidden: this is a password field)",
			strings.Repeat("•", len([]rune(f.Value))), len([]rune(f.Value)))
	}
	return v
}

// KeyboardView is the result of app_keyboard.
type KeyboardView struct {
	Shown   bool         `json:"shown"`
	Focused *FocusedView `json:"focused,omitempty"`
	Typed   string       `json:"typed,omitempty"`
	Key     string       `json:"key,omitempty"`
	Hidden  bool         `json:"hidden,omitempty"`
	Device  string       `json:"device"`
}

// FocusedView is the field with keyboard focus.
type FocusedView struct {
	ID       string `json:"id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Value    string `json:"value"`
	Password bool   `json:"password,omitempty"`
	Empty    bool   `json:"empty,omitempty"`
}

// shown is the value as prose: an empty field says so rather than printing
// nothing after "holding".
func (f *FocusedView) shown() string {
	if f.Empty {
		return "(empty)"
	}
	return f.Value
}

func (f *FocusedView) name() string {
	if f == nil {
		return "the focused field"
	}
	if f.ID != "" {
		return f.ID
	}
	return "the focused " + f.Kind
}
