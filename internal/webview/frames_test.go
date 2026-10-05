package webview

import (
	"context"
	"fmt"
	"strings"
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

// A frame on the page is paired with its frame tree entry by name where both
// have one — WebKit names a frame by its element's id when it has no name —
// and the rest in order. Chrome names a frame only by its name attribute, so
// MobiumApp's id-only frames pair by order there. CHALLENGES 229.
func TestFramesPairByNameThenOrder(t *testing.T) {
	doc := []pageFrame{{Path: "iframe#sameFrame", Name: "sameFrame"}, {Path: "iframe#crossFrame", Name: "crossFrame"}}
	webkit := []frameCtx{{ID: "0.3", Name: "sameFrame"}, {ID: "0.7", Name: "crossFrame"}}
	if p := pairFrames(doc, webkit); p[0].ID != "0.3" || p[1].ID != "0.7" {
		t.Errorf("by name: %+v", p)
	}
	chrome := []frameCtx{{ID: "A"}, {ID: "B"}}
	if p := pairFrames(doc, chrome); p[0].ID != "A" || p[1].ID != "B" {
		t.Errorf("by order: %+v", p)
	}
	// Names that disagree in order still pair by name.
	swapped := []frameCtx{{ID: "0.7", Name: "crossFrame"}, {ID: "0.3", Name: "sameFrame"}}
	if p := pairFrames(doc, swapped); p[0].ID != "0.3" || p[1].ID != "0.7" {
		t.Errorf("names out of order: %+v", p)
	}
	// Counts that do not match leave the unnamed unpaired rather than guessed.
	if p := pairFrames([]pageFrame{{}, {}}, []frameCtx{{ID: "A"}}); len(p) != 0 {
		t.Errorf("one tree frame for two unnamed page frames paired %+v", p)
	}
}

// Each protocol says which frame a context belongs to in its own place: CDP
// in auxData, with isDefault marking the frame's main world; WebKit beside
// the id, with type "normal".
func TestAContextKnowsItsFrame(t *testing.T) {
	var cdp, cdpIsolated, webkit, webkitUser contextCreated
	cdp.Context.AuxData.FrameID, cdp.Context.AuxData.IsDefault = "F", true
	cdpIsolated.Context.AuxData.FrameID = "F"
	webkit.Context.FrameID, webkit.Context.Type = "0.7", "normal"
	webkitUser.Context.FrameID, webkitUser.Context.Type = "0.7", "user"
	for _, c := range []struct {
		c    contextCreated
		want string
	}{{cdp, "F"}, {cdpIsolated, ""}, {webkit, "0.7"}, {webkitUser, ""}} {
		if got := c.c.frameOf(); got != c.want {
			t.Errorf("%+v: frame %q, want %q", c.c.Context, got, c.want)
		}
	}
}

// fakeFrames is a page with one cross-origin frame: its own map has the
// page's button, its frames script reports the closed frame at (10, 200),
// and the frame's context maps a field at (5, 6).
type fakeFrames struct{ inFrame []string }

func (f *fakeFrames) Evaluate(ctx context.Context, expression string) (string, error) {
	switch {
	case strings.Contains(expression, "children: open"):
		return `[{"path":"iframe#crossFrame","name":"crossFrame","closed":true,"shown":true,"x":10,"y":200,"children":[]}]`, nil
	case strings.Contains(expression, "tag: e.tagName"):
		return `[{"tag":"button","id":"pageBtn","label":"A button","x":1,"y":2,"w":3,"h":4}]`, nil
	}
	return "", nil
}

func (f *fakeFrames) frameContexts(ctx context.Context) ([]frameCtx, error) {
	return []frameCtx{{ID: "main", Context: 1}, {ID: "0.7", ParentID: "main", Name: "crossFrame", Context: 4}}, nil
}

func (f *fakeFrames) evaluateIn(ctx context.Context, id int, expression string) (string, error) {
	f.inFrame = append(f.inFrame, fmt.Sprint(id))
	return `[{"tag":"input","id":"crossField","label":"Card number","x":5,"y":6,"w":100,"h":20}]`, nil
}

// A cross-origin frame's elements are mapped in its own context and placed
// in the page: offset by where the frame's content is, named by the frame,
// and routed to the context for acting on. CHALLENGES 229.
func TestACrossOriginFrameIsMappedInItsContext(t *testing.T) {
	f := &fakeFrames{}
	els, unreached, err := mapAll(context.Background(), f)
	if err != nil || unreached != 0 || len(els) != 2 {
		t.Fatalf("mapped %d, unreached %d (%v), want the page's and the frame's", len(els), unreached, err)
	}
	field := els[1]
	if field.X != 15 || field.Y != 206 || field.Frame != "iframe#crossFrame" {
		t.Errorf("the frame's field is at (%g, %g) in %q, want (15, 206) in iframe#crossFrame", field.X, field.Y, field.Frame)
	}
	if field.context != 4 || field.local != 0 || field.box != "iframe#crossFrame" {
		t.Errorf("the field routes to context %d index %d via %q", field.context, field.local, field.box)
	}
	if field.Selector() != "iframe#crossFrame >> #crossField" {
		t.Errorf("selector %q", field.Selector())
	}
	if len(f.inFrame) != 1 || f.inFrame[0] != "4" {
		t.Errorf("evaluated in contexts %v, want the frame's, 4", f.inFrame)
	}
}
