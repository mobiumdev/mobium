// Package uitree parses a platform UI hierarchy into a neutral node tree and
// derives the locators and @refs that the CLI hands back to an agent.
//
// The tree is deliberately platform-neutral: the Android uiautomator parser
// here fills the same Node shape that an iOS/WDA source parser will, so
// everything above this file (locators, snapshots, refs) is written once.
package uitree

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Rect is a node's on-screen bounding box in device pixels.
type Rect struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

func (r Rect) Width() int  { return r.X2 - r.X1 }
func (r Rect) Height() int { return r.Y2 - r.Y1 }
func (r Rect) Empty() bool { return r.Width() <= 0 || r.Height() <= 0 }

// Center is the point a tap targets.
func (r Rect) Center() (int, int) {
	return r.X1 + r.Width()/2, r.Y1 + r.Height()/2
}

func (r Rect) String() string {
	return fmt.Sprintf("[%d,%d][%d,%d]", r.X1, r.Y1, r.X2, r.Y2)
}

// Node is one element of the UI hierarchy, normalized across platforms.
//
// Text/Label/TestID/Class are the four fields the semantic finders match on.
// On Android they come from text / content-desc / resource-id / class; on iOS
// they will come from label+value / accessibility label / identifier / type.
type Node struct {
	Text    string
	Label   string // content-desc on Android, accessibility label on iOS
	TestID  string // resource-id on Android, accessibilityIdentifier on iOS
	Class   string // fully-qualified widget class / XCUIElementType
	Package string
	Bounds  Rect

	Clickable     bool
	LongClickable bool
	Checkable     bool
	// DeclaredRole is a role the element claims for itself, rather than one
	// inferred from its class. Only iOS sets it, and only for custom controls
	// that compose their role into the accessibility value the way React
	// Native does — `value="checkbox, checked"`. Android needs nothing here:
	// its widget classes are the answer. See CHALLENGES 65.
	DeclaredRole string
	Checked      bool
	Scrollable   bool
	Focusable    bool
	Enabled      bool
	Selected     bool
	Password     bool
	// NotAccessible is iOS's accessible="false": not an element VoiceOver
	// reads. Its text is kept out of a label composed from what a container
	// holds — NetNewsWire's rows carried a disclosure arrow named "chevron"
	// that VoiceOver never says, and map printed "On My iPhone chevron".
	// Zero on Android, which has no such attribute, and on a Node built by
	// hand.
	NotAccessible bool
	// Value is a slider's position as the platform states it — on iOS
	// whatever the app made of it, "80%" or "1.2". Not its text: a slider
	// labeled by its value printed as `100% (button)` (CHALLENGES 219).
	Value string
	// shaped is iOS's Clickable before visibility is consulted: what the
	// node would be if it were shown. A locator asks it when deciding that a
	// node is a control's wrapper, icon or title, which is a matter of the
	// tree's shape, so a read without `visible` decides it as a full read
	// does. Unset on Android, which reports no hidden node.
	shaped bool
	// Hint is the field's placeholder: Android's `hint`, iOS's
	// `placeholderValue`. ShowingHint says the platform reported that the
	// field is empty and its text is that placeholder — UiAutomator2's
	// `showing-hint`, or on iOS a value equal to the placeholder. Both
	// platforms put an empty field's placeholder where its text goes, and a
	// password field's placeholder was reported as a hidden password of the
	// same length (CHALLENGES 102).
	Hint        string
	ShowingHint bool
	// Focused is Android's `focused`: this node has input focus. It is how
	// an action knows the keyboard may be up without asking the device.
	Focused bool
	// Virtual is Android's `drawing-order="0"`: a node an accessibility
	// provider made up rather than a real View, which a real View's drawing
	// order is never 0. Flutter's are; MobiumApp's React Native fields read 10
	// and 12. A virtual field may take text only once it has focus
	// (CHALLENGES 206).
	Virtual bool

	// Displayed is the platform's own visibility answer.
	//
	// Beware the zero value: an unset Displayed means *hidden*, so a Node
	// built by hand — in a test, or by a future parser — must set it or it
	// will be filtered out of `map` by Actionable. Every parser here sets it,
	// including on its synthetic root.
	//
	// What it is worth differs by platform, and Actionable says which.
	//
	// The dump format does not report it, so it defaults to true there
	// rather than hiding everything.
	Displayed bool

	Depth    int
	Index    int   // sibling index as reported by the platform
	Parent   *Node `json:"-"`
	Children []*Node

	// Path is the sibling-index path from the root ("0/3/1"). It is the
	// locator of last resort: unambiguous within one snapshot, but the first
	// thing to break when the screen changes.
	Path string
}

// Tree is a parsed snapshot of one screen.
type Tree struct {
	Root     *Node
	Rotation int
	// Screen is the display, from UiAutomator2's width and height on the
	// hierarchy; empty where the source gives none (iOS, the dump backend).
	// It is what tells a window that floats — a dialog — from the app's.
	Screen Rect
}

