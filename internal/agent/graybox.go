package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Gray box: an app launched with gray_box, and built with Mobium's gray-box
// library, says in the device log when it is busy, and every action waits
// for it to be idle before it resolves its target — so a tap after work the
// screen does not show lands on what the work produced. The app says when;
// nothing here guesses. An app launched the ordinary way is driven as before.

// checkIdle is the gray box's check, after the five every action makes.
const checkIdle = "idle"

// grayBoxIdleLimit bounds a wait for the app to go idle. An app polling in
// the background is never idle, so the wait ends, naming what kept it busy.
const grayBoxIdleLimit = 10 * time.Second

// grayBoxAnswer is how long a launch waits for the library's first line.
const grayBoxAnswer = 5 * time.Second

// awaitAppIdle waits for the gray-box app to be idle, when there is one and
// it has been heard. Called before a target is resolved, so the target is
// found on the screen the work produced.
func (h *Handlers) awaitAppIdle(ctx context.Context, s *session, target string) error {
	gb, ok := mobiumdriver.AsGrayBoxer(s.driver)
	if !ok {
		return nil
	}
	g := gb.GrayBox()
	if g == nil || !g.On() {
		return nil
	}
	w, err := g.AwaitIdle(ctx, h.prevCall, grayBoxIdleLimit)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return failedCheck(mobiumerr.Timeout, target, checkIdle,
			fmt.Sprintf("the app says it is still busy after %s, with %s", grayBoxIdleLimit, strings.Join(w.Busy, ", ")),
			"something in the app never finishes; to act on the screen as it is, launch the app again without gray_box").
			WithRemedy("app_launch without gray_box").
			WithDetail("busy", w.Busy)
	}
	h.idled = append(h.idled, w)
	return nil
}

// reportIdle adds what the gray box waited for to a result, because a check
// a result does not mention reads as one that did not run.
func (h *Handlers) reportIdle(res *ToolsCallResult, err error) (*ToolsCallResult, error) {
	if len(h.idled) == 0 || err != nil || res == nil {
		return res, err
	}
	var total time.Duration
	busy := map[string]bool{}
	unwaited := ""
	for _, w := range h.idled {
		total += w.Waited
		for _, b := range w.Busy {
			busy[b] = true
		}
		if w.Unwaited != "" {
			unwaited = w.Unwaited
		}
	}
	line := "gray box: the app was idle"
	if unwaited != "" {
		line = "gray box: not waited — " + unwaited
	} else if total >= time.Millisecond {
		line = fmt.Sprintf("gray box: waited %d ms for the app to go idle", total.Milliseconds())
		if len(busy) > 0 {
			var names []string
			for b := range busy {
				names = append(names, b)
			}
			sort.Strings(names)
			line += " (busy: " + strings.Join(names, ", ") + ")"
		}
	}
	if len(res.Content) > 0 && res.Content[0].Type == "text" {
		res.Content[0].Text += "\n" + line
	}
	if res.StructuredContent != nil {
		if raw, merr := json.Marshal(res.StructuredContent); merr == nil {
			var m map[string]interface{}
			if json.Unmarshal(raw, &m) == nil {
				m["app_idle"] = map[string]interface{}{"waited_ms": total.Milliseconds(), "waits": h.idled}
				res.StructuredContent = m
			}
		}
	}
	return res, nil
}

// grayBoxHeard waits for the app just launched with the gray-box library on
// to answer: one that does not link the library launches normally and says
// nothing, and its actions are not waited for.
func grayBoxHeard(ctx context.Context, s *session) (bool, error) {
	gb, ok := mobiumdriver.AsGrayBoxer(s.driver)
	if !ok {
		return false, nil
	}
	g := gb.GrayBox()
	if g == nil {
		return false, nil
	}
	return g.AwaitOn(ctx, grayBoxAnswer), nil
}

// grayBoxUnheard is the launch's note for an app that did not answer.
const grayBoxUnheard = "the app has not answered the gray box in %s, so actions are not waited for: it needs " +
	"Mobium's gray-box library, in a build that reads the MobiumGrayBox launch argument or intent extra"
