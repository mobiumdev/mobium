package webview

import (
	"context"
	"encoding/json"
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
func (f *cannedPage) Cookies(ctx context.Context) ([]Cookie, error)       { return nil, nil }
func (f *cannedPage) SetCookie(ctx context.Context, c Cookie) error       { return nil }
func (f *cannedPage) DeleteCookie(ctx context.Context, c Cookie) error    { return nil }
func (f *cannedPage) LayoutMetrics(ctx context.Context) (*Metrics, error) { return nil, nil }
func (f *cannedPage) Map(ctx context.Context) ([]Element, error)          { return nil, nil }
func (f *cannedPage) Text(ctx context.Context) (string, error)            { return "", nil }
func (f *cannedPage) Healthy(ctx context.Context) bool                    { return true }
func (f *cannedPage) Close() error                                        { return nil }

// The check picks its element by index from the very list map builds, so a
// ref and the element checked for it can never disagree.
func TestTheActionabilityCheckUsesMapsCandidates(t *testing.T) {
	f := &cannedPage{answer: `{"status":"ok","x":1,"y":2,"w":3,"h":4,"px":2.5,"py":4,"moved":true,"cover":"half cover"}`}
	a, err := CheckActionable(context.Background(), f, Element{local: 7})
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
	if _, err := CheckActionable(context.Background(), &cannedPage{answer: "undefined"}, Element{}); err == nil {
		t.Error("an unreadable answer was accepted")
	}
}

// A fill's answer never carries a field's value: a password is reported only
// as matching or not, which is what lets app_type confirm one without ever
// holding it in a result.
func TestAFillNeverSendsTheValueBack(t *testing.T) {
	f := &cannedPage{answer: `{"status":"ok","matches":true,"password":true}`}
	got, err := Fill(context.Background(), f, Element{local: 3}, `it's "quoted" & café`, false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Matches || !got.Password {
		t.Errorf("parsed %+v", got)
	}
	// The value reaches the page as one JSON string literal — quotes, & and
	// non-ASCII intact — decoded here rather than matched, since JSON may
	// spell & as \u0026.
	start := strings.Index(f.got, "const text = ") + len("const text = ")
	end := strings.Index(f.got[start:], ";\n") + start
	var decoded string
	if err := json.Unmarshal([]byte(f.got[start:end]), &decoded); err != nil || decoded != `it's "quoted" & café` {
		t.Errorf("the value reached the page as %s (decoded %q, %v)", f.got[start:end], decoded, err)
	}
	if strings.Contains(fillScript, "value: el.value") || strings.Contains(fillScript, "now,") {
		t.Error("the fill script sends a field's value back")
	}
	if !strings.Contains(f.got, "const append = false;") {
		t.Error("a fill did not reach the page as a replacement")
	}
	// app_type's fill keeps what the field held: the flag is what says so.
	if _, err := Fill(context.Background(), f, Element{local: 3}, "more", true); err != nil || !strings.Contains(f.got, "const append = true;") {
		t.Errorf("an append did not reach the page as one (%v)", err)
	}
}

// A checkbox's state is read from the page, by the same index map uses, and
// an element with no checked state is told apart from an unchecked one
// (CHALLENGES 299).
func TestCheckedReadsThePagesOwnState(t *testing.T) {
	f := &cannedPage{answer: `{"found":true,"kind":"checkbox","checked":true}`}
	st, err := Checked(context.Background(), f, Element{local: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.got, "__mobiumCandidates()[4]") || !strings.Contains(f.got, candidatesJS) {
		t.Errorf("the read does not pick its element from map's candidates by index:\n%s", f.got)
	}
	if !st.Found || st.Kind != "checkbox" || st.Checked == nil || !*st.Checked {
		t.Errorf("state = %+v, want a found, checked checkbox", st)
	}

	f.answer = `{"found":true}`
	if st, _ = Checked(context.Background(), f, Element{local: 1}); st.Checked != nil {
		t.Errorf("an element with no checked state read as %v; it must be nil, not unchecked", *st.Checked)
	}
	for _, want := range []string{"aria-checked", "'checkbox'", "'radio'"} {
		if !strings.Contains(f.got, want) {
			t.Errorf("the read does not consider %s", want)
		}
	}
}

// A range is a slider to map — named by its label, not its value — and not
// a text field to a fill (CHALLENGES 319).
func TestARangeIsASlider(t *testing.T) {
	if r := (Element{Tag: "input", Type: "range"}).NeutralRole(); r != "slider" {
		t.Errorf("a range's role is %q", r)
	}
	if strings.Contains(fillableInputTypesJS, "'range'") {
		t.Error("a fill would type into a range")
	}
	if !strings.Contains(mapScript, "t === 'range' ? '' : e.value") {
		t.Error("map names a range by its value")
	}
	f := &cannedPage{answer: `{"status":"ok","value":"0.25","was":"0.5","want":"0.25","matches":true}`}
	r, err := SetRange(context.Background(), f, Element{local: 3}, 0.25)
	if err != nil || !r.Matches || r.Value != "0.25" {
		t.Fatalf("%+v, %v", r, err)
	}
	if !strings.Contains(f.got, "__mobiumCandidates()[3]") || !strings.Contains(f.got, "const pos = 0.25;") {
		t.Errorf("the script does not move candidate 3 to 0.25:\n%.300s", f.got)
	}
}
