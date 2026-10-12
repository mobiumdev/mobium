package uitree

import (
	"strconv"
	"testing"
)

func TestWithinParses(t *testing.T) {
	l, err := ParseLocator(`label=More within text=Card 8`)
	if err != nil || l.Kind != KindLabel || l.Value != "More" || l.Within == nil ||
		l.Within.Kind != KindText || l.Within.Value != "Card 8" {
		t.Fatalf("got %+v, %v", l, err)
	}
	// Inside quotes the word is part of the value.
	l, err = ParseLocator(`text="Work within limits"`)
	if err != nil || l.Within != nil || l.Value != "Work within limits" {
		t.Errorf("a quoted value was split: %+v, %v", l, err)
	}
	// Each side is a whole locator, role and quotes included, and the anchor
	// may be scoped in turn.
	l, err = ParseLocator(`text="Delete",role=button within label="Draft 2" within testid=drafts`)
	if err != nil || !l.Whole || l.Role != "button" || l.Within == nil || !l.Within.Whole || l.Within.Within == nil {
		t.Errorf("got %+v, %v", l, err)
	}
	for _, bad := range []string{`label=More within `, ` within text=Card 8`, `label=More within role=nosuchrole`} {
		if _, err := ParseLocator(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	// It prints as it parses.
	for _, s := range []string{`label=More within text=Card 8`, `text="Delete",role=button within label="Draft 2"`} {
		l, _ := ParseLocator(s)
		if l.String() != s {
			t.Errorf("%s printed as %s", s, l)
		}
	}
}

// Ice Cubes' timeline: two posts, each with its own Reply (CHALLENGES 295's
// look-alikes). Scoped by the author, each Reply is its post's.
func TestWithinFindsTheRowsOwn(t *testing.T) {
	tree := loadIOS(t, "ios26-icecubes-timeline.xml")
	one := func(s string) *Node {
		t.Helper()
		l, err := ParseLocator(s)
		if err != nil {
			t.Fatal(err)
		}
		got := l.Resolve(tree)
		if len(got) != 1 {
			t.Fatalf("%s matched %d", s, len(got))
		}
		return got[0]
	}
	if l, _ := ParseLocator("label=Reply"); len(l.Resolve(tree)) != 2 {
		t.Fatalf("the fixture no longer has two Replies")
	}
	first, second := one("label=Reply within label=Author One"), one("label=Reply within label=Author Two")
	if first == second || first.Bounds.Y1 >= second.Bounds.Y1 {
		t.Errorf("Author One's Reply at %v, Author Two's at %v", first.Bounds, second.Bounds)
	}
}

func androidList(t *testing.T, rows string, wrapped bool) *Tree {
	t.Helper()
	return androidListAs(t, rows, wrapped, `class="androidx.recyclerview.widget.RecyclerView" scrollable="true"`)
}

func androidListAs(t *testing.T, rows string, wrapped bool, list0 string) *Tree {
	t.Helper()
	list := `<node index="0" ` + list0 + ` bounds="[0,0][1080,2000]" displayed="true" enabled="true">` + rows + `</node>`
	if wrapped {
		list = `<node index="0" class="android.widget.ScrollView" scrollable="true" bounds="[0,0][1080,2000]" displayed="true" enabled="true">` +
			`<node index="0" class="android.view.ViewGroup" bounds="[0,0][1080,4000]" displayed="true" enabled="true">` + rows + `</node></node>`
	}
	tree, err := ParseAndroid([]byte(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]" displayed="true" enabled="true">` +
		list + `</node></hierarchy>`))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func row(i int, title string, more bool) string {
	y := 100 + i*200
	r := `<node index="` + strconv.Itoa(i) + `" class="android.widget.LinearLayout" bounds="[0,` + strconv.Itoa(y) + `][1080,` + strconv.Itoa(y+200) + `]" displayed="true" enabled="true">` +
		`<node index="0" class="android.widget.TextView" text="` + title + `" bounds="[0,` + strconv.Itoa(y) + `][800,` + strconv.Itoa(y+200) + `]" displayed="true" enabled="true"/>`
	if more {
		r += `<node index="1" class="android.widget.ImageButton" content-desc="More" clickable="true" bounds="[900,` + strconv.Itoa(y) + `][1080,` + strconv.Itoa(y+200) + `]" displayed="true" enabled="true"/>`
	}
	return r + `</node>`
}

// A row without the target finds nothing — never the target of the row
// beside it — in a native list and in React Native's wrapper alike.
func TestWithinStopsAtTheRow(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		tree := androidList(t, row(0, "Card 7", true)+row(1, "Card 8", false)+row(2, "Card 9", true), wrapped)
		count := func(s string) int {
			l, err := ParseLocator(s)
			if err != nil {
				t.Fatal(err)
			}
			return len(l.Resolve(tree))
		}
		if n := count(`label=More within "Card 9"`); n != 1 {
			t.Errorf("wrapped=%v: Card 9's More matched %d", wrapped, n)
		}
		if n := count(`label=More within "Card 8"`); n != 0 {
			t.Errorf("wrapped=%v: Card 8 has no More, and %d were found — another row's", wrapped, n)
		}
		if n := count(`label=More within text=Card`); n != 2 {
			t.Errorf("wrapped=%v: an anchor in several rows found %d, want each row's own, 2", wrapped, n)
		}
	}
}

// A list that fits on screen is not marked scrollable — Settings' Network &
// internet found Airplane mode's switch for Hotspot & tethering, measured —
// and a Compose column is no list by class at all. Neither lets a row
// without the target borrow its neighbor's.
func TestWithinStopsAtARowNothingNamesAList(t *testing.T) {
	rows := row(0, "Card 7", true) + row(1, "Card 8", false) + row(2, "Card 9", true)
	for name, list := range map[string]string{
		"a RecyclerView that fits": `class="androidx.recyclerview.widget.RecyclerView" scrollable="false"`,
		"a plain column":           `class="android.view.View"`,
	} {
		tree := androidListAs(t, rows, false, list)
		l, _ := ParseLocator(`label=More within "Card 8"`)
		if n := len(l.Resolve(tree)); n != 0 {
			t.Errorf("%s: Card 8 has no More, and %d were found — another row's", name, n)
		}
		l, _ = ParseLocator(`label=More within "Card 9"`)
		if n := len(l.Resolve(tree)); n != 1 {
			t.Errorf("%s: Card 9's More matched %d", name, n)
		}
	}
}
