package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"math"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// Scrolling is a swipe-and-recheck loop rather than a call to the platform's
// own scrollIntoView, and the **axis comes from the caller**.
//
// It has to. Nothing in the hierarchy says which way a container scrolls:
// Android clips child bounds to the parent, so a horizontal pager and a
// vertical list are indistinguishable from outside — measured across every
// scrollable on the launcher and two Settings screens, overflow was zero in
// both axes every time (CHALLENGES 21). Guessing would be wrong roughly
// whenever it mattered, and a wrong guess is not a no-op: a vertical swipe on
// a pager does whatever the app does with an upward drag, which on a launcher
// opens the app drawer.
//
// So `direction` is the answer to a question only the caller can answer, and
// the loop below watches for the swipe having navigated instead of scrolled.
//
// Both device-side servers offer one — UiAutomator2 through the
// `-android uiautomator` selector strategy, WebDriverAgent through
// /wda/element/{id}/scroll — and either would be a single round trip instead
// of this loop. Neither takes Mobium's locators, though: UiSelector covers
// text, description, resource-id and class but not role or path, and the WDA
// endpoint needs an element id, which is the thing being looked for. Mapping
// what fits and falling back for the rest would mean scrolling behaving
// differently depending on how the caller happened to name the element.
//
// So the loop is the whole implementation: one behavior on every backend,
// including the dump backend, which has no native scroll at all. The cost is
// round trips, and a snapshot is 0.04s on UiAutomator2.
const (
	// maxScrolls bounds the loop. A list long enough to need more than this
	// is better reached by a deep link than by swiping.
	maxScrolls = 15

	// scrollDuration is slow enough not to fling. A fling keeps moving after
	// the gesture ends, so the snapshot that follows catches the list
	// mid-flight and the progress check compares two blurred frames.
	scrollDuration = 400 * time.Millisecond

	// settleAfterScroll lets the list stop before it is read.
	settleAfterScroll = 150 * time.Millisecond

	// loadWait bounds how long a list showing a busy indicator is given to
	// load what follows before the loop calls it the end.
	loadWait = 10 * time.Second
)

// busyIn reports a busy indicator inside the list: a page loading below its
// last row. Only inside it, so a spinner that is always on screen elsewhere
// cannot hold up every scroll. Shown or not: it sits below the last row, so
// when the list stops with that row at its bottom edge it is just out of
// view, and iOS reports it not visible — asking for a visible one, scroll-to
// still found "the end (7 scrolls)" in two runs of four, both of which went
// on to load the page. A stopped indicator hides itself and leaves the tree.
func busyIn(tree *uitree.Tree, container *uitree.Node) bool {
	busy := false
	tree.Walk(func(n *uitree.Node) bool {
		if !n.Bounds.Empty() && n.Within(container) && uitree.HasClassRole(n, "progressbar") {
			busy = true
		}
		return !busy
	})
	return busy
}

// waitNotBusy reads the screen until the list's busy indicator has gone, or
// loadWait has passed, and returns the last reading.
func (h *Handlers) waitNotBusy(ctx context.Context, s *session) (*uitree.Tree, error) {
	deadline := time.Now().Add(loadWait)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		c := scrollContainer(tree)
		if c == nil || !busyIn(tree, c) || time.Now().After(deadline) {
			return tree, nil
		}
	}
}

// scrollTo is app_scroll_to.
func (h *Handlers) scrollTo(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.scrollToOn(ctx, s, args)
}

// scrollToOn is app_scroll_to once the device is resolved.
func (h *Handlers) scrollToOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	if s.web != nil {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "app_scroll_to works on the native context — "+
			"switch back with app_context native")
	}
	target := stringArg(args, "target")
	if target == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_scroll_to needs a target (\"@e5\" or \"text=Sign out\")")
	}
	dir := strings.ToLower(stringArg(args, "direction"))
	if dir == "" {
		dir = "down"
	}
	if !isScrollDirection(dir) {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "direction must be \"down\", \"up\", \"left\" or \"right\", got %q — "+
			"and it is the caller's to give: nothing in the hierarchy says which way a "+
			"container scrolls", dir)
	}

	loc, err := h.locatorFor(s.dev.Serial, target)
	if err != nil {
		return nil, err
	}
	node, tree, scrolls, err := h.scrollIntoView(ctx, s, loc, dir, false, h.refName(s.dev.Serial, target))
	if err != nil {
		return nil, err
	}
	if gest, ok := mobiumdriver.AsGesturer(s.driver); ok {
		var more int
		node, tree, more, err = revealWhole(ctx, s.driver, gest, loc, node, tree, dir)
		if err != nil {
			return nil, err
		}
		scrolls += more
	}

	view := ScrollView{Target: target, Direction: dir, Scrolls: scrolls}
	msg := fmt.Sprintf("%s is on screen", loc)
	if scrolls > 0 {
		msg = fmt.Sprintf("%s after %d scroll%s %s", msg, scrolls, plural(scrolls), dir)
	} else {
		msg += " already"
	}
	if e, ok := h.rememberRefs(s.dev.Serial, tree, node); ok {
		ev := elementView(e)
		view.Element = &ev
		msg += " — " + e.Line()
	}
	return Result(msg, view), nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// scrollIntoView swipes until the locator resolves to a node that is wholly
