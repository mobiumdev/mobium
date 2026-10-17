package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
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
	//
	// Two rows of one name: a control and its own title are one match since
	// Wear OS's chips (CHALLENGES 315), which is what this fixture was.
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="com.example.list" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" package="com.example.list" class="androidx.recyclerview.widget.RecyclerView" ` +
		`scrollable="true" bounds="[0,200][1080,1200]">` +
		`<node index="0" class="android.widget.LinearLayout" clickable="true" ` +
		`enabled="true" bounds="[0,300][1080,500]">` +
		`<node index="0" text="Privacy" class="android.widget.TextView" bounds="[40,340][600,460]" />` +
		`</node>` +
		`<node index="1" class="android.widget.LinearLayout" clickable="true" ` +
		`enabled="true" bounds="[0,500][1080,700]">` +
		`<node index="0" text="Privacy" class="android.widget.TextView" bounds="[40,540][600,660]" />` +
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
	if ok, _, _, err := nudgeInto(ctx, r, list, below, false, false); !ok || err != nil {
		t.Fatalf("no nudge for a target below: %v, %v", ok, err)
	}
	// 33 out plus an eighth of 1896 is 270, centered on the list's middle.
	if got, want := r.swipes[0], [4]int{603, 1353 + 135, 603, 1353 - 135}; got != want {
		t.Errorf("below: swiped %v, want %v", got, want)
	}

	r = &swipeRecorder{}
	above := &uitree.Node{Bounds: uitree.Rect{X1: 48, Y1: 81, X2: 1158, Y2: 228}}
	if ok, _, _, _ := nudgeInto(ctx, r, list, above, false, false); !ok || r.swipes[0][3] <= r.swipes[0][1] {
		t.Errorf("a target above was not swiped down toward: %v", r.swipes)
	}
	// Past the top by 324, plus 237: 561, which is under half the list.
	if d := r.swipes[0][3] - r.swipes[0][1]; d != 560 && d != 561 {
		t.Errorf("above: swiped %d, want about 561", d)
	}

	r = &swipeRecorder{}
	tall := &uitree.Node{Bounds: uitree.Rect{X1: 0, Y1: 300, X2: 1206, Y2: 2500}}
	if ok, _, _, _ := nudgeInto(ctx, r, list, tall, false, false); ok || len(r.swipes) != 0 {
		t.Errorf("a target taller than the list was nudged: %v", r.swipes)
	}
	if ok, _, _, _ := nudgeInto(ctx, r, nil, nil, false, false); ok {
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

// pagerDriver is a horizontal pager the way Android reports one: eight cards
// 683px wide with 31px between them, and every card's bounds clipped to the
// pager, so one partly scrolled in reports only the part that shows. A swipe
// moves the content by the distance the finger travels.
type pagerDriver struct {
	offset, swipes int
	t              *testing.T
}

const pagerX1, pagerX2, cardW, cardGap = 42, 1038, 683, 31

func (d *pagerDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	var b strings.Builder
	b.WriteString(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="com.example.pager" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" package="com.example.pager" class="android.widget.HorizontalScrollView" scrollable="true" ` +
		`bounds="[42,455][1038,917]"><node index="0" package="com.example.pager" class="android.view.ViewGroup" ` +
		`bounds="[42,455][1038,917]">`)
	for i := 1; i <= 8; i++ {
		x1 := pagerX1 + (i-1)*(cardW+cardGap) - d.offset
		x2 := x1 + cardW
		if x2 <= pagerX1 || x1 >= pagerX2 {
			continue
		}
		if x1 < pagerX1 {
			x1 = pagerX1
		}
		if x2 > pagerX2 {
			x2 = pagerX2
		}
		fmt.Fprintf(&b, `<node index="%d" content-desc="Card %d" package="com.example.pager" class="android.view.ViewGroup" `+
			`clickable="true" enabled="true" bounds="[%d,476][%d,896]" />`, i-1, i, x1, x2)
	}
	b.WriteString(`</node></node></node></hierarchy>`)
	return uitree.ParseAndroid([]byte(b.String()))
}
func (d *pagerDriver) Screenshot(ctx context.Context) ([]byte, error) { return nil, nil }
func (d *pagerDriver) Tap(ctx context.Context, x, y int) error        { return nil }
func (d *pagerDriver) Name() string                                   { return "pager-fake" }
func (d *pagerDriver) LongPress(ctx context.Context, x, y int, _ time.Duration) error {
	return nil
}
func (d *pagerDriver) Swipe(ctx context.Context, x1, y1, x2, y2 int, _ time.Duration) error {
	d.swipes++
	d.offset += x1 - x2
	max := 8*(cardW+cardGap) - cardGap - (pagerX2 - pagerX1)
	if d.offset > max {
		d.offset = max
	}
	if d.offset < 0 {
		d.offset = 0
	}
	return nil
}

// scroll-to brings the whole target into view, not the sliver of it that
// clipped bounds report as wholly inside: Card 8 used to be left 101px wide
// at the pager's edge on MobiumApp's Pager Demo. CHALLENGES 169.
func TestScrollToBringsAClippedTargetWhollyIntoView(t *testing.T) {
	d := &pagerDriver{t: t}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}
	h.sessions["fake"] = s
	if _, err := scrollTo(h, s, map[string]interface{}{"target": "label=Card 8", "direction": "right"}); err != nil {
		t.Fatal(err)
	}
	tree, _ := d.Snapshot(context.Background())
	n, err := pickOne(uitree.Locator{Kind: uitree.KindLabel, Value: "Card 8", Exact: true}, tree)
	if err != nil {
		t.Fatal(err)
	}
	if w := n.Bounds.Width(); w != cardW {
		t.Errorf("Card 8 ends %dpx wide at %v, want all %dpx of it in view", w, n.Bounds, cardW)
	}
}

