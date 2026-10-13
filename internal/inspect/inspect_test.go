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
