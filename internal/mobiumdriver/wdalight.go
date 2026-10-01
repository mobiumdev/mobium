package mobiumdriver

import (
	"context"
	"net/http"
	"strings"

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
func (w *WDA) LightSnapshot(ctx context.Context) (*uitree.Tree, bool, error) {
	w.hintMu.Lock()
	hinted := w.expecting != ""
	w.hintMu.Unlock()
	if hinted {
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
	if app, banner := bannerOver(tree); app != "" || banner != nil {
		return nil, false, nil
	}
	tree.Scale(w.scale)
	return tree, true, nil
}

// ElementVisible asks WebDriverAgent whether one element is visible, finding
// it by its test id or its label when that is unique on the light read, so
// the element found is the node meant.
func (w *WDA) ElementVisible(ctx context.Context, n *uitree.Node, t *uitree.Tree) (bool, bool, error) {
	id, ok, err := w.findUnique(ctx, n, t)
	if !ok || err != nil {
		return false, ok, err
	}
	var resp struct {
		Value interface{} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, w.w3c.sessionPath("/element/"+id+"/attribute/visible"), nil, &resp); err != nil {
		return false, true, err
	}
	switch v := resp.Value.(type) {
	case bool:
		return v, true, nil
	case string:
		return v == "true" || v == "1", true, nil
	}
	return false, true, nil
}

// findUnique finds the element a node names by its test id, or its label,
// when that is unique in t, so what WebDriverAgent finds can only be that
// node. ok is false when neither is. The one found last is remembered until
// the next read of the screen, so a visibility check and a rectangle read of
// the same element find it once.
func (w *WDA) findUnique(ctx context.Context, n *uitree.Node, t *uitree.Tree) (string, bool, error) {
	var strategy, selector string
	switch {
	case n.TestID != "" && countNodes(t, func(m *uitree.Node) bool { return m.TestID == n.TestID }) == 1:
		strategy, selector = "accessibility id", n.TestID
	case n.Label != "" && !strings.ContainsAny(n.Label, `"\\`) &&
		countNodes(t, func(m *uitree.Node) bool { return m.Label == n.Label }) == 1:
		strategy, selector = "predicate string", `label == "`+n.Label+`"`
	default:
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