// A whole row flush against the list's edge is not a clipped one: it is the
// same height as its neighbors, and scroll-to leaves the list where it is.
func TestAWholeRowFlushAgainstTheEdgeIsNotNudged(t *testing.T) {
	h, s, d := withList(t, rows(12))
	// Row 04 is the last of five, flush with the bottom of the list.
	if _, err := scrollTo(h, s, map[string]interface{}{"target": "text=Row 04"}); err != nil {
		t.Fatal(err)
	}
	if d.swipes != 0 {
		t.Errorf("swiped %d times for a row already wholly in view", d.swipes)
	}
}

// Flutter on iOS: the scroll view is an empty element and the rows it scrolls
// are its siblings. Read through the parent, a scroll that brought new rows
// is movement; read alone, it was "the end of the list" after one swipe.
func TestAChildlessScrollViewIsReadThroughItsParent(t *testing.T) {
	screen := func(first int) *uitree.Node {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="F" enabled="true" visible="true" x="0" y="0" width="402" height="874">`)
		b.WriteString(`<XCUIElementTypeScrollView type="XCUIElementTypeScrollView" enabled="true" visible="true" x="0" y="118" width="402" height="756"/>`)
		for i := 0; i < 5; i++ {
			fmt.Fprintf(&b, `<XCUIElementTypeStaticText type="XCUIElementTypeStaticText" name="Row %d" label="Row %d" enabled="true" visible="true" x="16" y="%d" width="370" height="57"/>`, first+i, first+i, 200+i*57)
		}
		b.WriteString(`</XCUIElementTypeApplication></AppiumAUT>`)
		tree, err := uitree.ParseIOS([]byte(b.String()))
		if err != nil {
			t.Fatal(err)
		}
		return scrollContainer(tree)
	}
	before, after := screen(1), screen(20)
	if before == nil || len(before.Children) != 0 {
		t.Fatalf("the fixture's scroll view is not the childless one: %+v", before)
	}
	if !moved(read(before), read(after)) {
		t.Error("new rows after a swipe did not count as movement")
	}
	if moved(read(before), read(screen(1))) {
		t.Error("the same rows counted as movement")
	}
}

// A target out of its container on one axis only is nudged along that axis,
// whatever the caller asked: Pocket Casts' Discover carousel, rows in
// columns, the second column out to the right and in view top to bottom.
// An action asks for "down"; the swipe must travel sideways and hold still
// vertically. Out on both axes, the caller's axis stands. CHALLENGES 261.
func TestANudgeTravelsTheAxisTheTargetIsOutOn(t *testing.T) {
	carousel := &uitree.Node{Bounds: uitree.Rect{X1: 16, Y1: 316, X2: 398, Y2: 589}}
	row := &uitree.Node{Bounds: uitree.Rect{X1: 394, Y1: 316, X2: 756, Y2: 369}}
	g := &recordingGesturer{}
	nudged, _, _, err := nudgeInto(context.Background(), g, carousel, row, false, true)
	if err != nil || !nudged {
		t.Fatalf("nudged %v: %v", nudged, err)
	}
	if g.y1 != g.y2 || g.x2 >= g.x1 {
		t.Errorf("swiped (%d,%d) to (%d,%d), want leftward and level", g.x1, g.y1, g.x2, g.y2)
	}
	below := &uitree.Node{Bounds: uitree.Rect{X1: 500, Y1: 600, X2: 600, Y2: 650}}
	g = &recordingGesturer{}
	if nudged, _, _, _ := nudgeInto(context.Background(), g, carousel, below, false, true); nudged && g.x1 != g.x2 {
		t.Errorf("out on both axes, swiped (%d,%d) to (%d,%d), want the caller's vertical axis", g.x1, g.y1, g.x2, g.y2)
	}
	// app_scroll_to named its axis: a "down" there stays vertical.
	g = &recordingGesturer{}
	if nudged, _, _, _ := nudgeInto(context.Background(), g, carousel, row, false, false); nudged && g.y1 == g.y2 {
		t.Errorf("an explicit vertical scroll swiped sideways, (%d,%d) to (%d,%d)", g.x1, g.y1, g.x2, g.y2)
	}
}

// A ref that names a position, once its list has scrolled, may find a cell
// reused for something else: Freakonomics Radio's ref found Revisionist
// History, and the tap opened it. Refused when the name changed; let
// through when it did not. CHALLENGES 261.
func TestARefMustNameTheSameElementAfterAScroll(t *testing.T) {
	h := NewHandlers()
	h.refs["fake"] = &refTable{entries: map[string]uitree.Locator{}, seen: map[string]refSeen{
		"@e11": {name: "Freakonomics Radio Freakonomics Radio + Stitcher"}}}
	other := &uitree.Node{Label: "Revisionist History Pushkin Industries", Displayed: true, Enabled: true}
	if err := h.sameAfterScroll("fake", "@e11", other); mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement {
		t.Errorf("a reused cell was let through: %v", err)
	}
	same := &uitree.Node{Label: "Freakonomics Radio Freakonomics Radio + Stitcher", Displayed: true, Enabled: true}
	if err := h.sameAfterScroll("fake", "@e11", same); err != nil {
		t.Errorf("the same element was refused: %v", err)
	}
	if err := h.sameAfterScroll("fake", "text=Freakonomics Radio", other); err != nil {
		t.Errorf("a locator that is not a ref was refused: %v", err)
	}
}

// A ref's scroll follows the element map named, not the position the ref
// was given, once a reused cell holds something else there: Freakonomics
// Radio's ref found Revisionist History after a nudge, and the loop chased
// it until the carousel paged to its end. CHALLENGES 267.
func TestARefsScrollFollowsTheNameMapGave(t *testing.T) {
	tree, err := uitree.ParseIOS([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="x" label="x" enabled="true" visible="true" accessible="false" x="0" y="0" width="402" height="874">
 <XCUIElementTypeCollectionView type="XCUIElementTypeCollectionView" enabled="true" visible="true" accessible="false" x="16" y="316" width="382" height="273">
  <XCUIElementTypeButton type="XCUIElementTypeButton" label="Revisionist History" enabled="true" visible="true" accessible="true" x="16" y="316" width="362" height="53"/>
  <XCUIElementTypeButton type="XCUIElementTypeButton" label="Freakonomics Radio" enabled="true" visible="true" accessible="true" x="394" y="316" width="362" height="53"/>
 </XCUIElementTypeCollectionView>
</XCUIElementTypeApplication>`))
	if err != nil {
		t.Fatal(err)
	}
	at, _ := uitree.ParseLocator("label=Revisionist History")
	if n, err := refPicker(at, "Freakonomics Radio")(tree); err != nil || uitree.Describe(n) != "Freakonomics Radio" {
		t.Errorf("followed the position, not the name: %v, %v", n, err)
	}
	if n, err := refPicker(at, "Revisionist History")(tree); err != nil || uitree.Describe(n) != "Revisionist History" {
		t.Errorf("the position holding the named element was not used: %v, %v", n, err)
	}
	if _, err := refPicker(at, "Planet Money")(tree); mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement {
		t.Errorf("a name on no element resolved: %v", err)
	}
	if n, err := refPicker(at, "")(tree); err != nil || uitree.Describe(n) != "Revisionist History" {
		t.Errorf("a locator that is not a ref did not resolve as itself: %v, %v", n, err)
	}
}

