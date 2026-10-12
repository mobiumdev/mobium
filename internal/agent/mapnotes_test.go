package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

func fixture(t *testing.T, name string) *uitree.Tree {
	t.Helper()
	data, err := os.ReadFile("../uitree/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	parse := uitree.ParseAndroid
	if strings.HasPrefix(name, "ios") {
		parse = uitree.ParseIOS
	}
	tree, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// A feed loading its next page is said to be loading — the screen MobiumApp's
// Feed Demo showed mid-load — and a screen with no list loading says nothing
// (CHALLENGES 303).
func TestLoadingNote(t *testing.T) {
	if loadingNote(fixture(t, "ios26-mobiumapp-feed-loading.xml")) == "" {
		t.Error("a feed loading its next page is not said to be loading")
	}
	if note := loadingNote(fixture(t, "mobiumapp-slider-uia2.xml")); note != "" {
		t.Errorf("a screen with nothing loading has a loading note: %s", note)
	}
}

// treeDriver answers every read with one tree.
type treeDriver struct {
	fakeDriver
	tree *uitree.Tree
}

func (d *treeDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) { return d.tree, nil }

// map's notes are in its structured answer, not only its text: Claude Code
// hands its model the structured answer alone, and a note in the text never
// reached an agent (CHALLENGES 303).
func TestMapNotesAreInTheStructuredAnswer(t *testing.T) {
	h := NewHandlers()
	s := &session{dev: fakeDevice(), driver: &treeDriver{tree: fixture(t, "ios26-mobiumapp-feed-loading.xml")}, backend: BackendUIA2}
	h.sessions = map[string]*session{s.dev.Serial: s}
	res, err := h.mapScreen(context.Background(), map[string]interface{}{"device": s.dev.Serial})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := res.StructuredContent.(MapView)
	if !ok {
		t.Fatalf("map's structured answer is %T", res.StructuredContent)
	}
	if len(view.Notes) == 0 || !strings.Contains(strings.Join(view.Notes, " "), "loading") {
		t.Errorf("the loading note is not in map's structured answer: %v", view.Notes)
	}
	if !strings.Contains(res.Content[0].Text, view.Notes[0]) {
		t.Error("the text no longer carries the note")
	}
}
