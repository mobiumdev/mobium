package testrun

import (
	"encoding/xml"
	"fmt"
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
	// untraced are the calls sent marked agent.MetaUntraced.
	untraced []string
}

func (f *fake) call(tool string, args map[string]interface{}, meta ...map[string]interface{}) (*agent.ToolsCallResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, tool)
	if len(meta) > 0 && meta[0][agent.MetaUntraced] == true {
		f.untraced = append(f.untraced, tool)
	}
	f.mu.Unlock()
	if f.answer != nil {
		if r, err := f.answer(tool, args); r != nil || err != nil {
			return r, err
		}
	}
	if tool == "app_wait_for" && args["target"] == "testid=nothing" {
		return nil, mobiumerr.New(mobiumerr.Timeout, "timed out waiting for testid=nothing")
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
	// The same file reached another way — relative, from its own folder —
	// is the same test; it was keyed by the path as given, and missed.
	t.Chdir(dir)
	s = run(t, &Config{Dir: dir}, Options{Files: []string{"login.test.json"}, LastFailed: true, OutputDir: out}, &fake{})
	if len(s.Results) != 1 || s.Results[0].Test != "fails at its second step" {
		t.Fatalf("--last-failed by a relative path ran %+v", s.Results)
	}
}

// Projects run on their own devices, at once; a worker never shares one.
func TestWorkersRunProjectsAtOnce(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "one.test.json", `{"tests": [{"name": "slow", "steps": [
		{"name": "app_tap", "arguments": {"target": "@e1"}}]}]}`)
	cfg := &Config{Dir: dir, Projects: []Project{{Name: "a", Device: "A"}, {Name: "b", Device: "B"}}}
	// Counted, not timed: a wall clock on a busy Windows runner once made two
	// concurrent 300ms projects take 831ms, longer than running them in turn.
	var mu sync.Mutex
	devices := map[string]bool{}
	inFlight, most := 0, 0
	f := &fake{answer: func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
		if tool == "app_batch" {
			mu.Lock()
			devices[args["device"].(string)] = true
			inFlight++
			if inFlight > most {
				most = inFlight
			}
			mu.Unlock()
			time.Sleep(300 * time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
		}
		return nil, nil
	}}
	s := run(t, cfg, Options{Files: []string{p}}, f)
	if most != 2 {
		t.Errorf("two projects ran %d at a time, not at once", most)
	}
	if len(s.Results) != 2 || !devices["A"] || !devices["B"] {
		t.Errorf("results %d, devices %v", len(s.Results), devices)
	}
	most = 0
	run(t, cfg, Options{Files: []string{p}, Workers: 1}, f)
	if most != 1 {
		t.Errorf("one worker ran %d projects at a time", most)
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
		"shorthand typo":   `{"tests": [{"name": "x", "steps": [{"tapp": "@e1"}]}]}`,
		"two shorthands":   `{"tests": [{"name": "x", "steps": [{"tap": "@e1", "fill": "@e2"}]}]}`,
		"shorthand + name": `{"tests": [{"name": "x", "steps": [{"tap": "@e1", "name": "app_tap"}]}]}`,
		"no main argument": `{"tests": [{"name": "x", "steps": [{"swipe": "up"}]}]}`,
		"shorthand misses": `{"tests": [{"name": "x", "steps": [{"fill": "testid=username"}]}]}`,
		"soft action":      `{"tests": [{"name": "x", "steps": [{"tap": "@e1", "soft": true}]}]}`,
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

// A project's device can come from the environment, and an unset variable
// is refused by name — never read as "any device", a different project.
func TestAProjectDeviceFromTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	cfg := write(t, dir, ConfigName, `{"projects": [{"name": "ios", "device": "${MOBIUM_TEST_DEVICE}", "driver": "wda"}]}`)
	t.Setenv("MOBIUM_TEST_DEVICE", "")
	unset, err := LoadConfig(cfg)
	if err != nil {
		t.Fatalf("reading a config whose variable is unset: %v", err)
	}
	p := write(t, dir, "one.test.json", `{"tests": [{"name": "t", "steps": [{"name": "app_map"}]}]}`)
	f := &fake{}
	_, err = Run(unset, Options{Files: []string{p}, Timeout: time.Minute, OutputDir: t.TempDir()}, f.call)
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument || !strings.Contains(err.Error(), "MOBIUM_TEST_DEVICE") {
		t.Fatalf("running a project whose device is unset: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("called %v before refusing", f.calls)
	}
	// Another project runs without it.
	two := write(t, dir, "two.json", `{"projects": [{"name": "ios", "device": "${MOBIUM_TEST_DEVICE}"}, {"name": "android"}]}`)
	cfg2, _ := LoadConfig(two)
	if _, err := Run(cfg2, Options{Files: []string{p}, Projects: []string{"android"}, Timeout: time.Minute,
		OutputDir: t.TempDir()}, (&fake{}).call); err != nil {
		t.Fatalf("--project android with the iOS variable unset: %v", err)
	}
	t.Setenv("MOBIUM_TEST_DEVICE", "SIM-1")
	c, err := LoadConfig(cfg)
	if err != nil || c.Projects[0].Device != "SIM-1" {
		t.Fatalf("device %+v, %v", c, err)
	}
}

// A project that cannot start is refused before any test runs, not failed
// once per test.
func TestAProjectThatCannotStartRunsNothing(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "login.test.json", loginFile)
	f := &fake{answer: func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
		if tool == "app_current" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "2 iOS devices are available — pick one with --device")
		}
		return nil, nil
	}}
	_, err := Run(&Config{Dir: dir}, Options{Files: []string{p}, Timeout: time.Minute, OutputDir: t.TempDir()}, f.call)
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument || !strings.Contains(err.Error(), "cannot start") {
		t.Fatalf("an ambiguous device: %v", err)
	}
	for _, c := range f.calls {
		if c != "app_current" {
			t.Fatalf("ran %s after the project could not start", c)
		}
	}
}