// inside its scroll container, and returns that node with the snapshot it
// came from.
//
// It reports how many scrolls it took, which is the difference between "this
// was already visible" and "this was eleven swipes down a settings list" —
// worth saying, because the second is usually a sign the caller wants a deep
// link instead.
//
// followTarget is for an action's own scroll, which asks for "down" because
// nothing told it a direction: once the target is found, the nudge travels
// the axis it is out on (nudgeInto). app_scroll_to keeps the caller's axis —
// the caller named it, and a horizontal pager scrolled "down" must not
// answer by moving sideways.
//
// name is what map called the target when it is a ref, and "" otherwise: the
// loop follows the element by it rather than by the ref's position once the
// two part (refPicker).
func (h *Handlers) scrollIntoView(ctx context.Context, s *session, loc uitree.Locator, dir string, followTarget bool, name string) (*uitree.Node, *uitree.Tree, int, error) {
	follow := &follower{loc: loc, name: name}
	pick := picker(follow.pick)
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, nil, 0, err
	}

	// Already there? Resolve against the container it will be measured
	// against, so "visible" means the same thing before and after scrolling.
	container := scrollContainer(tree)
	n, resolveErr := resolvedAndVisible(pick, tree, container)
	if resolveErr == nil {
		return n, tree, 0, nil
	}
	// Two of them is not none of them, and no amount of scrolling turns one
	// into the other. Verified on an iPhone 17 Pro: a Settings cell and the
	// static text inside it carry the same label, so `label=Privacy &
	// Security` matches twice while the old message insisted nothing matched.
	if lostAmongLookAlikes(resolveErr) || !matchedNothing(resolveErr) && !errors.Is(resolveErr, errOffScreen) {
		return nil, nil, 0, resolveErr
	}
	// A screen in the middle of changing can read, for a moment, as having
	// nothing that scrolls: measured on Settings, a scroll-to 60ms after
	// pressing back failed 3 times in 15, and the list was there a moment
	// later. So "nothing scrolls" gets the implicit wait actions already
	// have, and ends as soon as the target resolves or a container appears.
	// Hidden until 2026-09-27 by an adb round trip before every call, which
	// the session reuse in sessionFor removed.
	deadline := time.Now().Add(h.implicitWait)
	for container == nil && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, nil, 0, ctx.Err()
		case <-time.After(pollInterval):
		}
		if tree, err = s.driver.Snapshot(ctx); err != nil {
			return nil, nil, 0, err
		}
		container = scrollContainer(tree)
		if n, resolveErr = resolvedAndVisible(pick, tree, container); resolveErr == nil {
			return n, tree, 0, nil
		}
		if lostAmongLookAlikes(resolveErr) || !matchedNothing(resolveErr) && !errors.Is(resolveErr, errOffScreen) {
			return nil, nil, 0, resolveErr
		}
	}
	if container == nil {
		// Distinguish the two ways this fails. A screen with nothing
		// scrollable cannot be searched by scrolling, and saying so is more
		// use than reporting that fifteen swipes found nothing.
		return nil, nil, 0, mobiumerr.New(mobiumerr.NoSuchElement, "no element matches %s and nothing on this screen scrolls", loc)
	}

	// Checked only now: a backend that cannot swipe can still answer for an
	// element that is already in view, and refusing above would have made
	// app_scroll_to useless on the dump backend even when it had nothing to do.
	gest, ok := mobiumdriver.AsGesturer(s.driver)
	if !ok {
		return nil, nil, 0, cannot(s, mobiumdriver.CapGestures, "scroll")
	}

	// prev detects the end of the list. A swipe against the end moves
	// nothing, so a reading that has not moved means there is no more to see
	// — without this the loop swipes uselessly to its limit and then blames
	// the locator.
	prev := read(container)
	lastErr := resolveErr

	// The axis came from the caller because nothing here can supply it, but a
	// caller can be wrong too — and swiping the wrong way is not a no-op.
	//
	// The app it happens in is the one signal available, so it is watched:
	// leaving the app mid-scroll means the swipe navigated rather than
	// scrolled, and continuing would swipe fourteen more times somewhere the
	// caller did not ask to be.
	app := tree.Package()

	// partial is the target once it has been found but is not wholly in
	// view, with the container that moves it. From then on the loop swipes
	// only as far as it takes to bring it in, on whichever side it is: a
	// full swipe from a near miss carried a button 33 pixels short of the
	// bottom edge past the top of the list on an iPhone simulator, where a
	// swipe coasts, and the next swipe found the end and gave up with the
	// button in plain sight above it. CHALLENGES 114.
	partial, partialIn := offScreenTarget(pick, tree)

	for i := 1; i <= maxScrolls; i++ {
		nudged, dx, dy, err := nudgeInto(ctx, gest, partialIn, partial, horizontal(dir), followTarget)
		if err != nil {
			return nil, nil, i, err
		}
		if nudged {
			follow.moved(partial.Bounds, dx, dy)
		} else {
			follow.expect = nil // a full swipe's distance is not known
		}
		if !nudged {
			if err := swipeWithin(ctx, gest, container.Bounds, dir); err != nil {
				return nil, nil, i, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, nil, i, ctx.Err()
		case <-time.After(settleAfterScroll):
		}

		tree, err = s.driver.Snapshot(ctx)
		if err != nil {
			return nil, nil, i, err
		}
		if now := tree.Package(); app != "" && now != "" && now != app {
			return nil, nil, i, mobiumerr.New(mobiumerr.ElementNotReachable, "swiping %s moved from %s to %s instead of scrolling — "+
				"the area being swiped does not scroll that way. Nothing in the hierarchy "+
				"says which way it does, so try the other axis, or app_swipe", dir, app, now)
		}
		container = scrollContainer(tree)
		if container == nil {
			return nil, nil, i, mobiumerr.New(mobiumerr.ElementNotReachable, "the scrollable area went away while scrolling %s for %s — "+
				"the swipe changed the screen instead of scrolling it, so it does not "+
				"scroll that way. Try the other axis, or app_swipe", dir, loc)
		}
		n, resolveErr := resolvedAndVisible(pick, tree, container)
		if resolveErr == nil {
			return n, tree, i, nil
		}
		if lostAmongLookAlikes(resolveErr) || !matchedNothing(resolveErr) && !errors.Is(resolveErr, errOffScreen) {
			return nil, nil, i, resolveErr
		}
		lastErr = resolveErr
		partial, partialIn = offScreenTarget(pick, tree)

		now := read(container)
		if !moved(prev, now) {
			// Not the end while the list is still loading what follows. A
			// feed fetches its next page when its end is reached, and the
			// swipe that reached it moves nothing because the rows are not
			// there yet: on MobiumApp's Feed Demo this loop answered "the end
			// of the list (3 scrolls)" for Row 55 while the second page was a
			// second from arriving (CHALLENGES 223). A busy indicator inside
			// the list says so, and it is waited out.
			if busyIn(tree, container) {
				waited, err := h.waitNotBusy(ctx, s)
				if err != nil {
					return nil, nil, i, err
				}
				tree = waited
				if container = scrollContainer(tree); container == nil {
					return nil, nil, i, mobiumerr.New(mobiumerr.ElementNotReachable, "the scrollable area went away "+
						"while it loaded, scrolling %s for %s", dir, loc)
				}
				if n, err := resolvedAndVisible(pick, tree, container); err == nil {
					return n, tree, i, nil
				}
				if busyIn(tree, container) {
					return nil, nil, i, mobiumerr.New(mobiumerr.Timeout, "%s — scrolled %s to where the list "+
						"loads more, and it was still loading after %s", whyNot(loc, resolveErr), dir, loadWait)
				}
				prev = read(container)
				continue
			}
			return nil, nil, i, mobiumerr.New(mobiumerr.NoSuchElement, "%s — scrolled %s to the end of the list "+
				"(%d scroll%s) without bringing it into view", whyNot(loc, resolveErr), dir, i, plural(i))
		}
		prev = now
	}
	return nil, nil, maxScrolls, mobiumerr.New(mobiumerr.NoSuchElement, "%s after scrolling %s %d times — "+
		"the list is longer than mobium will swipe; try a deep link with app_open_url",
		whyNot(loc, lastErr), dir, maxScrolls)
}

