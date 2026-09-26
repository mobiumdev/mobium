package uitree

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// The wire form of a UI tree, for drivers that live outside this process.
//
// An external driver's whole job is to hand back what is on screen. It should
// not have to know how Mobium indexes, paths or parents the nodes it sends —
// those are derived, and Link recomputes them here. A driver author supplies
// what the platform told them and nothing else.

// WireNode is one node as a driver sends it.
//
// Deliberately not `Node` with tags: Node carries Parent, Depth, Index and
// Path, which are Mobium's bookkeeping rather than the platform's facts, and
// putting them on the wire would invite a driver to get them subtly wrong.
type WireNode struct {
	Text    string `json:"text,omitempty"`
	Label   string `json:"label,omitempty"`
	TestID  string `json:"testid,omitempty"`
	Class   string `json:"class,omitempty"`
	Package string `json:"package,omitempty"`

	// Bounds is [x1, y1, x2, y2] in device pixels, on every platform.
	Bounds [4]int `json:"bounds"`

	Clickable     bool   `json:"clickable,omitempty"`
	LongClickable bool   `json:"longClickable,omitempty"`
	Checkable     bool   `json:"checkable,omitempty"`
	DeclaredRole  string `json:"declaredRole,omitempty"`
	Checked       bool   `json:"checked,omitempty"`
	Scrollable    bool   `json:"scrollable,omitempty"`
	Focusable     bool   `json:"focusable,omitempty"`
	// Enabled defaults to true when absent, as Displayed does, and for the
	// same reason: actions now wait for it (a disabled control ignores a
	// tap), and a driver that never sent it must not have every tap refused.
	Enabled  *bool `json:"enabled,omitempty"`
	Selected bool  `json:"selected,omitempty"`
	Password bool  `json:"password,omitempty"`

	// Displayed defaults to true when absent, because a driver that does not
	// track visibility should not accidentally hide the whole screen. Use a
	// pointer so "not sent" and "sent as false" are different.
	//
	// Sending false is acted on: Actionable leaves such a node out of `map`,
	// while the hierarchy keeps it so a locator still resolves and the error
	// can say why it cannot be touched. See CHALLENGES 39 — this field went
	// unread for months on the built-in backends, where Android reports it as
	// a constant and iOS folds it into Clickable.
	Displayed *bool `json:"displayed,omitempty"`

	Children []WireNode `json:"children,omitempty"`
}

// ToWire converts a tree to its wire form, dropping the synthetic root that
// FromWire puts back. Mostly for tests and for a driver written in Go that
// already has a Tree.
func ToWire(t *Tree) WireNode {
	root := t.Root
	if root.Class == "hierarchy" && len(root.Children) == 1 {
		root = root.Children[0]
	}
	return toWire(root)
}

func toWire(n *Node) WireNode {
	displayed := n.Displayed
	enabled := n.Enabled
	w := WireNode{
		Text: n.Text, Label: n.Label, TestID: n.TestID,
		Class: n.Class, Package: n.Package,
		Bounds:        [4]int{n.Bounds.X1, n.Bounds.Y1, n.Bounds.X2, n.Bounds.Y2},
		Clickable:     n.Clickable,
		LongClickable: n.LongClickable,
		Checkable:     n.Checkable,
		DeclaredRole:  n.DeclaredRole,
		Checked:       n.Checked,
		Scrollable:    n.Scrollable,
		Focusable:     n.Focusable,
		Enabled:       &enabled,
		Selected:      n.Selected,
		Password:      n.Password,
		Displayed:     &displayed,
	}
	for _, c := range n.Children {
		w.Children = append(w.Children, toWire(c))
	}
	return w
}

// FromWire builds a tree from what a driver sent, filling in everything
// derived. The result is indistinguishable from a parsed one.
//
// The driver sends the app's own root. Both XML parsers hang that under a
// synthetic "hierarchy" node, and depth and path are numbered from beneath it,
// so the same wrapper is added here — otherwise every path from an external
// driver would be off by one segment against every path from a built-in one,
// and a ref would mean two different things depending on the backend.
func FromWire(w WireNode) *Tree {
	root := &Node{Class: "hierarchy", Enabled: true, Displayed: true}
	child := fromWire(w)
	root.Children = []*Node{child}
	root.Bounds = child.Bounds
	t := &Tree{Root: root}
	Link(t)
	return t
}

func fromWire(w WireNode) *Node {
	displayed := true
	if w.Displayed != nil {
		displayed = *w.Displayed
	}
	n := &Node{
		Text: w.Text, Label: w.Label, TestID: w.TestID,
		Class: w.Class, Package: w.Package,
		Bounds:        Rect{w.Bounds[0], w.Bounds[1], w.Bounds[2], w.Bounds[3]},
		Clickable:     w.Clickable,
		LongClickable: w.LongClickable,
		Checkable:     w.Checkable,
		DeclaredRole:  w.DeclaredRole,
		Checked:       w.Checked,
		Scrollable:    w.Scrollable,
		Focusable:     w.Focusable,
		Enabled:       w.Enabled == nil || *w.Enabled,
		Selected:      w.Selected,
		Password:      w.Password,
		Displayed:     displayed,
	}
	for _, c := range w.Children {
		n.Children = append(n.Children, fromWire(c))
	}
	return n
}

// Link fills in the fields a tree needs but a driver does not supply: each
// node's parent, depth, sibling index and path.
//
// The XML parsers set these as they build, so this is the same rule written
// once more for trees that did not come from XML. A test asserts the two
// agree on a real capture; if they ever stop agreeing, a ref taken through
// one path would not resolve through the other.
func Link(t *Tree) {
	if t == nil || t.Root == nil {
		return
	}
	t.Root.Parent = nil
	t.Root.Depth = 0
	t.Root.Index = 0
	t.Root.Path = ""
	var walk func(parent *Node)
	walk = func(parent *Node) {
		for i, c := range parent.Children {
			c.Parent = parent
			c.Index = i
			// Children of the synthetic root are depth 0, not 1: the wrapper
			// is Mobium's, not the platform's, and both parsers number from
			// below it.
			if parent.Parent == nil {
				c.Depth = 0
			} else {
				c.Depth = parent.Depth + 1
			}
			path := strconv.Itoa(i)
			if parent.Path != "" || parent.Parent != nil {
				path = parent.Path + "/" + path
			}
			c.Path = path
			walk(c)
		}
	}
	walk(t.Root)
}

// UnmarshalWire reads a driver's snapshot reply.
func UnmarshalWire(data []byte) (*Tree, error) {
	var w WireNode
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("could not read the hierarchy the driver sent: %w", err)
	}
	return FromWire(w), nil
}
