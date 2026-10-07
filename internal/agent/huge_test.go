package agent

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// hugeRadiolab is Pocket Casts' Radiolab page as the driver reads it once it
// is known to be too large: without visible, shown worked out from geometry.
func hugeRadiolab(t *testing.T) *uitree.Tree {
	t.Helper()
	f, err := os.Open("../uitree/testdata/ios26-pocketcasts-radiolab.xml.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseIOS(regexp.MustCompile(` visible="(true|false)"`).ReplaceAll(raw, nil))
	if err != nil {
		t.Fatal(err)
	}
	tree.InferVisibility()
	return tree
}

// On a screen too large for iOS to say what is shown, an action does not
// scroll for its target: "Search episodes", under the tab bar, swiped the
// episode list to its end and on, steering by where an off-screen element
// was reported. It is refused, naming what works, and nothing is touched.
// One in view is tapped as ever. CHALLENGES 258.
func TestAnActionDoesNotScrollOnAHugeScreen(t *testing.T) {
	h, s, f := withFake(t, hugeRadiolab(t))
	h.implicitWait = 300 * time.Millisecond
	_, err := h.tapOn(context.Background(), s, map[string]interface{}{"target": "label=Search episodes"})
	if err == nil || len(f.tapped) != 0 {
		t.Fatalf("Search episodes, under the tab bar, was tapped (%d taps): %v", len(f.tapped), err)
	}
	// Not shown, or shown under the bars: either way not where a tap reaches.
	if code := mobiumerr.CodeOf(err); (code != mobiumerr.ElementNotReachable && code != mobiumerr.NoSuchElement) ||
		!strings.Contains(err.Error(), "app_swipe") {
		t.Errorf("refused as %s: %v", mobiumerr.CodeOf(err), err)
	}
	if _, err := h.tapOn(context.Background(), s, map[string]interface{}{"target": "label=Follow"}); err != nil {
		t.Fatalf("Follow, in view, was refused: %v", err)
	}
	if len(f.tapped) != 1 {
		t.Errorf("tapped %d times, want Follow once", len(f.tapped))
	}
}

// And a locator there matches only what is shown: an episode below the
// screen is in the tree, reported where it is not, and is no match.
func TestALocatorOnAHugeScreenMatchesWhatIsShown(t *testing.T) {
	tree := hugeRadiolab(t)
	loc, _ := uitree.ParseLocator("text=Radiolab Presents: Dolly Parton's America")
	if len(loc.Resolve(tree)) == 0 {
		t.Skip("the capture has no such episode")
	}
	if _, err := pickOne(loc, tree); mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement {
		t.Errorf("an episode below the screen resolved: %v", err)
	}
}