// revealWhole brings the rest of a target into view once scrolling has found
// it. Found is not the same as shown: Android clips a child's bounds to its
// container, so a card of which 101 pixels had scrolled in reported bounds
// wholly inside the pager, and scroll-to stopped with Card 8 a sliver at the
// edge of MobiumApp's Pager Demo (CHALLENGES 169). Clipped bounds give
// themselves away twice over: they end flush against the container's edge on
// the axis being scrolled, and they are shorter along it than a sibling of
// the same kind — Card 8 at 101 pixels beside Card 7 at 683. Flush alone is
// not enough: a whole row can sit flush, the same height as its neighbors.
// So when both signs are there, the list moves a third of the container
// toward the target and is read again, for as long as the target grows —
// until it stands clear of the edge, stops growing, or the list stops. iOS
// reports the whole frame, which the measured nudge in scrollIntoView already
// brings in; this changes nothing there.
//
// app_scroll_to only. An action that scrolls to its target needs it
// reachable, which it already is.
func revealWhole(ctx context.Context, d mobiumdriver.Driver, gest mobiumdriver.Gesturer, loc uitree.Locator,
	n *uitree.Node, tree *uitree.Tree, dir string) (*uitree.Node, *uitree.Tree, int, error) {
	horiz := horizontal(dir)
	extent := func(n *uitree.Node) (lo, hi int) {
		if horiz {
			return n.Bounds.X1, n.Bounds.X2
		}
		return n.Bounds.Y1, n.Bounds.Y2
	}
	swipes := 0
	for swipes < maxReveal {
		c := scrollContainerOf(n)
		if c == nil || n.Bounds.Empty() {
			return n, tree, swipes, nil
		}
		lo, hi := extent(n)
		clo, chi := extent(c)
		span := chi - clo
		// Clear of both edges is whole. Flush is whole too unless something
		// says otherwise — checked before the first swipe only: after one, the
		// swipe itself is the evidence, and a target that stopped growing is
		// as whole as it gets.
		if hi-lo >= span || (lo > clo && hi < chi) || (swipes == 0 && !clippedAt(n, clo, chi, extent)) {
			return n, tree, swipes, nil
		}
		// Toward whichever edge it touches: the content moves the other way.
		step := span / 3
		if lo <= clo && hi < chi {
			step = -step
		}
		mid := (clo + chi) / 2
		from, to := mid+step/2, mid-step/2
		var err error
		if horiz {
			y := (c.Bounds.Y1 + c.Bounds.Y2) / 2
			err = gest.Swipe(ctx, from, y, to, y, scrollDuration)
		} else {
			x := (c.Bounds.X1 + c.Bounds.X2) / 2
			err = gest.Swipe(ctx, x, from, x, to, scrollDuration)
		}
		if err != nil {
			return nil, nil, swipes, err
		}
		swipes++
		select {
		case <-ctx.Done():
			return nil, nil, swipes, ctx.Err()
		case <-time.After(settleAfterScroll):
		}
		next, err := d.Snapshot(ctx)
		if err != nil {
			return nil, nil, swipes, err
		}
		m, err := pickOne(loc, next)
		if err != nil {
			// It was there a swipe ago; what is on screen now is the answer
			// the caller gets, not a failure after a success.
			return n, tree, swipes, nil
		}
		nlo, nhi := extent(m)
		if nhi-nlo <= hi-lo {
			// No bigger than before: whole, and now clear of the edge — or
			// the list did not move. Either way this is as far as it goes.
			return m, next, swipes, nil
		}
		n, tree = m, next
	}
	return n, tree, swipes, nil
}