var boundsRe = regexp.MustCompile(`\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]`)

// ParseBounds reads uiautomator's "[x1,y1][x2,y2]" bounds attribute.
func ParseBounds(s string) (Rect, error) {
	m := boundsRe.FindStringSubmatch(s)
	if m == nil {
		return Rect{}, mobiumerr.New(mobiumerr.DeviceServer, "malformed bounds %q", s)
	}
	v := make([]int, 4)
	for i := 0; i < 4; i++ {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Rect{}, fmt.Errorf("malformed bounds %q: %w", s, err)
		}
		v[i] = n
	}
	return Rect{X1: v[0], Y1: v[1], X2: v[2], Y2: v[3]}, nil
}

// ParseAndroid parses an Android UI hierarchy.
//
// It accepts both shapes Android produces, because they carry the same
// attributes under different element names:
//
//	uiautomator dump:  <node class="android.widget.Button" text="OK" .../>
//	UiAutomator2:      <android.widget.Button class="..." text="OK" .../>
//
// So the element name is ignored entirely and every child element is a node.
// UiAutomator2 also omits empty attributes, which reads the same as absent.
//
// Leading junk is tolerated: some API levels prefix the dump with a status
// line, so parsing starts at the first '<'.
func ParseAndroid(data []byte) (*Tree, error) {
	if i := bytes.IndexByte(data, '<'); i > 0 {
		data = data[i:]
	} else if i < 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "parse ui hierarchy: no XML found")
	}

	dec := xml.NewDecoder(bytes.NewReader(data))
	// Hierarchies from real apps carry text in arbitrary encodings; without
	// this a non-UTF-8 declaration fails the whole parse.
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }

	// Displayed is set explicitly: the zero value of that field is "hidden",
	// and the iOS parser's root already sets it. The root has empty bounds so
	// nothing looks at it today, but the two parsers disagreeing about their
	// own root is the kind of difference that only matters once.
	root := &Node{Class: "hierarchy", Enabled: true, Displayed: true}
	tree := &Tree{Root: root}

	stack := []*Node{root}
	seenRoot := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse ui hierarchy: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if !seenRoot && t.Name.Local == "hierarchy" {
				seenRoot = true
				tree.Rotation, _ = strconv.Atoi(attr(t, "rotation"))
				w, _ := strconv.Atoi(attr(t, "width"))
				h, _ := strconv.Atoi(attr(t, "height"))
				tree.Screen = Rect{0, 0, w, h}
				continue
			}
			parent := stack[len(stack)-1]
			n := nodeFrom(t, parent, len(parent.Children))
			parent.Children = append(parent.Children, n)
			stack = append(stack, n)

		case xml.EndElement:
			if t.Name.Local == "hierarchy" && len(stack) == 1 {
				continue
			}
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if !seenRoot {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "parse ui hierarchy: no <hierarchy> element")
	}
	// A single top-level node is the common case; adopt its bounds so the
	// root has a usable screen rect.
	if len(root.Children) == 1 {
		root.Bounds = root.Children[0].Bounds
	}
	foldCheckableRows(root)
	return tree, nil
}

// foldCheckableRows makes an Android settings row one control, named by the
// row and carrying the switch's state — what foldSwitchRows does for iOS.
//
// Android's Settings draws a row as a clickable layout holding its title and,
// in a side frame, a switch that is not clickable, has no name and carries
// the state. map listed both: "Bold text (button)", which had no state, and
// "switchWidget (switch, unchecked)", named for its resource id and reachable
// only by position. Measured on a Pixel 7 AVD (Android 15), on Display size
// and text, and on Color and motion. Unlike iOS, the row is what a tap
// operates here — tapping it toggles the switch — so the row stays the
// target and takes the widget's role and state (CHALLENGES 120).
//
// Only a row with exactly one such widget and no other control inside it is
// folded: a row with a second button in it is two controls, and saying so is
// map's job.
func foldCheckableRows(n *Node) {
	for _, c := range n.Children {
		foldCheckableRows(c)
	}
	if !n.Clickable || n.Checkable {
		return
	}
	var widget *Node
	others := false
	var walk func(*Node)
	walk = func(x *Node) {
		for _, c := range x.Children {
			if c.Clickable || c.LongClickable {
				others = true
			}
			if c.Checkable {
				if widget != nil {
					others = true
				}
				widget = c
			}
			walk(c)
		}
	}
	walk(n)
	if widget == nil || others || widget.Label != "" || !unnamedSwitchText(widget.Text) {
		return
	}
	role := roleOf(widget)
	if role != "switch" && role != "checkbox" && role != "radio" {
		return
	}
	n.Checkable, n.Checked, n.DeclaredRole = true, widget.Checked, role
	widget.Checkable = false
}

// unnamedSwitchText reports whether a switch's text names nothing: empty, or
// the ON/OFF some Android versions print on the thumb.
func unnamedSwitchText(t string) bool {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "", "ON", "OFF":
		return true
	}
	return false
}

