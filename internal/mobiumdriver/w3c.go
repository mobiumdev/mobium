package mobiumdriver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// w3cClient speaks the subset of W3C WebDriver that both device-side servers
// implement.
//
// UiAutomator2 and WebDriverAgent expose the same endpoints — /status,
// /session, /source, /screenshot, /actions, /element and /element/{id}/value —
// so the transport, the session handling and the error unwrapping are written
// once here and the backends differ only in what they do with the results.
type w3cClient struct {
	mu        sync.Mutex
	base      string
	sessionID string
	client    *http.Client

	// reopen re-establishes a session that the server no longer knows about.
	// Both servers hold exactly one session per device, so anything else that
	// attaches — a second mobium process, or Appium — silently invalidates
	// ours. Recovering beats failing every later command.
	reopen func(context.Context) error
}

func newW3CClient(timeout time.Duration) *w3cClient {
	return &w3cClient{client: &http.Client{Timeout: timeout}}
}

func (c *w3cClient) setBase(base string) {
	c.mu.Lock()
	c.base = base
	c.mu.Unlock()
}

func (c *w3cClient) endpoint() (base, session string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.base, c.sessionID
}

// sessionPath builds a path under the current session.
func (c *w3cClient) sessionPath(suffix string) string {
	_, sid := c.endpoint()
	return "/session/" + sid + suffix
}

// ready reports whether the server is answering /status.
func (c *w3cClient) ready(ctx context.Context) bool {
	var status struct {
		Value struct {
			Ready bool `json:"ready"`
		} `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, "/status", nil, &status); err != nil {
		return false
	}
	return status.Value.Ready
}

// openSession creates a session with the given capabilities.
func (c *w3cClient) openSession(ctx context.Context, caps map[string]interface{}) error {
	if caps == nil {
		caps = map[string]interface{}{}
	}
	body := map[string]interface{}{
		"capabilities": map[string]interface{}{
			"firstMatch":  []interface{}{map[string]interface{}{}},
			"alwaysMatch": caps,
		},
	}
	var resp struct {
		Value struct {
			SessionID string `json:"sessionId"`
		} `json:"value"`
		// WebDriverAgent answers with sessionId at the top level as well.
		SessionID string `json:"sessionId"`
	}
	if err := c.do(ctx, http.MethodPost, "/session", body, &resp); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	id := resp.Value.SessionID
	if id == "" {
		id = resp.SessionID
	}
	if id == "" {
		return mobiumerr.New(mobiumerr.DeviceServer, "the server returned an empty session id")
	}
	c.mu.Lock()
	c.sessionID = id
	c.mu.Unlock()
	return nil
}

// closeSession deletes the session, ignoring a server that has already gone.
func (c *w3cClient) closeSession(ctx context.Context) {
	_, sid := c.endpoint()
	if sid == "" {
		return
	}
	c.do(ctx, http.MethodDelete, "/session/"+sid, nil, nil)
	c.mu.Lock()
	c.sessionID = ""
	c.mu.Unlock()
}

// source fetches the UI hierarchy as XML.
func (c *w3cClient) source(ctx context.Context) (string, error) {
	var resp struct {
		Value string `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, c.sessionPath("/source"), nil, &resp); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.Value) == "" {
		return "", mobiumerr.New(mobiumerr.DeviceServer, "the server returned an empty hierarchy")
	}
	return resp.Value, nil
}

// pointerSequence sends one W3C pointer action chain.
func (c *w3cClient) pointerSequence(ctx context.Context, actions []map[string]interface{}) error {
	body := map[string]interface{}{
		"actions": []map[string]interface{}{{
			"type":       "pointer",
			"id":         "finger1",
			"parameters": map[string]interface{}{"pointerType": "touch"},
			"actions":    actions,
		}},
	}
	return c.do(ctx, http.MethodPost, c.sessionPath("/actions"), body, nil)
}

