package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// listAt renders a scrollable list of rows 200px tall, showing the window
// starting at `first`. The container is 1000px high, so five rows fit and
// anything else is outside it — which is what makes containment, rather than
// mere presence in the hierarchy, the thing worth testing.
func listAt(t *testing.T, first int, rows []string) *uitree.Tree {
	t.Helper()
	return listAtIn(t, first, rows, "com.example.list")
}

func listAtIn(t *testing.T, first int, rows []string, pkg string) *uitree.Tree {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="` + pkg + `" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" package="` + pkg + `" class="androidx.recyclerview.widget.RecyclerView" ` +
		`scrollable="true" bounds="[0,200][1080,1200]">`)
	for i, label := range rows {
		y := 200 + (i-first)*200
		fmt.Fprintf(&b, `<node index="%d" text="%s" resource-id="app:id/row" package="%s" `+
			`class="android.widget.TextView" clickable="true" enabled="true" `+
			`bounds="[0,%d][1080,%d]" />`, i, label, pkg, y, y+200)
	}
	b.WriteString(`</node></node></hierarchy>`)
	tree, err := uitree.ParseAndroid([]byte(b.String()))
	if err != nil {
		t.Fatalf("parse list fixture: %v", err)
	}
	return tree
}

// scrollingDriver is a fake list that actually scrolls: a swipe moves the
// window, so the loop under test is driven by its own gestures rather than by
// a script that would pass no matter what it did.
type scrollingDriver struct {
	rows    []string
	first   int
	visible int
	swipes  int
	// stuck simulates a list that will not move — the end of the content.
	stuck  bool
	tapped []string
	t      *testing.T
}

func (d *scrollingDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	return listAt(d.t, d.first, d.rows), nil
}
func (d *scrollingDriver) Screenshot(ctx context.Context) ([]byte, error) { return nil, nil }
func (d *scrollingDriver) Tap(ctx context.Context, x, y int) error {
	d.tapped = append(d.tapped, fmt.Sprintf("%d,%d", x, y))
	return nil
}
func (d *scrollingDriver) Name() string { return "scrolling-fake" }

func (d *scrollingDriver) Swipe(ctx context.Context, x1, y1, x2, y2 int, _ time.Duration) error {
	d.swipes++
	if d.stuck {
		return nil
	}
	if y2 < y1 { // finger traveled up: look further down the list
		d.first += d.visible
	} else {
		d.first -= d.visible
	}
	if d.first > len(d.rows)-d.visible {
		d.first = len(d.rows) - d.visible
	}
	if d.first < 0 {
		d.first = 0
	}
	return nil
}
func (d *scrollingDriver) LongPress(ctx context.Context, x, y int, _ time.Duration) error {
	return nil
}

var _ mobiumdriver.Gesturer = (*scrollingDriver)(nil)

func withList(t *testing.T, rows []string) (*Handlers, *session, *scrollingDriver) {
	t.Helper()
	d := &scrollingDriver{rows: rows, visible: 5, t: t}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}
	h.sessions["fake"] = s
	return h, s, d
}

func rows(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("Row %02d", i)
	}
	return out
}

func scrollTo(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.scrollToOn(ctx, s, args)
}

func TestScrollToFindsSomethingBelowTheFold(t *testing.T) {
	h, sess, d := withList(t, rows(30))
	res, err := scrollTo(h, sess, map[string]interface{}{"target": "text=Row 22"})
	if err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if d.swipes == 0 {
		t.Error("claimed to find a row 22 places down without swiping")
	}
	view := res.StructuredContent.(ScrollView)
	if view.Scrolls == 0 {
		t.Error("reported zero scrolls after scrolling")
	}
	if view.Element == nil || view.Element.Ref == "" {
		t.Fatalf("no usable ref returned: %+v", view.Element)
	}
	// The whole point is that the caller can act on it now.
	if _, err := h.locatorFor("fake", view.Element.Ref); err != nil {
		t.Errorf("the ref handed back does not resolve: %v", err)
	}
}

