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

// A list taller than the panel that shows it is in view only within the
// panel. Ice Cubes' long-press menu, once it fit on the screen: a list 875
// points tall in a panel 334 tall, and Report Post 170 points below the
// panel's edge — which iOS reports hidden — was taken as in view, so
// scroll-to called it there and map did not list it. Translate, the last
// item the panel shows, is in view. CHALLENGES 262.
func TestAListIsInViewOnlyInsideThePanelThatShowsIt(t *testing.T) {
	tree := loadIOS(t, "ios26-icecubes-short-menu.xml")
	item := func(label string) *Node {
		var found *Node
		tree.Walk(func(n *Node) bool {
			if found == nil && n.Class == "XCUIElementTypeButton" && n.Label == label {
				found = n
			}
			return found == nil
		})
		if found == nil {
			t.Fatalf("no %q in the menu", label)
		}
		return found
	}
	for _, c := range []struct {
		label string
		in    bool
	}{{"Report Post", false}, {"Translate", true}} {
		n := item(c.label)
		var list *Node
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Scrollable {
				list = p
				break
			}
		}
		if list == nil {
			t.Fatalf("%s is in no list", c.label)
		}
		v := tree.Viewport(list)
		in := n.Bounds.Y1 >= v.Y1 && n.Bounds.Y2 <= v.Y2
		if in != c.in {
			t.Errorf("%s at %v against the viewport %v: in view %v, want %v", c.label, n.Bounds, v, in, c.in)
		}
	}
}

// iOS 27's Settings floats its search field over the bottom of its list,
// inset from each side and from the bottom edge, so no rule for a bar
// across the edge saw it. The General row under it is reported hidden, and
// was taken as in view: a tap was refused as behind another screen, with a
// remedy — close it — that nothing could follow (CHALLENGES 338). Captured
// on an iPhone 15 Plus on iOS 27.0.1, every name, label and value but these
// replaced. The rows above the field are still in view.
func TestARowUnderAFloatingSearchFieldIsNotInView(t *testing.T) {
	tree := loadIOS(t, "ios27-settings-root-search.xml")
	var list, general, field *Node
	tree.Walk(func(n *Node) bool {
		switch {
		case n.TestID == "com.apple.settings.sidebar.collectionView":
			list = n
		case n.TestID == "com.apple.settings.general":
			general = n
		case n.Class == "XCUIElementTypeSearchField":
			field = n
		}
		return true
	})
	if list == nil || general == nil || field == nil {
		t.Fatal("the fixture no longer has the list, the General row and the search field")
	}
	if general.Displayed || field.Bounds.Y2 >= list.Bounds.Y2 || field.Bounds.X1 <= list.Bounds.X1 {
		t.Fatalf("the fixture lost its shape: General displayed=%v, field %s, list %s",
			general.Displayed, field.Bounds, list.Bounds)
	}
	v := tree.Viewport(list)
	if x, y := general.Bounds.Center(); contains(v, x, y) {
		t.Errorf("the viewport %s holds General's center under the search field %s", v, field.Bounds)
	}
	if v.Y2 > field.Bounds.Y1 {
		t.Errorf("the viewport %s runs below the top of the search field %s", v, field.Bounds)
	}
	if tree.CoveredByScreen(general) {
		t.Error("General, under the search field, reads as behind another screen")
	}
	above := 0
	tree.Walk(func(n *Node) bool {
		if n.Class == "XCUIElementTypeCell" && n.Displayed && n.Within(list) {
			above++
			if !encloses(v, n.Bounds) {
				t.Errorf("a row reported visible, %s, is out of the viewport %s", n.Bounds, v)
			}
		}
		return true
	})
	if above == 0 {
		t.Fatal("no visible rows above the field: the in-view half cannot fail")
	}
}
