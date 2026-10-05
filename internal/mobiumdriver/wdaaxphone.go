package mobiumdriver

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A real iPhone's accessibility settings, through the Settings app.
//
// Nothing outside a phone changes them: WebDriverAgent's one such setting,
// reduceMotion, answers a write with success and changes nothing, measured
// twice. But Settings is an app like any other, and on the iPhone 15 Plus,
// iOS 26.6.2, switching each of these there reached MobiumApp's Accessibility
// Demo live, in both directions (2026-09-26). So a read or a change goes to
// Settings, finds the page and the switch by their identifiers — which do not
// change with the phone's language, where their labels do — reads or flips
// the switch and reads it back, and returns to the app that was in front.
//
// The identifiers were read off the phone: the Accessibility row is
// com.apple.settings.accessibility, its pages MOTION_TITLE and
// DISPLAY_AND_TEXT, and each switch's own name is its setting's.
type phoneAXRoute struct {
	pages  []string
	toggle string
}

var phoneAXRoutes = map[string]phoneAXRoute{
	device.AXReduceMotion:              {[]string{phoneAXRoot, "MOTION_TITLE"}, "REDUCE_MOTION"},
	device.AXBoldText:                  {[]string{phoneAXRoot, "DISPLAY_AND_TEXT"}, "ENHANCE_TEXT_LEGIBILITY"},
	device.AXIncreaseContrast:          {[]string{phoneAXRoot, "DISPLAY_AND_TEXT"}, "TEXT_COLORS_DARKEN"},
	device.AXReduceTransparency:        {[]string{phoneAXRoot, "DISPLAY_AND_TEXT"}, "REDUCE_TRANSPARENCY"},
	device.AXButtonShapes:              {[]string{phoneAXRoot, "DISPLAY_AND_TEXT"}, "BUTTON_SHAPES"},
	device.AXDifferentiateWithoutColor: {[]string{phoneAXRoot, "DISPLAY_AND_TEXT"}, "DIFFERENTIATE_WITHOUT_COLOR"},
}

// phoneAXLacks names what the phone route does not do, and why.
var phoneAXLacks = map[string]string{
	device.AXInvertColors: "Smart Invert is not set on a real iPhone: no screenshot shows a display filter, so " +
		"nothing here could confirm the screen inverted — switch it in Settings > Accessibility > Display & " +
		"Text Size",
	device.AXGrayscale: "grayscale is a Color Filters choice on a real iPhone, a page of its own, and not built " +
		"yet — choose it in Settings > Accessibility > Display & Text Size > Color Filters",
	device.AXTextSize: "text size is a slider on a real iPhone, and moving it through Settings is not built yet — " +
		"set it in Settings > Accessibility > Display & Text Size > Larger Text",
	device.AXTextScale: "iOS sizes text by a category, not a scale — and on a real iPhone not from here yet",
}

const (
	phoneAXRoot      = "com.apple.settings.accessibility"
	settingsBundleID = "com.apple.Preferences"
	// phoneAXStep bounds each step: a page to appear, a switch to answer.
	phoneAXStep = 5 * time.Second
)

// phoneAX reads a setting through Settings, and sets it first when want is
// "on" or "off". It answers the value the switch reads afterwards.
func (w *WDA) phoneAX(ctx context.Context, name, want string) (string, error) {
	if reason, ok := phoneAXLacks[name]; ok {
		return "", mobiumerr.New(mobiumerr.Unsupported, "%s", reason)
	}
	route, ok := phoneAXRoutes[name]
	if !ok {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "unknown accessibility setting %q", name)
	}
	if want != "" && want != device.AXOn && want != device.AXOff {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%s is a switch: on or off, not %q", name, want)
	}

	// A read of everything asks for each setting in turn, and five of the
	// six share a page: one visit reads every switch on it, and a read
	// soon after is answered from that visit rather than a trip of its own
	// — 45s to read all six on the iPhone 15 Plus, one trip each, before.
	// Any change forgets it.
	if want == "" {
		if v, ok := w.axSeen.get(name); ok {
			axLog(w.phone, "read %s %s (from the last visit to Settings)", name, v)
			return v, nil
		}
	} else {
		w.axSeen.forget()
	}

	back := w.foregroundBundle(ctx)
	defer w.returnFromSettings(ctx, back)
	if err := w.openSettingsPage(ctx, route); err != nil {
		return "", err
	}
	now, err := w.setSwitch(ctx, name, route.toggle, want)
	if err == nil && want == "" {
		w.axSeen.remember(name, now, w.readPage(ctx, route))
	}
	if want == "" {
		axLog(w.phone, "read %s %s (from Settings)%s", name, now, errNote(err))
	} else {
		axLog(w.phone, "set %s %s: reads %s%s", name, want, now, errNote(err))
	}
	return now, err
}

