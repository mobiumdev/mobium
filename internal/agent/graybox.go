package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

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
	regained := false
	for _, w := range h.idled {
		regained = regained || w.Regained
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
	if regained && unwaited == "" {
		line += ", after its log stream dropped and was reconnected"
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

// DropLogStreams drops every phone session's log stream, the way a dropped
// connection would, and says how many there were. The daemon calls it on
// SIGUSR1, so a check can drive the path a real drop takes; the sessions
// stay as they were, and each reconnects at its next action. It waits for
// the call in progress, as every call does.
func (h *Handlers) DropLogStreams() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, s := range h.sessions {
		if d, ok := s.driver.(interface{ DropLogStream() bool }); ok && d.DropLogStream() {
			n++
		}
	}
	return n
}

// grayBoxHookLimit bounds a wait for a hook's answer by default: on an
// iPhone the call is typed a key at a time, about 16 ms a character.
const grayBoxHookLimit = 15 * time.Second

// HookView is what a hook answered.
type HookView struct {
	Hook   string      `json:"hook"`
	Result interface{} `json:"result"`
}

// hook calls a hook the app registered with its gray-box library, by name,
// and returns what it answered. The call is written into the app's mailbox
// — a field that exists only in a gray-box launch — and the answer read
// from the device log, where the gray box already listens.
func (h *Handlers) hook(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(stringArg(args, "hook"))
	if name == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_hook needs the name of a hook the app registered")
	}
	var hookArgs []string
	if raw, ok := args["args"].([]interface{}); ok {
		for _, a := range raw {
			str, ok := a.(string)
			if !ok {
				return nil, mobiumerr.New(mobiumerr.InvalidArgument, "a hook's args are strings; got %v", a)
			}
			hookArgs = append(hookArgs, str)
		}
	}
	limit := grayBoxHookLimit
	if ms, ok := intArg(args, "timeout_ms"); ok && ms > 0 {
		limit = time.Duration(ms) * time.Millisecond
	}

	gb, ok := mobiumdriver.AsGrayBoxer(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapGrayBox, "call an app's hooks")
	}
	g := gb.GrayBox()
	if g == nil || !g.On() {
		return nil, mobiumerr.New(mobiumerr.DeviceNotReady, "the app is not listening for hooks: it was not launched "+
			"with the gray box on, or has not answered it — launch it with gray_box first").
			WithRemedy("app_launch with gray_box")
	}
	mb, ok := mobiumdriver.AsMailboxer(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapGrayBox, "call an app's hooks")
	}
	// A hook goes in when the app is idle, as an action does: what it sets
	// up should not race what the app is still doing.
	if err := h.awaitAppIdle(ctx, s, "hook "+name); err != nil {
		return nil, err
	}

	id := strconv.FormatInt(time.Now().UnixNano(), 36)
	call, err := asciiJSON(map[string]interface{}{"i": id, "h": name, "a": nonNilStrings(hookArgs)})
	if err != nil {
		return nil, err
	}
	if err := mb.WriteMailbox(ctx, call); err != nil {
		return nil, err
	}
	answer, err := g.AwaitAnswer(ctx, id, limit)
	if err != nil {
		if mobiumerr.CodeOf(err) == mobiumerr.Timeout {
			return nil, mobiumerr.New(mobiumerr.Timeout, "the app did not answer hook %s within %s — the call was "+
				"written; the app's handler may still be running, or it never reached the app", name, limit).
				WithDetail("hook", name)
		}
		return nil, err
	}
	if !answer.OK {
		if strings.HasPrefix(answer.Payload, "no hook named ") {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the app has %s", answer.Payload).
				WithDetail("hook", name)
		}
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "hook %s failed in the app: %s", name, answer.Payload).
			WithDetail("hook", name).WithDetail("hook_error", answer.Payload)
	}
	var result interface{}
	if err := json.Unmarshal([]byte(answer.Payload), &result); err != nil {
		result = answer.Payload
	}
	shown, _ := json.Marshal(result)
	return Result(fmt.Sprintf("hook %s answered: %s", name, shown), HookView{Hook: name, Result: result}), nil
}

func nonNilStrings(a []string) []string {
	if a == nil {
		return []string{}
	}
	return a
}

// asciiJSON is JSON with every character outside ASCII escaped. On an
// iPhone the call is typed on the keyboard, which has dropped letters
// outside its own layout (CHALLENGES 159); escapes are plain ASCII, and the
// app's JSON parser turns them back.
func asciiJSON(v interface{}) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "the hook call cannot be encoded: %v", err)
	}
	var b strings.Builder
	for _, r := range string(raw) {
		if r < 0x80 {
			b.WriteRune(r)
			continue
		}
		for _, u := range utf16.Encode([]rune{r}) {
			fmt.Fprintf(&b, "\\u%04x", u)
		}
	}
	return b.String(), nil
}