// On a grid each project gets a connection of its own: its first call is a
// session start carrying the platform — what the grid routes by — every call
// for its tests goes through it, and it is closed when the run ends.
func TestEachProjectConnectsOnItsOwn(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "one.test.json", `{"tests": [{"name": "t", "steps": [
		{"name": "app_tap", "arguments": {"target": "@e1"}}]}]}`)
	cfg := &Config{Dir: dir, Projects: []Project{{Name: "a", Platform: "android"}, {Name: "b", Platform: "ios"}}}
	var mu sync.Mutex
	calls := map[string][]string{}
	firstArgs := map[string]map[string]interface{}{}
	closed := map[string]bool{}
	opts := Options{Files: []string{p}, Timeout: time.Minute, OutputDir: t.TempDir(),
		Connect: func(pr Project) (Caller, func(), error) {
			return func(tool string, args map[string]interface{}, _ ...map[string]interface{}) (*agent.ToolsCallResult, error) {
					mu.Lock()
					defer mu.Unlock()
					if len(calls[pr.Name]) == 0 {
						firstArgs[pr.Name] = args
					}
					calls[pr.Name] = append(calls[pr.Name], tool)
					return &agent.ToolsCallResult{}, nil
				}, func() {
					mu.Lock()
					closed[pr.Name] = true
					mu.Unlock()
				}, nil
		}}
	shared := &fake{}
	s, err := Run(cfg, opts, shared.call)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.calls) != 0 {
		t.Errorf("the shared caller was used: %v", shared.calls)
	}
	for _, name := range []string{"a", "b"} {
		if len(calls[name]) < 2 || calls[name][0] != "app_session" {
			t.Errorf("%s's calls: %v", name, calls[name])
		}
		if !closed[name] {
			t.Errorf("%s's connection was not closed", name)
		}
	}
	if firstArgs["a"]["action"] != "start" || firstArgs["a"]["platform"] != "android" || firstArgs["b"]["driver"] != "wda" {
		t.Errorf("first calls: %v", firstArgs)
	}
	if len(s.Results) != 2 {
		t.Errorf("results %d", len(s.Results))
	}
}