// pointerSequences sends several pointer chains that run together.
//
// The W3C model is lockstep: the action at index i of every chain happens at
// the same moment, so two fingers moving apart is two chains of equal length
// rather than anything special. That is the whole of multi-touch here, and the
// reason a pinch needs no new protocol — only a second source.
func (c *w3cClient) pointerSequences(ctx context.Context, chains ...[]map[string]interface{}) error {
	var sources []map[string]interface{}
	for i, chain := range chains {
		sources = append(sources, map[string]interface{}{
			"type":       "pointer",
			"id":         fmt.Sprintf("finger%d", i+1),
			"parameters": map[string]interface{}{"pointerType": "touch"},
			"actions":    chain,
		})
	}
	return c.do(ctx, http.MethodPost, c.sessionPath("/actions"),
		map[string]interface{}{"actions": sources}, nil)
}

// pinchActions is the pair of chains for a two-finger pinch about a point.
//
// Both fingers travel the same distance in opposite directions along the
// horizontal, which keeps the midpoint fixed — a pinch that drifts is a drag
// with extra steps, and on a map or a photo it would scroll as well as scale.
//
// The pause before the move is not decoration. Both platforms decide what
// gesture is beginning from the first movement after touch-down, and starting
// to move in the same frame as the press is read as a fling often enough to
// matter.
func pinchActions(cx, cy, from, to int, d time.Duration) ([]map[string]interface{}, []map[string]interface{}) {
	ms := d.Milliseconds()
	left := []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": cx - from, "y": cy},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 80},
		{"type": "pointerMove", "duration": ms, "x": cx - to, "y": cy},
		{"type": "pointerUp", "button": 0},
	}
	right := []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": cx + from, "y": cy},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 80},
		{"type": "pointerMove", "duration": ms, "x": cx + to, "y": cy},
		{"type": "pointerUp", "button": 0},
	}
	return left, right
}

// rotateActions is the pair of chains for a two-finger rotation about a point.
//
// Stepped around the arc rather than moved straight to the end, and that is
// the whole difficulty. A W3C pointerMove interpolates in a straight line, so
// a single move from start angle to end angle would drag each finger along a
// **chord** — which passes closer to the center than the arc does, shortening
// the gap between the fingers and then lengthening it again. The platform
// reads that as a pinch in and back out, with some rotation incidentally
// attached. Stepping keeps both fingers at a constant radius, which is what
// makes it a rotation and not a pinch that happens to turn.
func rotateActions(cx, cy, radius int, degrees float64, d time.Duration) ([]map[string]interface{}, []map[string]interface{}) {
	// One step per ten degrees, and never fewer than four: too few and the
	// chords between them are long enough to bring the pinch problem back.
	steps := int(math.Abs(degrees) / 10)
	if steps < 4 {
		steps = 4
	}
	per := d.Milliseconds() / int64(steps)
	if per < 1 {
		per = 1
	}

	at := func(deg float64) (int, int) {
		r := deg * math.Pi / 180
		return cx + int(float64(radius)*math.Cos(r)), cy + int(float64(radius)*math.Sin(r))
	}

	// Fingers start opposite each other on the horizontal, which is how a
	// hand lands on a thing it means to turn.
	x1, y1 := at(0)
	x2, y2 := at(180)
	left := []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": x1, "y": y1},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 80},
	}
	right := []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": x2, "y": y2},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 80},
	}
	for i := 1; i <= steps; i++ {
		deg := degrees * float64(i) / float64(steps)
		ax, ay := at(deg)
		bx, by := at(deg + 180)
		left = append(left, map[string]interface{}{
			"type": "pointerMove", "duration": per, "x": ax, "y": ay})
		right = append(right, map[string]interface{}{
			"type": "pointerMove", "duration": per, "x": bx, "y": by})
	}
	left = append(left, map[string]interface{}{"type": "pointerUp", "button": 0})
	right = append(right, map[string]interface{}{"type": "pointerUp", "button": 0})
	return left, right
}

// tapActions is the pointer chain for a tap.
func tapActions(x, y int) []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": x, "y": y},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 60},
		{"type": "pointerUp", "button": 0},
	}
}

