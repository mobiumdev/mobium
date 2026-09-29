package mobiumdriver

import (
	"os"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// The phone's keyboard was Russian and the password Latin; this is the same
// case the other way round, on a captured English keyboard: every Cyrillic
// letter is keyless, no Latin one is, and digits are never counted — they are
// on another page of every layout, and they did arrive.
func TestKeylessCountsLettersTheKeyboardLacks(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/ios-keyboard-over-button.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Keyboard() == nil {
		t.Fatal("the fixture has no keyboard, so this test cannot fail")
	}
	cases := []struct {
		text string
		want int
	}{
		{"wrongpass1", 0},
		{"Hunter2", 0},
		{"пароль", 6},
		{"pass-пар1", 3},
		{"123456", 0},
	}
	for _, c := range cases {
		got, sample := keyless(tree, c.text)
		if got != c.want {
			t.Errorf("keyless(%q) = %d, want %d", c.text, got, c.want)
		}
		if sample == "" {
			t.Errorf("no keys named for %q", c.text)
		}
	}
}

// With no keyboard up nothing can be told, and nothing is claimed.
func TestKeylessSaysNothingWithoutAKeyboard(t *testing.T) {
	raw, err := os.ReadFile("testdata/springboard-banner-ios26.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := keyless(tree, "пароль"); n != 0 {
		t.Errorf("counted %d keyless letters with no keyboard", n)
	}
}