// CHALLENGES 295: every post in Ice Cubes has a "…" button of one name, so
// its ref is a position, and after a nudge the position held another post's
// button or nothing. The follower looks where the nudge moved it.
func TestAFollowedRefIsFoundWhereTheNudgeMovedIt(t *testing.T) {
	button := func(y int) *uitree.Node {
		return &uitree.Node{Clickable: true, Displayed: true, Label: "status.action.context-menu",
			Bounds: uitree.Rect{X1: 1000, Y1: y, X2: 1100, Y2: y + 80}}
	}
	screen := func(ys ...int) *uitree.Tree {
		root := &uitree.Node{Bounds: uitree.Rect{X2: 1206, Y2: 2622}}
		for _, y := range ys {
			b := button(y)
			b.Parent = root
			root.Children = append(root.Children, b)
		}
		return &uitree.Tree{Root: root}
	}
	// The ref's position: the first child, whatever it now holds.
	f := &follower{loc: uitree.Locator{Kind: uitree.KindPath, Value: "/0"}, name: "status.action.context-menu"}

	// Before: the target at the bottom edge, y 2560. A nudge of 900 moves
	// the list up; the target is now at 1660, and the first child — the
	// ref's position — holds another post's button at 760.
	f.moved(uitree.Rect{X1: 1000, Y1: 2560, X2: 1100, Y2: 2640}, 0, 900)
	got, err := f.pick(screen(760, 1660, 2500))
	if err != nil || got.Bounds.Y1 != 1660 {
		t.Fatalf("after a nudge the follower picked %+v (%v), want the button at 1660", got, err)
	}

	// Two look-alikes as near as each other: no guess.
	if n := nearestNamed(screen(1500, 1820), "status.action.context-menu", uitree.Rect{X1: 1000, Y1: 1660, X2: 1100, Y2: 1740}); n != nil {
		t.Errorf("between two equally near look-alikes it chose %+v", n.Bounds)
	}
	// Nothing near where it should be: none.
	if n := nearestNamed(screen(100), "status.action.context-menu", uitree.Rect{X1: 1000, Y1: 1660, X2: 1100, Y2: 1740}); n != nil {
		t.Errorf("a button far from where it went was taken for it: %+v", n.Bounds)
	}
}

