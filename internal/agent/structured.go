package agent

import "github.com/mobiumdev/mobium/internal/uitree"

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
}

// DeviceView is one attached device or simulator.
type DeviceView struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	State    string `json:"state"`
	Model    string `json:"model,omitempty"`
	Runtime  string `json:"runtime,omitempty"`
	Emulator bool   `json:"emulator"`
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
}

// ScreenshotView is the result of app_screenshot when written to disk.
type ScreenshotView struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
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
	return ElementView{
		Ref:   e.Ref,
		Label: e.Label,
		Role:  e.Role,
		Locator: &LocatorView{
			Kind:  string(e.Locator.Kind),
			Value: e.Locator.Value,
			Exact: e.Locator.Exact,
			Role:  e.Locator.Role,
		},
		Bounds:  boundsView(e.Bounds),
		Checked: e.Checked,
	}
}

func boundsView(r uitree.Rect) BoundsView {
	return BoundsView{X1: r.X1, Y1: r.Y1, X2: r.X2, Y2: r.Y2}
}