// openSettingsPage opens Settings at its root — it reopens on the page it
// was left on otherwise — and walks down to the route's page.
func (w *WDA) openSettingsPage(ctx context.Context, route phoneAXRoute) error {
	_ = w.phoneTerminate(ctx, settingsBundleID)
	if err := w.Launch(ctx, settingsBundleID); err != nil {
		return err
	}
	for _, page := range route.pages {
		if err := w.openSettingsRow(ctx, page); err != nil {
			return err
		}
	}
	return nil
}

// setSwitch reads a switch on the open page, and flips it first when want is
// "on" or "off" and it is not that already. It answers what it reads after.
func (w *WDA) setSwitch(ctx context.Context, name, toggle, want string) (string, error) {
	sw, err := w.waitFor(ctx, phoneSwitchQuery(toggle))
	if err != nil {
		return "", mobiumerr.New(mobiumerr.DeviceServer, "Settings did not show the %s switch: %w", toggle, err)
	}
	now, err := w.switchValue(ctx, sw)
	if err != nil || want == "" || now == want {
		return now, err
	}

	// The row's switch carries the name and the state, and its frame is the
	// whole row, whose center is the words; the toggle is a nameless switch
	// inside it, and only a tap there flips it (CHALLENGES 82).
	target := sw
	if inner, err := w.w3c.findElement(ctx, "class chain",
		fmt.Sprintf("**/XCUIElementTypeSwitch[`name == %q`]/**/XCUIElementTypeSwitch", toggle)); err == nil {
		target = inner
	}
	if err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/element/"+target+"/click"), map[string]interface{}{}, nil); err != nil {
		return now, err
	}
	// Looked up again on every read: a change such as Bold Text redraws the
	// page, and the switch found before it is gone — "stale element", on
	// the iPhone 15 Plus, after the change had taken.
	deadline := time.Now().Add(phoneAXStep)
	for {
		got, err := w.readSwitch(ctx, toggle)
		if err != nil && time.Now().Before(deadline) {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if err != nil {
			return now, err
		}
		if got == want {
			return got, nil
		}
		if time.Now().After(deadline) {
			return got, mobiumerr.New(mobiumerr.NotConfirmed, "switched %s in Settings to %s, and it reads %s", name, want, got)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Putting a phone back, all at once. Each setting's undo is a trip through
// Settings, 8 to 10 seconds, and undone one by one at the daemon's stop six
// of them ran out its time after two, leaving four of a person's settings on
// (CHALLENGES 160). So a change records what it found, and the first undo to
// run puts back everything recorded, one visit per page; the others find
// their setting already done.

// axPending is what each setting changed on a phone was before the change.
type axPending struct {
	mu  sync.Mutex
	was map[string]string
}

func (p *axPending) note(name, was string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.was == nil {
		p.was = map[string]string{}
	}
	if _, kept := p.was[name]; !kept {
		p.was[name] = was
	}
}

// restoreAX puts back every setting noted, one Settings visit per page.
func (w *WDA) restoreAX(ctx context.Context) error {
	w.axPend.mu.Lock()
	defer w.axPend.mu.Unlock()
	if len(w.axPend.was) == 0 {
		return nil
	}
	w.axSeen.forget()
	byPage := map[string][]string{}
	var pages []string
	for name := range w.axPend.was {
		r := phoneAXRoutes[name]
		last := r.pages[len(r.pages)-1]
		if _, seen := byPage[last]; !seen {
			pages = append(pages, last)
		}
		byPage[last] = append(byPage[last], name)
	}
	back := w.foregroundBundle(ctx)
	defer w.returnFromSettings(ctx, back)
	var first error
	for _, page := range pages {
		names := byPage[page]
		if err := w.openSettingsPage(ctx, phoneAXRoutes[names[0]]); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		for _, name := range names {
			got, err := w.setSwitch(ctx, name, phoneAXRoutes[name].toggle, w.axPend.was[name])
			axLog(w.phone, "restore %s to %s: reads %s%s", name, w.axPend.was[name], got, errNote(err))
			if err != nil {
				if first == nil {
					first = err
				}
				continue
			}
			delete(w.axPend.was, name)
		}
	}
	return first
}

func phoneSwitchQuery(name string) string {
	return fmt.Sprintf("type == 'XCUIElementTypeSwitch' AND name == %q", name)
}

// openSettingsRow scrolls a row into view by its identifier and opens it,
// then waits for the page to change. Rows are named twice, as a cell and as
// the button inside it; either opens the page.
func (w *WDA) openSettingsRow(ctx context.Context, id string) error {
	query := fmt.Sprintf("name == %q AND type IN {'XCUIElementTypeCell', 'XCUIElementTypeButton'}", id)
	row, err := w.waitFor(ctx, query)
	if err != nil {
		return mobiumerr.New(mobiumerr.DeviceServer, "Settings did not show the %s row: %w", id, err)
	}
	_ = w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/element/"+row+"/scroll"),
		map[string]interface{}{"toVisible": true}, nil)
	if err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/element/"+row+"/click"), map[string]interface{}{}, nil); err != nil {
		return err
	}
	// Opened once the row is gone: the next page does not have it.
	deadline := time.Now().Add(phoneAXStep)
	for time.Now().Before(deadline) {
		if _, err := w.w3c.findElement(ctx, "predicate string", query); err != nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return mobiumerr.New(mobiumerr.NotConfirmed, "tapped the %s row in Settings and its page did not open", id)
}

// waitFor finds an element by predicate, waiting a step for it to appear.
func (w *WDA) waitFor(ctx context.Context, predicate string) (string, error) {
	deadline := time.Now().Add(phoneAXStep)
	for {
		id, err := w.w3c.findElement(ctx, "predicate string", predicate)
		if err == nil || time.Now().After(deadline) || ctx.Err() != nil {
			return id, err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// readSwitch finds a Settings switch by name and reads it.
func (w *WDA) readSwitch(ctx context.Context, name string) (string, error) {
	id, err := w.w3c.findElement(ctx, "predicate string", phoneSwitchQuery(name))
	if err != nil {
		return "", err
	}
	return w.switchValue(ctx, id)
}

func (w *WDA) switchValue(ctx context.Context, id string) (string, error) {
	v, err := w.w3c.elementValue(ctx, id)
	if err != nil {
		return "", err
	}
	switch v {
	case "1":
		return device.AXOn, nil
	case "0":
		return device.AXOff, nil
	}
	return "", mobiumerr.New(mobiumerr.DeviceServer, "a Settings switch read %q, not 0 or 1", v)
}

// foregroundBundle names the app in front, to come back to.
func (w *WDA) foregroundBundle(ctx context.Context) string {
	var info struct {
		Value struct {
			BundleID string `json:"bundleId"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, "/wda/activeAppInfo", nil, &info); err != nil {
		return ""
	}
	return info.Value.BundleID
}

// returnFromSettings closes Settings and brings back the app that was in
// front, activated rather than launched, so it resumes where it was — and,
// as a person's return does, it hears the setting on the way back.
func (w *WDA) returnFromSettings(ctx context.Context, back string) {
	_ = w.phoneTerminate(ctx, settingsBundleID)
	if back == "" || back == settingsBundleID || back == springboardBundleID {
		w.clearExpected(ctx)
		return
	}
	w.expectApp(ctx, back)
	_ = w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/apps/activate"),
		map[string]interface{}{"bundleId": back}, nil)
}

// readPage reads every other switch on the page a route ends on.
func (w *WDA) readPage(ctx context.Context, route phoneAXRoute) map[string]string {
	seen := map[string]string{}
	for name, r := range phoneAXRoutes {
		if r.toggle == route.toggle || r.pages[len(r.pages)-1] != route.pages[len(route.pages)-1] {
			continue
		}
		if v, err := w.readSwitch(ctx, r.toggle); err == nil {
			seen[name] = v
		}
	}
	return seen
}

// axSeenFor is how long a visit to Settings answers reads without another:
// long enough for one read of everything, short enough that a switch a
// person flips by hand is not reported stale for long.
const axSeenFor = 10 * time.Second

// axSeen is what the last visit to Settings read.
type axSeen struct {
	mu     sync.Mutex
	at     time.Time
	values map[string]string
}

func (a *axSeen) get(name string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.values[name]
	return v, ok && time.Since(a.at) < axSeenFor
}

func (a *axSeen) remember(name, value string, others map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.at, a.values = time.Now(), others
	a.values[name] = value
}

func (a *axSeen) forget() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.values = nil
}