// clippedAt reports whether a node's bounds look cut off at its container's
// edge [clo, chi] on the scroll axis: flush against it, while a sibling of the
// same class is bigger along it. A whole row flush against the edge is the
// same size as its neighbors, and is left alone.
func clippedAt(n *uitree.Node, clo, chi int, extent func(*uitree.Node) (int, int)) bool {
	lo, hi := extent(n)
	if (lo > clo && hi < chi) || n.Parent == nil {
		return false
	}
	for _, sib := range n.Parent.Children {
		if sib == n || sib.Class != n.Class || sib.Bounds.Empty() {
			continue
		}
		if slo, shi := extent(sib); shi-slo > hi-lo {
			return true
		}
	}
	return false
}

// maxReveal bounds revealWhole: a third of the container a swipe brings in
// anything that fits in three, with room for a list that coasts.
const maxReveal = 5

// resolvedAndVisible reports the single node a locator names, but only when it
// is wholly inside the scroll container.
//
// Partly-scrolled-in elements are the reason this checks containment rather
// than mere presence: a row half off the bottom edge is in the hierarchy with
// real bounds, and its center — which is what a tap uses — can be off screen
// entirely.
//
// The error is handed back rather than swallowed. Not finding an element and
// finding two of them want completely different responses from the caller,
// and reporting the second as the first sends them looking for something that
// is on the screen in front of them.
func resolvedAndVisible(pick picker, tree *uitree.Tree, container *uitree.Node) (*uitree.Node, error) {
	n, err := pick(tree)
	if err != nil {
		return nil, err
	}
	// Judge it against the container that would move it, not whichever
	// scrollable happens to be biggest.
	if own, v := viewOf(tree, n); own != nil && !inView(v, n.Bounds) {
		return nil, errOffScreen
	}
	return n, nil
}

