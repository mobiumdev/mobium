package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// accessibility is app_accessibility: read the device's accessibility
// settings, or change one for the rest of the session.
//
// Every change lasts only as long as the session. The first change to each
// setting keeps an undo that restores the exact raw values found, and the
// undos run when the session ends — a daemon stop, a device gone, a restart
// — so a flow that tests Bold Text or a larger text size leaves the device
// as it was. On somebody's phone that is the point.
func (h *Handlers) accessibility(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.accessibilityOn(ctx, s, args)
}

// accessibilityOn is app_accessibility once the device is resolved.
func (h *Handlers) accessibilityOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ax, ok := mobiumdriver.AsAccessibility(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapAccessibility, "read or change accessibility settings")
	}
	name := strings.ToLower(stringArg(args, "setting"))
	value := stringArg(args, "value")
	if name != "" && !knownAXSetting(name) {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown accessibility setting %q — one of %s",
			name, strings.Join(device.AXSettings, ", "))
	}
	if name == "" && value != "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "a value needs a setting to go with it — one of %s",
			strings.Join(device.AXSettings, ", "))
	}

	// Every setting this device has, and why it lacks the rest.
	if name == "" {
		view := AccessibilityView{Device: s.dev.Serial, Settings: map[string]string{}, Unsupported: map[string]string{}}
		var lines []string
		for _, n := range device.AXSettings {
			v, err := ax.AccessibilitySetting(ctx, n)
			if mobiumerr.CodeOf(err) == mobiumerr.Unsupported {
				view.Unsupported[n] = err.Error()
				continue
			}
			if err != nil {
				return nil, err
			}
			view.Settings[n] = v
			lines = append(lines, fmt.Sprintf("%-28s %s%s", n, v, changedMark(s, n)))
		}
		return Result(strings.Join(lines, "\n"), view), nil
	}

	if value == "" {
		v, err := ax.AccessibilitySetting(ctx, name)
		if err != nil {
			return nil, err
		}
		return Result(fmt.Sprintf("%s %s%s", name, v, changedMark(s, name)),
			AccessibilityView{Device: s.dev.Serial, Setting: name, Value: v}), nil
	}

	before, err := ax.AccessibilitySetting(ctx, name)
	if err != nil {
		return nil, err
	}
	undo, err := ax.SetAccessibilitySetting(ctx, name, value)
	// Kept even when the write failed to confirm: something may have been
	// written, and putting it back is still owed.
	if undo != nil {
		if _, kept := s.axUndo[name]; !kept {
			if s.axUndo == nil {
				s.axUndo = map[string]device.AXUndo{}
			}
			s.axUndo[name] = undo
		}
	}
	if err != nil {
		return nil, err
	}
	after, err := ax.AccessibilitySetting(ctx, name)
	if err != nil {
		return nil, err
	}
	// The screen is drawn differently now — bigger or bolder text, a
	// different contrast — and on Android a text-size change recreates the
	// running app's activity. Refs from before belong to another screen.
	delete(h.refs, s.dev.Serial)
	return Result(fmt.Sprintf("%s %s (was %s) — put back when the session ends", name, after, before),
		AccessibilityView{Device: s.dev.Serial, Setting: name, Value: after, Previous: before, Restored: true}), nil
}

func knownAXSetting(name string) bool {
	for _, n := range device.AXSettings {
		if n == name {
			return true
		}
	}
	return false
}

// changedMark flags a setting this session changed, since it will go back.
func changedMark(s *session, name string) string {
	if _, ok := s.axUndo[name]; ok {
		return "   (changed by this session; put back when it ends)"
	}
	return ""
}

// restoreAccessibility runs every undo the session kept, within one budget
// for all of them: on a phone the first undo puts back every setting in one
// pass through Settings, which can take most of it. Bounded, so a device that
// has gone cannot hold a shutdown up.
func (s *session) restoreAccessibility() {
	ctx, cancel := context.WithTimeout(context.Background(), axRestoreTimeout)
	defer cancel()
	for name, undo := range s.axUndo {
		_ = undo(ctx) // a device that has gone is a device nothing more can be done to
		delete(s.axUndo, name)
	}
}

// axRestoreTimeout bounds putting a session's accessibility settings back,
// inside the daemon's own close budget.
const axRestoreTimeout = 60 * time.Second

// AccessibilityView is the result of app_accessibility.
type AccessibilityView struct {
	Device string `json:"device"`
	// Settings and Unsupported answer a read of everything: each setting
	// this device has, and why it lacks the others.
	Settings    map[string]string `json:"settings,omitempty"`
	Unsupported map[string]string `json:"unsupported,omitempty"`
	// Setting and Value answer a read or a change of one.
	Setting string `json:"setting,omitempty"`
	Value   string `json:"value,omitempty"`
	// Previous is set only when the call changed something, and Restored
	// says the change will be undone when the session ends.
	Previous string `json:"previous,omitempty"`
	Restored bool   `json:"restored,omitempty"`
}
