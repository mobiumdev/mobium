package uitree

import (
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"sort"
	"strconv"
	"strings"
)

// Kind is the semantic dimension a locator matches on. The set is deliberately
// small and platform-neutral — the same `mobium find text "Sign In"` compiles
// to a content-desc/text match on Android and a label/value predicate on iOS.
type Kind string

const (
	KindText   Kind = "text"   // visible text
	KindLabel  Kind = "label"  // accessibility label (content-desc / a11y label)
	KindTestID Kind = "testid" // resource-id / accessibilityIdentifier
	KindRole   Kind = "role"   // button, input, checkbox, ...
	KindClass  Kind = "class"  // platform widget class, escape hatch
	KindPath   Kind = "path"   // sibling-index path, locator of last resort
)

// Locator is a resolvable reference to an element. Exact controls whether
// string kinds compare by equality or by case-insensitive substring; Role
// narrows any of the string kinds when both are set. Whole is what a caller
// asks for by quoting the value: the whole of it, in any case — the derived
// locators' Exact is byte-for-byte, and Android draws a button's caption in
// capitals, so a whole caption typed as it reads must still match.
type Locator struct {
	Kind  Kind   `json:"kind"`
	Value string `json:"value"`
	Exact bool   `json:"exact,omitempty"`
	Whole bool   `json:"whole,omitempty"`
	Role  string `json:"role,omitempty"`
	// Nth picks one of the matches shown, counted from 1 in the order a
	// person reads the screen; 0 means all of them.
	Nth int `json:"nth,omitempty"`
	// Within scopes the locator to what it shares a row with: the target
	// nearest the anchor in the tree, and never past the list holding the
	// anchor. See Resolve.
	Within *Locator `json:"within,omitempty"`
}

// String is the locator as a caller writes it, and parses back to one that
// matches the same: an exact or whole value is quoted, since unquoted it
// would match a part. A near miss printed as `label=Card 8` and pasted back
// was a substring match again.
func (l Locator) String() string {
	v := l.Value
	if (l.Exact || l.Whole) && l.stringKind() {
		v = quoteValue(v)
	}
	s := fmt.Sprintf("%s=%s", l.Kind, v)
	if l.Role != "" && l.Kind != KindRole {
		s += "," + string(KindRole) + "=" + l.Role
	}
	if l.Nth > 0 {
		s += ",nth=" + strconv.Itoa(l.Nth)
	}
	if l.Within != nil {
		s += withinWord + l.Within.String()
	}
	return s
}

const withinWord = " within "

// splitWithin finds the first " within " outside a quoted value, or -1.
func splitWithin(s string) int {
	quoted := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && quoted:
			i++
		case s[i] == '"':
			quoted = !quoted
		case !quoted && strings.HasPrefix(s[i:], withinWord):
			return i
		}
	}
	return -1
}

func (l Locator) stringKind() bool {
	return l.Kind == KindText || l.Kind == KindLabel || l.Kind == KindTestID || l.Kind == KindClass
}

func quoteValue(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// unquoteValue reads a value that opens with a double quote: what is inside,
// with \" and \\ for a quote and a backslash, and what follows the closing
// quote. ok is false with no closing quote.
func unquoteValue(v string) (inside, rest string, ok bool) {
	var b strings.Builder
	for i := 1; i < len(v); i++ {
		switch v[i] {
		case '\\':
			if i+1 < len(v) {
				i++
				b.WriteByte(v[i])
			}
		case '"':
			return b.String(), v[i+1:], true
		default:
			b.WriteByte(v[i])
		}
	}
	return "", "", false
}

// ParseLocator reads the "kind=value" form used in ref files and on the CLI.
// A bare string with no "=" is treated as text. A value in double quotes is
// matched whole, in any case — `text="Internet"` is not met by "Network &
// internet", which `text=Internet` is (CHALLENGES 233) — and may contain
// commas; \" is a quote inside it. Only ",role=" may follow the closing quote.
//
// "label=More within text=Card 8" is the More in the row that says Card 8:
// the target, scoped to what it shares with the anchor. The anchor may be
// scoped in turn. A value containing " within " is quoted.
func ParseLocator(s string) (Locator, error) {
	if i := splitWithin(s); i >= 0 {
		target, anchor := strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(withinWord):])
		if target == "" || anchor == "" {
			return Locator{}, mobiumerr.New(mobiumerr.InvalidArgument, "%q needs a locator on each side of \"within\"", s)
		}
		l, err := parseOne(target)
		if err != nil {
			return Locator{}, err
		}
		in, err := ParseLocator(anchor)
		if err != nil {
			return Locator{}, err
		}
		l.Within = &in
		return l, nil
	}
	return parseOne(s)
}

