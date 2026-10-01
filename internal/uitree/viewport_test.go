package uitree

import "testing"

// NetNewsWire's feed list on iOS 26 runs the full height of the screen,
// under its toolbar, which is drawn over it from y 733: a row behind the
// toolbar is inside the list and not in view, and a tap on it touched the
// toolbar (CHALLENGES 197). The rows above the bar are in view.
func TestARowBehindTheToolbarIsNotInView(t *testing.T) {
	tree := loadIOS(t, "ios26-netnewswire-feeds-toolbar.xml")
	var list, behind, above *Node
	tree.Walk(func(n *Node) bool {
		switch {
		case n.Class == "XCUIElementTypeCollectionView" && list == nil:
			list = n
		case n.Class == "XCUIElementTypeCell" && n.Label == "NetNewsWire Blog 7 unread":
			behind = n
		case n.Class == "XCUIElementTypeCell" && n.Label == "Craig Hockenberry 2 unread":
			above = n
		}
		return true
	})
	if list == nil || behind == nil || above == nil {
		t.Fatal("the fixture no longer has the list and both rows")
	}
	v := tree.Viewport(list)
	if v.Y2 >= list.Bounds.Y2 || v.Y2 > behind.Bounds.Y1+behind.Bounds.Height()/2 {
		t.Errorf("the viewport %s does not stop at the toolbar, above %s", v, behind.Bounds)
	}
	if !encloses(v, above.Bounds) {
		t.Errorf("a row above the toolbar, %s, is out of the viewport %s", above.Bounds, v)
	}
}

// A screen without a bar over its list has the list's bounds as its
// viewport: MobiumApp's home screen, and Android's Settings.
func TestAListWithNoBarIsAllInView(t *testing.T) {
	for _, tree := range []*Tree{loadIOS(t, "ios26-mobiumapp-home.xml"), loadFixture(t, "settings-display-uia2.xml")} {
		tree.Walk(func(n *Node) bool {
			if n.Scrollable && !n.Bounds.Empty() && tree.Viewport(n) != n.Bounds {
				t.Errorf("%s %s has viewport %s", n.ShortClass(), n.Bounds, tree.Viewport(n))
			}
			return true
		})
	}
}