// pressActions is the pointer chain for a press-and-hold.
func pressActions(x, y int, d time.Duration) []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": x, "y": y},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": d.Milliseconds()},
		{"type": "pointerUp", "button": 0},
	}
}

// doubleTapGap is how long the finger is off the glass between the two taps.
//
// AOSP's ViewConfiguration bounds the window at both ends: DOUBLE_TAP_MIN_TIME
// is 40ms and DOUBLE_TAP_TIMEOUT is 300ms, so a second tap sooner than the
// first is discarded as a bounce and one later than the second is simply a
// second tap. 120ms sits with margin on both sides, which matters because the
// pause is honored by the device-side server rather than by us and neither
// end of that window is somewhere to be near.
//
// Read from the platform's own constants rather than measured here, which is
// a weaker kind of knowledge than the rest of this file. What a device run
// can confirm is the outcome — a page that counts `dblclick` separately from
// two `click`s — not the constant.
const doubleTapGap = 120 * time.Millisecond

// doubleTapActions is the pointer chain for two taps read as one gesture.
//
// One chain, not two calls. Two separate /actions requests would put a
// network round trip inside the 300ms window, which is the one place in this
// gesture where time is load-bearing.
func doubleTapActions(x, y int) []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": x, "y": y},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 60},
		{"type": "pointerUp", "button": 0},
		{"type": "pause", "duration": doubleTapGap.Milliseconds()},
		// No second pointerMove: the platform has a double-tap *slop* as well
		// as a timeout, so re-issuing the move would be a chance to arrive a
		// pixel off for no gain.
		//
		// This chain is Android's. **WebDriverAgent drops a pause that occurs
		// while the pointer is up** — measured on an iPhone 17 Pro simulator
		// by instrumenting the page: the two taps arrived 67ms long with
		// *zero* milliseconds between them, and WebKit rejected the second as
		// a bounce, so no dblclick was ever synthesized. A pause while the
		// pointer is down is honored only in part: a drag's closing hold,
		// after its moves, arrives in full, and its opening one, straight
		// after pointerDown, does not — see dragHoldChain.
		//
		// Spending the interval on a timed pointerMove instead reaches a
		// WebView as a second contact, two touchstarts before either
		// touchend — and reaches a native control as the two clean taps it
		// should be. So iOS chooses by target: see WDA.DoubleTap.
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 60},
		{"type": "pointerUp", "button": 0},
	}
}

// dragHoldActions is the pointer chain for a drag and drop.
//
// Three things separate this from dragActions below, and all three are the
// difference between moving an item and flinging the list it sits in.
//
// **It holds before moving.** Android's long-press timeout is 500ms by
// default, and a drag-to-reorder list arms on that long press. A chain that
// starts traveling before it elapses is read as a scroll, the item is never
// picked up, and the call reports success either way.
//
// **It steps.** A single pointerMove is interpolated by the device-side
// server, and how many intermediate events that produces is the server's
// business rather than ours. A drop target that highlights on hover, or a
// reorder that recomputes on each move, needs those events to exist — so they
// are sent explicitly. Same lesson as rotateActions, arrived at from the
// other direction: there the interpolation did the wrong thing, here it might
// do too little of the right one.
//
// **It holds before releasing.** A release in the same frame as the arrival
// is dropped before the target under the finger has seen it.
func dragHoldActions(x1, y1, x2, y2 int, hold, move time.Duration) []map[string]interface{} {
	return dragHoldChain(x1, y1, x2, y2, hold, move, false)
}