// A project the grid cannot give a device refuses the run, for any reason,
// and the connections already open are closed.
func TestAProjectTheGridCannotServeRefusesTheRun(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "one.test.json", `{"tests": [{"name": "t", "steps": [{"name": "app_map"}]}]}`)
	cfg := &Config{Dir: dir, Projects: []Project{{Name: "a", Platform: "android"}, {Name: "b", Platform: "android"}}}
	var closedA bool
	opts := Options{Files: []string{p}, Timeout: time.Minute, OutputDir: t.TempDir(),
		Connect: func(pr Project) (Caller, func(), error) {
			return func(tool string, args map[string]interface{}, _ ...map[string]interface{}) (*agent.ToolsCallResult, error) {
				if pr.Name == "b" {
					return nil, mobiumerr.New(mobiumerr.Timeout, "no free android device on the grid after 60s")
				}
				return &agent.ToolsCallResult{}, nil
			}, func() { closedA = closedA || pr.Name == "a" }, nil
		}}
	_, err := Run(cfg, opts, (&fake{}).call)
	if err == nil || !strings.Contains(err.Error(), `project "b" cannot start`) {
		t.Fatalf("a project with no device: %v", err)
	}
	if !closedA {
		t.Error("project a's connection was left open")
	}
}

// The shorthand is the long form spelled shorter: the same call reaches the
// device, and a string goes to the argument the tool's schema makes its main
// one.
func TestTheShorthandIsTheLongForm(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "short.test.json", `{"tests": [{"name": "x", "steps": [
		{"launch": "dev.mobium.mobiumapp"},
		{"tap": "label=Login Demo", "description": "open it"},
		{"fill": {"target": "testid=username", "text": "mobium"}},
		{"find": "role=button"},
		{"wait_for": {"target": "testid=loginBtn", "condition": "enabled"}},
		{"shake": {}}
	]}]}`)
	f, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ name, arg, value string }{
		{"app_launch", "app", "dev.mobium.mobiumapp"},
		{"app_tap", "target", "label=Login Demo"},
		{"app_fill", "text", "mobium"},
		{"app_find", "locator", "role=button"},
		{"app_wait_for", "condition", "enabled"},
		{"app_shake", "", ""},
	}
	steps := f.Tests[0].Steps
	for i, w := range want {
		s := steps[i]
		if s.Name != w.name || (w.arg != "" && s.Arguments[w.arg] != w.value) {
			t.Errorf("step %d is %s %v, want %s with %s=%s", i+1, s.Name, s.Arguments, w.name, w.arg, w.value)
		}
	}
	if steps[1].Description != "open it" {
		t.Errorf("the description beside a shorthand was lost")
	}
}

// A soft assertion that fails is recorded and the test carries on; the test
// fails, and says every one.
func TestSoftAssertionsCarryOn(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "soft.test.json", `{"tests": [{"name": "x", "steps": [
		{"wait_for": "testid=nothing", "soft": true, "description": "first soft"},
		{"tap": "@e1"},
		{"wait_for": "testid=nothing", "soft": true, "description": "second soft"},
		{"wait_for": "testid=there", "soft": true},
		{"tap": "@e2"}
	]}]}`)
	f := &fake{answer: func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
		if tool == "app_wait_for" && args["target"] == "testid=nothing" {
			return nil, mobiumerr.New(mobiumerr.Timeout, "timed out waiting for testid=nothing")
		}
		return nil, nil
	}}
	s := run(t, &Config{Dir: dir}, Options{Files: []string{p}}, f)
	r := s.Results[0]
	if r.Status != Failed || len(r.Failures) != 2 || r.Failure != r.Failures[0] {
		t.Fatalf("status %s, failures %d — want failed, with both soft ones", r.Status, len(r.Failures))
	}
	if r.Failures[0].Step != 1 || r.Failures[1].Step != 3 || !r.Failures[0].Soft {
		t.Errorf("failures at steps %d and %d, want 1 and 3, soft", r.Failures[0].Step, r.Failures[1].Step)
	}
	if got := strings.Join(f.calls, " "); strings.Count(got, "app_batch") != 2 {
		t.Errorf("the steps after a soft failure did not all run: %s", got)
	}
	line := Line(r)
	if strings.Count(line, "soft, carried on") != 2 {
		t.Errorf("the list line does not show both:\n%s", line)
	}
}

