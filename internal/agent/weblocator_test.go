package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
	"github.com/mobiumdev/mobium/internal/webview"
)

// A locator inside a WebView matches what the page map reads of each
// element, by a native locator's rules — it was refused outright, so a test
// file, which has no refs, could not touch a page.
func TestALocatorFindsAPageElement(t *testing.T) {
	els := []webview.Element{
		{Tag: "button", Label: "Save", X: 10, Y: 20, W: 100, H: 20},
		{Tag: "button", Label: "Save draft", X: 10, Y: 60, W: 100, H: 20},
		{Tag: "a", Label: "Learn more", TestID: "more", X: 10, Y: 100, W: 100, H: 20},
		{Tag: "button", Label: "Delete", X: 10, Y: 140, W: 100, H: 20},
		{Tag: "button", Label: "Delete", X: 10, Y: 180, W: 100, H: 20},
	}
	h := NewHandlers()
	s := &session{dev: fakeDevice(), driver: &hostDriver{}, backend: BackendUIA2,
		web: &layoutPage{els: els}, webCtx: "WEBVIEW_test"}
	find := func(target string) (webview.Element, error) {
		_, e, _, err := h.findWeb(context.Background(), s, target)
		return e, err
	}
	for target, want := range map[string]string{
		"text=Save":             "Save", // the whole match among a part
		`text="Save draft"`:     "Save draft",
		"testid=more":           "Learn more",
		"role=link":             "Learn more",
		"text=Delete,nth=2":     "Delete",
		"label=Learn,role=link": "Learn more",
	} {
		e, err := find(target)
		if err != nil || e.Label != want {
			t.Errorf("%s: got %q, %v; want %q", target, e.Label, err, want)
		}
	}
	if e, _ := find("text=Delete,nth=2"); e.Y != 180 {
		t.Errorf("nth=2 counted down the page to y %v, want 180", e.Y)
	}
	if _, err := find("text=Delete"); mobiumerr.CodeOf(err) != mobiumerr.AmbiguousLocator || !strings.Contains(err.Error(), "2 elements") {
		t.Errorf("two Deletes: %v", err)
	}
	if _, err := find("text=Nowhere"); mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement {
		t.Errorf("nothing: %v", err)
	}
	if _, err := find("label=Delete within text=Save"); mobiumerr.CodeOf(err) != mobiumerr.Unsupported ||
		!strings.Contains(err.Error(), "css=") {
		t.Errorf("within in a page: %v", err)
	}
}

// Look-alikes in frames are told apart by their frame in a refusal, and a
// selector written for a frame's own document, without the frame, is
// answered with the path that reaches what it matched — MobiumApp's
// three frames each hold a "Tap me".
func TestAFramedMatchIsNamedByItsFrame(t *testing.T) {
	els := []webview.Element{
		{Tag: "button", Label: "Tap me", Frame: "iframe#a", X: 10, Y: 20, W: 100, H: 20},
		{Tag: "button", Label: "Tap me", Frame: "iframe#a >> iframe#b", X: 10, Y: 60, W: 100, H: 20},
	}
	h := NewHandlers()
	s := &session{dev: fakeDevice(), driver: &hostDriver{}, backend: BackendUIA2,
		web: &layoutPage{els: els}, webCtx: "WEBVIEW_test"}
	_, _, _, err := h.findWeb(context.Background(), s, `text="Tap me"`)
	if mobiumerr.CodeOf(err) != mobiumerr.AmbiguousLocator ||
		!strings.Contains(err.Error(), `"Tap me" (button) in iframe#a >> iframe#b`) {
		t.Errorf("two framed look-alikes: %v", err)
	}
	got := framedHint(els, []int{1}, "#tap")
	if !strings.Contains(got, "css=iframe#a >> iframe#b >> #tap") {
		t.Errorf("a selector without its frame: %q", got)
	}
	if got := framedHint(els, []int{0}, "iframe#x >> #tap"); !strings.Contains(got, "css=iframe#a >> #tap") {
		t.Errorf("a selector with the wrong frame: %q", got)
	}
	if framedHint(els, nil, "#tap") != "" {
		t.Error("a hint with nothing elsewhere")
	}
}

// A page element with no id and no test id is given the locator its whole
// text names it by, narrowed by role where the text names several — map
// gave it only a ref, and a recording wrote the ref into a test.
func TestAPageElementWithNoIDGetsALocator(t *testing.T) {
	els := []webview.Element{
		{Tag: "a", Label: "Learn more"},
		{Tag: "a", Label: "More"},
		{Tag: "button", Label: "More"},
		{Tag: "button", Label: "Save"},
		{Tag: "button", Label: "Save"},
		{Tag: "div"},
	}
	for i, want := range []string{`text="Learn more"`, `text="More",role=link`, `text="More",role=button`, "", "", ""} {
		got := ""
		if v := deriveWebLocator(els, i); v != nil {
			got = uitree.Locator{Kind: uitree.Kind(v.Kind), Value: v.Value, Exact: v.Exact, Role: v.Role}.String()
		}
		if got != want {
			t.Errorf("element %d (%q): %q, want %q", i, els[i].Label, got, want)
		}
	}
}
