package uitree

import (
	"encoding/json"
	"os"
	"testing"
)

// The wire form exists so a driver outside this process can hand back a
// hierarchy. Every test here defends one property: what comes back through
// JSON must be indistinguishable from what the XML parsers build, because a
// @ref taken against one has to resolve against the other.

func parseFixture(t *testing.T, name string) *Tree {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var tree *Tree
	if name == "ios-springboard.xml" {
		tree, err = ParseIOS(data)
	} else {
		tree, err = ParseAndroid(data)
	}
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return tree
}

var fixtures = []string{"launcher.xml", "launcher-uia2.xml", "login.xml", "ios-springboard.xml"}

// TestLinkAgreesWithTheParsers is the load-bearing one. Depth, Index, Path and
// Parent are derived inline by each XML parser; Link is that same rule written
// once more for trees that never saw XML. If the two ever disagree, a path
// captured through a built-in backend addresses a different node through an
// external one, and nothing else in the codebase would notice.
func TestLinkAgreesWithTheParsers(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			original := parseFixture(t, name)

			// Wipe every derived field, then have Link put them back.
			relinked := parseFixture(t, name)
			var clear func(*Node)
			clear = func(n *Node) {
				n.Depth, n.Index, n.Path, n.Parent = -1, -1, "wrong", nil
				for _, c := range n.Children {
					clear(c)
				}
			}
			clear(relinked.Root)
			Link(relinked)

			compare(t, original.Root, relinked.Root, "root")
		})
	}
}

// TestRoundTripThroughJSON drives the path an external driver actually takes:
// tree to wire, wire to JSON bytes, bytes back to a tree.
func TestRoundTripThroughJSON(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			original := parseFixture(t, name)

			data, err := json.Marshal(ToWire(original))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			rebuilt, err := UnmarshalWire(data)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			// The synthetic root is Mobium's own wrapper and the two parsers
			// already build it differently from each other — Android leaves it
			// with zero bounds, iOS copies its child's. Everything below it is
			// the platform's own data and must survive exactly.
			if len(rebuilt.Root.Children) != 1 {
				t.Fatalf("rebuilt root has %d children, want 1", len(rebuilt.Root.Children))
			}
			compare(t, original.Root.Children[0], rebuilt.Root.Children[0], "0")
		})
	}
}

// TestPathsResolveAfterRoundTrip checks the property that matters to a user
// rather than to the struct: a ref handed out before the round trip still
// finds the same element after it.
func TestPathsResolveAfterRoundTrip(t *testing.T) {
	original := parseFixture(t, "login.xml")
	data, _ := json.Marshal(ToWire(original))
	rebuilt, err := UnmarshalWire(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	checked := 0
	var walk func(*Node)
	walk = func(n *Node) {
		if n.Path != "" {
			found := findByPath(rebuilt.Root, n.Path)
			if found == nil {
				t.Errorf("path %q resolved before the round trip and not after", n.Path)
			} else if found.Text != n.Text || found.TestID != n.TestID || found.Bounds != n.Bounds {
				t.Errorf("path %q now names a different element: %+v vs %+v", n.Path, found, n)
			}
			checked++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(original.Root)
	if checked == 0 {
		t.Fatal("no paths were checked — the fixture or the walk is wrong")
	}
}

func findByPath(root *Node, path string) *Node {
	var found *Node
	var walk func(*Node)
	walk = func(n *Node) {
		if n.Path == path {
			found = n
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}

// TestDisplayedDefaultsToVisible pins the asymmetry in WireNode.Displayed. A
// driver that never heard of the field must not have its whole screen treated
// as hidden, but one that deliberately sends false must be believed — which is
// why the field is a pointer and not a bool.
func TestDisplayedDefaultsToVisible(t *testing.T) {
	absent, err := UnmarshalWire([]byte(`{"class":"X","bounds":[0,0,10,10]}`))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !absent.Root.Children[0].Displayed {
		t.Error("a node with no displayed field was treated as hidden")
	}

	explicit, err := UnmarshalWire([]byte(`{"class":"X","bounds":[0,0,10,10],"displayed":false}`))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if explicit.Root.Children[0].Displayed {
		t.Error("an explicit displayed:false was ignored")
	}
}

func TestUnmarshalWireRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "null-ish", "[1,2,3]", `{"bounds":"0,0,1,1"}`} {
		if _, err := UnmarshalWire([]byte(in)); err == nil {
			t.Errorf("accepted %q", in)
		}
	}
}

// compare walks two trees in lockstep, checking the platform's data and every
// derived field, including that Parent points back at the node that was walked
// through rather than merely at some node with the right contents.
func compare(t *testing.T, want, got *Node, where string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: missing", where)
		return
	}
	if got.Text != want.Text || got.Label != want.Label || got.TestID != want.TestID ||
		got.Class != want.Class || got.Package != want.Package || got.Bounds != want.Bounds {
		t.Errorf("%s: content differs\n want %+v\n got  %+v", where, want, got)
	}
	if got.Clickable != want.Clickable || got.LongClickable != want.LongClickable ||
		got.Checkable != want.Checkable || got.Checked != want.Checked ||
		got.Scrollable != want.Scrollable || got.Focusable != want.Focusable ||
		got.Enabled != want.Enabled || got.Selected != want.Selected ||
		got.Password != want.Password || got.Displayed != want.Displayed {
		t.Errorf("%s: flags differ\n want %+v\n got  %+v", where, want, got)
	}
	if got.Depth != want.Depth {
		t.Errorf("%s: depth = %d, want %d", where, got.Depth, want.Depth)
	}
	if got.Index != want.Index {
		t.Errorf("%s: index = %d, want %d", where, got.Index, want.Index)
	}
	if got.Path != want.Path {
		t.Errorf("%s: path = %q, want %q", where, got.Path, want.Path)
	}
	if len(got.Children) != len(want.Children) {
		t.Fatalf("%s: %d children, want %d", where, len(got.Children), len(want.Children))
	}
	for i := range want.Children {
		if got.Children[i].Parent != got {
			t.Errorf("%s/%d: parent pointer does not lead back up", where, i)
		}
		compare(t, want.Children[i], got.Children[i], where+"/"+want.Children[i].Path)
	}
}
