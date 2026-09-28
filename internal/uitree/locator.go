package uitree

import (
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"sort"
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
// narrows any of the string kinds when both are set.
type Locator struct {
	Kind  Kind   `json:"kind"`
	Value string `json:"value"`
	Exact bool   `json:"exact,omitempty"`
	Role  string `json:"role,omitempty"`
}

func (l Locator) String() string {
	s := fmt.Sprintf("%s=%s", l.Kind, l.Value)
	if l.Role != "" && l.Kind != KindRole {
		s += "," + string(KindRole) + "=" + l.Role
	}
	return s
}

// ParseLocator reads the "kind=value" form used in ref files and on the CLI.
// A bare string with no "=" is treated as text.
func ParseLocator(s string) (Locator, error) {
	main := s
	var role string
	// Only split a trailing ",role=" — values may legitimately contain commas.
	if i := strings.LastIndex(s, ",role="); i >= 0 {
		main, role = s[:i], s[i+len(",role="):]
	}
	k, v, ok := strings.Cut(main, "=")
	if !ok {
		return Locator{Kind: KindText, Value: main, Role: role}, nil
	}
	switch Kind(k) {
	case KindText, KindLabel, KindTestID, KindRole, KindClass, KindPath:
		return Locator{Kind: Kind(k), Value: v, Role: role}, nil
	default:
		return Locator{}, mobiumerr.New(mobiumerr.InvalidArgument, "unknown locator kind %q (want text, label, testid, role, class or path)", k)
	}
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
	"image":    {"ImageView", "ImageButton"},
	"text":     {"TextView"},
	"list":     {"RecyclerView", "ListView", "ScrollView", "NestedScrollView", "ViewPager"},
	"tab":      {"TabView", "TabItem"},
}

// Roles lists the accepted role values, for error messages and help text.
func Roles() []string {
	seen := map[string]bool{"link": true}
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
	"input":    {"TextField", "SecureTextField", "SearchField"},
	"checkbox": {"CheckBox"},
	"switch":   {"Switch", "Toggle"},
	"radio":    {"RadioButton"},
	"image":    {"Image"},
	"text":     {"StaticText"},
	"link":     {"Link"},
	"list":     {"ScrollView", "Table", "CollectionView"},
	"tab":      {"Tab", "TabBar"},
	"cell":     {"Cell"},
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
func fallbackRole(n *Node) string {
	if n.Password || hasNamedRole(n, "input", false) {
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
		return match(n.Text, l.Value, l.Exact)
	case KindLabel:
		return match(n.Label, l.Value, l.Exact)
	case KindTestID:
		// "submit_btn" should match "com.example:id/submit_btn", so compare
		// the short form too.
		return match(n.TestID, l.Value, l.Exact) || match(n.ShortTestID(), l.Value, l.Exact)
	case KindRole:
		return HasRole(n, l.Value)
	case KindClass:
		return match(n.Class, l.Value, l.Exact) || match(n.ShortClass(), l.Value, l.Exact)
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
func (l Locator) Resolve(t *Tree) []*Node {
	var out, exact []*Node
	t.Walk(func(n *Node) bool {
		if l.Matches(n) {
			out = append(out, n)
			if l.Kind == KindTestID && !l.Exact && (n.TestID == l.Value || n.ShortTestID() == l.Value) {
				exact = append(exact, n)
			}
		}
		return true
	})
	if len(exact) == 1 {
		return exact
	}
	return out
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