// dragHoldChain is dragHoldActions, with the opening hold sent either as a
// pause or, when holdByMoving, as a move to the point the finger is already on.
//
// WebDriverAgent needs the move. A pause straight after pointerDown arrived
// as 183ms whatever was asked — measured three times in three on an iPhone
// 17 Pro simulator, by MobiumApp's drop zone, with 1500ms asked — while the
// closing pause, after the moves, arrived in full. Sent as a move in place,
// the opening hold arrived as 1517ms, three times in three. So a long-press
// drag on iOS, which arms on that hold, had never really been one. It is
// CHALLENGES 84's lesson on the other platform: a finger that is down holds
// still by moving to where it already is. Android's pause arrives in full
// (1522ms, the same measurement) and keeps it.
func dragHoldChain(x1, y1, x2, y2 int, hold, move time.Duration, holdByMoving bool) []map[string]interface{} {
	// One step per 50 device pixels of travel, never fewer than 5 and never
	// more than 40: below the floor a short drag sends too few events to be
	// a hover, above the ceiling a long one only makes the payload bigger.
	dist := math.Hypot(float64(x2-x1), float64(y2-y1))
	steps := int(dist / 50)
	if steps < 5 {
		steps = 5
	}
	if steps > 40 {
		steps = 40
	}
	per := move.Milliseconds() / int64(steps)
	if per < 1 {
		per = 1
	}

	chain := []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": x1, "y": y1},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": hold.Milliseconds()},
	}
	if holdByMoving {
		chain[2] = map[string]interface{}{"type": "pointerMove", "duration": hold.Milliseconds(), "x": x1, "y": y1}
	}
	for i := 1; i <= steps; i++ {
		f := float64(i) / float64(steps)
		chain = append(chain, map[string]interface{}{
			"type": "pointerMove", "duration": per,
			"x": x1 + int(float64(x2-x1)*f),
			"y": y1 + int(float64(y2-y1)*f),
		})
	}
	chain = append(chain,
		map[string]interface{}{"type": "pause", "duration": hold.Milliseconds()},
		map[string]interface{}{"type": "pointerUp", "button": 0})
	return chain
}

// dragActions is the pointer chain for a swipe. The duration lives on the
// move, not a pause: that is what separates a fling from a slow drag.
func dragActions(x1, y1, x2, y2 int, d time.Duration) []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": x1, "y": y1},
		{"type": "pointerDown", "button": 0},
		{"type": "pointerMove", "duration": d.Milliseconds(), "x": x2, "y": y2},
		{"type": "pointerUp", "button": 0},
	}
}

// wdaDoubleTap asks WebDriverAgent for a double tap at a point, in points.
//
// WebDriverAgent-only: there is no such endpoint on UiAutomator2, which is
// why this is called from the WDA backend rather than offered to both.
func (c *w3cClient) wdaDoubleTap(ctx context.Context, x, y int) error {
	return c.do(ctx, http.MethodPost, c.sessionPath("/wda/doubleTap"),
		map[string]interface{}{"x": x, "y": y}, nil)
}

// findElement resolves a locator strategy to a server-side element handle.
// setClipboard writes the device clipboard through UiAutomator2's own
// endpoint, which takes base64 so the payload never passes through a shell.
//
// There is deliberately no counterpart. `get_clipboard` exists on the same
// server and returns an empty string on Android 10 and later whatever the
// clipboard holds: reading it requires the *requesting* app to have focus, and
// the UiAutomator2 server has no activity of its own. Appium works around that
// with a separate helper app; mobium installs no such thing, so the honest
// answer is to refuse rather than to report an empty clipboard that may be
// full. Measured on Android 15 — see CHALLENGES 60.
func (c *w3cClient) setClipboard(ctx context.Context, text string) error {
	body := map[string]interface{}{
		"content":     base64.StdEncoding.EncodeToString([]byte(text)),
		"contentType": "plaintext",
		"label":       "mobium",
	}
	return c.do(ctx, http.MethodPost, c.sessionPath("/appium/device/set_clipboard"), body, nil)
}

func (c *w3cClient) findElement(ctx context.Context, strategy, selector string) (string, error) {
	var resp struct {
		Value map[string]string `json:"value"`
	}
	body := map[string]interface{}{"using": strategy, "value": selector,
		// UiAutomator2 accepts the older spelling and ignores the newer one;
		// sending both keeps one client working against both servers.
		"strategy": strategy, "selector": selector}
	if err := c.do(ctx, http.MethodPost, c.sessionPath("/element"), body, &resp); err != nil {
		return "", fmt.Errorf("locate element (%s=%s): %w", strategy, selector, err)
	}
	for _, key := range []string{"ELEMENT", "element-6066-11e4-a52e-4f735466cecf"} {
		if id := resp.Value[key]; id != "" {
			return id, nil
		}
	}
	return "", mobiumerr.New(mobiumerr.DeviceServer, "the server did not return an element for %s=%s", strategy, selector)
}

