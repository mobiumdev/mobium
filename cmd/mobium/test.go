package main

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/testrun"
	"github.com/mobiumdev/mobium/internal/testui"
)

// defaultOutputDir is where a run's reports go, beside the config.
const defaultOutputDir = "mobium-report"

func newTestCmd() *cobra.Command {
	var (
		grep, configPath, outDir string
		trace                    string
		debug                    bool
		projects, reporters      []string
		workers, retries         int
		timeout                  time.Duration
		lastFailed, list, noShot bool
		ui, uiOpen               bool
		uiPort                   int
	)
	cmd := &cobra.Command{
		Use:   "test [file or directory...]",
		Short: "Run *.test.json tests on the devices mobium.config.json names",
		Long: "Runs JSON test files: each test's steps are app_batch steps, its assertions\n" +
			"app_wait_for steps and expects, run from a freshly launched app on each\n" +
			"project's device. With no files, runs every *.test.json under the config's\n" +
			"testDir. With MOBIUM_GRID set, each project leases a device of its own from\n" +
			"the grid — by its platform, or its device — for the whole run. Exits 0 when\n" +
			"every test passed — a flaky one passes and is listed — and 1 when any failed.\n" +
			"See docs/decisions/0006-a-test-runner.md.",
		Example: `  mobium test                              # everything, every project
  mobium test tests/login.test.json
  mobium test -g "wrong password"
  mobium test --project android --retries 2
  mobium test --reporter list,junit,html && mobium show-report
  mobium test --last-failed
  mobium test --trace retain-on-failure --reporter list,html
  mobium test --debug -g "wrong password" --project android
  mobium test --ui --open                  # pick, run and watch tests on a page`,
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			var cfg *testrun.Config
			if configPath != "" {
				cfg, err = testrun.LoadConfig(configPath)
			} else {
				cfg, err = testrun.FindConfig(wd)
			}
			if err != nil {
				return err
			}
			files, err := testrun.Discover(cfg, args)
			if err != nil {
				return err
			}
			opts := testrun.Options{Files: files, Projects: projects, Workers: workers, LastFailed: lastFailed,
				Evidence: !noShot, Retries: cfg.Retries, Timeout: 60 * time.Second}
			if cfg.TimeoutMs > 0 {
				opts.Timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
			}
			if cmd.Flags().Changed("retries") {
				opts.Retries = retries
			}
			if cmd.Flags().Changed("timeout") {
				opts.Timeout = timeout
			}
			if opts.Retries < 0 || opts.Timeout <= 0 {
				return mobiumerr.New(mobiumerr.InvalidArgument, "--retries must not be negative and --timeout must be positive")
			}
			if grep != "" {
				if opts.Grep, err = regexp.Compile(grep); err != nil {
					return mobiumerr.New(mobiumerr.InvalidArgument, "-g %q is not a regular expression: %v", grep, err)
				}
			}
			opts.Trace = cfg.Trace
			if cmd.Flags().Changed("trace") {
				opts.Trace = trace
			}
			switch opts.Trace {
			case "", testrun.TraceOff, testrun.TraceOn, testrun.TraceRetainOnFailure:
			default:
				return mobiumerr.New(mobiumerr.InvalidArgument, "--trace %q: on, off or retain-on-failure", opts.Trace)
			}
			if debug && ui {
				return mobiumerr.New(mobiumerr.InvalidArgument, "--debug waits at a terminal and --ui on a page; give one")
			}
			if debug {
				opts.Debug = debugger(os.Stdin, os.Stderr)
				opts.Workers = 1
			}
			opts.OutputDir = outDir
			if opts.OutputDir == "" {
				opts.OutputDir = filepath.Join(cfg.Dir, defaultOutputDir)
				if cfg.OutputDir != "" {
					opts.OutputDir = cfg.Resolve(cfg.OutputDir)
				}
			}
			want := map[string]bool{}
			for _, r := range reporters {
				for _, name := range strings.Split(r, ",") {
					switch name = strings.TrimSpace(name); name {
					case "list", "json", "junit", "html":
						want[name] = true
					default:
						return mobiumerr.New(mobiumerr.InvalidArgument, "unknown reporter %q (have: list, json, junit, html)", name)
					}
				}
			}

			if list {
				return listTests(cfg, files, opts)
			}
			if want["list"] {
				opts.Progress = func(r testrun.Result) { fmt.Println(testrun.Line(r)) }
			}
			// Each project gets a `mobium pipe` of its own. Its first call
			// starts the project's session — on a grid, leasing a device —
			// and closing it ends the session, which puts back whatever the
			// tests changed. Off a grid each also gets a daemon of its own,
			// named for the run, so projects on different devices do not
			// queue behind one daemon that serves one call at a time; it is
			// stopped when the run ends. This process makes no call itself:
			// on a grid it would lease a device nothing used.
			self, err := os.Executable()
			if err != nil {
				return err
			}
			run := newRunID()
			n := 0
			opts.Connect = func(testrun.Project) (testrun.Caller, func(), error) {
				var env []string
				if !gridActive && os.Getenv("MOBIUM_SESSION") == "" {
					n++
					env = []string{fmt.Sprintf("MOBIUM_SESSION=%s-%d", run, n)}
				}
				p, err := testrun.DialPipe(self, env...)
				if err != nil {
					return nil, nil, err
				}
				return p.Call, func() {
					p.Close()
					if len(env) > 0 {
						stop := exec.Command(self, "daemon", "stop")
						stop.Env = append(os.Environ(), env...)
						_ = stop.Run()
					}
				}, nil
			}
			// Interrupted, every project's connection still open is closed
			// before exiting: its session ended and its daemon stopped. A
			// killed run runs no deferred cleanup, and an interrupted
			// --ui left a run's daemons alive until their idle timeout.
			var closeOpen func()
			opts.Connect, closeOpen = closedOnExit(opts.Connect)
			interrupted := make(chan os.Signal, 1)
			signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
			var stopping atomic.Bool
			go func() {
				<-interrupted
				stopping.Store(true)
				fmt.Fprintln(os.Stderr, "\nstopping: ending each project's session")
				closeOpen()
				os.Exit(130)
			}()
			call := func(tool string, args map[string]interface{}, meta ...map[string]interface{}) (*agent.ToolsCallResult, error) {
				var m map[string]interface{}
				if len(meta) > 0 {
					m = meta[0]
				}
				return daemonCallMeta(tool, args, m)
			}
			if ui {
				return serveTestUI(cfg, args, opts, call, uiPort, uiOpen)
			}
			sum, err := testrun.Run(cfg, opts, call)
			// Closing the connections ends the run early, and it must not
			// then exit before the daemons it is stopping have stopped: the
			// handler exits, when it is done.
			if stopping.Load() {
				select {}
			}
			if err != nil {
				return err
			}
			if want["list"] {
				fmt.Println(testrun.SummaryLine(sum))
			}
			for _, w := range []struct {
				name  string
				write func(string, *testrun.Summary) (string, error)
			}{{"json", testrun.WriteJSON}, {"junit", testrun.WriteJUnit}, {"html", testrun.WriteHTML}} {
				if !want[w.name] {
					continue
				}
				p, err := w.write(opts.OutputDir, sum)
				if err != nil {
					return err
				}
				fmt.Printf("%s report: %s\n", w.name, p)
			}
			if _, failed, _ := sum.Counts(); failed > 0 {
				return mobiumerr.New(mobiumerr.Unclassified, "%d of %d tests failed", failed, len(sum.Results))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&grep, "grep", "g", "", "Run only tests whose title (file › name) matches this regular expression")
	f.StringSliceVar(&projects, "project", nil, "Run only these projects, by name (repeat or comma-separate)")
	f.IntVar(&workers, "workers", 0, "How many devices run at once; at most one per project (default: all)")
	f.IntVar(&retries, "retries", 0, "Run a failed test again this many times; a pass after a failure is flaky")
	f.DurationVar(&timeout, "timeout", 60*time.Second, "Time for each test")
	f.StringSliceVar(&reporters, "reporter", []string{"list"}, "list, json, junit, html — comma-separate for several")
	f.BoolVar(&lastFailed, "last-failed", false, "Run only the tests that failed in the last run")
	f.BoolVar(&list, "list", false, "List the tests that would run, and run nothing")
	f.BoolVar(&noShot, "no-screenshots", false, "Keep no screenshot of a failure — on a real phone it is somebody's screen")
	f.StringVar(&configPath, "config", "", "The config to use (default: mobium.config.json here or above)")
	f.StringVar(&outDir, "output", "", "Where reports go (default: mobium-report beside the config)")
	f.StringVar(&trace, "trace", "off", "Keep a screenshot and the map after every step, and the test as a recording in Vibium's record format: on, off, or retain-on-failure")
	f.BoolVar(&debug, "debug", false, "Stop before each step, show it and the screen, and wait: Enter steps, c continues, q quits")
	f.BoolVar(&ui, "ui", false, "Serve a page on this machine to pick tests, run them and watch each step (docs/decisions/0009)")
	f.IntVar(&uiPort, "ui-port", 0, "With --ui, the port on 127.0.0.1 (default: any free one)")
	f.BoolVar(&uiOpen, "open", false, "With --ui, open the page in the browser")
	return cmd
}

// debugger is --debug's side of a DebugPoint: it prints the step and the
// screen, and reads what to do from the terminal.
func debugger(in io.Reader, out io.Writer) func(testrun.DebugPoint) testrun.DebugAction {
	r := bufio.NewReader(in)
	return func(p testrun.DebugPoint) testrun.DebugAction {
		where := fmt.Sprintf("step %d", p.Step)
		if p.Step == 0 {
			where = "beforeEach"
		}
		fmt.Fprintf(out, "\n[%s] %s — %s\n  %s\n\n%s\n", p.Project, p.Title, where, p.Call, p.Map())
		for {
			fmt.Fprint(out, "\nEnter runs it · m maps again · c runs the rest of the test · q quits > ")
			line, err := r.ReadString('\n')
			if err != nil && line == "" {
				// Nobody is answering: run the rest rather than hang.
				fmt.Fprintln(out)
				return testrun.DebugContinue
			}
			switch strings.TrimSpace(line) {
			case "":
				return testrun.DebugStep
			case "c":
				return testrun.DebugContinue
			case "q":
				fmt.Fprintln(out)
				return testrun.DebugQuit
			case "m":
				fmt.Fprintf(out, "\n%s\n", p.Map())
			}
		}
	}
}

// listTests prints what a run would cover, without a device.
func listTests(cfg *testrun.Config, files []string, opts testrun.Options) error {
	// The projects the run would cover, for --last-failed, which remembers a
	// failure per project.
	var projects []string
	for _, p := range cfg.Projects {
		if len(opts.Projects) == 0 || slices.Contains(opts.Projects, p.Name) {
			projects = append(projects, p.Name)
		}
	}
	n := 0
	for _, p := range files {
		f, err := testrun.LoadFile(p)
		if err != nil {
			return err
		}
		for _, t := range f.Tests {
			if opts.Grep != nil && !opts.Grep.MatchString(f.Title(t)) {
				continue
			}
			// --list once ignored --last-failed, and so named tests the run
			// would not have run.
			if opts.LastFailed && !testrun.FailedLast(opts.OutputDir, projects, p, t.Name) {
				continue
			}
			fmt.Println("  " + f.Title(t))
			n++
		}
	}
	fmt.Printf("%s in %s\n", plural(n, "test"), plural(len(files), "file"))
	return nil
}

func newShowReportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show-report [directory]",
		Short: "Open the last HTML test report",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := defaultOutputDir
			if len(args) == 1 {
				dir = args[0]
			} else if wd, err := os.Getwd(); err == nil {
				if cfg, err := testrun.FindConfig(wd); err == nil {
					dir = filepath.Join(cfg.Dir, defaultOutputDir)
					if cfg.OutputDir != "" {
						dir = cfg.Resolve(cfg.OutputDir)
					}
				}
			}
			page := filepath.Join(dir, "index.html")
			if _, err := os.Stat(page); err != nil {
				return mobiumerr.New(mobiumerr.InvalidArgument, "no report at %s — run `mobium test --reporter html` first", page)
			}
			opener := map[string][]string{"darwin": {"open"}, "windows": {"cmd", "/c", "start", ""}}[runtime.GOOS]
			if opener == nil {
				opener = []string{"xdg-open"}
			}
			if err := exec.Command(opener[0], append(opener[1:], page)...).Start(); err != nil {
				fmt.Println(page)
				return nil
			}
			fmt.Println("opened " + page)
			return nil
		},
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// newRunID names a test run's daemons: short, because a daemon's name is
// part of a socket path the OS caps at about 104 bytes.
func newRunID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("t%x", b)
}

