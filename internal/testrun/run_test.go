package testrun

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// write puts a test file in a temporary directory and returns its path.
func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const loginFile = `{
  "app": "dev.mobium.mobiumapp",
  "tests": [
    {"name": "passes", "steps": [
      {"name": "app_tap", "arguments": {"target": "label=Login Demo"}},
      {"name": "app_wait_for", "arguments": {"target": "testid=username"}}
    ]},
    {"name": "fails at its second step", "steps": [
      {"name": "app_tap", "arguments": {"target": "label=Login Demo"}},
      {"name": "app_wait_for", "arguments": {"target": "testid=nothing"}, "description": "a field that is not there"}
    ]}
  ]
}`

// fake answers tool calls: a batch fails at the first app_wait_for on
// testid=nothing, as app_batch does, with the step's number in details.
type fake struct {
	mu    sync.Mutex
	calls []string
	// batchFails, when set, decides a batch's fate instead.
	batchFails func(steps []interface{}) (int, bool)
	answer     func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error)
}

func (f *fake) call(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, tool)
	f.mu.Unlock()
	if f.answer != nil {
		if r, err := f.answer(tool, args); r != nil || err != nil {
			return r, err
		}
	}
	if tool == "app_batch" {
		steps := args["steps"].([]interface{})
		fails := func(steps []interface{}) (int, bool) {
			for i, s := range steps {
				a := s.(map[string]interface{})["arguments"].(map[string]interface{})
				if a["target"] == "testid=nothing" {
					return i + 1, true
				}
			}
			return 0, false
		}
		if f.batchFails != nil {
			fails = f.batchFails
		}
		if n, bad := fails(steps); bad {
			return nil, mobiumerr.New(mobiumerr.Timeout, "step %d failed: timed out", n).WithDetail("step", float64(n))
		}
	}
	return &agent.ToolsCallResult{Content: []agent.Content{{Type: "text", Text: "ok"}}}, nil
}

func run(t *testing.T, cfg *Config, opts Options, f *fake) *Summary {
	t.Helper()
	if opts.Timeout == 0 {
		opts.Timeout = time.Minute
	}
	if opts.OutputDir == "" {
		opts.OutputDir = t.TempDir()
	}
	s, err := Run(cfg, opts, f.call)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAFailureNamesItsStepAndCode(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "login.test.json", loginFile)
	s := run(t, &Config{Dir: dir}, Options{Files: []string{p}}, &fake{})
	pass, fail, flaky := s.Counts()
	if pass != 1 || fail != 1 || flaky != 0 {
		t.Fatalf("passed %d, failed %d, flaky %d — want 1, 1, 0", pass, fail, flaky)
	}
	var failed Result
	for _, r := range s.Results {
		if r.Status == Failed {
			failed = r
		}
	}
	f := failed.Failure
	if f == nil || f.Step != 2 || f.Code != "timeout" || f.Description != "a field that is not there" {
		t.Fatalf("failure %+v", f)
	}
}

// Each test starts from a fresh app: terminated, then launched.
func TestEachTestStartsTheAppFresh(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "login.test.json", loginFile)
	f := &fake{}
	run(t, &Config{Dir: dir}, Options{Files: []string{p}}, f)
	got := strings.Join(f.calls, " ")
	if strings.Count(got, "app_terminate app_launch app_batch") != 2 {
		t.Errorf("calls: %s", got)
	}
}

// A pass after a failure is flaky, not passed, and a test that never passes
// uses every retry.
func TestRetriesReportFlaky(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "login.test.json", loginFile)
	var attempts atomic.Int32
	f := &fake{batchFails: func(steps []interface{}) (int, bool) {
		a := steps[1].(map[string]interface{})["arguments"].(map[string]interface{})
		if a["target"] == "testid=nothing" {
			return 2, true
		}
		return 1, attempts.Add(1) == 1 // the passing test fails once
	}}
	s := run(t, &Config{Dir: dir}, Options{Files: []string{p}, Retries: 2}, f)
	for _, r := range s.Results {
		switch r.Test {
		case "passes":
			if r.Status != Flaky || r.Attempts != 2 || r.Failure == nil {
				t.Errorf("the test that failed once: %s after %d, failure %v", r.Status, r.Attempts, r.Failure)
			}
		default:
			if r.Status != Failed || r.Attempts != 3 {
				t.Errorf("the test that always fails: %s after %d", r.Status, r.Attempts)
			}
		}
	}
}

func TestGrepAndLastFailedPickTests(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "login.test.json", loginFile)
	out := t.TempDir()
	s := run(t, &Config{Dir: dir}, Options{Files: []string{p}, Grep: regexp.MustCompile("second"), OutputDir: out}, &fake{})
	if len(s.Results) != 1 || s.Results[0].Test != "fails at its second step" {
		t.Fatalf("-g second ran %+v", s.Results)
	}
	// That run failed its one test; --last-failed runs exactly it.
	s = run(t, &Config{Dir: dir}, Options{Files: []string{p}, LastFailed: true, OutputDir: out}, &fake{})
	if len(s.Results) != 1 || s.Results[0].Test != "fails at its second step" {
		t.Fatalf("--last-failed ran %+v", s.Results)
	}
}

