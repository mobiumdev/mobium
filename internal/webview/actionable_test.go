package webview

import (
	"context"
	"strings"
	"testing"
)

// cannedPage answers every Evaluate with one canned result and keeps the script.
type cannedPage struct {
	answer string
	got    string
}

func (f *cannedPage) Evaluate(ctx context.Context, expr string) (string, error) {
	f.got = expr
	return f.answer, nil
}
func (f *cannedPage) LayoutMetrics(ctx context.Context) (*Metrics, error) { return nil, nil }
func (f *cannedPage) Map(ctx context.Context) ([]Element, error)          { return nil, nil }
func (f *cannedPage) Text(ctx context.Context) (string, error)            { return "", nil }
func (f *cannedPage) Healthy(ctx context.Context) bool                    { return true }
func (f *cannedPage) Close() error                                        { return nil }

// The check picks its element by index from the very list map builds, so a
// ref and the element checked for it can never disagree.
func TestTheActionabilityCheckUsesMapsCandidates(t *testing.T) {
	f := &cannedPage{answer: `{"status":"ok","x":1,"y":2,"w":3,"h":4,"px":2.5,"py":4,"moved":true,"cover":"half cover"}`}
	a, err := CheckActionable(context.Background(), f, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.got, "__mobiumCandidates()[7]") || !strings.Contains(f.got, candidatesJS) {
		t.Errorf("the script does not pick candidate 7 from map's list:\n%s", f.got[:200])
	}
	if !strings.Contains(mapScript, candidatesJS) {
		t.Error("map no longer builds from the shared candidate list")
	}
	if a.Status != "ok" || !a.Moved || a.Cover != "half cover" || a.PX != 2.5 {
		t.Errorf("parsed %+v", a)
	}
}

// A page answer that is not JSON is a device-server error, named, not a panic
// or a silent success.
func TestAnUnreadableActionabilityAnswerIsAnError(t *testing.T) {
	if _, err := CheckActionable(context.Background(), &cannedPage{answer: "undefined"}, 0); err == nil {
		t.Error("an unreadable answer was accepted")
	}
}