func TestScrollToDoesNotScrollWhenAlreadyVisible(t *testing.T) {
	h, sess, d := withList(t, rows(30))
	res, err := scrollTo(h, sess, map[string]interface{}{"target": "text=Row 01"})
	if err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if d.swipes != 0 {
		t.Errorf("swiped %d times for a row already on screen", d.swipes)
	}
	if got := res.StructuredContent.(ScrollView).Scrolls; got != 0 {
		t.Errorf("scrolls = %d, want 0", got)
	}
}

func TestScrollToRequiresTheElementToBeWhollyInside(t *testing.T) {
	// Row 5 begins exactly at the container's bottom edge, so it is in the
	// hierarchy with real bounds while being entirely out of view. Treating
	// presence as visibility would tap a point off screen.
	h, sess, d := withList(t, rows(30))
	if _, err := scrollTo(h, sess, map[string]interface{}{"target": "text=Row 05"}); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if d.swipes == 0 {
		t.Error("a row flush against the container edge was treated as visible")
	}
}

func TestScrollToStopsAtTheEndOfTheList(t *testing.T) {
	// A list that will not move must be noticed, not swiped at fifteen times
	// before blaming the locator.
	h, sess, d := withList(t, rows(30))
	d.stuck = true
	_, err := scrollTo(h, sess, map[string]interface{}{"target": "text=Nowhere"})
	if err == nil {
		t.Fatal("found something in a list that never moved")
	}
	if !strings.Contains(err.Error(), "end of the list") {
		t.Errorf("error %q does not say the list stopped moving", err)
	}
	if d.swipes > 2 {
		t.Errorf("swiped %d times at an unmoving list", d.swipes)
	}
}

func TestScrollToSaysWhenNothingScrolls(t *testing.T) {
	// A screen with no scrollable area cannot be searched by scrolling, and
	// saying so beats reporting a failed search.
	h, sess, _ := withFake(t, screen(t, "Only this"))
	_, err := scrollTo(h, sess, map[string]interface{}{"target": "text=Elsewhere"})
	if err == nil {
		t.Fatal("scrolled a screen with nothing scrollable")
	}
	if !strings.Contains(err.Error(), "nothing on this screen scrolls") {
		t.Errorf("error %q does not name the real problem", err)
	}
}

func TestScrollToUp(t *testing.T) {
	h, sess, d := withList(t, rows(30))
	d.first = 20
	if _, err := scrollTo(h, sess, map[string]interface{}{
		"target": "text=Row 01", "direction": "up",
	}); err != nil {
		t.Fatalf("scroll up: %v", err)
	}
	if d.first > 1 {
		t.Errorf("window is at %d; it did not scroll back to the top", d.first)
	}
}

func TestScrollToRejectsABadDirection(t *testing.T) {
	h, sess, _ := withList(t, rows(10))
	_, err := scrollTo(h, sess, map[string]interface{}{"target": "text=Row 01", "direction": "sideways"})
	if err == nil || !strings.Contains(err.Error(), "down") {
		t.Errorf("err = %v", err)
	}
}

func TestTapScrollsToATargetBelowTheFold(t *testing.T) {
	// The half that removes work from callers: no explicit scroll, no map.
	h, sess, d := withList(t, rows(30))
	if _, err := tap(h, sess, map[string]interface{}{"target": "text=Row 18"}); err != nil {
		t.Fatalf("tap: %v", err)
	}
	if d.swipes == 0 {
		t.Error("tapped without scrolling to a row 18 places down")
	}
	if len(d.tapped) != 1 {
		t.Fatalf("tapped %v", d.tapped)
	}
	// It must have tapped inside the visible container, not at a computed
	// point somewhere off screen.
	var x, y int
	if _, err := fmt.Sscanf(d.tapped[0], "%d,%d", &x, &y); err != nil {
		t.Fatalf("unreadable tap point %q: %v", d.tapped[0], err)
	}
	if y < 200 || y > 1200 {
		t.Errorf("tapped at y=%d, outside the container's [200,1200]", y)
	}
}

func TestAmbiguousTargetsAreNotScrolledFor(t *testing.T) {
	// Scrolling cannot fix a locator that already matches twice, and trying
	// would both waste the swipes and leave the screen somewhere else.
	h, sess, d := withList(t, rows(30))
	_, err := tap(h, sess, map[string]interface{}{"target": "testid=row"})
	if err == nil {
		t.Fatal("tapped an ambiguous locator")
	}
	if !strings.Contains(err.Error(), "matches") {
		t.Errorf("error %q is not the ambiguity error", err)
	}
	if d.swipes != 0 {
		t.Errorf("swiped %d times for an ambiguous locator", d.swipes)
	}
}

