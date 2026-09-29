package testrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Caller calls one tool, as the CLI's own commands do — through the daemon,
// starting it if need be. The runner holds no connection of its own.
type Caller func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error)

// Options is one run.
type Options struct {
	Files      []string
	Grep       *regexp.Regexp
	Projects   []string // by name; empty is every project
	Workers    int      // 0 is one per project
	Retries    int
	Timeout    time.Duration // per test
	LastFailed bool
	// Evidence says whether a failure takes a screenshot and a map. A
	// screenshot of a real phone can show a person's data.
	Evidence  bool
	OutputDir string
	// Progress hears each result as it comes, for the list reporter.
	Progress func(Result)
	// Trace keeps a screenshot and the map after every step: "on", or
	// "retain-on-failure", which keeps them only for a test that failed.
	// Steps then run one at a time rather than as a batch. Without Evidence
	// a trace keeps the maps and no screenshot.
	Trace string
	// Debug, when set, is asked before every step what to do. Steps then run
	// one at a time, and the test's timeout is off: a person is reading.
	Debug func(DebugPoint) DebugAction
	// Connect, when set, gives each project a connection of its own, which
	// its first call routes and which closing releases — how a run goes
	// through a grid, where a route is one process's. Unset, every project
	// shares the Caller Run was given.
	Connect func(Project) (Caller, func(), error)
}

// Trace settings.
const (
	TraceOff             = "off"
	TraceOn              = "on"
	TraceRetainOnFailure = "retain-on-failure"
)

// DebugPoint is where a debugged test has stopped: before a step.
type DebugPoint struct {
	Project string
	Title   string
	// Step is 1-based, and 0 for beforeEach's.
	Step int
	// Call is the step as JSON, in its long form.
	Call string
	// Map reads the screen as it is now.
	Map func() string
}

// DebugAction is what to do at a DebugPoint.
type DebugAction int

const (
	// DebugStep runs the step and stops before the next.
	DebugStep DebugAction = iota
	// DebugContinue runs the rest of this test without stopping.
	DebugContinue
	// DebugQuit ends the run: this test is reported stopped, and no other runs.
	DebugQuit
)

// TraceStep is one step of a trace: what ran, how it went, and the screen after.
type TraceStep struct {
	Step        int           `json:"step"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration_ns"`
	Screenshot  string        `json:"screenshot,omitempty"`
	Map         string        `json:"map,omitempty"`
}

// Status is how a test ended.
type Status string

const (
	Passed Status = "passed"
	Failed Status = "failed"
	// Flaky is a pass after a failure. It passes the run, and it is said.
	Flaky Status = "flaky"
)

// Result is one test on one project.
type Result struct {
	File     string        `json:"file"`
	Test     string        `json:"test"`
	Title    string        `json:"title"`
	Project  string        `json:"project"`
	Device   string        `json:"device,omitempty"`
	Status   Status        `json:"status"`
	Attempts int           `json:"attempts"`
	Duration time.Duration `json:"duration_ns"`
	// Failure is the last attempt's, for a failed test, and the first
	// attempt's for a flaky one: its first failure, when soft assertions
	// failed before it.
	Failure *Failure `json:"failure,omitempty"`
	// Failures is every failure of that attempt, in order, when there was
	// more than one — soft assertions, and whatever ended the test.
	Failures []*Failure `json:"failures,omitempty"`
	// Trace is every step of that attempt with the screen after it, when the
	// run was traced.
	Trace []TraceStep `json:"trace,omitempty"`
}

// ID names a test on a project, for --last-failed.
func (r Result) ID() string { return r.Project + "\x00" + r.File + "\x00" + r.Test }

