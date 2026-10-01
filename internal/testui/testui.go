// Package testui is `mobium test --ui`: a page on this machine that lists a
// suite, runs any part of it on its projects, and shows each step with the
// screen after it as it happens. See docs/decisions/0009.
//
// It runs nothing of its own: a run is the one `mobium test` makes from the
// same flags and config, with the tests to run picked by ID.
package testui

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/testrun"
)

//go:embed page.html
var page []byte

// Setup is what the CLI hands over: how to find the files, which projects
// the run covers, the options every run starts from, and how to run.
type Setup struct {
	// Discover finds the test files, again for every list and run, so an
	// edited file is the file that runs.
	Discover func() ([]string, error)
	// Projects are the names of the projects a run covers, in order.
	Projects []string
	// Base is the options a run starts from, as `mobium test` built them.
	Base testrun.Options
	// Run runs, as `mobium test` does, and Report writes the run's reports.
	Run    func(testrun.Options) (*testrun.Summary, error)
	Report func(dir string, s *testrun.Summary) error
}

// Server is one test UI.
type Server struct {
	setup Setup
	Token string
	addr  string

	mu  sync.Mutex
	run *Run
	n   int
}

// Run is one run as the page sees it: what has finished, and what is running
// step by step.
type Run struct {
	ID       int               `json:"id"`
	Running  bool              `json:"running"`
	Started  time.Time         `json:"started"`
	Took     string            `json:"took,omitempty"`
	Results  []testrun.Result  `json:"results"`
	Live     map[string]*Live  `json:"live"`
	Error    string            `json:"error,omitempty"`
	Passed   int               `json:"passed"`
	Failed   int               `json:"failed"`
	Flaky    int               `json:"flaky"`
	Selected []string          `json:"selected"`
	statuses map[string]string // by test ID, for the list
}

// Live is a test that is running: its steps so far.
type Live struct {
	Project string              `json:"project"`
	Title   string              `json:"title"`
	Attempt int                 `json:"attempt"`
	Steps   []testrun.TraceStep `json:"steps"`
}

// New makes a server, with a token only its printed URL holds.
func New(setup Setup) (*Server, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Server{setup: setup, Token: hex.EncodeToString(b)}, nil
}

// Listen opens 127.0.0.1:port — any free one for 0.
func (s *Server) Listen(port string) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, err
	}
	s.addr = ln.Addr().String()
	return ln, nil
}

// URL is the page's address, token included.
func (s *Server) URL() string { return "http://" + s.addr + "/" + s.Token + "/" }

// Handler serves the page and its calls, each refused unless it carries the
// token and names this server as its Host, as the inspector's are: the page
// runs tests on a device, and any other page in the same browser could
// otherwise ask it to.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	p := "/" + s.Token + "/"
	mux.HandleFunc(p, s.index)
	mux.HandleFunc(p+"tests", s.tests)
	mux.HandleFunc(p+"run", s.start)
	mux.HandleFunc(p+"status", s.status)
	mux.HandleFunc(p+"artifact", s.artifact)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.addr != "" && r.Host != s.addr {
			http.Error(w, "wrong host", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.URL.Path, p) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/"+s.Token+"/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src 'self'; connect-src 'self'")
	_, _ = w.Write(page)
}

// Test is one test of the list, with its ID on each project.
type Test struct {
	Name  string            `json:"name"`
	Title string            `json:"title"`
	IDs   map[string]string `json:"ids"`
	// Status is how it went on each project in the last run, if it ran.
	Status map[string]string `json:"status,omitempty"`
}

// File is one test file of the list.
type File struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Tests []Test `json:"tests"`
	Error string `json:"error,omitempty"`
}

// List is the suite as the page shows it.
type List struct {
	Projects []string `json:"projects"`
	Files    []File   `json:"files"`
	Error    string   `json:"error,omitempty"`
}

func (s *Server) list() List {
	l := List{Projects: s.setup.Projects, Files: []File{}}
	paths, err := s.setup.Discover()
	if err != nil {
		l.Error = err.Error()
		return l
	}
	s.mu.Lock()
	var statuses map[string]string
	if s.run != nil {
		statuses = s.run.statuses
	}
	s.mu.Unlock()
	for _, p := range paths {
		f := File{Path: p, Name: filepath.Base(p), Tests: []Test{}}
		tf, err := testrun.LoadFile(p)
		if err != nil {
			// A file mid-edit is shown with why it will not run, and the
			// rest of the suite still runs.
			f.Error = err.Error()
			l.Files = append(l.Files, f)
			continue
		}
		for _, t := range tf.Tests {
			lt := Test{Name: t.Name, Title: tf.Title(t), IDs: map[string]string{}, Status: map[string]string{}}
			for _, proj := range s.setup.Projects {
				id := testrun.Result{Project: proj, File: p, Test: t.Name}.ID()
				lt.IDs[proj] = id
				if st, ok := statuses[id]; ok {
					lt.Status[proj] = st
				}
			}
			f.Tests = append(f.Tests, lt)
		}
		l.Files = append(l.Files, f)
	}
	return l
}