// leavingDriver is a list whose first swipe navigates somewhere else, which
// is what a vertical swipe does to a horizontal pager.
type leavingDriver struct {
	scrollingDriver
	left bool
}

func (d *leavingDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	if d.left {
		xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
			`<node index="0" package="com.other.app" class="android.widget.ScrollView" ` +
			`scrollable="true" bounds="[0,0][1080,2400]">` +
			`<node index="0" text="Somewhere else" class="android.widget.TextView" bounds="[0,0][500,100]" />` +
			`</node></hierarchy>`
		return uitree.ParseAndroid([]byte(xml))
	}
	return listAtIn(d.t, d.first, d.rows, "com.example.list"), nil
}

func (d *leavingDriver) Swipe(ctx context.Context, x1, y1, x2, y2 int, t time.Duration) error {
	d.left = true
	return nil
}

func TestScrollStopsWhenTheSwipeNavigatesAway(t *testing.T) {
	// Found on the launcher: nothing in the hierarchy says which way a
	// container scrolls, so a vertical swipe can land on a horizontal pager
	// and open something instead. Noticing after one swipe beats swiping
	// fifteen times through an app the caller never asked for.
	d := &leavingDriver{scrollingDriver: scrollingDriver{rows: rows(30), visible: 5, t: t}}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}
	h.sessions["fake"] = s

	_, err := scrollTo(h, s, map[string]interface{}{"target": "text=Row 22"})
	if err == nil {
		t.Fatal("kept scrolling after the swipe left the app")
	}
	if !strings.Contains(err.Error(), "instead of scrolling") {
		t.Errorf("error %q does not explain what happened", err)
	}
	if !strings.Contains(err.Error(), "com.other.app") {
		t.Errorf("error %q does not say where it ended up", err)
	}
}

// tickingDriver is a list pinned to the bottom with a clock on it: nothing
// scrolls, but one string changes every time it is read.
type tickingDriver struct {
	scrollingDriver
	tick int
}

func (d *tickingDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	d.tick++
	xml := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">`+
		`<node index="0" package="com.example.list" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">`+
		`<node index="0" package="com.example.list" class="androidx.recyclerview.widget.RecyclerView" `+
		`scrollable="true" bounds="[0,200][1080,1200]">`+
		`<node index="0" text="Android version" class="android.widget.TextView" bounds="[0,200][1080,400]" />`+
		`<node index="1" text="Up time 40:%02d" class="android.widget.TextView" bounds="[0,400][1080,600]" />`+
		`</node></node></hierarchy>`, d.tick%60)
	return uitree.ParseAndroid([]byte(xml))
}

func (d *tickingDriver) Swipe(ctx context.Context, x1, y1, x2, y2 int, _ time.Duration) error {
	d.swipes++
	return nil // pinned to the bottom
}

func TestATickingClockIsNotProgress(t *testing.T) {
	// Found on the emulator's About screen, which shows an uptime counter.
	// Comparing everything the container said found a difference every pass,
	// so the loop swiped its full fifteen times at a list that had been at
	// the bottom from the start.
	d := &tickingDriver{scrollingDriver: scrollingDriver{t: t}}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}

	_, err := scrollTo(h, s, map[string]interface{}{"target": "text=Not here"})
	if err == nil {
		t.Fatal("found something on a list that never moved")
	}
	if !strings.Contains(err.Error(), "end of the list") {
		t.Errorf("error %q does not say the list stopped moving", err)
	}
	if d.swipes > 2 {
		t.Errorf("swiped %d times at a pinned list with a clock on it", d.swipes)
	}
}

