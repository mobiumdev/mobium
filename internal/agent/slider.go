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
	if err := slider.SetSliderPosition(ctx, node, pos); err != nil {
		return nil, err
	}
	after, _, err := h.resolveNode(ctx, s, target)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "moved %s to %s and could not read it back: %w", target, text, err)
	}
	msg := fmt.Sprintf("moved %s to position %s — it reads %q", target, formatPosition(pos), after.Value)
	if was != "" {
		msg += fmt.Sprintf(", was %q", was)
	}
	return Result(msg, ActionView{Action: "fill", Target: target, Value: after.Value}), nil
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