// picker resolves the scroll's target on a tree.
type picker func(*uitree.Tree) (*uitree.Node, error)

// refPicker resolves a locator, and for a ref follows the element map named
// rather than the position the ref was given. A row with no words of its
// own maps to a position, and a list reuses its cells: after a nudge in
// Pocket Casts' Discover carousel the position held another podcast, the
// loop chased that one, and the carousel, which snaps a page at a time,
// paged to its end (CHALLENGES 267). So while the position still holds the
// named element it is used; once it does not, the one element of that name
// is, and when there is none, or several, the target is not on this screen.
func refPicker(loc uitree.Locator, name string) picker {
	return (&follower{loc: loc, name: name}).pick
}

// follower is refPicker with a memory of where its element went. A name
// shared by many — every post in Ice Cubes has a "…" button called
// status.action.context-menu — is a position in map, and once a nudge moved
// the list the position named another post's button, or nothing, and the
// one-of-that-name rule found several: the loop swiped fifteen times and
// blamed the list's length, two runs in three on the simulator and the
// iPhone. After a nudge the loop knows how far it moved the list, so the
// element is looked for where it went: the one of its name nearest the
// place it was, moved by the nudge, when it is clearly the nearest. That
// comes before the position, which after a nudge may hold a look-alike.
// CHALLENGES 295.
type follower struct {
	loc    uitree.Locator
	name   string
	expect *uitree.Rect
}

func (f *follower) pick(t *uitree.Tree) (*uitree.Node, error) {
	if f.name != "" && f.expect != nil {
		if n := nearestNamed(t, f.name, *f.expect); n != nil {
			return n, nil
		}
	}
	n, err := pickOne(f.loc, t)
	if f.name == "" || (err == nil && uitree.Describe(n) == f.name) {
		return n, err
	}
	var found []*uitree.Node
	t.Walk(func(m *uitree.Node) bool {
		if m.Clickable && !m.Bounds.Empty() && uitree.Describe(m) == f.name {
			found = append(found, m)
		}
		return true
	})
	if len(found) == 1 {
		return found[0], nil
	}
	if len(found) > 1 && f.expect == nil {
		// Its position no longer holds it, and several share its name:
		// the screen changed since the map — a live timeline re-laid out
		// between map and tap — and no swipe can say which one it was.
		// Swiping on, the loop blamed the list's length after fifteen.
		return nil, mobiumerr.New(mobiumerr.NoSuchElement, "the element map named %q is no longer where the "+
			"map saw it, and %d elements on the screen share that name, so which one it was cannot be "+
			"told — the screen has changed since the map", f.name, len(found)).
			WithRemedy("run app_map again, and use the ref it gives").
			WithDetail(lookAlikesKey, len(found))
	}
	return nil, mobiumerr.New(mobiumerr.NoSuchElement, "no element named %q on the current screen", f.name)
}

// lookAlikesKey marks a ref that cannot be told from its look-alikes, which
// the scroll loop answers at once rather than swiping for. CHALLENGES 295.
const lookAlikesKey = "look_alikes"

