package inspect

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/agent"
)

// fake answers every tool and remembers which were called.
type fake struct{ calls []string }

func (f *fake) call(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
	f.calls = append(f.calls, tool)
	// What the daemon adds on the way: a test file must not keep it.
	args["device"] = "emulator-5554"
	return &agent.ToolsCallResult{Content: []agent.Content{{Type: "text", Text: "ok"}}}, nil
}

func server(t *testing.T) (*Server, *fake, string) {
	t.Helper()
	f := &fake{}
	s, err := New(f.call)
	if err != nil {
		t.Fatal(err)
	}
	s.addr = "127.0.0.1:4321"
	return s, f, "/" + s.Token + "/"
}

func do(s *Server, method, host, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.Host = host
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// A page that taps a device answers only the page it printed: without the
// token, from another Host, by GET, or naming a tool outside its list, a
// request is refused before any tool is called.
func TestOnlyThePrintedPageIsAnswered(t *testing.T) {
	s, f, p := server(t)
	tap := `{"tool":"app_tap","arguments":{"target":"text=OK"}}`
	for name, w := range map[string]*httptest.ResponseRecorder{
		"no token":       do(s, "POST", "127.0.0.1:4321", "/act", tap),
		"wrong token":    do(s, "POST", "127.0.0.1:4321", "/0123456789abcdef0123456789abcdef/act", tap),
		"rebound host":   do(s, "POST", "evil.example:4321", p+"act", tap),
		"a GET":          do(s, "GET", "127.0.0.1:4321", p+"act", ""),
		"a tool too far": do(s, "POST", "127.0.0.1:4321", p+"act", `{"tool":"app_uninstall","arguments":{"app":"x"}}`),
	} {
		if w.Code < 400 {
			t.Errorf("%s: answered %d", name, w.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("a refused request reached the device: %v", f.calls)
	}
	if w := do(s, "POST", "127.0.0.1:4321", p+"act", tap); w.Code != 200 || len(f.calls) != 1 {
		t.Errorf("the printed page's own tap: %d, calls %v", w.Code, f.calls)
	}
	if w := do(s, "GET", "127.0.0.1:4321", p, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "mobium inspect") {
		t.Errorf("the page itself: %d", w.Code)
	}
}

// What was done is a test file in the step shorthand, by locator, with
// nothing the daemon added.
func TestWhatWasDoneIsATestFile(t *testing.T) {
	s, _, p := server(t)
	for _, body := range []string{
		`{"tool":"app_tap","arguments":{"target":"text=Form Demo"}}`,
		`{"tool":"app_fill","arguments":{"target":"testid=username","text":"mobium"}}`,
		`{"tool":"app_tap","arguments":{"x":540,"y":1200}}`,
	} {
		if w := do(s, "POST", "127.0.0.1:4321", p+"act", body); w.Code != 200 {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	w := do(s, "GET", "127.0.0.1:4321", p+"test", "")
	var file struct {
		Tests []struct {
			Steps []map[string]interface{} `json:"steps"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &file); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(file.Tests[0].Steps)
	want := `[{"tap":"text=Form Demo"},{"fill":{"target":"testid=username","text":"mobium"}},{"tap":{"x":540,"y":1200}}]`
	if string(got) != want {
		t.Errorf("steps\n  %s\nwant\n  %s", got, want)
	}
}

// A recorded step names its element by the locator map derived, as map
// derived it: every one is the whole value, so it is recorded quoted. It was
// recorded unquoted, and text=Welcome is met by "Welcome back" too.
func TestARecordedLocatorKeepsItsQuotes(t *testing.T) {
	for _, c := range []struct {
		loc  agent.LocatorView
		want string
	}{
		{agent.LocatorView{Kind: "text", Value: "Welcome", Exact: true}, `text="Welcome"`},
		{agent.LocatorView{Kind: "label", Value: "Delete", Exact: true, Role: "button"}, `label="Delete",role=button`},
		{agent.LocatorView{Kind: "testid", Value: "loginBtn", Exact: true}, `testid="loginBtn"`},
		{agent.LocatorView{Kind: "css", Value: `[data-testid="wPass"]`, Exact: true}, `css=[data-testid="wPass"]`},
	} {
		loc := c.loc
		if got := target(agent.ElementView{Ref: "@e3", Locator: &loc}); got != c.want {
			t.Errorf("%+v recorded as %s, want %s", c.loc, got, c.want)
		}
	}
}

// The recording downloads as a program in the language asked for, and one
// a language cannot hold is refused saying why, not downloaded as the
// refusal.
func TestARecordingDownloadsAsCode(t *testing.T) {
	s, _, p := server(t)
	s.app = "dev.mobium.mobiumapp"
	for _, body := range []string{
		`{"tool":"app_tap","arguments":{"target":"text=\"Form Demo\""}}`,
		`{"tool":"app_wait_for","arguments":{"target":"testid=status","condition":"text","text":"Saved"}}`,
	} {
		if w := do(s, "POST", "127.0.0.1:4321", p+"act", body); w.Code != 200 {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	w := do(s, "GET", "127.0.0.1:4321", p+"test?lang=python", "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="recorded.py"`) {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	for _, want := range []string{`device.launch("dev.mobium.mobiumapp")`, `device.tap("text=\"Form Demo\"")`,
		`device.wait_for("testid=status", condition="text", text="Saved")`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("no %s in\n%s", want, w.Body)
		}
	}
	if w := do(s, "GET", "127.0.0.1:4321", p+"test?lang=java", ""); !strings.Contains(w.Header().Get("Content-Disposition"), "Recorded.java") {
		t.Errorf("a Java program is not named for its class: %v", w.Header())
	}
	do(s, "POST", "127.0.0.1:4321", p+"act", `{"tool":"app_tap","arguments":{"target":"@e1","fingers":2}}`)
	if w := do(s, "GET", "127.0.0.1:4321", p+"test?lang=go", ""); w.Code != 422 || !strings.Contains(w.Body.String(), "step 3") {
		t.Errorf("an untranslatable step: %d %s", w.Code, w.Body)
	}
}

// finder answers app_find from a screen of look-alikes: three rows, each
// with a title and a "More" button, as Table Demo has.
type finder struct{ finds []string }

func (f *finder) call(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
	box := func(y int) agent.BoundsView { return agent.BoundsView{X1: 800, Y1: y, X2: 1000, Y2: y + 100} }
	more := func(y int) agent.ElementView { return agent.ElementView{Label: "More", Role: "button", Bounds: box(y)} }
	if tool != "app_find" {
		return &agent.ToolsCallResult{Content: []agent.Content{{Type: "text", Text: "ok"}}}, nil
	}
	loc, _ := args["locator"].(string)
	f.finds = append(f.finds, loc)
	var els []agent.ElementView
	switch loc {
	case `label="More",role=button`:
		els = []agent.ElementView{more(100), more(300), more(500)}
	case `label="More",role=button within text="Item 2"`:
		els = []agent.ElementView{more(300)}
	case `label="More",role=button within text="Item 3"`:
		els = []agent.ElementView{more(300), more(500)} // an anchor that names two
	case `label="More",role=button,nth=3`:
		els = []agent.ElementView{more(500)}
	}
	return &agent.ToolsCallResult{StructuredContent: map[string]interface{}{"elements": els}}, nil
}

// A ref lasts one map, so an element map gave no locator of its own — one
// of several "More" buttons — is recorded by its name within the nearest
// neighbor that names it alone, as a person writing the test would.
func TestALookAlikeIsRecordedWithinItsRow(t *testing.T) {
	f := &finder{}
	s, err := New(f.call)
	if err != nil {
		t.Fatal(err)
	}
	s.addr = "127.0.0.1:4321"
	p := "/" + s.Token + "/"
	exact := func(v string) *agent.LocatorView { return &agent.LocatorView{Kind: "text", Value: v, Exact: true} }
	s.last = []agent.ElementView{
		{Ref: "@e3", Label: "Item 3", Locator: exact("Item 3"), Bounds: agent.BoundsView{X1: 0, Y1: 290, X2: 700, Y2: 410}},
		{Ref: "@e2", Label: "Item 2", Locator: exact("Item 2"), Bounds: agent.BoundsView{X1: 0, Y1: 300, X2: 700, Y2: 400}},
		{Ref: "@e5", Label: "More", Role: "button", Bounds: agent.BoundsView{X1: 800, Y1: 300, X2: 1000, Y2: 400}},
	}
	w := do(s, "POST", "127.0.0.1:4321", p+"act", `{"tool":"app_tap","arguments":{"target":"@e5"}}`)
	if !strings.Contains(w.Body.String(), `recorded as label=\"More\",role=button within text=\"Item 2\"`) {
		t.Errorf("answered %s; finds %v", w.Body, f.finds)
	}
	if got, _ := json.Marshal(s.steps); string(got) != `[{"tap":"label=\"More\",role=button within text=\"Item 2\""}]` {
		t.Errorf("recorded %s", got)
	}
}

// With no neighbor that names it, its place among the look-alikes does.
func TestALookAlikeWithNoAnchorIsRecordedByItsPlace(t *testing.T) {
	f := &finder{}
	s, err := New(f.call)
	if err != nil {
		t.Fatal(err)
	}
	s.last = []agent.ElementView{{Ref: "@e5", Label: "More", Role: "button",
		Bounds: agent.BoundsView{X1: 800, Y1: 500, X2: 1000, Y2: 600}}}
	if got := s.scope("@e5"); got != `label="More",role=button,nth=3` {
		t.Errorf("scoped as %q; finds %v", got, f.finds)
	}
}
