package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

var visibleAttr = regexp.MustCompile(` visible="(true|false)"`)

// The light read rests on one property: without visible every element is
// taken as shown, which can only add matches and covers, so when a light read
// decides an action cleanly — one match, nothing drawn over it — a full read
// decides it the same way, once the one element is confirmed visible. Held
// here against every captured iOS hierarchy, for every locator map gives and
// every label and test id on the screen.
func TestALightReadDecidesCleanlyOnlyAsAFullReadWould(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "uitree", "testdata", "*.xml"))
	checked := 0
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		if !strings.Contains(string(raw), "XCUIElementType") || !strings.Contains(string(raw), ` visible="`) {
			continue
		}
		full, err := uitree.ParseIOS(raw)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		light, _ := uitree.ParseIOS([]byte(visibleAttr.ReplaceAllString(string(raw), "")))
		if hasDialogOrKeyboard(light) {
			continue
		}
		var locs []string
		seen := map[string]bool{}
		add := func(s string) {
			if s != "" && !seen[s] {
				seen[s] = true
				locs = append(locs, s)
			}
		}
		for _, e := range full.Map() {
			add(e.Locator.String())
		}
		light.Walk(func(n *uitree.Node) bool {
			if n.TestID != "" {
				add("testid=" + n.TestID)
			}
			if n.Label != "" {
				add("label=" + n.Label)
			}
			return true
		})
		for _, l := range locs {
			loc, err := uitree.ParseLocator(l)
			if err != nil {
				continue
			}
			ln, lerr := pickOne(loc, light)
			if lerr != nil {
				continue
			}
			if aim := light.AimAt(ln); aim.Moved || aim.Blocker != nil || aim.Over != nil {
				continue
			}
			// Light decided cleanly; the element is asked alone whether it
			// is visible, which is what the full read says of it.
			fln := findPath(full, ln.Path)
			if fln == nil || !fln.Displayed {
				continue
			}
			checked++
			fn, ferr := pickOne(loc, full)
			if ferr != nil || fn.Path != ln.Path {
				t.Errorf("%s: %s resolved to %s on a light read and %v %v on a full one",
					filepath.Base(f), l, ln.Path, fn, ferr)
				continue
			}
			if la, fa := light.AimAt(ln), full.AimAt(fn); la.X != fa.X || la.Y != fa.Y ||
				fa.Moved || fa.Blocker != nil || fa.Over != nil {
				t.Errorf("%s: %s aimed at %d,%d on a light read, and on a full one %+v",
					filepath.Base(f), l, la.X, la.Y, fa)
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d clean light decisions were checked — the fixtures no longer exercise this", checked)
	}
}

func findPath(t *uitree.Tree, path string) *uitree.Node {
	var found *uitree.Node
	t.Walk(func(n *uitree.Node) bool {
		if n.Path == path {
			found = n
		}
		return found == nil
	})
	return found
}

// lightOnly reads a screen only lightly, and says what the device would of
// the one element: whether it is visible.
type lightOnly struct {
	fakeDriver
	tree    string
	visible bool
	full    int
}

func (d *lightOnly) LightSnapshot(ctx context.Context) (*uitree.Tree, bool, error) {
	t, err := uitree.ParseIOS([]byte(d.tree))
	return t, true, err
}

func (d *lightOnly) ElementVisible(ctx context.Context, n *uitree.Node, t *uitree.Tree) (bool, bool, error) {
	return d.visible, true, nil
}

func (d *lightOnly) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	d.full++
	return uitree.ParseIOS([]byte(d.tree))
}

// A light read takes every element as shown, so it can find, alone and
// uncovered, a button the device calls hidden — one left in the tree by a
// screen navigated away from. Asked alone, the element says so, and the
// action goes back to a full read rather than acting on the light one.
func TestALightReadDoesNotActOnAnElementTheDeviceCallsHidden(t *testing.T) {
	screen := `<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="App" bundleId="com.example" ` +
		`x="0" y="0" width="402" height="874" visible="true" enabled="true" accessible="false">` +
		`<XCUIElementTypeButton type="XCUIElementTypeButton" name="checkout" label="Checkout" ` +
		`x="16" y="700" width="370" height="50" enabled="true" accessible="true"/>` +
		`</XCUIElementTypeApplication>`
	for _, visible := range []bool{true, false} {
		d := &lightOnly{tree: screen, visible: visible}
		h := NewHandlers()
		h.lightAbove = 0
		s := &session{dev: fakeDevice(), driver: d, backend: BackendWDA}
		loc, _ := uitree.ParseLocator("testid=checkout")
		_, _, _, used := h.lightResolve(context.Background(), s, loc, "testid=checkout")
		if used != visible {
			t.Errorf("visible=%v: the light read was used: %v", visible, used)
		}
	}
}
