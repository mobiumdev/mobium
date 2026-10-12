package uitree

import (
	"strconv"
	"testing"
)

func TestNthParses(t *testing.T) {
	for in, want := range map[string]Locator{
		`text=Delete,nth=2`:                   {Kind: KindText, Value: "Delete", Nth: 2},
		`text="Delete",role=button,nth=3`:     {Kind: KindText, Value: "Delete", Whole: true, Role: "button", Nth: 3},
		`text="a,nth=2"`:                      {Kind: KindText, Value: "a,nth=2", Whole: true},
		`label=More,nth=1 within text=Card 8`: {Kind: KindLabel, Value: "More", Nth: 1},
	} {
		got, err := ParseLocator(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		got.Within = nil
		if got != want {
			t.Errorf("%s: got %+v, want %+v", in, got, want)
		}
	}
	for _, bad := range []string{`text=Delete,nth=0`, `text=Delete,nth=two`, `text=Delete,nth=-1`} {
		if _, err := ParseLocator(bad); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
	for _, s := range []string{`text="Delete",role=button,nth=2`, `label=More,nth=2 within text=Card,nth=3`} {
		if l, _ := ParseLocator(s); l.String() != s {
			t.Errorf("%s printed as %s", s, l)
		}
	}
}

// Counted as a person reads the screen, shown only: a hidden or sizeless
// match is not one of them, and past the last is nothing.
func TestNthCountsWhatIsShownInReadingOrder(t *testing.T) {
	node := func(x, y int, shown bool) string {
		return `<node index="0" class="android.widget.Button" text="Delete" clickable="true" bounds="[` + strconv.Itoa(x) + `,` +
			strconv.Itoa(y) + `][` + strconv.Itoa(x+100) + `,` + strconv.Itoa(y+100) + `]" displayed="` + strconv.FormatBool(shown) + `" enabled="true"/>`
	}
	tree, err := ParseAndroid([]byte(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]" displayed="true" enabled="true">` +
		// Document order is not reading order: the lower one comes first.
		node(0, 600, true) + node(500, 300, true) + node(0, 300, true) + node(0, 900, false) +
		`</node></hierarchy>`))
	if err != nil {
		t.Fatal(err)
	}
	at := func(n int) string {
		l := Locator{Kind: KindText, Value: "Delete", Nth: n}
		got := l.Resolve(tree)
		if len(got) == 0 {
			return "none"
		}
		return strconv.Itoa(got[0].Bounds.X1) + "," + strconv.Itoa(got[0].Bounds.Y1)
	}
	for n, want := range map[int]string{1: "0,300", 2: "500,300", 3: "0,600", 4: "none"} {
		if got := at(n); got != want {
			t.Errorf("nth=%d is at %s, want %s", n, got, want)
		}
	}
}
