package uitree

import (
	"compress/gzip"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

// radiolab is Pocket Casts' Radiolab page on the iPhone, read in full: 2,847
// elements, most of them the 673 rows of its episode list. Compressed, as
// the one capture that size.
func radiolab(t *testing.T) []byte {
	t.Helper()
	f, err := os.Open("testdata/ios26-pocketcasts-radiolab.xml.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// What geometry says of a read without visible, against what iOS said of
// the same screen with it. It may miss a covered element — that is why a
// tree read this way is marked — but it must never hide one iOS shows.
// CHALLENGES 258.
func TestInferredVisibilityAgreesWithIOS(t *testing.T) {
	raw := radiolab(t)
	full, err := ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	light, err := ParseIOS(regexp.MustCompile(` visible="(true|false)"`).ReplaceAll(raw, nil))
	if err != nil {
		t.Fatal(err)
	}
	if full.Count() < HugeScreen || light.Count() != full.Count() {
		t.Fatalf("the capture has %d elements, light %d; want the same, and at least %d", full.Count(), light.Count(), HugeScreen)
	}
	light.InferVisibility()
	if !light.VisibilityInferred {
		t.Error("the tree is not marked as inferred")
	}
	var fullNodes, lightNodes []*Node
	full.Walk(func(n *Node) bool { fullNodes = append(fullNodes, n); return true })
	light.Walk(func(n *Node) bool { lightNodes = append(lightNodes, n); return true })
	agree, covered := 0, 0
	for i, f := range fullNodes {
		l := lightNodes[i]
		switch {
		case f.Displayed == l.Displayed:
			agree++
		case f.Displayed && !l.Displayed:
			t.Errorf("%s %q at %v is shown to iOS and hidden by geometry", f.Class, f.Label, f.Bounds)
		default:
			covered++
		}
	}
	// 31 measured, every one covered: under the mini player and the tab bar,
	// or inside the description iOS clips.
	if covered > 31 {
		t.Errorf("geometry agreed on %d of %d, and showed %d iOS hid", agree, len(fullNodes), covered)
	}
	// And map, on it, lists the screen and not the list below it.
	for _, e := range light.Map() {
		if e.Node.Bounds.Y1 > 932 {
			t.Errorf("map lists %q at %v, below the screen", e.Label, e.Node.Bounds)
		}
	}
}

// A page in a WebView, under two wrappers at 0,0 of its size, is where its
// WebView is: Radiolab's description is at 16,500 on screen, and moved again
// as content drawn by another process it read as 32,1000, below the screen,
// in every read. The share sheet, which is drawn that way, still moves
// (TestIOSShareSheetContentIsOnScreenWhereItIsDrawn). CHALLENGES 259.
func TestContentAlreadyOnScreenIsNotMovedAgain(t *testing.T) {
	tree, err := ParseIOS(radiolab(t))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	tree.Walk(func(n *Node) bool {
		if n.Class == "XCUIElementTypeStaticText" && strings.HasPrefix(n.Label, "Radiolab is on a curiosity bender") {
			found = true
			if n.Bounds.X1 != 16 || n.Bounds.Y1 != 500 {
				t.Errorf("the description is at %v, want where iOS put it, 16,500", n.Bounds)
			}
		}
		return true
	})
	if !found {
		t.Fatal("no description in the capture")
	}
}
