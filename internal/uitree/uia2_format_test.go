package uitree

import (
	"regexp"
	"strings"
	"testing"
)

// launcher-uia2.xml is the same Pixel 7 home screen as launcher.xml, captured
// through the UiAutomator2 server instead of `uiautomator dump`. The two files
// use different element names and UiAutomator2 omits empty attributes, so this
// is where a parser that only understood one of them would show up.

func TestUIA2FormatParses(t *testing.T) {
	tree := loadFixture(t, "launcher-uia2.xml")
	if len(tree.All()) == 0 {
		t.Fatal("parsed no nodes from the UiAutomator2 source")
	}

	// Element names are class names in this format; the class must still land
	// in the right field.
	var gmail *Node
	for _, n := range tree.All() {
		if n.Label == "Gmail" {
			gmail = n
		}
	}
	if gmail == nil {
		t.Fatal("Gmail icon not found")
	}
	if gmail.ShortClass() != "TextView" {
		t.Errorf("class = %q, want TextView", gmail.ShortClass())
	}
	if !gmail.Clickable {
		t.Error("clickable attribute not parsed")
	}
	if gmail.Bounds.Empty() {
		t.Error("bounds not parsed")
	}
}

func TestBothFormatsProduceTheSameMap(t *testing.T) {
	dump := loadFixture(t, "launcher.xml")
	uia2 := loadFixture(t, "launcher-uia2.xml")

	// The two captures straddled midnight, so the smartspace date widget
	// legitimately reads differently ("Fri, Sep 11" vs "Sat, Sep 12"). That
	// is the screen changing, not the parsers disagreeing, so the clock line
	// is normalized and everything else compared exactly.
	dateLine := regexp.MustCompile(`^(@e\d+) \w{3}, \w{3} \d+ \(button\)$`)
	lines := func(tr *Tree) []string {
		var out []string
		for _, e := range tr.Map() {
			line := e.Line()
			if m := dateLine.FindStringSubmatch(line); m != nil {
				line = m[1] + " <date> (button)"
			}
			out = append(out, line)
		}
		return out
	}
	a, b := lines(dump), lines(uia2)
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("the two backends disagree about the same screen:\n"+
			"uiautomator dump:\n%s\n\nUiAutomator2:\n%s",
			strings.Join(a, "\n"), strings.Join(b, "\n"))
	}
}

func TestDisplayedDefaultsTrueWithoutTheAttribute(t *testing.T) {
	// The dump format has no `displayed` attribute. Defaulting it to false
	// there would filter every element off the screen.
	dump := loadFixture(t, "launcher.xml")
	for _, n := range dump.All() {
		if !n.Displayed {
			t.Fatalf("node %q defaulted to not displayed", describe(n))
		}
	}

	// UiAutomator2 does report it, and it must survive.
	uia2 := loadFixture(t, "launcher-uia2.xml")
	found := false
	for _, n := range uia2.All() {
		if n.Displayed {
			found = true
			break
		}
	}
	if !found {
		t.Error("no node reported displayed in the UiAutomator2 source")
	}
}

func TestParseRejectsNonXML(t *testing.T) {
	for _, bad := range []string{"", "not xml at all", "<other/>"} {
		if _, err := ParseAndroid([]byte(bad)); err == nil {
			t.Errorf("ParseAndroid(%q) did not error", bad)
		}
	}
}