// lostAmongLookAlikes reports whether err is that refusal.
func lostAmongLookAlikes(err error) bool {
	e, ok := mobiumerr.As(err)
	return ok && e.Details[lookAlikesKey] != nil
}

// moved records that a nudge moved the list by dx, dy from where the element
// was, so the next pick looks for it there.
func (f *follower) moved(from uitree.Rect, dx, dy int) {
	r := uitree.Rect{X1: from.X1 - dx, Y1: from.Y1 - dy, X2: from.X2 - dx, Y2: from.Y2 - dy}
	f.expect = &r
}

// nearestNamed is the element named name nearest want, when it is clearly
// the nearest: no other of the name within twice its distance, and itself
// within a quarter of the screen's height. Otherwise nil — a guess between
// two look-alikes is the wrong tap this exists to avoid.
func nearestNamed(t *uitree.Tree, name string, want uitree.Rect) *uitree.Node {
	cx, cy := (want.X1+want.X2)/2, (want.Y1+want.Y2)/2
	dist := func(r uitree.Rect) float64 {
		return math.Hypot(float64((r.X1+r.X2)/2-cx), float64((r.Y1+r.Y2)/2-cy))
	}
	var best, second *uitree.Node
	t.Walk(func(m *uitree.Node) bool {
		if !m.Clickable || m.Bounds.Empty() || uitree.Describe(m) != name {
			return true
		}
		switch {
		case best == nil || dist(m.Bounds) < dist(best.Bounds):
			best, second = m, best
		case second == nil || dist(m.Bounds) < dist(second.Bounds):
			second = m
		}
		return true
	})
	if best == nil {
		return nil
	}
	d := dist(best.Bounds)
	limit := 0.0
	if t.Root != nil {
		limit = float64(t.Root.Bounds.Height()) / 4
	}
	if limit > 0 && d > limit {
		return nil
	}
	if second != nil && dist(second.Bounds) < 2*d {
		return nil
	}
	return best
}

// refName is what map called a ref when it was taken, or "" for a locator
// that is not a ref.
func (h *Handlers) refName(serial, target string) string {
	table, ok := h.refs[serial]
	if !ok || !strings.HasPrefix(target, "@") {
		return ""
	}
	return table.seen[target].name
}

// offScreenTarget returns the node a locator names and the container that
// moves it, when it resolves with real bounds but not wholly inside that
// container — the case a measured swipe can finish. Otherwise both are nil.
func offScreenTarget(pick picker, tree *uitree.Tree) (*uitree.Node, *uitree.Node) {
	n, err := pick(tree)
	if err != nil || n.Bounds.Empty() {
		return nil, nil
	}
	own, v := viewOf(tree, n)
	if own == nil || inView(v, n.Bounds) {
		return nil, nil
	}
	// What the nudge measures against is the part of the list in view, not
	// the part behind a bar.
	return n, &uitree.Node{Bounds: v}
}

// nudgeInto swipes a found target into its container by the distance it is
// out, plus an eighth of the container so it lands clear of the edge, and
// reports whether it swiped. It does not when there is no such target, or
// the target is bigger than the container along the axis and can never fit;
// the caller's full swipe goes instead. The direction is from where the
// target is, not from the caller's: once found, which side it is on is known.
//
// The axis, too, is from where the target is when it is out on one axis
// only: a row in a horizontal carousel inside a vertical page, out to the
// right and in view top to bottom, comes in only by moving sideways. An
// action's own scroll asks for "down", and on Pocket Casts' Discover a tap
// on Freakonomics Radio, the second column of a carousel of rows, swiped
// the page down once and was refused as never scrolled into view. Nothing
// here says which way the carousel scrolls; where the target lies does.
// Out on both axes, or neither, the caller's axis stands — and always for
// app_scroll_to, whose caller named it. CHALLENGES 261.
func nudgeInto(ctx context.Context, gest mobiumdriver.Gesturer, c, n *uitree.Node, horiz, followTarget bool) (bool, int, int, error) {
	if c == nil || n == nil {
		return false, 0, 0, nil
	}
	outX := n.Bounds.X1 < c.Bounds.X1 || n.Bounds.X2 > c.Bounds.X2
	outY := n.Bounds.Y1 < c.Bounds.Y1 || n.Bounds.Y2 > c.Bounds.Y2
	if followTarget && outX != outY {
		horiz = outX
	}
	lo, hi, clo, chi := n.Bounds.Y1, n.Bounds.Y2, c.Bounds.Y1, c.Bounds.Y2
	if horiz {
		lo, hi, clo, chi = n.Bounds.X1, n.Bounds.X2, c.Bounds.X1, c.Bounds.X2
	}
	span := chi - clo
	if hi-lo > span {
		return false, 0, 0, nil
	}
	// d > 0 moves the content toward lower coordinates: up, or left.
	d := 0
	switch {
	case hi > chi:
		d = hi - chi + span/8
	case lo < clo:
		d = -(clo - lo + span/8)
	default:
		return false, 0, 0, nil
	}
	if limit := span / 2; d > limit {
		d = limit
	} else if d < -limit {
		d = -limit
	}
	mid := (clo + chi) / 2
	from, to := mid+d/2, mid-d/2
	if horiz {
		y := (c.Bounds.Y1 + c.Bounds.Y2) / 2
		return true, from - to, 0, gest.Swipe(ctx, from, y, to, y, scrollDuration)
	}
	x := (c.Bounds.X1 + c.Bounds.X2) / 2
	return true, 0, from - to, gest.Swipe(ctx, x, from, x, to, scrollDuration)
}

