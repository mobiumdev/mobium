package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// NetNewsWire's Add Feed sheet maps its URL field as "URL (input)": that is
// the placeholder, which is the field's text, and the field has no label.
// label=URL was refused with "the keyboard may be covering it", a remedy
// that cannot work for a field at the top of the screen; the refusal now
// names text=URL, which does, and is not a miss to scroll or hide the
// keyboard for.
func TestAMissUnderOtherWordsNamesTheLocatorThatWorks(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/ios26-netnewswire-add-feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := uitree.ParseLocator("label=URL")
	_, err = pickOne(loc, tree)
	if mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement || !strings.Contains(err.Error(), "but text=URL does") {
		t.Fatalf("label=URL: %v", err)
	}
	if !isNearMiss(err) || matchedNothing(err) {
		t.Errorf("a near miss is treated as an element not on screen")
	}
	alt, _ := uitree.ParseLocator("text=URL")
	if _, err := pickOne(alt, tree); err != nil {
		t.Errorf("the locator the remedy names does not resolve: %v", err)
	}
	// The positive control: words nothing on screen carries are a plain
	// miss, still scrolled for.
	none, _ := uitree.ParseLocator("label=Nothing Here At All")
	if _, err := pickOne(none, tree); isNearMiss(err) || !matchedNothing(err) {
		t.Errorf("a plain miss: %v", err)
	}
}