// CHALLENGES 295: the live timeline re-laid out between map and tap, the
// ref's position held something else, several buttons shared its name, and
// the loop swiped fifteen times before blaming the list's length. With no
// nudge to follow, the follower refuses at once, saying why.
func TestARefLostAmongLookAlikesIsRefusedAtOnce(t *testing.T) {
	root := &uitree.Node{Bounds: uitree.Rect{X2: 1206, Y2: 2622}}
	for _, y := range []int{700, 1500} {
		b := &uitree.Node{Clickable: true, Displayed: true, Label: "status.action.context-menu",
			Bounds: uitree.Rect{X1: 1000, Y1: y, X2: 1100, Y2: y + 80}, Parent: root}
		root.Children = append(root.Children, b)
	}
	other := &uitree.Node{Clickable: true, Displayed: true, Label: "Reply", Bounds: uitree.Rect{X1: 100, Y1: 300, X2: 200, Y2: 380}, Parent: root}
	root.Children = append([]*uitree.Node{other}, root.Children...)
	f := &follower{loc: uitree.Locator{Kind: uitree.KindPath, Value: "/0"}, name: "status.action.context-menu"}
	_, err := f.pick(&uitree.Tree{Root: root})
	if !lostAmongLookAlikes(err) || !strings.Contains(err.Error(), "2 elements on the screen share that name") {
		t.Fatalf("a ref lost among look-alikes: %v", err)
	}
	if e, _ := mobiumerr.As(err); !strings.Contains(e.Remedy, "app_map again") {
		t.Errorf("remedy %q", e.Remedy)
	}
	// One of its name is still found, as before (CHALLENGES 267).
	root.Children = root.Children[:2]
	if n, err := f.pick(&uitree.Tree{Root: root}); err != nil || n.Bounds.Y1 != 700 {
		t.Errorf("the one of its name: %v %v", n, err)
	}
}

