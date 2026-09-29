package mobiumdriver

import (
	"strings"
	"unicode"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// keyless counts the letters of text that the keyboard on screen has no key
// for, and names a few of the keys it does have. A real iPhone types a
// password field key by key on whatever keyboard is up: with a Russian one,
// "wrongpass1" arrived as its one digit and "abc" as nothing, while an
// ordinary field took Latin text whole (CHALLENGES 159). Nothing of the text
// itself is returned — it is a password.
//
// Zero when there is no keyboard in the tree, or it shows no letter keys —
// the numbers page, say — since then nothing can be told.
func keyless(tree *uitree.Tree, text string) (missing int, sample string) {
	k := tree.Keyboard()
	if k == nil {
		return 0, ""
	}
	keys := map[rune]bool{}
	var shown []string
	var walk func(n *uitree.Node)
	walk = func(n *uitree.Node) {
		if r := []rune(n.Label); n.Class == "XCUIElementTypeKey" && len(r) == 1 && unicode.IsLetter(r[0]) {
			keys[unicode.ToLower(r[0])] = true
			if len(shown) < 3 {
				shown = append(shown, n.Label)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(k)
	if len(keys) == 0 {
		return 0, ""
	}
	for _, r := range text {
		if unicode.IsLetter(r) && !keys[unicode.ToLower(r)] {
			missing++
		}
	}
	return missing, strings.Join(shown, ", ")
}
