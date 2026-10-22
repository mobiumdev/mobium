package agent

import (
	"fmt"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// The shapes below are the data half of every tool result. They are a public
// contract — the CLI's --json and the language clients read them — so field
// names are chosen once and changed deliberately.

// ElementView is one mapped element.
type ElementView struct {
	Ref     string       `json:"ref"`
	Label   string       `json:"label"`
	Role    string       `json:"role,omitempty"`
	Locator *LocatorView `json:"locator,omitempty"`
	Bounds  BoundsView   `json:"bounds"`
	// Context names the WebView an element came from, absent for native ones.
	Context string `json:"context,omitempty"`
	// Checked is a checkbox, radio or switch's state, absent for anything
	// with no such state. map's text had it and this did not, so no client
	// could tell whether a box was ticked.
	Checked *bool `json:"checked,omitempty"`
	// Selected is true for what the platform reports chosen — the current
	// tab, a segment — and absent otherwise.
	Selected bool `json:"selected,omitempty"`
	// Focused is true for what the platform reports holding input focus —
	// the field typed into, the tile a TV's D-pad reached — and absent
	// otherwise.
	Focused bool `json:"focused,omitempty"`
	// Disabled is true for what the platform reports not enabled, which an
	// action would wait for and then refuse, and absent otherwise.
	Disabled bool `json:"disabled,omitempty"`
	// Value is what a slider reads, as the app states it, and absent for
	// everything else.
	Value string `json:"value,omitempty"`
	// ValueScale says what Value is counted in when it is not the app's own
	// number: an Android seek bar's steps, whose maximum is not reported.
	ValueScale string `json:"value_scale,omitempty"`
	// Covered names a control drawn over all of the element, which a tap
	// would be refused for, and is absent otherwise.
	Covered string `json:"covered,omitempty"`
}

// LocatorView is how a ref resolves on a later screen.
type LocatorView struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Exact bool   `json:"exact,omitempty"`
	Role  string `json:"role,omitempty"`
}

// BoundsView is an on-screen rectangle in device pixels, on both platforms.
type BoundsView struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// Center is the point a tap targets, included so a caller need not recompute
// it from the corners.
func (b BoundsView) Center() (int, int) { return (b.X1 + b.X2) / 2, (b.Y1 + b.Y2) / 2 }

// MapView is the result of app_map and app_find.
type MapView struct {
	Elements []ElementView `json:"elements"`
	Context  string        `json:"context"`
	Device   string        `json:"device"`
	// Diff is what changed since the last map, when diff was asked for.
	Diff *MapDiffView `json:"diff,omitempty"`
	// Notes are what map says about the screen beyond its elements: a
	// popover hiding what is behind it, a screen read by geometry, a lock
	// screen, a list still loading. The text has them too.
	Notes []string `json:"notes,omitempty"`
}

// DeviceView is one attached device or simulator.
type DeviceView struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	State    string `json:"state"`
	Model    string `json:"model,omitempty"`
	Runtime  string `json:"runtime,omitempty"`
	Emulator bool   `json:"emulator"`
	// Navigation is an Android device's navigation mode: "gestures",
	// "three-button" or "two-button". It decides what an edge swipe is.
	Navigation string `json:"navigation,omitempty"`
}

// DevicesView is the result of app_devices.
type DevicesView struct {
	Devices []DeviceView `json:"devices"`
	// Notes carries a missing toolchain, which is normal rather than fatal:
	// most machines have only one of the two installed.
	Notes []string `json:"notes,omitempty"`
}

// TextView is the result of app_text.
type TextView struct {
	Text string `json:"text"`
}

// ActionView is the result of a tool that did something to the screen.
type ActionView struct {
	Action  string `json:"action"`
	Target  string `json:"target,omitempty"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Context string `json:"context,omitempty"`
	// Cover is what the app had drawn over the target, when anything was:
	// a control the touch was aimed around, or a view that may take it.
	Cover *CoverView `json:"cover,omitempty"`
	// Value is what a slider reads after app_fill moved it.
	Value string `json:"value,omitempty"`
	// Position is where app_fill moved a slider to, 0 to 1 — the caller's
	// own scale, which the reading above may not be.
	Position *float64 `json:"position,omitempty"`
	// ValueScale is ElementView's: what Value is counted in, when not the
	// app's number.
	ValueScale string `json:"value_scale,omitempty"`
}

// ScreenshotView is the result of app_screenshot when written to disk.
type ScreenshotView struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

// ScreenshotSavedMessage is what a screenshot says once it is on disk — the
// daemon's when it saved it, and the CLI's or pipe's when the image came back
// and they saved it where the caller asked.
func ScreenshotSavedMessage(path string, n int) string {
	return fmt.Sprintf("saved %s (%d bytes)", path, n)
}

// ContextsView is the result of app_contexts.
type ContextsView struct {
	Contexts []ContextView `json:"contexts"`
	Current  string        `json:"current"`
	// Behind names pages whose app is not in front: open, not on screen, and
	// refused by app_context until the app is launched.
	Behind []string `json:"behind,omitempty"`
}

// ContextView is one automatable context.
type ContextView struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
	Current bool   `json:"current,omitempty"`
}

// elementView converts a mapped entry to its wire shape.
func elementView(e uitree.Entry) ElementView {
	v := ElementView{
		Ref:   e.Ref,
		Label: e.Label,
		Role:  e.Role,
		Locator: &LocatorView{
			Kind:  string(e.Locator.Kind),
			Value: e.Locator.Value,
			Exact: e.Locator.Exact,
			Role:  e.Locator.Role,
		},
		Bounds:   boundsView(e.Bounds),
		Checked:  e.Checked,
		Selected: e.Selected,
		Focused:  e.Focused,
		Disabled: e.Disabled,
		Value:    e.Value,
		Covered:  e.Covered,
	}
	if e.ValueSteps {
		v.ValueScale = seekBarSteps
	}
	return v
}

// seekBarSteps is what an Android seek bar's value is counted in, said
// beside it in map and app_fill (CHALLENGES 302).
const seekBarSteps = "the seek bar's own steps — Android does not report their maximum, so this is not a " +
	"percentage or the app's number; the app's own screen says what it holds"

func boundsView(r uitree.Rect) BoundsView {
	return BoundsView{X1: r.X1, Y1: r.Y1, X2: r.X2, Y2: r.Y2}
}
