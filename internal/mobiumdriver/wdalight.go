package mobiumdriver

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// A read of the screen without visible.
//
// WebDriverAgent works out visible for every element of a read, and on an
// iPhone 15 Plus that was 1.5s of the 1.81s Settings' hierarchy took; without
// it, 0.29s. Leaving out the attributes Mobium does not parse changed
// nothing. Without visible every element is taken as shown, which can only
// add to what a decision sees — a hidden duplicate makes a locator match
// twice, a hidden view is drawn over a target — and never take from it. So
// a decision that comes out clean on a light read is the one a full read
// would make, once the one element acted on is asked whether it is visible;
// anything else is read again in full (internal/agent, resolveNodeOnce).
// Measured on 20 screens of a simulator: 17 made every decision the same
// both ways, and the other three only added matches and covers.

// LightSnapshot reads without visible. Not while a phone's active-app hint
// is on: the read that shows the expected app is what takes it off, and a
// light read that took it off early let the next read hang (CHALLENGES 71).
// Not over a notification banner either, which only the full read sees
// through (CHALLENGES 155).
//
// Nor on a screen too large to read in full (hugeRead): its full read is
// already this one, and the one element's visibility, asked by finding it
// among thousands, did not come back — a tap on Radiolab's page asked again
// on every pass of the cover wait and ran 342s. CHALLENGES 258.
func (w *WDA) LightSnapshot(ctx context.Context) (*uitree.Tree, bool, error) {
	w.hintMu.Lock()
	hinted := w.expecting != ""
	w.hintMu.Unlock()
	if hinted || w.wasHuge() {
		return nil, false, nil
	}
	w.forgetFound()
	xml, err := w.w3c.sourceWithout(ctx, "visible")
	if err != nil {
		return nil, false, err
	}
	tree, err := uitree.ParseIOS([]byte(xml))
	if err != nil {
		return nil, false, err
	}
	if count := tree.Count(); count >= uitree.HugeScreen {
		w.setCount(count)
		return nil, false, nil
	}
	if app, banner := bannerOver(tree); app != "" || banner != nil {
		return nil, false, nil
	}
	tree.Scale(w.scale)
	return tree, true, nil
}

// ElementVisible asks WebDriverAgent whether one element is visible, finding
// it by its test id or its label when that is unique on the light read, so
// the element found is the node meant.
//
// Asked in the find itself — the element named, and visible — rather than
// found and then asked: one request where there were two, 171ms and 78ms of
// a tap on the iPhone 15 Plus. Found, it is visible, and remembered under
// its plain search so the rectangle read that follows finds it at no cost;
// not found, it is not shown to be visible, which sends the action to a full
// read as a hidden one always did. CHALLENGES 272.
func (w *WDA) ElementVisible(ctx context.Context, n *uitree.Node, t *uitree.Tree) (bool, bool, error) {
	strategy, selector, ok := uniqueSelector(n, t)
	if !ok {
		return false, false, nil
	}
	id, err := w.w3c.findElement(ctx, strategy, selector+" AND visible == 1")
	if err != nil {
		if mobiumerr.CodeOf(err) == mobiumerr.NoSuchElement {
			return false, true, nil
		}
		return false, true, err
	}
	w.foundMu.Lock()
	w.found = map[string]string{strategy + "\x00" + selector: id}
	w.foundMu.Unlock()
	return true, true, nil
}

// findUnique finds the element a node names by its test id, or its label,
// when that is unique in t, so what WebDriverAgent finds can only be that
// node. ok is false when neither is. The one found last is remembered until
// the next read of the screen, so a visibility check and a rectangle read of
// the same element find it once.
func (w *WDA) findUnique(ctx context.Context, n *uitree.Node, t *uitree.Tree) (string, bool, error) {
	strategy, selector, ok := uniqueSelector(n, t)
	if !ok {
		return "", false, nil
	}
	key := strategy + "\x00" + selector
	w.foundMu.Lock()
	id, hit := w.found[key]
	w.foundMu.Unlock()
	if hit {
		return id, true, nil
	}
	id, err := w.w3c.findElement(ctx, strategy, selector)
	if err == nil {
		w.foundMu.Lock()
		w.found = map[string]string{key: id}
		w.foundMu.Unlock()
	}
	return id, true, err
}

// uniqueSelector is the search that can find only n: its test id with its
// type, or its label, when no other node in t has it. ok is false otherwise.
func uniqueSelector(n *uitree.Node, t *uitree.Tree) (strategy, selector string, ok bool) {
	switch {
	case n.TestID != "" && countNodes(t, func(m *uitree.Node) bool { return m.TestID == n.TestID }) == 1:
		// By name and type together: "accessibility id" also matches labels,
		// so a name no other node has can still find another element
		// (CHALLENGES 240).
		p, ok := namePredicate(n)
		if !ok {
			return "", "", false
		}
		return "predicate string", p, true
	case n.Label != "" && !strings.ContainsAny(n.Label, `"\\`) &&
		countNodes(t, func(m *uitree.Node) bool { return m.Label == n.Label }) == 1:
		return "predicate string", `label == "` + n.Label + `"`, true
	}
	return "", "", false
}

// forgetFound drops the element remembered by findUnique: a new read of the
// screen may hold a different one under the same name.
func (w *WDA) forgetFound() {
	w.foundMu.Lock()
	w.found = nil
	w.foundMu.Unlock()
}

func countNodes(t *uitree.Tree, match func(*uitree.Node) bool) int {
	c := 0
	t.Walk(func(n *uitree.Node) bool {
		if match(n) {
			c++
		}
		return true
	})
	return c
}

// A screen too large to read in full.
//
// A long table is reported whole, every row whether on screen or not, and
// WebDriverAgent works visible out for each: Pocket Casts' Radiolab page,
// 673 episodes and 2,847 elements, took 83.8s on an iPhone 15 Plus, past
// the 60s a read is given, so map, text and every action failed on it.
// Without visible it took 6.3s. So once a session's screen is that large,
// it is read without visible, and what is shown is worked out from where
// it is (uitree.InferVisibility) — marked, so map can say so. A screen
// that turns out small again is read in full. The first read of a large
// screen does not know it is one: it times out, and is read this way then.
// CHALLENGES 258.

// hugeRead reads without visible, and answers only for a screen of
// uitree.HugeScreen elements or more, with visibility inferred. Not while a
// phone's active-app hint is on, nor over a notification banner, as for
// LightSnapshot.
func (w *WDA) hugeRead(ctx context.Context) (*uitree.Tree, bool) {
	w.hintMu.Lock()
	hinted := w.expecting != ""
	w.hintMu.Unlock()
	if hinted {
		return nil, false
	}
	xml, err := w.w3c.sourceWithout(ctx, "visible")
	if err != nil {
		return nil, false
	}
	tree, err := uitree.ParseIOS([]byte(xml))
	if err != nil {
		return nil, false
	}
	count := tree.Count()
	w.setCount(count)
	if count < uitree.HugeScreen {
		return nil, false
	}
	if app, banner := bannerOver(tree); app != "" || banner != nil {
		return nil, false
	}
	tree.InferVisibility()
	tree.Scale(w.scale)
	return tree, true
}

func (w *WDA) wasHuge() bool {
	w.countMu.Lock()
	defer w.countMu.Unlock()
	return w.lastCount >= uitree.HugeScreen
}

func (w *WDA) setCount(n int) {
	w.countMu.Lock()
	w.lastCount = n
	w.countMu.Unlock()
}

// timedOut says whether a request gave up waiting for the server.
func timedOut(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
