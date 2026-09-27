package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

const (
	// pressLead is how long the first finger rests before the second one
	// acts. Under Android's 500ms long-press timeout on purpose: the element
	// under a held finger opens its own context menu past that, and then the
	// second finger lands on the menu instead of the screen. Raise it for an
	// app that wants the hold to register as a long press first.
	pressLead = 300 * time.Millisecond

	// pressDragMove is how long the second finger's travel takes.
	pressDragMove = 600 * time.Millisecond
)

// pressTap is app_press_tap: one finger holds, a second taps.
//
// Named for the touch-gesture charts it comes from ("press and tap"), and not
// a tap with an option, because the two fingers do different things at
// different times — which is also why it needs its own driver capability
// rather than Pincher's mirrored pair. Like app_drag and app_zoom it reports
// that the gesture was delivered: what a held finger plus a tap *means* is the
// app's own decision.
func (h *Handlers) pressTap(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.pressTapOn(ctx, s, args)
}

// pressTapOn is app_press_tap once the device is resolved.
func (h *Handlers) pressTapOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	mt, ok := mobiumdriver.AsMultiToucher(s.driver)
	if !ok {
		return nil, noMultiTouch(s.backend, "press and tap")
	}
	lead, err := durationArg(args, "lead_ms", pressLead)
	if err != nil {
		return nil, err
	}
	pts, names, err := h.points(ctx, s, args, "app_press_tap", []string{"hold", "tap"})
	if err != nil {
		return nil, err
	}
	if err := mt.PressTap(ctx, pts[0], pts[1], lead); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)
	return Result(fmt.Sprintf("held %s at (%d,%d) and tapped %s at (%d,%d) with a second finger, "+
		"%s after the first landed — the gesture was delivered; what it did is the app's "+
		"own state, so map the screen again to see it",
		names[0], pts[0].X, pts[0].Y, names[1], pts[1].X, pts[1].Y, lead),
		PressTapView{Hold: stringArg(args, "hold"), Tap: stringArg(args, "tap"),
			X1: pts[0].X, Y1: pts[0].Y, X2: pts[1].X, Y2: pts[1].Y,
			LeadMS: int(lead.Milliseconds())}), nil
}

// pressDrag is app_press_drag: one finger holds, a second drags.
func (h *Handlers) pressDrag(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.pressDragOn(ctx, s, args)
}

// pressDragOn is app_press_drag once the device is resolved.
func (h *Handlers) pressDragOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	mt, ok := mobiumdriver.AsMultiToucher(s.driver)
	if !ok {
		return nil, noMultiTouch(s.backend, "press and drag")
	}
	lead, err := durationArg(args, "lead_ms", pressLead)
	if err != nil {
		return nil, err
	}
	move, err := durationArg(args, "duration_ms", pressDragMove)
	if err != nil {
		return nil, err
	}
	pts, names, err := h.points(ctx, s, args, "app_press_drag", []string{"hold", "from", "to"})
	if err != nil {
		return nil, err
	}
	if pts[1] == pts[2] {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "from and to are both at (%d, %d), so the second finger "+
			"would not move — that is app_press_tap", pts[1].X, pts[1].Y)
	}
	if err := mt.PressDrag(ctx, pts[0], pts[1], pts[2], lead, move); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)
	return Result(fmt.Sprintf("held %s at (%d,%d) and dragged a second finger from %s at (%d,%d) "+
		"to %s at (%d,%d) — the gesture was delivered; what it did is the app's own state, "+
		"so map the screen again to see it",
		names[0], pts[0].X, pts[0].Y, names[1], pts[1].X, pts[1].Y, names[2], pts[2].X, pts[2].Y),
		PressDragView{Hold: stringArg(args, "hold"), From: stringArg(args, "from"), To: stringArg(args, "to"),
			X1: pts[0].X, Y1: pts[0].Y, X2: pts[1].X, Y2: pts[1].Y, X3: pts[2].X, Y3: pts[2].Y,
			LeadMS: int(lead.Milliseconds()), DurationMS: int(move.Milliseconds())}), nil
}