// serveTestUI is --ui: a page that runs the run this command would, any
// part of it at a time (docs/decisions/0009).
func serveTestUI(cfg *testrun.Config, args []string, opts testrun.Options, call testrun.Caller, port int, open bool) error {
	var projects []string
	for _, p := range cfg.Projects {
		if len(opts.Projects) == 0 || slices.Contains(opts.Projects, p.Name) {
			projects = append(projects, p.Name)
		}
	}
	if len(cfg.Projects) == 0 {
		projects = []string{"default"}
	}
	srv, err := testui.New(testui.Setup{
		Discover: func() ([]string, error) { return testrun.Discover(cfg, args) },
		Projects: projects,
		Base:     opts,
		Run:      func(o testrun.Options) (*testrun.Summary, error) { return testrun.Run(cfg, o, call) },
		Report: func(dir string, s *testrun.Summary) error {
			for _, write := range []func(string, *testrun.Summary) (string, error){testrun.WriteJSON, testrun.WriteHTML} {
				if _, err := write(dir, s); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	ln, err := srv.Listen(strconv.Itoa(port))
	if err != nil {
		return err
	}
	url := srv.URL()
	fmt.Printf("mobium test --ui on %s — Ctrl-C to stop\n", url)
	if open {
		opener := map[string][]string{"darwin": {"open"}, "windows": {"cmd", "/c", "start", ""}}[runtime.GOOS]
		if opener == nil {
			opener = []string{"xdg-open"}
		}
		_ = exec.Command(opener[0], append(opener[1:], url)...).Start()
	}
	// Ctrl-C ends the process through the run's own handler, which closes
	// whatever a run left open.
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return hs.Serve(ln)
}

// closedOnExit wraps connect so every connection it opens is remembered
// until it is closed, and returns closeOpen, which closes the ones still
// open — each once, whoever gets there first.
func closedOnExit(connect func(testrun.Project) (testrun.Caller, func(), error)) (
	func(testrun.Project) (testrun.Caller, func(), error), func()) {
	var mu sync.Mutex
	open := map[int]func(){}
	next := 0
	take := func(k int) func() {
		mu.Lock()
		defer mu.Unlock()
		f := open[k]
		delete(open, k)
		return f
	}
	wrapped := func(p testrun.Project) (testrun.Caller, func(), error) {
		c, closer, err := connect(p)
		if err != nil {
			return nil, nil, err
		}
		mu.Lock()
		next++
		k := next
		open[k] = closer
		mu.Unlock()
		return c, func() {
			if f := take(k); f != nil {
				f()
			}
		}, nil
	}
	closeOpen := func() {
		mu.Lock()
		var keys []int
		for k := range open {
			keys = append(keys, k)
		}
		mu.Unlock()
		for _, k := range keys {
			if f := take(k); f != nil {
				f()
			}
		}
	}
	return wrapped, closeOpen
}