func (s *Server) tests(w http.ResponseWriter, r *http.Request) {
	reply(w, s.list())
}

// RunRequest picks what to run: test IDs, or every test when none are given.
type RunRequest struct {
	IDs []string `json:"ids"`
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "a run request is {\"ids\": [...]}", http.StatusBadRequest)
		return
	}
	files, err := s.setup.Discover()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if s.run != nil && s.run.Running {
		s.mu.Unlock()
		http.Error(w, "a run is already going; it ends on its own", http.StatusConflict)
		return
	}
	s.n++
	run := &Run{ID: s.n, Running: true, Started: time.Now(), Results: []testrun.Result{}, Live: map[string]*Live{},
		Selected: req.IDs, statuses: map[string]string{}}
	if s.run != nil {
		// What ran before keeps its status in the list until it runs again.
		for id, st := range s.run.statuses {
			run.statuses[id] = st
		}
	}
	s.run = run
	s.mu.Unlock()

	opts := s.setup.Base
	opts.Files = files
	opts.Only = nil
	if len(req.IDs) > 0 {
		opts.Only = map[string]bool{}
		for _, id := range req.IDs {
			opts.Only[id] = true
		}
	}
	// Traced, so each step comes with the screen after it.
	if opts.Trace == "" || opts.Trace == testrun.TraceOff {
		opts.Trace = testrun.TraceOn
	}
	opts.Progress = func(res testrun.Result) {
		s.mu.Lock()
		defer s.mu.Unlock()
		run.Results = append(run.Results, res)
		delete(run.Live, res.Project+"\x00"+res.Title)
		run.statuses[res.ID()] = string(res.Status)
	}
	opts.StepDone = func(project, title string, attempt int, step testrun.TraceStep) {
		s.mu.Lock()
		defer s.mu.Unlock()
		key := project + "\x00" + title
		l := run.Live[key]
		if l == nil || l.Attempt != attempt {
			l = &Live{Project: project, Title: title, Attempt: attempt}
			run.Live[key] = l
		}
		l.Steps = append(l.Steps, step)
	}
	go func() {
		sum, err := s.setup.Run(opts)
		if err == nil && s.setup.Report != nil {
			err = s.setup.Report(opts.OutputDir, sum)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		run.Running = false
		run.Took = time.Since(run.Started).Round(100 * time.Millisecond).String()
		if err != nil {
			run.Error = err.Error()
		}
		if sum != nil {
			run.Passed, run.Failed, run.Flaky = sum.Counts()
		}
	}()
	reply(w, map[string]int{"id": run.ID})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil {
		reply(w, map[string]interface{}{"id": 0})
		return
	}
	reply(w, s.run)
}

// artifact serves a file a run kept — a step's screenshot, a trace zip —
// from the run's output directory and from nowhere else.
func (s *Server) artifact(w http.ResponseWriter, r *http.Request) {
	rel := filepath.Clean(filepath.FromSlash(r.URL.Query().Get("p")))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.Error(w, "not an artifact", http.StatusBadRequest)
		return
	}
	types := map[string]string{".png": "image/png", ".jpeg": "image/jpeg", ".jpg": "image/jpeg", ".zip": "application/zip"}
	ct, ok := types[strings.ToLower(filepath.Ext(rel))]
	if !ok {
		http.Error(w, "not an artifact", http.StatusBadRequest)
		return
	}
	root, err := filepath.Abs(s.setup.Base.OutputDir)
	if err == nil {
		// The real path, as the file's is below: /tmp is /private/tmp.
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		http.Error(w, "no output directory", http.StatusInternalServerError)
		return
	}
	full := filepath.Join(root, rel)
	if real, err := filepath.EvalSymlinks(full); err != nil || !strings.HasPrefix(real, root+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	if ct == "application/zip" {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(rel)+"\"")
	}
	_, _ = w.Write(data)
}

func reply(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
