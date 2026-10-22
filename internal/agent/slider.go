package agent

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// setSlider is app_fill on a slider: the text is a position on its track,
// from 0 at its start to 1 at its end, and the result is what the app
// reports once it has moved.
//
// A position rather than a value, because a position is what the platform
// can set, and the value is the app's to state: Ice Cubes' font scaling
// slider reports "50%" at 0 and "150%" at 1, and its caption beside it
// "Font Scaling: 1.0" at 0.5. So the value is read back and reported, never
// computed from the position, and the move is not claimed to be exact:
// WebDriverAgent moves the thumb as a finger would, and 0.5 read "110%" from
// one start and "100%" from another (CHALLENGES 219). A caller wanting a
// value reads it and fills again.
func (h *Handlers) setSlider(ctx context.Context, s *session, target string, node *uitree.Node, text string) (*ToolsCallResult, error) {
	pos, err := sliderPosition(text)
	if err != nil {
		return nil, err
	}
	slider, ok := mobiumdriver.AsSlider(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapSlider, "move a slider")
	}
	was := node.Value
	_, otherThumb, isThumb := uitree.RangeThumb(node)
	if err := slider.SetSliderPosition(ctx, node, pos); err != nil {
		return nil, err
	}
	if isThumb {
		return h.readThumbBack(ctx, s, target, node, otherThumb, pos, was)
	}
	after, _, err := h.resolveNode(ctx, s, target)
	if err != nil {
		// A drag can be taken as a system gesture, and close the app.
		if tree, serr := s.driver.Snapshot(ctx); serr == nil && node.Package != "" && tree.Package() != node.Package {
			return nil, mobiumerr.New(mobiumerr.NotConfirmed, "moved %s to %s, and %s left the front — %s is in "+
				"front now: the move was taken as a system gesture, the back gesture from the screen's edge most "+
				"likely", target, text, node.Package, tree.Package()).
				WithRemedy("bring the app back and fill the slider with a position away from the screen's edge")
		}
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "moved %s to %s and could not read it back: %w", target, text, err)
	}
	msg := fmt.Sprintf("moved %s to position %s — it reads %q", target, formatPosition(pos), after.Value)
	if was != "" {
		msg += fmt.Sprintf(", was %q", was)
	}
	// What moved is what was read: a fill that read the same value after
	// was reported as moved, while a touch inside a range slider's thumb
	// had moved nothing (CHALLENGES 357).
	if was != "" && after.Value == was {
		msg = fmt.Sprintf("touched %s at position %s and it still reads %q — nothing moved, or it was there already",
			target, formatPosition(pos), after.Value)
	}
	view := ActionView{Action: "fill", Target: target, Value: after.Value, Position: &pos}
	if after.ValueSteps {
		msg += ", in " + seekBarSteps
		view.ValueScale = seekBarSteps
	}
	return Result(msg, view), nil
}

// sliderPosition reads a position from 0 to 1, and refuses anything else by
// name: WebDriverAgent took "abc" as 0 and moved the slider to the start.
func sliderPosition(text string) (float64, error) {
	pos, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(pos) || pos < 0 || pos > 1 {
		return 0, mobiumerr.New(mobiumerr.InvalidArgument, "a slider is filled with its position, a number from 0 "+
			"(the start of its track) to 1 (the end), not %q; map shows the value it reads now", text).
			WithRemedy("app_fill with a number from 0 to 1, e.g. 0.5 for the middle")
	}
	return pos, nil
}

func formatPosition(p float64) string { return strconv.FormatFloat(p, 'f', -1, 64) }

// readThumbBack reads a range slider's thumb after a fill from one read of
// the screen, never resolving it again, which on a miss scrolls to look.
// A thumb stopped against the other one can be drawn under it, and then
// only the other is in the tree: Material's RangeSlider with both at $30
// listed "Range end" alone, and the scroll that looked for "Range start"
// swiped the chip row and pressed a chip (CHALLENGES 357). Both thumbs then
// read the same, which is what is reported.
func (h *Handlers) readThumbBack(ctx context.Context, s *session, target string, node, other *uitree.Node,
	pos float64, was string) (*ToolsCallResult, error) {
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "moved %s to %s and could not read it back: %w",
			target, formatPosition(pos), err)
	}
	if node.Package != "" && tree.Package() != node.Package {
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "moved %s to %s, and %s left the front — %s is in "+
			"front now: the move was taken as a system gesture, the back gesture from the screen's edge most "+
			"likely", target, formatPosition(pos), node.Package, tree.Package()).
			WithRemedy("bring the app back and fill the slider with a position away from the screen's edge")
	}
	var thumb, beside *uitree.Node
	tree.Walk(func(n *uitree.Node) bool {
		if _, _, ok := uitree.RangeThumb(n); ok || uitree.HasClassRole(n, "slider") || uitree.HasClassRole(n, "adjustable") {
			switch n.Label {
			case node.Label:
				thumb = n
			case other.Label:
				beside = n
			}
		}
		return true
	})
	view := ActionView{Action: "fill", Target: target, Position: &pos}
	switch {
	case thumb != nil:
		view.Value = thumb.Value
		msg := fmt.Sprintf("moved %s to position %s — it reads %q, was %q", target, formatPosition(pos), thumb.Value, was)
		if thumb.Value == was {
			msg = fmt.Sprintf("touched %s at position %s and it still reads %q — nothing moved, or it was there already",
				target, formatPosition(pos), thumb.Value)
		}
		if thumb.ValueSteps {
			msg += ", in " + seekBarSteps
			view.ValueScale = seekBarSteps
		}
		return Result(msg, view), nil
	case beside != nil:
		view.Value = beside.Value
		return Result(fmt.Sprintf("moved %s toward position %s; it stopped against %q and is drawn under it, so "+
			"only %q is on the screen — both read %q", target, formatPosition(pos), other.Label, other.Label,
			beside.Value), view), nil
	}
	return nil, mobiumerr.New(mobiumerr.NotConfirmed, "moved %s to %s and neither thumb of its slider is on the "+
		"screen now", target, formatPosition(pos))
}