// errOffScreen means the locator resolved but the element is outside the part
// of the list you can see — which is the one case scrolling can fix.
var errOffScreen = mobiumerr.New(mobiumerr.ElementNotReachable, "the element is not in view")

// inView reports whether a target's bounds are as far into a viewport as
// scrolling can bring them: on each axis, inside it, or past both its edges.
// A target bigger than its container on an axis can never fit, and no swipe
// along that axis shows more of it: Pocket Casts' search filter chips are 36
// points high in a 34-point horizontal scroll view, a point over each edge,
// and a tap on the Podcasts chip swiped the results down fifteen times and
// gave up, the chip in plain sight the whole time. CHALLENGES 241.
func inView(view, b uitree.Rect) bool {
	within := func(lo, hi, vlo, vhi int) bool {
		return (lo >= vlo && hi <= vhi) || (lo < vlo && hi > vhi)
	}
	return within(b.X1, b.X2, view.X1, view.X2) && within(b.Y1, b.Y2, view.Y1, view.Y2)
}

// viewOf is n's scroll container and the part of it in view: its bounds
// less any bar drawn over its top or bottom edge (uitree.Viewport). A row
// behind iOS 26's toolbar is inside the list and not in view. Nil and empty
// when nothing around n scrolls.
func viewOf(tree *uitree.Tree, n *uitree.Node) (*uitree.Node, uitree.Rect) {
	c := scrollContainerOf(n)
	if c == nil {
		return nil, uitree.Rect{}
	}
	return c, tree.Viewport(c)
}

// scrollContainerOf returns the scrollable that would actually move a node:
// its nearest scrollable ancestor, or nil if it has none.
//
// This is not the same question as "what is the biggest scrollable on screen",
// and conflating the two was a real bug. On a Pixel 8 Pro, Calculator's only
// scrollable is the history strip across the top, [0,0][1008,285]; the "7"
// button sits at [9,1218][249,1452] and is not inside it or under it. Judging
// the button against that container concluded it was out of view and tried to
// scroll a list it has nothing to do with. The same shape breaks any screen
// with a fixed button bar below a scrolling list, which is most of them.
func scrollContainerOf(n *uitree.Node) *uitree.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Scrollable && !p.Bounds.Empty() {
			return p
		}
	}
	return nil
}

// scrollContainer picks the scrollable node to swipe inside: the largest one,
// which on a real screen is the list rather than a small horizontal carousel
// inside it. Returns nil when nothing on the screen scrolls.
//
// Only for the case where the target cannot be found at all, so there is no
// node to ask about ancestry. Once a node is in hand, use scrollContainerOf.
func scrollContainer(tree *uitree.Tree) *uitree.Node {
	var best *uitree.Node
	tree.Walk(func(n *uitree.Node) bool {
		if !n.Scrollable || n.Bounds.Empty() {
			return true
		}
		if best == nil || area(n.Bounds) > area(best.Bounds) {
			best = n
		}
		return true
	})
	return best
}

// swipeWithin drags inside the container rather than across the whole screen.
//
// Swiping the screen would work on a full-height list and would hit whatever
// is above or below a short one. The gesture spans the middle half of the
// container, away from the edges where the system takes over the gesture.
// isScrollDirection reports whether a direction is one this loop can swipe.
func isScrollDirection(dir string) bool {
	switch dir {
	case "down", "up", "left", "right":
		return true
	}
	return false
}