// Failure is what went wrong, where, and what the screen showed.
type Failure struct {
	// Step is 1-based in the test's steps; 0 is before them — launching the
	// app, or beforeEach.
	Step        int    `json:"step"`
	StepName    string `json:"step_name,omitempty"`
	Description string `json:"description,omitempty"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Screenshot  string `json:"screenshot,omitempty"`
	Map         string `json:"map,omitempty"`
	// Soft says the test carried on after this one.
	Soft bool `json:"soft,omitempty"`
}

// Summary is a whole run.
type Summary struct {
	Results  []Result      `json:"results"`
	Duration time.Duration `json:"duration_ns"`
	Started  time.Time     `json:"started"`
}

// Counts are the run's totals by status.
func (s Summary) Counts() (passed, failed, flaky int) {
	for _, r := range s.Results {
		switch r.Status {
		case Passed:
			passed++
		case Failed:
			failed++
		case Flaky:
			flaky++
		}
	}
	return
}

// lastRunName is where --last-failed reads the previous run's failures.
const lastRunName = ".last-run.json"

// job is one file on one project: tests in a file run in order, on one
// device, and files are what workers share out.
type job struct {
	project Project
	file    *File
}

// Run runs the tests. An error is a run that could not start — a file that
// does not check, a project that does not exist — never a test that failed.
func Run(cfg *Config, opts Options, call Caller) (*Summary, error) {
	var closers []func()
	var files []*File
	for _, p := range opts.Files {
		f, err := LoadFile(p)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	projects := cfg.Projects
	if len(projects) == 0 {
		projects = []Project{{Name: "default"}}
	}
	if len(opts.Projects) > 0 {
		var picked []Project
		for _, want := range opts.Projects {
			found := false
			for _, p := range projects {
				if p.Name == want {
					picked, found = append(picked, p), true
				}
			}
			if !found {
				var names []string
				for _, p := range projects {
					names = append(names, p.Name)
				}
				return nil, mobiumerr.New(mobiumerr.InvalidArgument, "no project named %q (have: %s)", want,
					strings.Join(names, ", "))
			}
		}
		projects = picked
	}

	for _, p := range projects {
		if len(p.unset) > 0 {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "project %q takes its device from $%s, which is not set",
				p.Name, strings.Join(p.unset, ", $")).
				WithRemedy("set it to the device's serial or udid (mobium devices lists them), or run another project with --project")
		}
	}

	if opts.Debug != nil && len(projects) > 1 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "--debug stops before each step for a person to read, "+
			"so it runs one project at a time; name it with --project")
	}

	var only map[string]bool
	if opts.LastFailed {
		only = readLastFailed(opts.OutputDir)
	}

	// Which tests of each file this run covers, per project.
	type pick struct {
		project Project
		file    *File
		tests   []Test
	}
	var picks []pick
	for _, p := range projects {
		for _, f := range files {
			var tests []Test
			for _, t := range f.Tests {
				if opts.Grep != nil && !opts.Grep.MatchString(f.Title(t)) {
					continue
				}
				if only != nil && !only[Result{Project: p.Name, File: f.Path, Test: t.Name}.ID()] {
					continue
				}
				tests = append(tests, t)
			}
			if len(tests) > 0 {
				g := *f
				g.Tests = tests
				picks = append(picks, pick{p, &g, tests})
			}
		}
	}
	if len(picks) == 0 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "no test matches — check -g, --project and --last-failed")
	}

	// Every project's device is settled before any test runs: a project
	// that cannot start — two devices where one was meant, none at all —
	// would otherwise fail every one of its tests for the same reason, and
	// report seven failures where there is one problem, which is not a test's.
	// On a grid this is also where each project's device is leased: its
	// connection's first call says what it wants.
	callers := map[string]Caller{}
	leased := map[string]string{}
	defer func() {
		for _, c := range closers {
			c()
		}
	}()
	seen := map[string]bool{}
	for _, p := range picks {
		if seen[p.project.Name] {
			continue
		}
		seen[p.project.Name] = true
		pc := call
		args := projectArgs(p.project)
		first := "app_current"
		if opts.Connect != nil {
			c, closer, err := opts.Connect(p.project)
			if err != nil {
				return nil, mobiumerr.New(mobiumerr.CodeOf(err), "project %q cannot start: %v", p.project.Name, err)
			}
			closers = append(closers, closer)
			pc = c
			// A session start carries the platform, which is what a grid
			// routes by when the project names no device.
			first = "app_session"
			args["action"] = "start"
			if p.project.Platform != "" {
				args["platform"] = p.project.Platform
			}
		}
		callers[p.project.Name] = pc
		res, err := pc(first, args)
		if err == nil && p.project.Device == "" {
			// A grid leased a device the config did not name; the session's
			// answer says which, so every result can.
			if d, ok := field(res.StructuredContent, "device"); ok {
				if s, ok := d.(string); ok {
					leased[p.project.Name] = s
				}
			}
		}
		if err != nil {
			code := mobiumerr.CodeOf(err)
			// With a connection of its own, the first call is the route and
			// the session: any failure leaves the project with no device.
			switch {
			case opts.Connect != nil, code == mobiumerr.InvalidArgument, code == mobiumerr.NoDevice,
				code == mobiumerr.DeviceNotReady, code == mobiumerr.ToolchainMissing:
				e, _ := mobiumerr.As(err)
				out := mobiumerr.New(mobiumerr.CodeOf(err), "project %q cannot start: %v", p.project.Name, err)
				if e != nil && e.Remedy != "" {
					out = out.WithRemedy(e.Remedy)
				}
				return nil, out
			}
		}
	}

	// One worker per device at most: a device holds one session, and two
	// workers on one would drive the same screen.
	byProject := map[string][]job{}
	var order []string
	for _, p := range picks {
		if _, ok := byProject[p.project.Name]; !ok {
			order = append(order, p.project.Name)
		}
		byProject[p.project.Name] = append(byProject[p.project.Name], job{p.project, p.file})
	}
	workers := opts.Workers
	if workers <= 0 || workers > len(order) {
		workers = len(order)
	}

	sum := &Summary{Started: time.Now()}
	var mu sync.Mutex
	queue := make(chan string, len(order))
	for _, name := range order {
		queue <- name
	}
	close(queue)
	var wg sync.WaitGroup
	var stopped atomic.Bool
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range queue {
				for _, j := range byProject[name] {
					for _, t := range j.file.Tests {
						if stopped.Load() {
							break
						}
						r := runTest(j, t, opts, callers[name])
						if r.Failure != nil && r.Failure.Code == codeStopped {
							stopped.Store(true)
						}
						if r.Device == "" {
							r.Device = leased[name]
						}
						mu.Lock()
						sum.Results = append(sum.Results, r)
						if opts.Progress != nil {
							opts.Progress(r)
						}
						mu.Unlock()
					}
				}
			}
		}()
	}
	wg.Wait()
	sum.Duration = time.Since(sum.Started)
	writeLastRun(opts.OutputDir, sum)
	return sum, nil
}

// runTest runs a test, and again after a failure while retries remain.
func runTest(j job, t Test, opts Options, call Caller) Result {
	r := Result{File: j.file.Path, Test: t.Name, Title: j.file.Title(t), Project: j.project.Name,
		Device: j.project.Device}
	start := time.Now()
	var first []*Failure
	var firstTrace, lastTrace []TraceStep
	for attempt := 1; attempt <= opts.Retries+1; attempt++ {
		r.Attempts = attempt
		fs, tr := runOnce(j, t, opts, call, attempt)
		lastTrace = tr
		if len(fs) == 0 {
			r.Status = Passed
			if first != nil {
				r.Status = Flaky
				r.setFailures(first)
			}
			break
		}
		if first == nil {
			first, firstTrace = fs, tr
		}
		r.Status = Failed
		r.setFailures(fs)
		if fs[len(fs)-1].Code == codeStopped {
			break
		}
	}
	// The trace kept is the attempt reported: a flaky test's failure, a
	// failed test's last attempt, a passing test's pass.
	r.Trace = lastTrace
	if r.Status == Flaky {
		r.Trace = firstTrace
	}
	if opts.Trace == TraceRetainOnFailure && r.Status == Passed {
		r.Trace = nil
	}
	pruneTraces(opts.OutputDir, j, t, r.Trace)
	r.Duration = time.Since(start)
	return r
}

// codeStopped is a test the person debugging it stopped. Not one of
// mobiumerr's codes: nothing failed.
const codeStopped = "stopped"

// traceStem names an attempt's trace directory; traceDir is where it is,
// under the output directory.
func traceStem(j job, t Test) string {
	return slug(fmt.Sprintf("%s-%s-%s", j.project.Name, filepath.Base(j.file.Path), t.Name))
}

func traceDir(j job, t Test, attempt int) string {
	return filepath.Join("artifacts", "trace", fmt.Sprintf("%s-%d", traceStem(j, t), attempt))
}

// pruneTraces removes every attempt's trace screenshots but the one kept.
func pruneTraces(out string, j job, t Test, kept []TraceStep) {
	keep := ""
	for _, s := range kept {
		if s.Screenshot != "" {
			keep = filepath.Base(filepath.Dir(s.Screenshot))
			break
		}
	}
	prefix := traceStem(j, t) + "-"
	entries, _ := os.ReadDir(filepath.Join(out, "artifacts", "trace"))
	for _, e := range entries {
		rest := strings.TrimPrefix(e.Name(), prefix)
		if e.Name() != keep && rest != e.Name() && rest != "" && strings.Trim(rest, "0123456789") == "" {
			_ = os.RemoveAll(filepath.Join(out, "artifacts", "trace", e.Name()))
		}
	}
}

func (r *Result) setFailures(fs []*Failure) {
	r.Failure, r.Failures = fs[0], nil
	if len(fs) > 1 {
		r.Failures = fs
	}
}

// runOnce runs a test from a fresh app, and says how it failed, if it did:
// every soft assertion that failed, and what ended it — and its trace.
func runOnce(j job, t Test, opts Options, call Caller, attempt int) ([]*Failure, []TraceStep) {
	deadline := time.Now().Add(opts.Timeout)
	if opts.Debug != nil {
		deadline = time.Now().Add(24 * time.Hour)
	}
	dev := func(args map[string]interface{}) map[string]interface{} {
		out := projectArgs(j.project)
		for k, v := range args {
			out[k] = v
		}
		return out
	}

	n := 0 // the failure's number in this attempt, for its evidence's name
	fail := func(step int, s *Step, err error) *Failure {
		f := &Failure{Step: step, Code: string(mobiumerr.CodeOf(err)), Message: err.Error()}
		if err == errStopped {
			f.Code = codeStopped
			if s != nil {
				f.StepName = s.label()
			}
			return f
		}
		if s != nil {
			f.StepName, f.Description = s.Name, s.Description
			if s.Expect != nil {
				f.StepName = "expect " + s.Expect.Tool
			}
		}
		if opts.Evidence {
			evidence(f, j, t, attempt, n, opts.OutputDir, call, dev)
		}
		n++
		return f
	}

	h := &hooks{}
	var trace []TraceStep
	if opts.Trace == TraceOn || opts.Trace == TraceRetainOnFailure {
		h.single = true
		dir := traceDir(j, t, attempt)
		h.after = func(step int, s *Step, err error, took time.Duration) {
			ts := TraceStep{Step: step, Name: s.label(), Description: s.Description, Duration: took}
			if err != nil {
				ts.Error = err.Error()
			}
			ts.Map = settledMap(call, dev)
			if opts.Evidence {
				name := fmt.Sprintf("%02d.png", len(trace)+1)
				if abs, e := filepath.Abs(filepath.Join(opts.OutputDir, dir, name)); e == nil && os.MkdirAll(filepath.Dir(abs), 0o755) == nil {
					if _, e := call("app_screenshot", dev(map[string]interface{}{"path": abs})); e == nil {
						ts.Screenshot = filepath.Join(dir, name)
					}
				}
			}
			trace = append(trace, ts)
		}
	}
	if opts.Debug != nil {
		h.single = true
		running := false
		h.before = func(step int, s *Step) error {
			if running {
				return nil
			}
			b, _ := json.Marshal(s.longForm())
			p := DebugPoint{Project: j.project.Name, Title: j.file.Title(t), Step: step, Call: string(b),
				Map: func() string { return settledMap(call, dev) }}
			switch opts.Debug(p) {
			case DebugContinue:
				running = true
			case DebugQuit:
				return errStopped
			}
			return nil
		}
	}

	if app := j.file.App; app != "" {
		_, _ = call("app_terminate", dev(map[string]interface{}{"app": app}))
		if _, err := call("app_launch", dev(map[string]interface{}{"app": app})); err != nil {
			return []*Failure{fail(0, nil, err)}, trace
		}
	}
	var soft []*Failure
	if f := runSteps(j.file.BeforeEach, 0, deadline, call, dev, fail, &soft, h); f != nil {
		f.Message = "in beforeEach: " + f.Message
		return append(soft, f), trace
	}
	if f := runSteps(t.Steps, 1, deadline, call, dev, fail, &soft, h); f != nil {
		return append(soft, f), trace
	}
	return soft, trace
}

// settledMap reads the screen once it has stopped changing: a tap returns
// when it is delivered, not when the next screen is up, so a map read straight
// after one showed MobiumApp's home under a step that ran on the Form Demo.
// Two reads 300ms apart that agree are the answer, or the last read after
// two seconds — a screen with a clock on it never agrees, and that is fine:
// it is shown to a person, or kept in a trace, and never compared.
func settledMap(call Caller, dev func(map[string]interface{}) map[string]interface{}) string {
	read := func() string {
		if res, e := call("app_map", dev(map[string]interface{}{})); e == nil && len(res.Content) > 0 {
			return res.Content[0].Text
		}
		return ""
	}
	end := time.Now().Add(settleFor)
	last := read()
	for time.Now().Before(end) {
		time.Sleep(settlePause)
		now := read()
		if now == last {
			return now
		}
		last = now
	}
	return last
}

// How long settledMap waits for the screen to stop changing, and between reads.
var settleFor, settlePause = 2 * time.Second, 300 * time.Millisecond

// errStopped is a debugged test the person stopped.
var errStopped = mobiumerr.New(mobiumerr.Unclassified, "stopped in the debugger")

// hooks change how steps run: one at a time, with something before or after
// each — a trace's screenshot, a debugger's pause.
type hooks struct {
	single bool
	before func(step int, s *Step) error
	after  func(step int, s *Step, err error, took time.Duration)
}

// runSteps runs steps in order: consecutive tool calls as one app_batch,
// which stops at the first failure and says which, and each expect by
// polling its call. base is the number of the first step, 0 for beforeEach
// (reported as step 0).
//
// A soft step runs on its own, outside any batch, and its failure is added
// to soft rather than ending the steps — so a test reports every soft check
// that failed, and still fails.
func runSteps(steps []Step, base int, deadline time.Time, call Caller,
	dev func(map[string]interface{}) map[string]interface{},
	fail func(int, *Step, error) *Failure, soft *[]*Failure, h *hooks) *Failure {
	number := func(i int) int {
		if base == 0 {
			return 0
		}
		return i + 1
	}
	for i := 0; i < len(steps); {
		if time.Now().After(deadline) {
			return fail(number(i), &steps[i], mobiumerr.New(mobiumerr.Timeout, "the test ran out of its time "+
				"before step %d", i+1))
		}
		if steps[i].Expect != nil || steps[i].Soft || h.single {
			if h.before != nil {
				if err := h.before(number(i), &steps[i]); err != nil {
					return fail(number(i), &steps[i], err)
				}
			}
			start := time.Now()
			var err error
			if steps[i].Expect != nil {
				err = expect(*steps[i].Expect, call, dev, deadline)
			} else {
				_, err = call(steps[i].Name, dev(steps[i].argumentsOrEmpty()))
			}
			if h.after != nil {
				h.after(number(i), &steps[i], err, time.Since(start))
			}
			if err != nil {
				f := fail(number(i), &steps[i], err)
				if !steps[i].Soft {
					return f
				}
				f.Soft = true
				*soft = append(*soft, f)
			}
			i++
			continue
		}
		j := i
		var batch []interface{}
		for j < len(steps) && steps[j].Expect == nil && !steps[j].Soft {
			args := steps[j].Arguments
			if args == nil {
				args = map[string]interface{}{}
			}
			batch = append(batch, map[string]interface{}{"name": steps[j].Name, "arguments": args})
			j++
		}
		if _, err := call("app_batch", dev(map[string]interface{}{"steps": batch})); err != nil {
			k := i
			if e, ok := mobiumerr.As(err); ok {
				if n, ok := e.Details["step"].(float64); ok && int(n) >= 1 && i+int(n)-1 < j {
					k = i + int(n) - 1
				} else if n, ok := e.Details["step"].(int); ok && n >= 1 && i+n-1 < j {
					k = i + n - 1
				}
			}
			return fail(number(k), &steps[k], err)
		}
		i = j
	}
	return nil
}

// expectPoll is the pause between an expect's reads, app_wait_for's own.
const expectPoll = 250 * time.Millisecond

// defaultExpectTimeout is app_wait_for's default, for the same reason.
const defaultExpectTimeout = 10 * time.Second

// expect reads a tool until one field of its answer is what was asked, or
// the expect's time — never past the test's — runs out, and then says what
// it last saw. A read that fails is retried too: the app being checked on
// may not be up yet.
func expect(e Expect, call Caller, dev func(map[string]interface{}) map[string]interface{}, deadline time.Time) error {
	timeout := defaultExpectTimeout
	if e.TimeoutMs > 0 {
		timeout = time.Duration(e.TimeoutMs) * time.Millisecond
	}
	end := time.Now().Add(timeout)
	if deadline.Before(end) {
		end = deadline
	}
	want, _ := json.Marshal(e.Equals)
	var saw string
	for {
		res, err := call(e.Tool, dev(e.Arguments))
		if err != nil {
			saw = "the call failed: " + err.Error()
		} else {
			got, found := field(res.StructuredContent, e.Field)
			b, _ := json.Marshal(got)
			switch {
			case !found:
				saw = fmt.Sprintf("its answer has no %s", e.Field)
			case (string(b) == string(want)) != e.Not:
				return nil
			default:
				saw = fmt.Sprintf("%s is %s", e.Field, b)
			}
		}
		if time.Now().After(end) {
			verb := "to be"
			if e.Not {
				verb = "to stop being"
			}
			return mobiumerr.New(mobiumerr.Timeout, "expected %s's %s %s %s, and after %s %s", e.Tool, e.Field, verb,
				want, timeout, saw)
		}
		time.Sleep(expectPoll)
	}
}

// field follows a dotted path into a structured answer.
func field(v interface{}, path string) (interface{}, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var cur interface{}
	if json.Unmarshal(b, &cur) != nil {
		return nil, false
	}
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// evidence keeps what the screen showed when a test failed: a screenshot,
// and the map, which is redacted as every map is.
func evidence(f *Failure, j job, t Test, attempt, n int, out string, call Caller,
	dev func(map[string]interface{}) map[string]interface{}) {
	dir := filepath.Join(out, "artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	stem := slug(fmt.Sprintf("%s-%s-%s-%d", j.project.Name, filepath.Base(j.file.Path), t.Name, attempt))
	if n > 0 {
		// A second failure in one attempt — after a soft one — keeps its own.
		stem += fmt.Sprintf("-%d", n+1)
	}
	shot := filepath.Join(dir, stem+".png")
	if abs, err := filepath.Abs(shot); err == nil {
		if _, err := call("app_screenshot", dev(map[string]interface{}{"path": abs})); err == nil {
			f.Screenshot = filepath.Join("artifacts", stem+".png")
		}
	}
	if res, err := call("app_map", dev(map[string]interface{}{})); err == nil && len(res.Content) > 0 {
		f.Map = res.Content[0].Text
	}
}

var slugRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func slug(s string) string { return strings.Trim(slugRe.ReplaceAllString(s, "-"), "-") }

func writeLastRun(dir string, s *Summary) {
	var failed []string
	for _, r := range s.Results {
		if r.Status == Failed {
			failed = append(failed, r.ID())
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	b, _ := json.MarshalIndent(map[string]interface{}{"failed": failed}, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, lastRunName), b, 0o644)
}

func readLastFailed(dir string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(dir, lastRunName))
	if err != nil {
		return out
	}
	var v struct {
		Failed []string `json:"failed"`
	}
	if json.Unmarshal(raw, &v) == nil {
		for _, id := range v.Failed {
			out[id] = true
		}
	}
	return out
}

// projectArgs are the arguments that put a call on a project's device: its
// serial, and its driver — named, or the one its platform implies.
func projectArgs(p Project) map[string]interface{} {
	out := map[string]interface{}{}
	if p.Device != "" {
		out["device"] = p.Device
	}
	switch {
	case p.Driver != "":
		out["driver"] = p.Driver
	case p.Platform == "ios":
		out["driver"] = "wda"
	case p.Platform == "android":
		out["driver"] = "uiautomator2"
	}
	return out
}
