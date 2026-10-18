package agent

import (
	"os"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// With gesture navigation UiAutomator2 named the Android 15 emulator's
// display 1080x2201, the display without the gesture bar's inset, under
// Morpho Market's edge-to-edge window of 1080x2400: the Cart tab, centered
// at y=2232 and plainly on screen, was refused as off it. The screen takes
// in the app's window (CHALLENGES 336).
func TestABottomBarUnderTheGestureInsetIsOnScreen(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/morpho-catalog-gesture-nav-uia2.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseAndroid(raw)
	if err != nil {
		t.Fatal(err)
	}
	var tab *uitree.Node
	tree.Walk(func(n *uitree.Node) bool {
		if n.TestID == "tab_cart" {
			tab = n
		}
		return tab == nil
	})
	if tab == nil {
		t.Fatal("the capture has no tab_cart")
	}
	if on, _ := centerOnScreen(tree, tab); !on {
		t.Errorf("tab_cart %v is off a screen of %v", tab.Bounds, displayOf(tree))
	}
	if got := displayOf(tree); got != (uitree.Rect{X1: 0, Y1: 0, X2: 1080, Y2: 2400}) {
		t.Errorf("screen = %v, want the window's 1080x2400", got)
	}
}