// A trace is every step with the screen after it, one call at a time, and
// retain-on-failure keeps only a failed test's.
func TestATraceIsEveryStepAndRetainOnFailureKeepsFailures(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "login.test.json", loginFile)
	for _, mode := range []string{TraceOn, TraceRetainOnFailure} {
		f := &fake{}
		out := t.TempDir()
		s := run(t, &Config{Dir: dir}, Options{Files: []string{p}, Trace: mode, Evidence: true, OutputDir: out}, f)
		got := strings.Join(f.calls, " ")
		if strings.Contains(got, "app_batch") {
			t.Errorf("%s: a traced run batched its steps: %s", mode, got)
		}
		if p, fl, _ := s.Counts(); p != 1 || fl != 1 {
			t.Fatalf("%s: passed %d, failed %d — want one of each, or the trace cases prove nothing", mode, p, fl)
		}
		for _, r := range s.Results {
			switch {
			case r.Status == Failed && len(r.Trace) != 2:
				t.Errorf("%s: the failed test's trace has %d steps, want 2", mode, len(r.Trace))
			case r.Status == Failed && r.Trace[1].Error == "":
				t.Errorf("%s: the failing step's trace does not say it failed", mode)
			case r.Status == Passed && mode == TraceOn && len(r.Trace) != 2:
				t.Errorf("on: the passing test's trace has %d steps, want 2", len(r.Trace))
			case r.Status == Passed && mode == TraceRetainOnFailure && r.Trace != nil:
				t.Errorf("retain-on-failure kept a passing test's trace")
			}
		}
	}
}

// A traced test is a Playwright trace too: app_trace starts before the app
// is launched and stops after the last step, into the attempt's trace
// directory, and the runner's own screenshots and maps are sent untraced so
// the zip holds the test. retain-on-failure keeps a failed test's zip only.
func TestATracedTestIsAPlaywrightTrace(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "login.test.json", loginFile)
	for _, mode := range []string{TraceOn, TraceRetainOnFailure} {
		out := t.TempDir()
		var starts, stops int
		f := &fake{}
		f.answer = func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
			if tool != "app_trace" {
				return nil, nil
			}
			switch args["action"] {
			case "start":
				starts++
			case "stop":
				stops++
				path, _ := args["path"].(string)
				if !filepath.IsAbs(path) || filepath.Base(path) != traceZip {
					t.Errorf("%s: the trace was saved to %q", mode, path)
				}
				_ = os.WriteFile(path, []byte("zip"), 0o600)
			}
			return &agent.ToolsCallResult{}, nil
		}
		s := run(t, &Config{Dir: dir}, Options{Files: []string{p}, Trace: mode, Evidence: true, OutputDir: out}, f)
		if starts != 2 || stops != 2 {
			t.Errorf("%s: %d starts and %d stops for two tests", mode, starts, stops)
		}
		if strings.Contains(strings.Join(f.untraced, " "), "app_tap") || !strings.Contains(strings.Join(f.untraced, " "), "app_screenshot") ||
			!strings.Contains(strings.Join(f.untraced, " "), "app_map") {
			t.Errorf("%s: untraced calls %v — want the runner's screenshots and maps, and no step", mode, f.untraced)
		}
		for _, r := range s.Results {
			want := r.Status == Failed || mode == TraceOn
			if got := r.TraceFile != ""; got != want {
				t.Errorf("%s: %s test has trace file %q", mode, r.Status, r.TraceFile)
				continue
			}
			if want {
				if _, err := os.Stat(filepath.Join(out, r.TraceFile)); err != nil {
					t.Errorf("%s: the trace file the result names is not there: %v", mode, err)
				}
			}
		}
	}
}

// The debugger stops before each step, in order; continue runs the rest of
// the test without stopping; quit ends the run with nothing after it.
func TestTheDebuggerStopsBeforeEachStep(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "login.test.json", loginFile)
	var seen []string
	answers := []DebugAction{DebugStep, DebugStep, DebugContinue}
	debug := func(pt DebugPoint) DebugAction {
		seen = append(seen, fmt.Sprintf("%s:%d", pt.Title, pt.Step))
		a := answers[0]
		if len(answers) > 1 {
			answers = answers[1:]
		}
		return a
	}
	run(t, &Config{Dir: dir}, Options{Files: []string{p}, Debug: debug}, &fake{})
	want := "login.test.json › passes:1 login.test.json › passes:2 login.test.json › fails at its second step:1"
	if got := strings.Join(seen, " "); got != want {
		t.Errorf("stopped at\n  %s\nwant\n  %s", got, want)
	}

	quits := 0
	s := run(t, &Config{Dir: dir}, Options{Files: []string{p},
		Debug: func(DebugPoint) DebugAction { quits++; return DebugQuit }}, &fake{})
	if quits != 1 || len(s.Results) != 1 || s.Results[0].Failure.Code != codeStopped {
		t.Errorf("quit asked %d times, %d results — want once, and the one test stopped", quits, len(s.Results))
	}

	cfg := &Config{Dir: dir, Projects: []Project{{Name: "a", Device: "emulator-5554"}, {Name: "b", Device: "emulator-5556"}}}
	_, err := Run(cfg, Options{Files: []string{p}, Timeout: time.Minute, OutputDir: t.TempDir(),
		Debug: func(DebugPoint) DebugAction { return DebugStep }}, (&fake{}).call)
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("--debug over two projects was not refused: %v", err)
	}
}