// setElementValue types into an element.
func (c *w3cClient) setElementValue(ctx context.Context, elementID, text string) error {
	return c.setElementValueAt(ctx, elementID, text, 0)
}

// setElementValueAt is setElementValue at a given typing speed, in keys per
// second; zero leaves the server's own. WebDriverAgent reads `frequency` from
// the request and otherwise types at 60 — XCTest's iOSMaximumTypingFrequency
// — and UiAutomator2 sets a value whole and ignores it.
func (c *w3cClient) setElementValueAt(ctx context.Context, elementID, text string, frequency int) error {
	body := map[string]interface{}{"text": text, "value": strings.Split(text, "")}
	if frequency > 0 {
		body["frequency"] = frequency
	}
	return c.do(ctx, http.MethodPost, c.sessionPath("/element/"+elementID+"/value"), body, nil)
}

// errNoAlert marks "there is no alert on screen", which is an answer rather
// than a failure. Both servers report it as the W3C `no such alert` error, so
// it arrives here looking exactly like a fault, and a tool that passed it
// through would make "nothing is asking you anything" indistinguishable from
// "the device is broken".
var errNoAlert = mobiumerr.New(mobiumerr.NoSuchAlert, "no alert is on screen")

// noAlert reports whether an error is that one.
func noAlert(err error) bool {
	return mobiumerr.CodeOf(err) == mobiumerr.NoSuchAlert
}

// alertText reads what a system dialog says.
//
// The W3C alert endpoints, which **both** device-side servers implement —
// measured, not assumed: UiAutomator2 answered for an Android permission
// prompt and WebDriverAgent for an iOS one, with the same shape. That is why
// this sits in the shared client rather than in either backend.
func (c *w3cClient) alertText(ctx context.Context) (string, error) {
	var resp struct {
		Value string `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, c.sessionPath("/alert/text"), nil, &resp); err != nil {
		if noAlert(err) {
			return "", errNoAlert
		}
		return "", err
	}
	return resp.Value, nil
}

// sendAlertText types into a prompt's text field.
//
// The W3C endpoint is the same path as the read, POSTed. Whether anything is
// listening is the platform's business: an alert with no text field has
// nothing to type into and says so.
func (c *w3cClient) sendAlertText(ctx context.Context, text string) error {
	// Both spellings, for the same reason setElementValue sends both: the W3C
	// name is `text` and WebDriverAgent wants the older JSONWP `value`,
	// answering "Missing 'value' parameter" to a spec-correct request.
	err := c.do(ctx, http.MethodPost, c.sessionPath("/alert/text"),
		map[string]interface{}{"text": text, "value": strings.Split(text, "")}, nil)
	if noAlert(err) {
		return errNoAlert
	}
	return err
}

// answerAlert accepts or dismisses whatever is on screen.
func (c *w3cClient) answerAlert(ctx context.Context, accept bool) error {
	path := "/alert/dismiss"
	if accept {
		path = "/alert/accept"
	}
	err := c.do(ctx, http.MethodPost, c.sessionPath(path), map[string]interface{}{}, nil)
	if noAlert(err) {
		return errNoAlert
	}
	return err
}

// elementValue reads back what a text field now holds.
//
// The `value` attribute rather than `/text`: for a text field the first is
// what was typed and the second is the placeholder when the field is empty,
// which would make an empty field look full.
func (c *w3cClient) elementValue(ctx context.Context, elementID string) (string, error) {
	var resp struct {
		Value *string `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet,
		c.sessionPath("/element/"+elementID+"/attribute/value"), nil, &resp); err != nil {
		return "", err
	}
	if resp.Value == nil {
		return "", nil
	}
	return *resp.Value, nil
}