func noMultiTouch(backend Backend, gesture string) error {
	return mobiumerr.New(mobiumerr.Unsupported, "the %s backend cannot put a second finger down while the first "+
		"holds, so it cannot %s — switch to --backend uiautomator2 on Android, or use an iOS device",
		backend, gesture)
}

func durationArg(args map[string]interface{}, key string, def time.Duration) (time.Duration, error) {
	ms := intArgOr(args, key, int(def.Milliseconds()))
	if ms <= 0 {
		return 0, mobiumerr.New(mobiumerr.InvalidArgument, "%s is a time and must be above zero, got %d", key, ms)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// points resolves the named ends of a multi-finger gesture: either every
// name as a ref or locator, or x1,y1 … one pair per name. All of them are
// resolved before anything is touched, for app_drag's reason — once a finger
// is down the screen may move under the rest.
func (h *Handlers) points(ctx context.Context, s *session, args map[string]interface{},
	tool string, keys []string) ([]mobiumdriver.Point, []string, error) {

	var given, missing []string
	for _, k := range keys {
		if stringArg(args, k) != "" {
			given = append(given, k)
		} else {
			missing = append(missing, k)
		}
	}
	coords := make([]mobiumdriver.Point, len(keys))
	nCoords := 0
	for i := range keys {
		x, hx := intArg(args, fmt.Sprintf("x%d", i+1))
		y, hy := intArg(args, fmt.Sprintf("y%d", i+1))
		if hx && hy {
			coords[i] = mobiumdriver.Point{X: x, Y: y}
			nCoords++
		} else if hx || hy {
			return nil, nil, mobiumerr.New(mobiumerr.InvalidArgument, "x%d and y%d must be given together", i+1, i+1)
		}
	}

	switch {
	case len(given) > 0 && nCoords > 0:
		return nil, nil, mobiumerr.New(mobiumerr.InvalidArgument, "give either %v as refs or locators, or "+
			"coordinates, not both", keys)
	case nCoords > 0 && nCoords < len(keys):
		return nil, nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s needs a point for each of %v: x1,y1 through "+
			"x%d,y%d", tool, keys, len(keys), len(keys))
	case nCoords == len(keys):
		names := make([]string, len(keys))
		for i, k := range keys {
			names[i] = "the " + k + " point"
		}
		return coords, names, nil
	case len(missing) > 0:
		return nil, nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s needs %v as refs or locators (missing %v), "+
			"or a point for each", tool, keys, missing)
	}

	pts := make([]mobiumdriver.Point, len(keys))
	names := make([]string, len(keys))
	for i, k := range keys {
		target := stringArg(args, k)
		names[i] = target
		if s.web != nil {
			x, y, _, err := h.aimWeb(ctx, s, target)
			if err != nil {
				return nil, nil, err
			}
			pts[i] = mobiumdriver.Point{X: x, Y: y}
			continue
		}
		node, _, err := h.resolveNode(ctx, s, target)
		if err != nil {
			return nil, nil, err
		}
		x, y := node.Bounds.Center()
		pts[i] = mobiumdriver.Point{X: x, Y: y}
	}
	return pts, names, nil
}

// PressTapView is the result of app_press_tap.
type PressTapView struct {
	Hold   string `json:"hold,omitempty"`
	Tap    string `json:"tap,omitempty"`
	X1     int    `json:"x1"`
	Y1     int    `json:"y1"`
	X2     int    `json:"x2"`
	Y2     int    `json:"y2"`
	LeadMS int    `json:"lead_ms"`
}

// PressDragView is the result of app_press_drag.
type PressDragView struct {
	Hold       string `json:"hold,omitempty"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	X1         int    `json:"x1"`
	Y1         int    `json:"y1"`
	X2         int    `json:"x2"`
	Y2         int    `json:"y2"`
	X3         int    `json:"x3"`
	Y3         int    `json:"y3"`
	LeadMS     int    `json:"lead_ms"`
	DurationMS int    `json:"duration_ms"`
}
