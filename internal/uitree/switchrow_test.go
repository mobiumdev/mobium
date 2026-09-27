package uitree

import (
	"strings"
	"testing"
)

func mapLines(t *testing.T, fixture string) []string {
	t.Helper()
	var lines []string
	for _, e := range loadTree(t, fixture).Map() {
		lines = append(lines, e.Line())
	}
	return lines
}

func findLine(lines []string, prefix string) string {
	for _, l := range lines {
		if strings.Contains(l, " "+prefix) {
			return l
		}
	}
	return ""
}

// Android's Settings draws a row as a clickable layout holding its title and
// a nameless, unclickable switch that carries the state. map listed both —
// the row with no state, the switch named "switchWidget" — and now lists one
// control, named by the row and carrying the switch's state (CHALLENGES 120).
func TestASettingsRowIsOneSwitch(t *testing.T) {
	for fixture, rows := range map[string][]string{
		"settings-display-uia2.xml": {"Bold text", "High contrast text"},
		"settings-color-uia2.xml":   {"Remove animations", "Large mouse pointer"},
	} {
		lines := mapLines(t, fixture)
		for _, l := range lines {
			if strings.Contains(l, "switchWidget") {
				t.Errorf("%s: a switch is still named for its resource id: %s", fixture, l)
			}
		}
		for _, row := range rows {
			l := findLine(lines, row)
			if !strings.Contains(l, "(switch, unchecked)") {
				t.Errorf("%s: %q maps as %q, want one switch entry with its state", fixture, row, l)
			}
		}
	}
}

// A row that is two controls stays two: Dark theme's row opens a page and its
// own named, clickable switch toggles it.
func TestARowWithTwoControlsIsNotFolded(t *testing.T) {
	lines := mapLines(t, "settings-color-uia2.xml")
	button, toggle := 0, 0
	for _, l := range lines {
		if strings.Contains(l, " Dark theme") {
			if strings.HasSuffix(l, "(button)") {
				button++
			}
			if strings.Contains(l, "(switch,") {
				toggle++
			}
		}
	}
	if button != 1 || toggle != 1 {
		t.Errorf("Dark theme: %d button and %d switch entries, want one of each:\n%s", button, toggle, strings.Join(lines, "\n"))
	}
}