// A config's testDir is its own folder's when relative, and as written when
// absolute; the absolute one was once looked for inside the config's folder.
func TestAnAbsoluteTestDirIsTakenAsWritten(t *testing.T) {
	tests := t.TempDir()
	p := write(t, tests, "one.test.json", `{"tests": [{"name": "t", "steps": []}]}`)
	for _, c := range []struct{ dir, testDir string }{
		{t.TempDir(), tests},
		{filepath.Dir(tests), filepath.Base(tests)},
	} {
		files, err := Discover(&Config{Dir: c.dir, TestDir: c.testDir}, nil)
		if err != nil || len(files) != 1 || files[0] != p {
			t.Errorf("testDir %q from %s: %v, %v", c.testDir, c.dir, files, err)
		}
	}
}

// A real phone is reported by its model, never its id — a phone's id is
// somebody's, and a report is made to be passed around. An emulator keeps
// its id, which says which of several it was.
func TestAPhoneIsReportedByItsModel(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "one.test.json", `{"tests": [{"name": "t", "steps": [
		{"name": "app_tap", "arguments": {"target": "@e1"}}]}]}`)
	cfg := &Config{Dir: dir, Projects: []Project{{Name: "phone", Device: "PHONE-NAME"}, {Name: "emu", Device: "emulator-5554"}}}
	f := &fake{answer: func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
		if tool == "app_current" && args["device"] == "PHONE-NAME" {
			// Named one way in the config, resolved to the id the listing has.
			return agent.Result("", map[string]interface{}{"device": "PHONE-ID", "app": "x"}), nil
		}
		if tool == "app_devices" {
			return agent.Result("", agent.DevicesView{Devices: []agent.DeviceView{
				{ID: "PHONE-ID", Platform: "ios", Model: "iPhone 15 Plus"},
				{ID: "emulator-5554", Platform: "android", Model: "sdk_gphone64_arm64", Emulator: true},
			}}), nil
		}
		return nil, nil
	}}
	s := run(t, cfg, Options{Files: []string{p}}, f)
	got := map[string]string{}
	for _, r := range s.Results {
		got[r.Project] = r.Device
		if strings.Contains(Line(r), "PHONE-") {
			t.Errorf("the list line shows the phone's id: %s", Line(r))
		}
	}
	if got["phone"] != "iPhone 15 Plus" || got["emu"] != "emulator-5554" {
		t.Errorf("devices reported as %v", got)
	}
}

// FailedLast is --last-failed's choice, for --list: a test that failed on a
// project the run covers, found however its file was named.
func TestFailedLastIsWhatLastFailedWouldRun(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	p := write(t, dir, "a.test.json", `{"tests": []}`)
	writeLastRun(out, &Summary{Results: []Result{
		{Project: "pixel", File: p, Test: "broke", Status: Failed},
		{Project: "pixel", File: p, Test: "held", Status: Passed},
	}})
	t.Chdir(dir)
	for _, c := range []struct {
		projects []string
		file     string
		test     string
		want     bool
	}{
		{[]string{"pixel"}, "a.test.json", "broke", true},
		{[]string{"pixel"}, p, "held", false},
		{[]string{"iphone"}, p, "broke", false},
	} {
		if got := FailedLast(out, c.projects, c.file, c.test); got != c.want {
			t.Errorf("FailedLast(%v, %s, %s) = %v", c.projects, c.file, c.test, got)
		}
	}
}
