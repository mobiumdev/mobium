package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/testrun"
)

// defaultOutputDir is where a run's reports go, beside the config.
const defaultOutputDir = "mobium-report"

func newTestCmd() *cobra.Command {
	var (
		grep, configPath, outDir string
		projects, reporters      []string
		workers, retries         int
		timeout                  time.Duration
		lastFailed, list, noShot bool
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
  mobium test --last-failed`,
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
			opts.OutputDir = outDir
			if opts.OutputDir == "" {
				opts.OutputDir = filepath.Join(cfg.Dir, defaultOutputDir)
				if cfg.OutputDir != "" {
					opts.OutputDir = filepath.Join(cfg.Dir, cfg.OutputDir)
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
				return listTests(files, opts)
			}
			if want["list"] {
				opts.Progress = func(r testrun.Result) { fmt.Println(testrun.Line(r)) }
			}
			if gridActive {
				// A grid route is one process's, so each project gets a
				// `mobium pipe` of its own, which leases its device on its
				// first call and releases it when the run closes it. This
				// process must not route itself: any call from it here would
				// lease a device nothing uses.
				self, err := os.Executable()
				if err != nil {
					return err
				}
				opts.Connect = func(testrun.Project) (testrun.Caller, func(), error) {
					p, err := testrun.DialPipe(self)
					if err != nil {
						return nil, nil, err
					}
					return p.Call, p.Close, nil
				}
			} else if _, err := daemonCall("app_devices", map[string]interface{}{}); err != nil {
				// One call before the workers start, so a daemon that is not
				// up is started once, not raced for by every worker.
				return err
			}
			sum, err := testrun.Run(cfg, opts, daemonCall)
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
	return cmd
}

// listTests prints what a run would cover, without a device.
func listTests(files []string, opts testrun.Options) error {
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
						dir = filepath.Join(cfg.Dir, cfg.OutputDir)
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