func TestMovedNeedsMoreThanOneChangedString(t *testing.T) {
	// The other half of the rule: a recycled list can hand back its rows at
	// identical coordinates and only the text will say it moved.
	same := reading{geometry: "g", texts: []string{"a", "b", "c", "d"}}
	clock := reading{geometry: "g", texts: []string{"a", "b", "c", "d'"}}
	scrolled := reading{geometry: "g", texts: []string{"e", "f", "g", "h"}}

	if moved(same, clock) {
		t.Error("one changed string counted as scrolling")
	}
	if !moved(same, scrolled) {
		t.Error("a screenful of new rows did not count as scrolling")
	}
	if !moved(same, reading{geometry: "other", texts: same.texts}) {
		t.Error("a change of position did not count as scrolling")
	}
}

func TestAmbiguousIsNotReportedAsMissing(t *testing.T) {
	// Found on an iPhone 17 Pro: a Settings cell and the static text inside it
	// carry the same label, so `label=Privacy & Security` matches twice. The
	// scroll loop treated any resolution failure as "not here yet", swiped to
	// the end of the list, and reported "no element matches" about something
	// sitting on the screen in front of you. It sent me looking in the wrong
	// place twice before I read the hierarchy.
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="com.example.list" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" package="com.example.list" class="androidx.recyclerview.widget.RecyclerView" ` +
		`scrollable="true" bounds="[0,200][1080,1200]">` +
		`<node index="0" text="Privacy" class="android.widget.LinearLayout" clickable="true" ` +
		`enabled="true" bounds="[0,300][1080,500]">` +
		`<node index="0" text="Privacy" class="android.widget.TextView" bounds="[40,340][600,460]" />` +
		`</node></node></node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d := &scrollingDriver{rows: rows(5), visible: 5, t: t}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: &fixedTree{tree: tree, gest: d}, backend: BackendDump}

	_, err = scrollTo(h, s, map[string]interface{}{"target": "text=Privacy"})
	if err == nil {
		t.Fatal("an ambiguous locator was resolved")
	}
	if !strings.Contains(err.Error(), "matches 2 elements") {
		t.Errorf("error %q does not say the locator is ambiguous", err)
	}
	if strings.Contains(err.Error(), "no element matches") {
		t.Errorf("error %q claims nothing matched", err)
	}
	if d.swipes != 0 {
		t.Errorf("swiped %d times for a locator that was never going to resolve", d.swipes)
	}
}

// fixedTree always returns the same screen, and delegates gestures so a test
// can see whether it bothered swiping.
type fixedTree struct {
	tree *uitree.Tree
	gest *scrollingDriver
}

func (f *fixedTree) Snapshot(ctx context.Context) (*uitree.Tree, error) { return f.tree, nil }
func (f *fixedTree) Screenshot(ctx context.Context) ([]byte, error)     { return nil, nil }
func (f *fixedTree) Tap(ctx context.Context, x, y int) error            { return nil }
func (f *fixedTree) Name() string                                       { return "fixed" }
func (f *fixedTree) LongPress(ctx context.Context, x, y int, d time.Duration) error {
	return nil
}
func (f *fixedTree) Swipe(ctx context.Context, x1, y1, x2, y2 int, d time.Duration) error {
	return f.gest.Swipe(ctx, x1, y1, x2, y2, d)
}

func TestAnElementOutsideEveryScrollableIsNotScrolledFor(t *testing.T) {
	// Found on a Pixel 8 Pro. Calculator's only scrollable is the history
	// strip across the top, [0,0][1008,285]; the "7" button sits at
	// [9,1218][249,1452], outside it and not under it. Judging the button
	// against "the biggest scrollable on screen" concluded it was out of
	// view, and tapping it tried to scroll a list it has nothing to do with
	// and then refused. The same shape is any screen with a fixed button bar
	// below a scrolling list, which is most of them.
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="com.example" class="android.widget.FrameLayout" bounds="[0,0][1008,2244]">` +
		`<node index="0" class="androidx.recyclerview.widget.RecyclerView" scrollable="true" ` +
		`bounds="[0,0][1008,285]">` +
		`<node index="0" text="history" class="android.widget.TextView" bounds="[0,0][500,285]" />` +
		`</node>` +
		`<node index="1" text="7" class="android.widget.Button" clickable="true" enabled="true" ` +
		`bounds="[9,1218][249,1452]" />` +
		`</node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d := &scrollingDriver{visible: 5, t: t}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: &fixedTree{tree: tree, gest: d}, backend: BackendDump}

	if _, err := tap(h, s, map[string]interface{}{"target": "text=7"}); err != nil {
		t.Fatalf("a button outside the scroll container could not be tapped: %v", err)
	}
	if d.swipes != 0 {
		t.Errorf("swiped %d times at a list the button is not inside", d.swipes)
	}
}

func TestAnElementInsideItsScrollContainerIsStillJudgedByIt(t *testing.T) {
	// The other half: a row genuinely scrolled out of its own list must
	// still be recognized as out of view. Fixing the case above must not
	// turn the containment check off.
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="com.example" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" class="androidx.recyclerview.widget.RecyclerView" scrollable="true" ` +
		`bounds="[0,200][1080,1200]">` +
		`<node index="0" text="Far below" class="android.widget.Button" clickable="true" ` +
		`enabled="true" bounds="[0,1800][1080,2000]" />` +
		`</node></node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d := &scrollingDriver{visible: 5, t: t}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: &fixedTree{tree: tree, gest: d}, backend: BackendDump}

	if _, err := tap(h, s, map[string]interface{}{"target": "text=Far below"}); err == nil {
		t.Fatal("a row outside its own list was tapped where it is not")
	}
	if d.swipes == 0 {
		t.Error("did not even try to scroll a row that is inside a scrollable")
	}
}

// recordingGesturer captures one swipe's coordinates.
type recordingGesturer struct {
	x1, y1, x2, y2 int
}

func (g *recordingGesturer) Tap(ctx context.Context, x, y int) error { return nil }
func (g *recordingGesturer) Swipe(ctx context.Context, x1, y1, x2, y2 int, _ time.Duration) error {
	g.x1, g.y1, g.x2, g.y2 = x1, y1, x2, y2
	return nil
}
func (g *recordingGesturer) LongPress(ctx context.Context, x, y int, _ time.Duration) error {
	return nil
}

// The axis is the whole feature, so the geometry is asserted directly rather
// than inferred from a list moving. Each direction has to travel along one
// axis and hold the other still: a "right" swipe that drifts vertically would
// scroll a list that happened to be underneath it.
func TestSwipeWithinTravelsAlongTheNamedAxis(t *testing.T) {
	r := uitree.Rect{X1: 0, Y1: 0, X2: 400, Y2: 800}
	cases := []struct {
		dir         string
		wantAxis    string
		wantForward bool // coordinate decreases, i.e. content advances
	}{
		{"down", "y", true},
		{"up", "y", false},
		{"right", "x", true},
		{"left", "x", false},
	}
	for _, tc := range cases {
		g := &recordingGesturer{}
		if err := swipeWithin(context.Background(), g, r, tc.dir); err != nil {
			t.Fatalf("%s: %v", tc.dir, err)
		}
		if tc.wantAxis == "x" {
			if g.y1 != g.y2 {
				t.Errorf("%s drifted vertically: y %d -> %d", tc.dir, g.y1, g.y2)
			}
			if g.x1 == g.x2 {
				t.Errorf("%s did not move horizontally", tc.dir)
			}
			if got := g.x2 < g.x1; got != tc.wantForward {
				t.Errorf("%s traveled x %d -> %d", tc.dir, g.x1, g.x2)
			}
		} else {
			if g.x1 != g.x2 {
				t.Errorf("%s drifted horizontally: x %d -> %d", tc.dir, g.x1, g.x2)
			}
			if g.y1 == g.y2 {
				t.Errorf("%s did not move vertically", tc.dir)
			}
			if got := g.y2 < g.y1; got != tc.wantForward {
				t.Errorf("%s traveled y %d -> %d", tc.dir, g.y1, g.y2)
			}
		}
	}
}

// The insets are not cosmetic. A drag starting at the very edge is the back
// gesture on both platforms, so a pager swipe that began at x=0 would navigate
// rather than scroll — and the loop would report that instead of moving.
func TestSwipeWithinStaysClearOfTheEdges(t *testing.T) {
	r := uitree.Rect{X1: 0, Y1: 0, X2: 400, Y2: 800}
	for _, dir := range []string{"left", "right"} {
		g := &recordingGesturer{}
		if err := swipeWithin(context.Background(), g, r, dir); err != nil {
			t.Fatal(err)
		}
		for _, x := range []int{g.x1, g.x2} {
			if x <= r.X1 || x >= r.X2 {
				t.Errorf("%s touched the edge at x=%d, within %d..%d", dir, x, r.X1, r.X2)
			}
		}
	}
}

func TestScrollDirectionsAreTheFourNamed(t *testing.T) {
	for _, ok := range []string{"down", "up", "left", "right"} {
		if !isScrollDirection(ok) {
			t.Errorf("%q should be a scroll direction", ok)
		}
	}
	for _, bad := range []string{"", "sideways", "north", "DOWN"} {
		if isScrollDirection(bad) {
			t.Errorf("%q should not be a scroll direction", bad)
		}
	}
}

// swipeRecorder records the swipes it is asked for and does nothing else.
type swipeRecorder struct{ swipes [][4]int }

func (r *swipeRecorder) Swipe(ctx context.Context, x1, y1, x2, y2 int, _ time.Duration) error {
	r.swipes = append(r.swipes, [4]int{x1, y1, x2, y2})
	return nil
}
func (r *swipeRecorder) LongPress(ctx context.Context, x, y int, _ time.Duration) error { return nil }

// A found target is swiped in by the distance it is out, from whichever
// side it is on. The geometry is the iPhone simulator's Dialog Demo: the list
// at y 405–2301, Location permission 33 pixels short of it at 2190–2334 —
// which a full swipe carried past the top, to 81–228.
func TestAFoundTargetIsSwipedInByTheDistanceItIsOut(t *testing.T) {
	list := &uitree.Node{Bounds: uitree.Rect{X1: 0, Y1: 405, X2: 1206, Y2: 2301}}
	ctx := context.Background()

	r := &swipeRecorder{}
	below := &uitree.Node{Bounds: uitree.Rect{X1: 48, Y1: 2190, X2: 1158, Y2: 2334}}
	if ok, err := nudgeInto(ctx, r, list, below, false); !ok || err != nil {
		t.Fatalf("no nudge for a target below: %v, %v", ok, err)
	}
	// 33 out plus an eighth of 1896 is 270, centered on the list's middle.
	if got, want := r.swipes[0], [4]int{603, 1353 + 135, 603, 1353 - 135}; got != want {
		t.Errorf("below: swiped %v, want %v", got, want)
	}

	r = &swipeRecorder{}
	above := &uitree.Node{Bounds: uitree.Rect{X1: 48, Y1: 81, X2: 1158, Y2: 228}}
	if ok, _ := nudgeInto(ctx, r, list, above, false); !ok || r.swipes[0][3] <= r.swipes[0][1] {
		t.Errorf("a target above was not swiped down toward: %v", r.swipes)
	}
	// Past the top by 324, plus 237: 561, which is under half the list.
	if d := r.swipes[0][3] - r.swipes[0][1]; d != 560 && d != 561 {
		t.Errorf("above: swiped %d, want about 561", d)
	}

	r = &swipeRecorder{}
	tall := &uitree.Node{Bounds: uitree.Rect{X1: 0, Y1: 300, X2: 1206, Y2: 2500}}
	if ok, _ := nudgeInto(ctx, r, list, tall, false); ok || len(r.swipes) != 0 {
		t.Errorf("a target taller than the list was nudged: %v", r.swipes)
	}
	if ok, _ := nudgeInto(ctx, r, nil, nil, false); ok {
		t.Error("nudged with no target")
	}
}

func TestScrollToWaitsOutAScreenStillChanging(t *testing.T) {
	// Measured on Settings: 60ms after back, the screen read as having
	// nothing scrollable, and a moment later the list was there. Deciding
	// "nothing scrolls" from that one reading failed 3 times in 15.
	h, sess, _ := withFake(t, screen(t, ""), screen(t, "Target"))
	h.implicitWait = 2 * time.Second
	res, err := scrollTo(h, sess, map[string]interface{}{"target": "text=Target"})
	if err != nil {
		t.Fatalf("a target that arrived a moment later was reported missing: %v", err)
	}
	if v, ok := res.StructuredContent.(ScrollView); ok && v.Element == nil {
		t.Error("the element was found but not returned")
	}
}