// The same through the loop, as it failed: the ref's position now holds
// another element below the fold, so the tap scrolls for it, and the rows
// on the screen all share the name map gave it. It used to swipe fifteen
// times and blame the list's length; it is refused before the first swipe.
func TestATapOnARefLostAmongLookAlikesDoesNotSwipe(t *testing.T) {
	same := make([]string, 30)
	for i := range same {
		same[i] = "More"
	}
	same[7] = "Row 07"
	h, sess, d := withList(t, same)
	there, _ := uitree.ParseLocator("text=Row 07")
	h.refs["fake"] = &refTable{entries: map[string]uitree.Locator{"@e9": there},
		// Where map saw it, so staleRef's unchanged-position escape lets the
		// ref through, as it lets a reused cell (261).
		seen: map[string]refSeen{"@e9": {name: "More", bounds: uitree.Rect{Y1: 1600, X2: 1080, Y2: 1800}}}}
	_, err := tap(h, sess, map[string]interface{}{"target": "@e9"})
	if !lostAmongLookAlikes(err) {
		t.Fatalf("not refused as lost among look-alikes: %v", err)
	}
	if d.swipes != 0 || len(d.tapped) != 0 {
		t.Errorf("swiped %d times and tapped %v", d.swipes, d.tapped)
	}
}

// The limit's remedy must work. It named a deep link, which few apps have
// for a row; the list stays where a scroll left it, so scroll-to again goes
// on from there, and in a list long enough it gets there. CHALLENGES 309.
func TestTheSwipeLimitSaysToGoOn(t *testing.T) {
	h, sess, _ := withList(t, rows(400))
	_, err := scrollTo(h, sess, map[string]interface{}{"target": "text=Row 399"})
	e, ok := mobiumerr.As(err)
	if !ok || e.Code != mobiumerr.NoSuchElement || !strings.Contains(e.Remedy, "app_scroll_to again") {
		t.Fatalf("at the limit: %v (remedy %q)", err, e.Remedy)
	}
	if e.Details["scrolls"] != maxScrolls {
		t.Errorf("scrolls = %v, want %d", e.Details["scrolls"], maxScrolls)
	}
	for i := 0; i < 40; i++ {
		if _, err := scrollTo(h, sess, map[string]interface{}{"target": "text=Row 399"}); err == nil {
			return
		}
	}
	t.Error("scroll-to again never went on to Row 399")
}

// How far a list moved is the middle of what its named rows moved; fewer
// than three to go by is no measurement.
func TestShiftIsWhatTheRowsMoved(t *testing.T) {
	r := func(ys ...int) reading {
		at := map[string]uitree.Rect{}
		for i, y := range ys {
			at[fmt.Sprintf("Row %d", i)] = uitree.Rect{X1: 0, Y1: y, X2: 100, Y2: y + 50}
		}
		return reading{at: at}
	}
	if d, ok := shift(r(0, 60, 120), r(300, 360, 420), false); !ok || d != 300 {
		t.Errorf("shift = %d, %v; want 300", d, ok)
	}
	if _, ok := shift(r(0, 60), r(300, 360), false); ok {
		t.Error("two rows were taken for a measurement")
	}
}

// Toward the start, the miss says the last swipe may have been a pull an
// app answered by refreshing, in the text and the structured answer; toward
// the end it says nothing of the kind. CHALLENGES 310.
func TestTheStartOfAListSaysItMayHaveRefreshed(t *testing.T) {
	loc, _ := uitree.ParseLocator("text=Nowhere")
	up, _ := mobiumerr.As(endOfList(loc, nil, "up", 6, false))
	if !strings.Contains(up.Error(), "start of the list") || !strings.Contains(up.Error(), "refreshing") ||
		up.Details["may_have_refreshed"] != true {
		t.Errorf("up: %v %v", up, up.Details)
	}
	down, _ := mobiumerr.As(endOfList(loc, nil, "down", 3, false))
	if !strings.Contains(down.Error(), "end of the list") || down.Details["may_have_refreshed"] != nil {
		t.Errorf("down: %v %v", down, down.Details)
	}
}

// promptDriver is a list over which Android's permission prompt comes up —
// after the first swipe, as a prompt comes a moment after the tap that asked
// for it, or before any (up) — and goes when one of its buttons is tapped.
type promptDriver struct {
	leavingDriver
	up, answered bool
	prompt       *uitree.Tree
}

func (d *promptDriver) showing() bool { return (d.up || d.left) && !d.answered }

func (d *promptDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	if d.showing() {
		return d.prompt, nil
	}
	return listAtIn(d.t, d.first, d.rows, "com.example.list"), nil
}

func (d *promptDriver) Swipe(ctx context.Context, x1, y1, x2, y2 int, t time.Duration) error {
	if d.showing() {
		return nil // the prompt takes the touch, and nothing moves
	}
	if !d.left && !d.up {
		// The prompt comes up under this swipe, which lands on it and
		// moves nothing, as on the emulator.
		d.left = true
		return nil
	}
	return d.scrollingDriver.Swipe(ctx, x1, y1, x2, y2, t)
}

