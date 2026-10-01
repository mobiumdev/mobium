package agent

import (
	"strings"
	"testing"
)

func el(ref, label, role string, x, y int) ElementView {
	return ElementView{Ref: ref, Label: label, Role: role, Bounds: BoundsView{x - 50, y - 20, x + 50, y + 20},
		Locator: &LocatorView{Kind: "path", Value: ref}}
}

func checked(e ElementView, on bool) ElementView { e.Checked = &on; return e }

func withID(e ElementView, id string) ElementView {
	e.Locator = &LocatorView{Kind: "testid", Value: id}
	return e
}

// What an action did, read off two maps whose refs were renumbered.
func TestDiffMapsSaysWhatAnActionDid(t *testing.T) {
	before := []ElementView{
		el("@e1", "Back", "button", 50, 50),
		checked(el("@e2", "Accept terms", "checkbox", 200, 300), false),
		el("@e3", "Sign in", "button", 200, 500),
		withID(el("@e4", "3 items", "", 200, 700), "cartCount"),
		el("@e5", "Status: idle", "", 200, 900),
	}
	after := []ElementView{
		el("@e1", "Back", "button", 50, 50),
		el("@e2", "Welcome, mobium", "", 200, 150),
		checked(el("@e3", "Accept terms", "checkbox", 200, 300), true),
		withID(el("@e4", "4 items", "", 200, 700), "cartCount"),
		el("@e5", "Status: done", "", 200, 902),
		el("@e6", "Back", "button", 200, 1200),
	}
	d := diffMaps(before, after)
	if len(d.Removed) != 1 || d.Removed[0].Label != "Sign in" {
		t.Errorf("removed %+v", d.Removed)
	}
	added := map[string]bool{}
	for _, e := range d.Added {
		added[e.Label] = true
	}
	// The second Back is new; the first paired with itself, not with it.
	if len(d.Added) != 2 || !added["Welcome, mobium"] || !added["Back"] {
		t.Errorf("added %+v", d.Added)
	}
	what := map[string]string{}
	for _, c := range d.Changed {
		what[c.After.Label] = strings.Join(c.What, ",")
	}
	// Paired by its test id although its label changed; paired by place
	// although its label changed and it moved two pixels, which is not a move.
	if what["Accept terms"] != "checked" || what["4 items"] != "label" || what["Status: done"] != "label" || len(what) != 3 {
		t.Errorf("changed %v", what)
	}
	text := diffText(d, len(after))
	for _, want := range []string{"- Sign in (button)", "+ @e2 Welcome, mobium", "~ @e3 Accept terms (checkbox, checked) — was unchecked",
		`~ @e4 4 items — was "3 items"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the text has no %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "- Sign in") > strings.Index(text, "+ @e2") {
		t.Error("removals are listed after additions")
	}
}

// A moved element is paired and said to have moved; the same map twice is
// nothing changed.
func TestDiffMapsMovedAndUnchanged(t *testing.T) {
	before := []ElementView{el("@e1", "Card 8", "button", 900, 600)}
	after := []ElementView{el("@e1", "Card 8", "button", 300, 600)}
	d := diffMaps(before, after)
	if len(d.Changed) != 1 || d.Changed[0].What[0] != "moved" || !strings.Contains(diffText(d, 1), "moved from (900, 600)") {
		t.Errorf("a move: %+v", d)
	}
	if got := diffText(diffMaps(before, before), 1); !strings.HasPrefix(got, "nothing changed") {
		t.Errorf("the same map twice: %q", got)
	}
}

// A checked box's row grows and everything under it shifts, as on iOS: the
// box is resized, and the rest is one line of moving together, not a line
// each.
func TestAReflowIsOneLine(t *testing.T) {
	box := checked(el("@e1", "Accept terms", "checkbox", 600, 660), false)
	grown := checked(box, true)
	grown.Bounds.Y2 += 18
	before := []ElementView{box, el("@e2", "Free", "radio", 600, 800), el("@e3", "Pro", "radio", 600, 900), el("@e4", "Team", "radio", 600, 1000)}
	after := []ElementView{grown, el("@e2", "Free", "radio", 600, 818), el("@e3", "Pro", "radio", 600, 918), el("@e4", "Team", "radio", 600, 1018)}
	text := diffText(diffMaps(before, after), len(after))
	lines := strings.Split(text, "\n")
	if len(lines) != 2 {
		t.Fatalf("want the box and one line for the rest:\n%s", text)
	}
	if !strings.HasPrefix(lines[0], "~ @e1 Accept terms (checkbox, checked) — was unchecked, resized from 100x40") {
		t.Errorf("the box: %q", lines[0])
	}
	if lines[1] != "~ 3 elements moved together, down 18px: @e2 Free, @e3 Pro, @e4 Team" {
		t.Errorf("the rest: %q", lines[1])
	}
}

// A segment chosen is a change, said both ways: NetNewsWire's search scope,
// Here and All Articles, where a tap moves the selection from one to the
// other and nothing else about either button changes.
func TestDiffMapsSaysWhatWasSelected(t *testing.T) {
	sel := func(e ElementView) ElementView { e.Selected = true; return e }
	before := []ElementView{sel(el("@e1", "Here", "button", 100, 60)), el("@e2", "All Articles", "button", 300, 60)}
	after := []ElementView{el("@e1", "Here", "button", 100, 60), sel(el("@e2", "All Articles", "button", 300, 60))}
	text := diffText(diffMaps(before, after), len(after))
	for _, want := range []string{"~ @e1 Here (button) — was selected",
		"~ @e2 All Articles (button, selected) — was not selected"} {
		if !strings.Contains(text, want) {
			t.Errorf("the text has no %q:\n%s", want, text)
		}
	}
}