// horizontal reports whether a direction moves along the x axis.
func horizontal(dir string) bool { return dir == "left" || dir == "right" }

// swipeWithin drags inside a container, along the axis the caller named.
//
// The quarter insets matter more horizontally than vertically: a drag that
// starts at the very edge of the screen is the system's back gesture on both
// platforms, so it would navigate rather than scroll and the loop would report
// that instead of moving the pager. Starting a quarter of the way in keeps the
// gesture inside the app.
func swipeWithin(ctx context.Context, gest mobiumdriver.Gesturer, r uitree.Rect, dir string) error {
	if horizontal(dir) {
		y := (r.Y1 + r.Y2) / 2
		near := r.X1 + r.Width()/4
		far := r.X2 - r.Width()/4
		if dir == "right" {
			// Looking further right means the content moves left, so the
			// finger travels from right to left.
			return gest.Swipe(ctx, far, y, near, y, scrollDuration)
		}
		return gest.Swipe(ctx, near, y, far, y, scrollDuration)
	}
	x := (r.X1 + r.X2) / 2
	near := r.Y1 + r.Height()/4
	far := r.Y2 - r.Height()/4
	if dir == "down" {
		// Looking further down the list means the content moves up, so the
		// finger travels from low to high.
		return gest.Swipe(ctx, x, far, x, near, scrollDuration)
	}
	return gest.Swipe(ctx, x, near, x, far, scrollDuration)
}

// reading is what a container is showing, in the two parts that have to be
// judged separately.
type reading struct {
	// geometry is where everything is. Scrolling moves things; a screen doing
	// nothing does not.
	geometry string
	// texts is what everything says, in document order.
	texts []string
}

// read captures a container for later comparison.
//
// A container with no children is read through its parent. Flutter on iOS
// reports its scroll view as an empty element with a frame, and the rows it
// scrolls as siblings after it: reading the scroll view alone found nothing
// that moved, and scroll-to gave up after one swipe at "the end of the list"
// with thirty rows to go (CHALLENGES 206).
func read(container *uitree.Node) reading {
	if len(container.Children) == 0 && container.Parent != nil {
		container = container.Parent
	}
	var r reading
	var b strings.Builder
	var visit func(*uitree.Node)
	visit = func(n *uitree.Node) {
		for _, c := range n.Children {
			fmt.Fprintf(&b, "%s\n", c.Bounds)
			r.texts = append(r.texts, c.Text+"\x00"+c.Label)
			visit(c)
		}
	}
	visit(container)
	r.geometry = b.String()
	return r
}

// moved reports whether the list actually went anywhere between two readings.
//
// Text alone is not evidence. The About screen on a Pixel 7 shows an uptime
// counter that ticks every second, so comparing everything the container said
// found a difference on every pass and the loop swiped its full fifteen times
// at a list that had been pinned to the bottom from the start. Geometry alone
// is not evidence either: a recycled list can present its rows at the same
// coordinates after a scroll, changing only what they say.
//
// So: the list moved if anything is in a different place, or if more than one
// thing is saying something new. One changed string is a clock. Several are a
// screenful of different rows.
func moved(before, after reading) bool {
	if before.geometry != after.geometry {
		return true
	}
	if len(before.texts) != len(after.texts) {
		return true
	}
	changed := 0
	for i := range before.texts {
		if before.texts[i] != after.texts[i] {
			changed++
			if changed > 1 {
				return true
			}
		}
	}
	return false
}

// fingerprint renders a subtree as one string, for the coarser question of
// whether a screen changed at all.
func fingerprint(root *uitree.Node) string {
	r := read(root)
	return r.geometry + strings.Join(r.texts, "\n")
}

// ScrollView is the result of app_scroll_to.
type ScrollView struct {
	Target    string `json:"target"`
	Direction string `json:"direction"`
	// Scrolls is how many swipes it took; 0 means it was already on screen.
	Scrolls int          `json:"scrolls"`
	Element *ElementView `json:"element,omitempty"`
}

// whyNot phrases a scroll failure by what actually happened, since "no element
// matches" and "it never came into view" send a caller to different places.
func whyNot(loc uitree.Locator, err error) string {
	if errors.Is(err, errOffScreen) {
		return fmt.Sprintf("%s is on the screen but never scrolled fully into view", loc)
	}
	return fmt.Sprintf("no element matches %s", loc)
}
