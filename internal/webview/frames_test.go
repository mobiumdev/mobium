package webview

import (
	"context"
	"testing"
)

// An element inside a frame is named by its frame as well: CSS does not
// cross a frame, and the same id can be in a frame and on the page.
// CHALLENGES 228.
func TestASelectorNamesTheFrame(t *testing.T) {
	for _, c := range []struct {
		e    Element
		want string
	}{
		{Element{ID: "pageBtn"}, "#pageBtn"},
		{Element{ID: "sameBtn", Frame: "iframe#sameFrame"}, "iframe#sameFrame >> #sameBtn"},
		{Element{TestID: "go", Frame: "iframe#sameFrame >> iframe#nestedFrame"},
			`iframe#sameFrame >> iframe#nestedFrame >> [data-testid="go"]`},
		{Element{Frame: "iframe[0]"}, ""},
	} {
		if got := c.e.Selector(); got != c.want {
			t.Errorf("%+v: selector %q, want %q", c.e, got, c.want)
		}
	}
	page, frame := Element{ID: "x"}, Element{ID: "x", Frame: "iframe#f"}
	if page.Selector() == frame.Selector() {
		t.Error("an element on the page and one of the same id in a frame have one selector")
	}
}

type answers string

func (a answers) Evaluate(ctx context.Context, expression string) (string, error) {
	return string(a), nil
}

// The count of closed frames is read however the transport quotes it.
func TestClosedFramesReadsTheCount(t *testing.T) {
	for raw, want := range map[string]int{`1`: 1, `"2"`: 2, `"0"`: 0} {
		n, err := ClosedFrames(context.Background(), answers(raw))
		if err != nil || n != want {
			t.Errorf("%s: %d (%v), want %d", raw, n, err, want)
		}
	}
}