// parseOne reads one locator, unscoped. A trailing ",nth=N" — last, after
// any ",role=" — picks the Nth of its matches shown.
func parseOne(s string) (Locator, error) {
	nth := 0
	if i := strings.LastIndex(s, ",nth="); i >= 0 && !strings.ContainsAny(s[i+len(",nth="):], `,"`) {
		n, err := strconv.Atoi(s[i+len(",nth="):])
		if err != nil || n < 1 {
			return Locator{}, mobiumerr.New(mobiumerr.InvalidArgument, "nth must be a whole number from 1, got %q",
				s[i+len(",nth="):])
		}
		nth, s = n, s[:i]
	}
	l, err := parseBare(s)
	l.Nth = nth
	return l, err
}

// parseBare reads one locator with no nth.
func parseBare(s string) (Locator, error) {
	main := s
	var role string
	whole := false
	k, v, ok := strings.Cut(main, "=")
	if strings.HasPrefix(s, `"`) {
		k, v, ok = string(KindText), s, true
	}
	if ok && strings.HasPrefix(v, `"`) {
		inside, rest, closed := unquoteValue(v)
		if !closed {
			return Locator{}, mobiumerr.New(mobiumerr.InvalidArgument, "the quoted value in %q has no closing quote", s)
		}
		if rest != "" {
			if !strings.HasPrefix(rest, ",role=") {
				return Locator{}, mobiumerr.New(mobiumerr.InvalidArgument, "only \",role=\" may follow a quoted value, "+
					"and %q has %q after it", s, rest)
			}
			role = rest[len(",role="):]
		}
		v, whole = inside, true
	} else {
		// Only split a trailing ",role=" — values may legitimately contain commas.
		if i := strings.LastIndex(s, ",role="); i >= 0 {
			main, role = s[:i], s[i+len(",role="):]
		}
		k, v, ok = strings.Cut(main, "=")
		if !ok {
			k, v = string(KindText), main
		}
	}
	// A role nothing has matches nothing, and a wait for nothing to be
	// hidden is over at once: `mobium wait role=progressbar --for hidden`,
	// the help's own example, answered "hidden" in 0.8s before progressbar
	// was a role, and so did role=nosuchrole (CHALLENGES 222).
	roles := []string{role}
	if Kind(k) == KindRole {
		roles = append(roles, v)
	}
	for _, r := range roles {
		if r != "" && !knownRole(r) {
			return Locator{}, mobiumerr.New(mobiumerr.InvalidArgument, "unknown role %q (want one of %s)",
				r, strings.Join(Roles(), ", "))
		}
	}
	switch Kind(k) {
	case KindText, KindLabel, KindTestID, KindClass:
		return Locator{Kind: Kind(k), Value: v, Whole: whole, Role: role}, nil
	case KindRole, KindPath:
		// Quotes change nothing here: a role is a name, and a path is
		// compared whole already.
		return Locator{Kind: Kind(k), Value: v, Role: role}, nil
	default:
		return Locator{}, mobiumerr.New(mobiumerr.InvalidArgument, "unknown locator kind %q (want text, label, testid, role, class or path)", k)
	}
}

func knownRole(r string) bool {
	r = strings.ToLower(r)
	for _, k := range Roles() {
		if k == r {
			return true
		}
	}
	return false
}

