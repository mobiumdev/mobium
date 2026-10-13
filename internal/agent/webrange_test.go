package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/webview"
)

// rangePage answers every script with one canned answer and keeps the last.
type rangePage struct {
	layoutPage
	answer string
	got    string
}

func (p *rangePage) Evaluate(ctx context.Context, expr string) (string, error) {
	p.got = expr
	return p.answer, nil
}

func fillRange(t *testing.T, p *rangePage, text string, replace bool) (*ToolsCallResult, error) {
	t.Helper()
	h := NewHandlers()
	h.implicitWait = 0
	s := &session{dev: fakeDevice(), driver: &hostDriver{}, backend: BackendUIA2, web: p, webCtx: "WEBVIEW_test"}
	el := webview.Element{Tag: "input", Type: "range", Label: "Range", TestID: "webRange", Value: "50"}
	return h.webFill(context.Background(), s, "testid=webRange", func(context.Context) (webview.Element, error) {
		return el, nil
	}, text, replace)
}

// A web range is a slider: filled with a position from 0 to 1 like a native
// one, and never typed into. fill 0.2 typed "0.2", the page clamped it to a
// step, and the failure said the page had refused it (CHALLENGES 319).
func TestAWebRangeIsFilledByPosition(t *testing.T) {
	p := &rangePage{answer: `{"status":"ok","value":"20","was":"50","want":"20","matches":true}`}
	res, err := fillRange(t, p, "0.2", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.got, "const pos = 0.2;") || strings.Contains(p.got, "const text") {
		t.Errorf("the page was not asked to move the range to 0.2:\n%.300s", p.got)
	}
	v := res.StructuredContent.(ActionView)
	if v.Value != "20" || v.Position == nil || *v.Position != 0.2 || !strings.Contains(textOf(res), `reads "20", was "50"`) {
		t.Errorf("result %q, %+v", textOf(res), v)
	}

	if _, err := fillRange(t, &rangePage{}, "20", true); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument ||
		!strings.Contains(err.Error(), "from 0") {
		t.Errorf("a value, not a position: %v", err)
	}
	if _, err := fillRange(t, &rangePage{}, "20", false); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument ||
		!strings.Contains(err.Error(), "app_fill") {
		t.Errorf("type on a range: %v", err)
	}
	p = &rangePage{answer: `{"status":"ok","value":"40","was":"50","want":"20","matches":false}`}
	if _, err := fillRange(t, p, "0.2", true); mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed ||
		!strings.Contains(err.Error(), "reads 40") {
		t.Errorf("a page that rewrote the value: %v", err)
	}
}
