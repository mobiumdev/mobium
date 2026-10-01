package testui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/testrun"
)

const twoTests = `{"tests": [
  {"name": "one", "steps": [{"tap": "@e1"}]},
  {"name": "two", "steps": [{"tap": "@e2"}]}
]}`

// server is a test UI over one file, whose run is release-controlled: it
// reports one step and one result, then waits for release before ending.
func server(t *testing.T) (*Server, string, chan struct{}, *testrun.Options) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "a.test.json")
	if err := os.WriteFile(file, []byte(twoTests), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "report")
	if err := os.MkdirAll(filepath.Join(out, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(out, "artifacts", "01.png"), []byte("png"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "secret.png"), []byte("not yours"), 0o644)
	release := make(chan struct{})
	got := &testrun.Options{}
	s, err := New(Setup{
		Discover: func() ([]string, error) { return []string{file}, nil },
		Projects: []string{"android", "ios"},
		Base:     testrun.Options{OutputDir: out},
		Run: func(o testrun.Options) (*testrun.Summary, error) {
			*got = o
			o.StepDone("android", "a.test.json › one", 1, testrun.TraceStep{Step: 1, Name: "app_tap", Screenshot: "artifacts/01.png"})
			o.Progress(testrun.Result{Project: "android", File: file, Test: "one", Title: "a.test.json › one", Status: testrun.Failed})
			<-release
			return &testrun.Summary{Results: []testrun.Result{{Status: testrun.Failed}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.addr = "127.0.0.1:1"
	return s, file, release, got
}

func do(s *Server, method, host, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = host
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// The page answers only its own URL: the token in the path, and its own
// address as the Host — another page in the browser, or a rebound name,
// cannot run tests on the device.
func TestOnlyThePrintedPageIsAnswered(t *testing.T) {
	s, _, _, _ := server(t)
	p := "/" + s.Token + "/"
	if w := do(s, "GET", s.addr, p, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "mobium test --ui") {
		t.Fatalf("the page: %d", w.Code)
	}
	if w := do(s, "GET", s.addr, "/wrong/tests", ""); w.Code != 404 {
		t.Errorf("no token: %d", w.Code)
	}
	if w := do(s, "GET", "evil.example:80", p+"tests", ""); w.Code != 403 {
		t.Errorf("another Host: %d", w.Code)
	}
}

// The list is read from disk each time it is asked for, with each test's ID
// on each project — the IDs a run picks by, as --last-failed does.
func TestTheListIsTheFilesAsTheyAreNow(t *testing.T) {
	s, file, _, _ := server(t)
	var l List
	_ = json.Unmarshal(do(s, "GET", s.addr, "/"+s.Token+"/tests", "").Body.Bytes(), &l)
	if len(l.Files) != 1 || len(l.Files[0].Tests) != 2 || l.Files[0].Tests[0].IDs["ios"] == "" {
		t.Fatalf("list %+v", l)
	}
	if err := os.WriteFile(file, []byte(`{"tests": [{"name": "three", "steps": [{"tap": "@e3"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(do(s, "GET", s.addr, "/"+s.Token+"/tests", "").Body.Bytes(), &l)
	if len(l.Files[0].Tests) != 1 || l.Files[0].Tests[0].Name != "three" {
		t.Errorf("an edited file was not read again: %+v", l.Files[0].Tests)
	}
	_ = os.WriteFile(file, []byte(`{"tests": [`), 0o644)
	_ = json.Unmarshal(do(s, "GET", s.addr, "/"+s.Token+"/tests", "").Body.Bytes(), &l)
	if l.Files[0].Error == "" {
		t.Error("a file mid-edit was not shown with why it cannot run")
	}
}

// A run runs exactly the IDs asked for, traced; its steps and results are
// in the status as they come; a second run waits for the first.
func TestARunIsTheIDsAskedForAndShowsAsItGoes(t *testing.T) {
	s, file, release, got := server(t)
	id := testrun.Result{Project: "android", File: file, Test: "one"}.ID()
	body, _ := json.Marshal(RunRequest{IDs: []string{id}})
	if w := do(s, "POST", s.addr, "/"+s.Token+"/run", string(body)); w.Code != 200 {
		t.Fatalf("run: %d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(2 * time.Second)
	var st Run
	for time.Now().Before(deadline) {
		_ = json.Unmarshal(do(s, "GET", s.addr, "/"+s.Token+"/status", "").Body.Bytes(), &st)
		if len(st.Results) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !st.Running || len(st.Results) != 1 {
		t.Fatalf("status while running: %+v", st)
	}
	if len(got.Only) != 1 || !got.Only[id] || got.Trace != testrun.TraceOn || len(got.Files) != 1 {
		t.Errorf("the run was given only %v, trace %q, files %v", got.Only, got.Trace, got.Files)
	}
	if w := do(s, "POST", s.addr, "/"+s.Token+"/run", `{"ids": []}`); w.Code != http.StatusConflict {
		t.Errorf("a second run while one goes: %d", w.Code)
	}
	close(release)
	for time.Now().Before(deadline) {
		_ = json.Unmarshal(do(s, "GET", s.addr, "/"+s.Token+"/status", "").Body.Bytes(), &st)
		if !st.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st.Running || st.Failed != 1 {
		t.Errorf("status at the end: %+v", st)
	}
	var l List
	_ = json.Unmarshal(do(s, "GET", s.addr, "/"+s.Token+"/tests", "").Body.Bytes(), &l)
	if l.Files[0].Tests[0].Status["android"] != "failed" {
		t.Errorf("the list does not show the failure: %+v", l.Files[0].Tests[0])
	}
}

// A screenshot is served from the run's output directory, and nothing
// outside it, nor anything but an image or a trace, is.
func TestOnlyTheRunsArtifactsAreServed(t *testing.T) {
	s, _, _, _ := server(t)
	a := "/" + s.Token + "/artifact?p="
	if w := do(s, "GET", s.addr, a+"artifacts/01.png", ""); w.Code != 200 || w.Body.String() != "png" {
		t.Errorf("an artifact: %d", w.Code)
	}
	for _, p := range []string{"../secret.png", "/etc/hosts", "artifacts/../../secret.png", "results.json"} {
		if w := do(s, "GET", s.addr, a+p, ""); w.Code == 200 {
			t.Errorf("%s was served", p)
		}
	}
}