// roleClasses maps a neutral role onto the Android widget classes that satisfy
// it. Matching is on the class suffix, so subclasses ("AppCompatButton",
// "MaterialButton") are covered without enumerating vendor widgets.
var roleClasses = map[string][]string{
	"button":   {"Button", "ImageButton", "CompoundButton"},
	"input":    {"EditText", "AutoCompleteTextView", "SearchView"},
	"checkbox": {"CheckBox", "CheckedTextView"},
	"switch":   {"Switch", "SwitchCompat", "ToggleButton"},
	"radio":    {"RadioButton"},
	"slider":   {"SeekBar", "Slider"},
	// A busy indicator. Not a SeekBar, though Android derives it from
	// ProgressBar: matching is on the class's own name.
	"progressbar": {"ProgressBar"},
	"image":       {"ImageView", "ImageButton"},
	"text":        {"TextView"},
	"list":        {"RecyclerView", "ListView", "ScrollView", "NestedScrollView", "ViewPager"},
	"tab":         {"TabView", "TabItem"},
}

// Roles lists the accepted role values, for error messages and help text.
func Roles() []string {
	// adjustable is declared by an iOS element itself (its Adjustable trait),
	// so no class table holds it.
	seen := map[string]bool{"link": true, "password": true, "adjustable": true}
	for r := range roleClasses {
		seen[r] = true
	}
	for r := range iosRoleTypes {
		seen[r] = true
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// iosRoleTypes maps a neutral role onto XCUIElementTypes.
//
// This cannot share Android's suffix table: "TextView" is a read-only label on
// Android and a multi-line text input on iOS, so one table matching on
// suffixes would classify half of every Android screen as an input.
var iosRoleTypes = map[string][]string{
	"button": {"Button", "Icon"},
	// TextView is deliberately absent. iOS uses it for read-only prose as
	// well as for editable multi-line fields — Safari's privacy sheet renders
	// three paragraphs of explainer text as TextViews — and an agent that
	// believes prose is an input will try to type into it. A genuinely
	// editable TextView is missed by role=input as a result; it is still
	// mapped and tappable, and a testid or label reaches it.
	"input":       {"TextField", "SecureTextField", "SearchField"},
	"checkbox":    {"CheckBox"},
	"switch":      {"Switch", "Toggle"},
	"radio":       {"RadioButton"},
	"slider":      {"Slider"},
	"progressbar": {"ActivityIndicator", "ProgressIndicator"},
	"image":       {"Image"},
	"text":        {"StaticText"},
	"link":        {"Link"},
	"list":        {"ScrollView", "Table", "CollectionView"},
	"tab":         {"Tab", "TabBar"},
	"cell":        {"Cell"},
}

// IsIOS reports whether a node came from a WebDriverAgent hierarchy.
func IsIOS(n *Node) bool { return strings.HasPrefix(n.Class, "XCUIElementType") }

// HasRole reports whether a node satisfies a neutral role.
func HasRole(n *Node, role string) bool { return hasRole(n, role, true) }

// HasClassRole is HasRole without the clickable fallback: the role the
// element's class or its own declaration names, and nothing inferred from its
// being touchable. For deciding what a node certainly is — a clickable custom
// view is a button to a locator, and may be a text field to the app.
func HasClassRole(n *Node, role string) bool { return hasRole(n, role, false) }

func hasRole(n *Node, role string, clickableFallback bool) bool {
	role = strings.ToLower(role)
	if hasNamedRole(n, role, clickableFallback) {
		return true
	}
	// map names a node by what it does when no class says what it is — a
	// scrollable one is a list, a touchable one a button — and a locator has
	// to find it by the role map printed. It did not on iOS: every Cell,
	// keyboard key and React Native view printed as (button) and matched no
	// role=button, so in Safari's share sheet "Add to Home Screen (button)"
	// could not be found by label and role, and map fell back to a path 22
	// levels deep. 93 entries across the captured hierarchies, one of them
	// Android's (a GridView map calls a list). CHALLENGES 136.
	return clickableFallback && role != "" && fallbackRole(n) == role
}

// fallbackRole is the role map gives a node from its behavior alone. A text
// field is never a button, however touchable: typing into what a locator
// called a button is the mistake the input role exists to prevent. Nor is a
// row that holds a real button: iOS Settings draws "About" as a touchable
// Cell around a Button of the same label, and calling both buttons made
// `label=About,role=button` ambiguous — the Button inside answers it, as it
// always did, and a tap on it lands in the row.
//
// Nor is an iOS link, which map prints as (link) before any other role:
// calling it a button too made NetNewsWire's back button ambiguous with an
// article's link of the same name.
func fallbackRole(n *Node) string {
	if n.Password || hasNamedRole(n, "input", false) {
		return ""
	}
	if IsIOS(n) && hasNamedRole(n, "link", false) {
		return ""
	}
	if n.Scrollable {
		return "list"
	}
	if n.Clickable && !hasNamedDescendant(n, "button") {
		return "button"
	}
	return ""
}

func hasNamedDescendant(n *Node, role string) bool {
	for _, c := range n.Children {
		if hasNamedRole(c, role, false) || hasNamedDescendant(c, role) {
			return true
		}
	}
	return false
}

// hasNamedRole is the role a node's class, or its own declaration, names.
func hasNamedRole(n *Node, role string, clickableFallback bool) bool {

	// The platform says which fields hold a secret, on both platforms —
	// `password="true"` on Android, XCUIElementTypeSecureTextField on iOS —
	// and it is worth surfacing rather than calling these "input" like any
	// other. An agent that can see which field is a password can avoid
	// echoing what it typed.
	if role == "password" {
		return n.Password
	}

	// A role the element declares for itself beats one inferred from its
	// class. iOS has no element type for a checkbox, so React Native renders
	// one as XCUIElementTypeOther and says what it is in the accessibility
	// value instead — which is the app telling us directly, and better
	// evidence than any table here.
	if n.DeclaredRole != "" && strings.EqualFold(n.DeclaredRole, role) {
		return true
	}

	if IsIOS(n) {
		// iOS types are exact, not a hierarchy of subclasses, so the type
		// name after the prefix is compared directly. No clickable-fallback
		// is needed either: the platform names what each element is.
		kind := strings.TrimPrefix(n.Class, "XCUIElementType")
		for _, want := range iosRoleTypes[role] {
			if kind == want {
				return true
			}
		}
		return false
	}

	// A clickable node advertising no button class is still a button to a
	// user, and Compose/React Native screens are full of them.
	if clickableFallback && role == "button" && n.Clickable && n.Class != "" {
		if !strings.Contains(n.Class, "EditText") {
			return true
		}
	}
	// A link has no distinct widget class on Android: it is a text view the
	// user can touch. Without the clickable requirement, role=link would
	// match every label on screen.
	if role == "link" {
		return n.Clickable && strings.HasSuffix(n.ShortClass(), "TextView")
	}
	suffixes, ok := roleClasses[role]
	if !ok {
		return false
	}
	short := n.ShortClass()
	for _, s := range suffixes {
		if strings.HasSuffix(short, s) {
			return true
		}
	}
	return false
}

// field compares one of a node's strings with the value, as the locator asks:
// whole and in any case when quoted, else byte for byte when Exact, else a
// part in any case.
//
// A whole value also takes straight and curly quotes as one: Apple writes
// Don’t Allow, and a caller types Don't Allow — app_alert's button matches
// them alike for the same reason (CHALLENGES 305).
func (l Locator) field(s string) bool {
	if l.Whole {
		return l.Value != "" && strings.EqualFold(foldQuotes(strings.TrimSpace(s)), foldQuotes(strings.TrimSpace(l.Value)))
	}
	return match(s, l.Value, l.Exact)
}

var curlyQuotes = strings.NewReplacer("\u2019", "'", "\u2018", "'", "\u201c", "\"", "\u201d", "\"")

func foldQuotes(s string) string { return curlyQuotes.Replace(s) }

func match(field, value string, exact bool) bool {
	if value == "" {
		return false
	}
	if exact {
		return field == value
	}
	return strings.Contains(strings.ToLower(field), strings.ToLower(value))
}

// Matches reports whether a single node satisfies the locator.
func (l Locator) Matches(n *Node) bool {
	if l.Role != "" && l.Kind != KindRole && !HasRole(n, l.Role) {
		return false
	}
	switch l.Kind {
	case KindText:
		return l.field(n.Text)
	case KindLabel:
		return l.field(n.Label) || (namedByHint(n) && l.field(n.Hint))
	case KindTestID:
		// "submit_btn" should match "com.example:id/submit_btn", so compare
		// the short form too.
		return l.field(n.TestID) || l.field(n.ShortTestID())
	case KindRole:
		return HasRole(n, l.Value)
	case KindClass:
		return l.field(n.Class) || l.field(n.ShortClass())
	case KindPath:
		return n.Path == l.Value
	}
	return false
}

// Resolve returns every node in the tree satisfying the locator, in document
// order.
//
// A hand-written testid resolves exact first: if exactly one node's ID is the
// value, case and all, that node is the answer; otherwise every substring
// match is, as before. A test ID is an identifier, and the ordinary way to
// name things — `username` beside `usernameError` — made the field's own
// locator ambiguous under substring alone. On iOS it was worse: a label's ID
// is its own text, so `testid=password` matched the "Password" heading and a
// hint mentioning the password as well as the field, seven in all on
// MobiumApp's login screen. Exact first keeps every partial-ID locator that
// worked, working.
//
// text= also finds an iOS control by its label when it has no text of its
// own, as labelOnly says.
//
// A scoped locator keeps, of its own matches, those nearest an anchor: from
// each match of the anchor, up through its ancestors, the first that holds
// any — the row the anchor is in, when the row has one. Never past the list:
// the climb stops at a scroll container, and at a scroll container's only
// child, the wrapper React Native puts every row in — except an iOS Cell,
// which is a row whatever holds it — and at a level whose targets all sit in
// rows beside the anchor's, which is a list nothing names as one. So the More in Card 8's row is found,
// and a Card 8 with no More finds nothing, never the More of the row beside
// it. The cost is a list of one row that is its scroll view's only child on
// Android, which looks like a wrapper and finds nothing: a miss, never the
// wrong row. Resolved whole on every read, as any locator is: lists reuse
// their cells, and a row found once is not the row a moment later.
//
// With Nth, of what that leaves the Nth shown — displayed, with a size — in
// reading order: top to bottom, and along a row left to right. Shown only:
// iOS keeps rows off screen and buttons under a dialog in the tree, and
// counting them would make "the second" move with every scroll.
func (l Locator) Resolve(t *Tree) []*Node {
	var out []*Node
	if l.Within == nil {
		out = l.resolveOwn(t)
	} else {
		own := l
		own.Within = nil
		out = nearest(own.resolveOwn(t), l.Within.Resolve(t))
	}
	if l.Nth == 0 {
		return out
	}
	var shown []*Node
	for _, n := range out {
		if n.Displayed && !n.Bounds.Empty() {
			shown = append(shown, n)
		}
	}
	ReadingOrder(shown)
	if l.Nth > len(shown) {
		return nil
	}
	return []*Node{shown[l.Nth-1]}
}

// ReadingOrder sorts nodes as a person reads them: top to bottom, and left to
// right along a row — two are one row when they overlap by more than half the
// shorter's height.
func ReadingOrder(ns []*Node) {
	sort.SliceStable(ns, func(i, j int) bool {
		a, b := ns[i].Bounds, ns[j].Bounds
		h := min(a.Y2-a.Y1, b.Y2-b.Y1)
		if overlap := min(a.Y2, b.Y2) - max(a.Y1, b.Y1); overlap*2 > h {
			return a.X1 < b.X1
		}
		return a.Y1 < b.Y1
	})
}

// nearest is the scoping Resolve describes, in document order.
func nearest(targets, anchors []*Node) []*Node {
	keep := map[*Node]bool{}
	for _, a := range anchors {
		prev := a
		for p := a; p != nil; prev, p = p, p.Parent {
			if p != a && (isList(p) || wrapper(p)) {
				// At the list with nothing found: the rows may be flat.
				for _, n := range inRowBox(p, prev, a, targets) {
					keep[n] = true
				}
				break
			}
			var found []*Node
			for _, n := range targets {
				if n.Within(p) && !otherRow(p, a, n) {
					found = append(found, n)
				}
			}
			if len(found) > 0 {
				for _, n := range found {
					keep[n] = true
				}
				break
			}
			// A level whose targets are all in the rows beside the anchor's
			// is the list, whatever it is called: going on only crosses more.
			if p != a && anyWithin(targets, p) {
				break
			}
		}
	}
	var out []*Node
	for _, n := range targets {
		if keep[n] {
			out = append(out, n)
		}
	}
	return out
}

// inRowBox is the scope where a list's rows are flat. React Native's rows
// reach UiAutomator2 as empty nodes the size of the row, with the row's text
// and buttons beside them under the list rather than inside — measured on
// MobiumApp's Table Demo on the Android 15 emulator — so the tree joins
// "Item 21" to its More only at the list. The row is then the smallest of
// the list's children, other than the anchor's own, whose rectangle holds
// the anchor; its targets are those inside that rectangle and the list.
func inRowBox(list, top, anchor *Node, targets []*Node) []*Node {
	var box *Node
	for _, c := range list.Children {
		if c == top || c.Bounds.Empty() || !enclosesRect(c.Bounds, anchor.Bounds) {
			continue
		}
		if box == nil || area(c.Bounds) < area(box.Bounds) {
			box = c
		}
	}
	if box == nil {
		return nil
	}
	var out []*Node
	for _, n := range targets {
		if n.Within(list) && !n.Bounds.Empty() && enclosesRect(box.Bounds, n.Bounds) {
			out = append(out, n)
		}
	}
	return out
}

// wrapper is a node between a list and its rows: an only child under the
// list, or under another wrapper. React Native puts one under an Android
// ScrollView and two under an iOS one (MobiumApp's Table Demo, iOS 26.5),
// each holding every row. An iOS Cell is a row whatever holds it.
//
// Only child but for scroll indicators: an iOS scroll view also holds its
// scroll bars (CHALLENGES 167), and counted, they hid the wrapper.
func wrapper(n *Node) bool {
	if n.Class == "XCUIElementTypeCell" || n.Parent == nil {
		return false
	}
	for _, c := range n.Parent.Children {
		if c != n && !isScrollIndicator(c) {
			return false
		}
	}
	return isList(n.Parent) || wrapper(n.Parent)
}

// isList is a node that holds rows: one that scrolls, or one whose class is
// a list's whether or not it scrolls now — Android marks a RecyclerView
// scrollable only while its rows overflow it, and Settings' Network &
// internet, which fits, had no boundary: a switch was found for Hotspot &
// tethering in Airplane mode's row, measured on the Android 15 emulator.
func isList(n *Node) bool {
	if n.Scrollable {
		return true
	}
	for _, c := range []string{"RecyclerView", "ListView", "GridView", "ScrollView", "XCUIElementTypeTable",
		"XCUIElementTypeCollectionView", "XCUIElementTypeScrollView"} {
		if strings.HasSuffix(n.Class, c) {
			return true
		}
	}
	return false
}

// otherRow says a target lies in a row beside the anchor's: under p, the two
// are in different children of the same class and width, as rows are, where
// a row's own parts — a title and its button — differ in one or the other.
// It catches a list nothing names as one, such as a Compose column.
func otherRow(p, anchor, target *Node) bool {
	var ab, tb *Node
	for _, c := range p.Children {
		if anchor.Within(c) {
			ab = c
		}
		if target.Within(c) {
			tb = c
		}
	}
	if ab == nil || tb == nil || ab == tb {
		return false
	}
	return ab.Class == tb.Class && abs(ab.Bounds.Width()-tb.Bounds.Width()) <= 2 && ab.Bounds.Width() > 0
}

func anyWithin(ns []*Node, p *Node) bool {
	for _, n := range ns {
		if n.Within(p) {
			return true
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (l Locator) resolveOwn(t *Tree) []*Node {
	var out, exact []*Node
	byLabel := map[*Node]bool{}
	t.Walk(func(n *Node) bool {
		if l.Matches(n) {
			out = append(out, n)
			if l.Kind == KindTestID && !l.Exact && !l.Whole && (n.TestID == l.Value || n.ShortTestID() == l.Value) {
				exact = append(exact, n)
			}
		} else if l.labelOnly(n) {
			out = append(out, n)
			byLabel[n] = true
		}
		return true
	})
	if len(exact) == 1 {
		return exact
	}
	if l.Kind != KindText && l.Kind != KindLabel {
		return out
	}
	// A control whose label is the text of something inside it is not a
	// second match: the text node is the one, as it was before. Nor is a node
	// inside another at the same bounds: React Native nests a Text in a Text
	// and iOS reports both, so text=Back found two on MobiumApp's Dialog Demo
	// — one thing drawn once, which Android reports once. label= is the same
	// case: iOS's Face ID sheet reports each of its buttons as a button
	// inside a button, same label, same frame, and label=Cancel found two,
	// with a remedy — ",role=button" — that could not tell them apart. A
	// test ID is a name the app gave, so two nodes carrying one stay two.
	first := make([]*Node, 0, len(out))
	for _, n := range out {
		if byLabel[n] && containsAny(n, out) || sameAsAncestor(n, out) {
			continue
		}
		first = append(first, n)
	}
	// Nor is a node that is not a target, around or inside one that is.
	// SwiftUI on iOS 26 puts each toolbar button in a container with its
	// label and a frame a few points wider, so label=Close on Ice Cubes'
	// image viewer found two while map printed one; and it labels a button's
	// icon and title too, so label=Settings found the tab and the gear in
	// it, and label=Display Settings the row and its title. Asked of what
	// is left, not of every match: the viewer's Info button sits in a
	// container with its very frame, which the rule above already keeps in
	// its place, and asking of both removed both.
	kept := make([]*Node, 0, len(first))
	for _, n := range first {
		if wrapsTarget(n, first) || l.Kind == KindLabel && contentOfTarget(n, first) {
			continue
		}
		kept = append(kept, n)
	}
	// And a whole match beats a part, as it does for a test id: iOS's paste
	// prompt has "Allow Paste" and "Don’t Allow Paste", and label=Allow Paste
	// found both — with a remedy, ",role=button", that could not tell two
	// buttons apart, and no syntax to ask for the whole of it (CHALLENGES 185).
	if !l.Exact && !l.Whole && len(kept) > 1 {
		var whole []*Node
		for _, n := range kept {
			if l.wholeMatch(n) {
				whole = append(whole, n)
			}
		}
		if len(whole) == 1 {
			return whole
		}
	}
	return kept
}

// wholeMatch reports whether n's text or label — the field this locator
// reads — is its whole value, ignoring case and surrounding space.
func (l Locator) wholeMatch(n *Node) bool {
	eq := func(s string) bool { return strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(l.Value)) }
	if l.Kind == KindLabel {
		return eq(n.Label) || (namedByHint(n) && eq(n.Hint))
	}
	return eq(n.Text) || (n.Text == "" && eq(n.Label))
}

// namedByHint reports an empty field that map names by its hint, which label=
// then finds by it too: a name map prints is a name a locator takes. Flutter's
// field reports no label and no text, only the hint (CHALLENGES 206); a
// native Android field already carries its hint as its text.
func namedByHint(n *Node) bool {
	return n.Label == "" && n.Text == "" && n.Hint != ""
}

// sameAsAncestor reports whether one of nodes is an ancestor of n at exactly
// n's bounds.
//
// Not where n is a target and that ancestor is not: the one kept must be the
// control. Kiwix's toolbar wraps its disabled List button in an Other of the
// same frame and label, and this kept the wrapper — which reports itself
// enabled — so a tap by the button's own ref was reported done on a button
// that could not take it. CHALLENGES 247.
func sameAsAncestor(n *Node, nodes []*Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Bounds != n.Bounds {
			continue
		}
		for _, m := range nodes {
			if m == p && !(targetShaped(n) && !targetShaped(p)) {
				return true
			}
		}
	}
	return false
}

// labelOnly reports whether n matches text= by its label: an iOS node with
// no text of its own. On iOS a node's text is its value, and a React Native
// button has none — only the label VoiceOver reads — so text=Dialog Demo
// found nothing on MobiumApp while Android, where that label is a child
// TextView's text, found the button. Android's label is content-desc, which
// text= has never matched, so an Android node is never one of these.
// CHALLENGES 166.
func (l Locator) labelOnly(n *Node) bool {
	if l.Kind != KindText || n.Text != "" || !strings.HasPrefix(n.Class, "XCUIElementType") {
		return false
	}
	if l.Role != "" && !HasRole(n, l.Role) {
		return false
	}
	return l.field(n.Label)
}

// containsAny reports whether any of nodes is a descendant of n.
// wrapsTarget reports whether n is not a target itself and holds one of
// nodes that is.
func wrapsTarget(n *Node, nodes []*Node) bool {
	if targetShaped(n) {
		return false
	}
	for _, m := range nodes {
		if m != n && targetShaped(m) && isAncestor(n, m) {
			return true
		}
	}
	return false
}

// contentOfTarget reports whether n is an image or a text inside one of
// nodes that is a target: the icon or the title of the button it names.
//
// A Button too, where it is no target: SwiftUI reports a combined card's
// words as Buttons inside it, and label=Ray Charles on Kiwix's catalog found
// each card's title and description as well as the card (CHALLENGES 244).
func contentOfTarget(n *Node, nodes []*Node) bool {
	if n.Class != "XCUIElementTypeImage" && n.Class != "XCUIElementTypeStaticText" &&
		n.Class != "XCUIElementTypeButton" || targetShaped(n) {
		return false
	}
	for _, m := range nodes {
		if m != n && targetShaped(m) && isAncestor(m, n) {
			return true
		}
	}
	return false
}

// targetShaped reports whether n would be a target if it were shown. Being
// a control's wrapper, icon or title is a matter of the tree's shape, not of
// what is on screen, and asking Actionable instead made the answer depend on
// `visible`: a read without it, which takes everything as shown, dropped a
// hidden row's title as part of the row while a full read kept it, and
// label=System on Ice Cubes' font menu found one element on the one and six
// on the other. A light read must only ever add matches (lightread_test.go).
func targetShaped(n *Node) bool {
	shown := *n
	shown.Displayed = true
	shown.Clickable = n.Clickable || n.shaped
	return Actionable(&shown)
}

func containsAny(n *Node, nodes []*Node) bool {
	for _, m := range nodes {
		for p := m.Parent; p != nil; p = p.Parent {
			if p == n {
				return true
			}
		}
	}
	return false
}

// Derive picks the most durable locator that uniquely identifies n within t.
//
// The order is deliberate: a resource-id survives copy changes and relayouts,
// an accessibility label survives relayouts, visible text survives neither
// reliably, and a sibling path survives nothing — but always resolves.
func Derive(n *Node, t *Tree) Locator {
	candidates := []Locator{}
	if n.TestID != "" {
		candidates = append(candidates, Locator{Kind: KindTestID, Value: n.TestID, Exact: true})
	}
	if n.Label != "" {
		candidates = append(candidates, Locator{Kind: KindLabel, Value: n.Label, Exact: true})
	}
	if n.Text != "" {
		candidates = append(candidates, Locator{Kind: KindText, Value: n.Text, Exact: true})
	}
	for _, c := range candidates {
		if len(c.Resolve(t)) == 1 {
			return c
		}
	}
	// Nothing unique on its own — try qualifying by role before giving up on
	// a semantic locator entirely.
	for _, c := range candidates {
		for _, role := range []string{"button", "input", "checkbox", "switch"} {
			if !HasRole(n, role) {
				continue
			}
			q := c
			q.Role = role
			if len(q.Resolve(t)) == 1 {
				return q
			}
		}
	}
	return Locator{Kind: KindPath, Value: n.Path, Exact: true}
}