func (d *promptDriver) Tap(ctx context.Context, x, y int) error {
	if d.showing() {
		d.answered = true
	}
	return d.scrollingDriver.Tap(ctx, x, y)
}

func (d *promptDriver) AlertText(ctx context.Context) (string, error) {
	if d.showing() {
		return "Allow MobiumApp to take pictures and record video?", nil
	}
	return "", mobiumdriver.ErrNoAlert
}
func (d *promptDriver) AnswerAlert(ctx context.Context, accept bool) error   { return nil }
func (d *promptDriver) SendAlertText(ctx context.Context, text string) error { return nil }

func newPromptDriver(t *testing.T, up bool) *promptDriver {
	raw, err := os.ReadFile("../uitree/testdata/android-permission-uia2.xml")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := uitree.ParseAndroid(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &promptDriver{leavingDriver: leavingDriver{scrollingDriver: scrollingDriver{rows: rows(30), visible: 5, t: t}},
		up: up, prompt: prompt}
}

// A dialog that comes up while scrolling, or is up when it starts, is not
// the swipe navigating nor a screen that cannot scroll: a declared rule
// answers it and the scroll goes on, and without one the refusal names it.
// Measured on the Android 15 emulator: a camera prompt after the tap that
// asked for it, with a rule declared, was "swiping up moved from
// dev.mobium.mobiumapp to com.google.android.permissioncontroller instead of
// scrolling", three times in three. CHALLENGES 328.
func TestADialogWhileScrollingIsNamedOrAnswered(t *testing.T) {
	for _, c := range []struct {
		name     string
		up, rule bool
	}{
		{"comes up, a rule", false, true},
		{"comes up, no rule", false, false},
		{"already up, a rule", true, true},
		{"already up, no rule", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := newPromptDriver(t, c.up)
			h := NewHandlers()
			h.implicitWait = 0
			h.settleWindow = 0
			s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}
			h.sessions["fake"] = s
			if c.rule {
				h.dialogRules[s.dev.Serial] = []dialogRule{{When: "take pictures", Press: "While using the app"}}
			}
			_, err := scrollTo(h, s, map[string]interface{}{"target": "text=Row 22"})
			if c.rule {
				if err != nil {
					t.Fatalf("the rule did not answer the prompt: %v", err)
				}
				if !d.answered || len(h.handled) != 1 {
					t.Errorf("answered %v, handled %v", d.answered, h.handled)
				}
				return
			}
			if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "take pictures") {
				t.Fatalf("want device_not_ready naming the dialog, got %v", err)
			}
			if strings.Contains(err.Error(), "other axis") || strings.Contains(err.Error(), "nothing on this screen scrolls") {
				t.Errorf("still blames the list: %v", err)
			}
		})
	}
}

// heldListDriver is a list an iOS alert comes up over, after the first swipe:
// the app in front does not change, as iOS keeps an alert in the app's own
// tree, and no swipe moves the list while it is up.
type heldListDriver struct {
	scrollingDriver
	up bool
}

func (d *heldListDriver) Swipe(ctx context.Context, x1, y1, x2, y2 int, t time.Duration) error {
	if !d.up {
		d.up = true // the alert takes this swipe
		return nil
	}
	return nil
}

func (d *heldListDriver) AlertText(ctx context.Context) (string, error) {
	if d.up {
		return "“MobiumApp” would like to access the Camera.", nil
	}
	return "", mobiumdriver.ErrNoAlert
}
func (d *heldListDriver) AnswerAlert(ctx context.Context, accept bool) error   { return nil }
func (d *heldListDriver) SendAlertText(ctx context.Context, text string) error { return nil }

// A list an alert holds still is not at its end: the refusal names the alert.
// Measured on the iPhone 17 Pro simulator: a scroll up under the camera prompt
// said "scrolled up to the start of the list (1 scroll)". CHALLENGES 330.
func TestAListHeldByAnAlertIsNotItsEnd(t *testing.T) {
	d := &heldListDriver{scrollingDriver: scrollingDriver{rows: rows(30), visible: 5, t: t}}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}
	h.sessions["fake"] = s
	_, err := scrollTo(h, s, map[string]interface{}{"target": "text=Row 22"})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "access the Camera") {
		t.Fatalf("want device_not_ready naming the alert, got %v", err)
	}
	if strings.Contains(err.Error(), "end of the list") || strings.Contains(err.Error(), "start of the list") {
		t.Errorf("still blames the list: %v", err)
	}
}