// Projects run on their own devices, at once; a worker never shares one.
func TestWorkersRunProjectsAtOnce(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "one.test.json", `{"tests": [{"name": "slow", "steps": [
		{"name": "app_tap", "arguments": {"target": "@e1"}}]}]}`)
	cfg := &Config{Dir: dir, Projects: []Project{{Name: "a", Device: "A"}, {Name: "b", Device: "B"}}}
	var mu sync.Mutex
	devices := map[string]bool{}
	f := &fake{answer: func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
		if tool == "app_batch" {
			mu.Lock()
			devices[args["device"].(string)] = true
			mu.Unlock()
			time.Sleep(300 * time.Millisecond)
		}
		return nil, nil
	}}
	start := time.Now()
	s := run(t, cfg, Options{Files: []string{p}}, f)
	if took := time.Since(start); took > 550*time.Millisecond {
		t.Errorf("two projects of 300ms each took %s — not at once", took)
	}
	if len(s.Results) != 2 || !devices["A"] || !devices["B"] {
		t.Errorf("results %d, devices %v", len(s.Results), devices)
	}
	start = time.Now()
	run(t, cfg, Options{Files: []string{p}, Workers: 1}, f)
	if took := time.Since(start); took < 550*time.Millisecond {
		t.Errorf("one worker ran two projects in %s — at once", took)
	}
}

// An expect retries a read until its field matches, and a field that never
// does fails saying what it was.
func TestExpectPollsAReadAndSaysWhatItSaw(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "state.test.json", `{"tests": [
		{"name": "comes to the front", "steps": [{"expect": {"tool": "app_state", "arguments": {"app": "x"},
			"field": "state", "equals": "foreground", "timeout_ms": 3000}}]},
		{"name": "never goes away", "steps": [{"expect": {"tool": "app_state", "arguments": {"app": "x"},
			"field": "state", "equals": "foreground", "not": true, "timeout_ms": 400}}]}
	]}`)
	var reads atomic.Int32
	f := &fake{answer: func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
		if tool != "app_state" {
			return nil, nil
		}
		state := "background"
		if reads.Add(1) > 2 {
			state = "foreground"
		}
		return &agent.ToolsCallResult{StructuredContent: map[string]interface{}{"app": "x", "state": state}}, nil
	}}
	s := run(t, &Config{Dir: dir}, Options{Files: []string{p}}, f)
	for _, r := range s.Results {
		switch r.Test {
		case "comes to the front":
			if r.Status != Passed {
				t.Errorf("an expect that came true: %s %+v", r.Status, r.Failure)
			}
		default:
			if r.Status != Failed || !strings.Contains(r.Failure.Message, `state is "foreground"`) {
				t.Errorf("an expect that stayed false: %s %+v", r.Status, r.Failure)
			}
		}
	}
}

func TestFilesAreCheckedBeforeAnythingRuns(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown key":      `{"tests": [{"name": "x", "step": []}]}`,
		"no tool":          `{"tests": [{"name": "x", "steps": [{"name": "app_nonsense"}]}]}`,
		"bad argument":     `{"tests": [{"name": "x", "steps": [{"name": "app_tap", "arguments": {"targt": "@e1"}}]}]}`,
		"acting expect":    `{"tests": [{"name": "x", "steps": [{"expect": {"tool": "app_tap", "arguments": {"target": "@e1"}, "field": "x", "equals": 1}}]}]}`,
		"setting expect":   `{"tests": [{"name": "x", "steps": [{"expect": {"tool": "app_network", "arguments": {"offline": true}, "field": "online", "equals": false}}]}]}`,
		"duplicate test":   `{"tests": [{"name": "x", "steps": [{"name": "app_map"}]}, {"name": "x", "steps": [{"name": "app_map"}]}]}`,
		"a test with none": `{"tests": [{"name": "x", "steps": []}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := write(t, dir, strings.ReplaceAll(name, " ", "-")+".test.json", body)
			f := &fake{}
			_, err := Run(&Config{Dir: dir}, Options{Files: []string{p}, Timeout: time.Minute, OutputDir: t.TempDir()}, f.call)
			if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
				t.Errorf("accepted, or refused as %s: %v", mobiumerr.CodeOf(err), err)
			}
			if len(f.calls) != 0 {
				t.Errorf("called %v before refusing", f.calls)
			}
		})
	}
}

// A report nobody can read is not an artifact: the JUnit file parses, counts
// the failure, and names the flaky test's attempt.
func TestJUnitParsesAndCounts(t *testing.T) {
	s := &Summary{Duration: time.Second, Results: []Result{
		{File: "a.test.json", Test: "ok", Title: "a.test.json › ok", Project: "android", Status: Passed},
		{File: "a.test.json", Test: "flaky", Title: "a.test.json › flaky", Project: "android", Status: Flaky, Attempts: 2},
		{File: "a.test.json", Test: "bad <one>", Title: "a.test.json › bad <one>", Project: "android", Status: Failed,
			Failure: &Failure{Step: 2, Code: "timeout", Message: "timed out & gave up"}},
	}}
	dir := t.TempDir()
	p, err := WriteJUnit(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	var doc struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Suites   []struct {
			Cases []struct {
				Name    string `xml:"name,attr"`
				Failure *struct {
					Type string `xml:"type,attr"`
				} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("junit.xml does not parse: %v", err)
	}
	if doc.Tests != 3 || doc.Failures != 1 {
		t.Errorf("tests %d, failures %d", doc.Tests, doc.Failures)
	}
	if !strings.Contains(string(raw), `name="flaky" value="passed on attempt 2"`) {
		t.Error("the flaky test is not marked")
	}
	h, err := WriteHTML(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(h)
	if strings.Contains(string(page), "bad <one>") || !strings.Contains(string(page), "bad &lt;one&gt;") {
		t.Error("the HTML report does not escape a test's name")
	}
}