// clearElement empties a text field.
func (c *w3cClient) clearElement(ctx context.Context, elementID string) error {
	return c.do(ctx, http.MethodPost, c.sessionPath("/element/"+elementID+"/clear"),
		map[string]interface{}{}, nil)
}

// staleSession reports whether an error means the server has forgotten our
// session, as opposed to the command itself failing.
func staleSession(err error) bool {
	if err == nil {
		return false
	}
	// The server's own code decides when it sent one. The text match stays
	// for the replies that arrive without one — an HTTP status line, or a
	// server that puts the reason only in the message.
	if e, ok := mobiumerr.As(err); ok && e.Details["w3c"] == "invalid session id" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid session id") ||
		strings.Contains(msg, "session is not known") ||
		strings.Contains(msg, "no such session")
}

// do performs one request, re-establishing the session once if the server has
// forgotten it, and retrying the request against the new one.
func (c *w3cClient) do(ctx context.Context, method, path string, body, out interface{}) error {
	err := c.doOnce(ctx, method, path, body, out)
	if !staleSession(err) || c.reopen == nil {
		return err
	}

	_, oldID := c.endpoint()
	if rerr := c.reopen(ctx); rerr != nil {
		return fmt.Errorf("%w (and re-establishing the session failed: %v)", err, rerr)
	}
	// The path embeds the old session id, so it has to be rewritten before
	// the retry can land anywhere useful.
	_, newID := c.endpoint()
	if oldID != "" && newID != "" {
		path = strings.Replace(path, oldID, newID, 1)
	}
	return c.doOnce(ctx, method, path, body, out)
}

func (c *w3cClient) doOnce(ctx context.Context, method, path string, body, out interface{}) error {
	base, _ := c.endpoint()
	if base == "" {
		return mobiumerr.New(mobiumerr.Internal, "the driver is not started")
	}

	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}

	// Both servers report failures inside a 200 as often as with a status
	// code, so the body decides, not resp.StatusCode.
	if e := errorFrom(raw); e != nil {
		return e
	}
	if resp.StatusCode >= 400 {
		return mobiumerr.New(mobiumerr.DeviceServer, "%s %s: %s", method, path, resp.Status)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// errorFrom extracts a W3C error payload, returning "" for a success.
// errorFrom reads the error a W3C server put in a response body, keeping the
// server's own code: WebDriverAgent and UiAutomator2 both answer in W3C
// WebDriver's vocabulary ("no such element", "invalid session id"), and until
// docs/decisions/0005 that code was flattened into the message and later
// recovered by string matching. It is now kept, in Details["w3c"], and mapped
// onto Mobium's codes where one names the same thing. nil means no error.
func errorFrom(raw json.RawMessage) *mobiumerr.Error {
	w3c, msg := w3cError(raw)
	if w3c == "" {
		return nil
	}
	code := mobiumerr.DeviceServer
	switch w3c {
	case "no such element":
		code = mobiumerr.NoSuchElement
	case "no such alert":
		code = mobiumerr.NoSuchAlert
	case "timeout", "script timeout":
		code = mobiumerr.Timeout
	case "invalid argument":
		code = mobiumerr.InvalidArgument
	case "unknown command", "unsupported operation", "unknown method":
		code = mobiumerr.Unsupported
	}
	return mobiumerr.New(code, "%s", msg).WithDetail("w3c", w3c)
}

// w3cError returns the server's error code and the message it is printed
// as, or two empty strings when the body carries no error.
func w3cError(raw json.RawMessage) (code, message string) {
	var envelope struct {
		Value json.RawMessage `json:"value"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Value) == 0 {
		return "", ""
	}
	var werr struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(envelope.Value, &werr) != nil || werr.Error == "" {
		return "", ""
	}
	if werr.Message != "" {
		return werr.Error, werr.Error + ": " + firstLine(werr.Message)
	}
	return werr.Error, werr.Error
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