// attr reads one attribute, returning "" when absent — UiAutomator2 omits
// empty attributes rather than emitting them.
func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func boolAttr(e xml.StartElement, name string) bool {
	return attr(e, name) == "true"
}

func nodeFrom(e xml.StartElement, parent *Node, sibling int) *Node {
	path := strconv.Itoa(sibling)
	if parent.Path != "" || parent.Parent != nil {
		path = parent.Path + "/" + path
	}

	class := attr(e, "class")
	if class == "" {
		// UiAutomator2 names the element after the class; fall back to it so
		// a node is never classless.
		class = e.Name.Local
	}
	idx, _ := strconv.Atoi(attr(e, "index"))
	bounds, _ := ParseBounds(attr(e, "bounds"))

	n := &Node{
		Text:          attr(e, "text"),
		Label:         attr(e, "content-desc"),
		TestID:        attr(e, "resource-id"),
		Class:         class,
		Package:       attr(e, "package"),
		Bounds:        bounds,
		Clickable:     boolAttr(e, "clickable"),
		LongClickable: boolAttr(e, "long-clickable"),
		Checkable:     boolAttr(e, "checkable"),
		Checked:       boolAttr(e, "checked"),
		Scrollable:    boolAttr(e, "scrollable"),
		Focusable:     boolAttr(e, "focusable"),
		Enabled:       boolAttr(e, "enabled"),
		Selected:      boolAttr(e, "selected"),
		Password:      boolAttr(e, "password"),
		Hint:          attr(e, "hint"),
		ShowingHint:   boolAttr(e, "showing-hint"),
		Focused:       boolAttr(e, "focused"),
		Virtual:       attr(e, "drawing-order") == "0",
		Depth:         parent.Depth + 1,
		Index:         idx,
		Parent:        parent,
		Path:          path,
	}
	if parent.Parent == nil {
		n.Depth = 0
	}
	// UiAutomator2 reports real visibility; the dump format has no such
	// attribute, so absent means "assume displayed" rather than "hidden".
	n.Displayed = attr(e, "displayed") != "false"
	return n
}

// Package reports the app in the foreground, taken from the hierarchy that was
// already fetched rather than from a separate device call.
//
// Android puts the package on every node; iOS puts a bundleId on the
// application element. Both amount to the same answer.
func (t *Tree) Package() string {
	if t == nil || t.Root == nil {
		return ""
	}
	var pkg string
	t.Walk(func(n *Node) bool {
		if n.Package != "" {
			pkg = n.Package
			return false
		}
		return true
	})
	return pkg
}

// Walk visits every node below the root in document order.
func (t *Tree) Walk(fn func(*Node) bool) {
	if t == nil || t.Root == nil {
		return
	}
	var visit func(*Node) bool
	visit = func(n *Node) bool {
		for _, c := range n.Children {
			if !fn(c) {
				return false
			}
			if !visit(c) {
				return false
			}
		}
		return true
	}
	visit(t.Root)
}

// All returns every node below the root in document order.
func (t *Tree) All() []*Node {
	var out []*Node
	t.Walk(func(n *Node) bool { out = append(out, n); return true })
	return out
}

// ShortClass is the class name without its package ("android.widget.Button" ->
// "Button"), which is what map output shows.
//
// On iOS the XCUITest prefix goes too: an unlabeled table printed as
// "XCUIElementTypeTable (list)", where Android's prints "RecyclerView".
func (n *Node) ShortClass() string {
	if s, ok := strings.CutPrefix(n.Class, "XCUIElementType"); ok && s != "" {
		return s
	}
	if i := strings.LastIndexByte(n.Class, '.'); i >= 0 {
		return n.Class[i+1:]
	}
	return n.Class
}

// ShortTestID strips the "package:id/" prefix from an Android resource-id.
func (n *Node) ShortTestID() string {
	if i := strings.LastIndexByte(n.TestID, '/'); i >= 0 {
		return n.TestID[i+1:]
	}
	return n.TestID
}

// Scale multiplies every node's bounds by factor, in place.
//
// WebDriverAgent reports geometry in points while screenshots are in pixels —
// 3x apart on a modern iPhone. Mobium's coordinate space is device pixels on
// both platforms, so an iOS tree is scaled on the way in and gestures are
// scaled back on the way out. A factor of 1 is a no-op.
func (t *Tree) Scale(factor float64) {
	if t == nil || t.Root == nil || factor == 1 || factor <= 0 {
		return
	}
	scaleRect := func(r Rect) Rect {
		return Rect{
			X1: int(float64(r.X1)*factor + 0.5),
			Y1: int(float64(r.Y1)*factor + 0.5),
			X2: int(float64(r.X2)*factor + 0.5),
			Y2: int(float64(r.Y2)*factor + 0.5),
		}
	}
	t.Root.Bounds = scaleRect(t.Root.Bounds)
	t.Walk(func(n *Node) bool {
		n.Bounds = scaleRect(n.Bounds)
		return true
	})
}
