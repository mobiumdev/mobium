package apisurface

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot is where the sweep runs from. Tests execute in the package
// directory, so the repository is two levels up.
const repoRoot = "../.."

func sweep(t *testing.T) *Surface {
	t.Helper()
	s, err := Collect(repoRoot)
	if err != nil {
		t.Fatalf("sweeping the API surface: %v", err)
	}
	return s
}

// TestNoUndeclaredDrift is the control. Every tool must be reachable from the
// CLI and from all five clients, unless the gap is written down with a reason.
//
// This is the test that would have caught the real drift the sweep found when
// it was first written: `doctor` existed in the Java client and in none of the
// other three, which nobody had noticed because nothing compared them.
func TestNoUndeclaredDrift(t *testing.T) {
	problems := Check(sweep(t))
	for _, p := range problems {
		t.Error(p)
	}
}

// TestEveryClientCoversTheToolSurface states the per-client half separately,
// so a failure names the client rather than a list of tools.
func TestEveryClientCoversTheToolSurface(t *testing.T) {
	s := sweep(t)
	for _, c := range s.Clients {
		if c.Covers != s.Counts.Tools {
			t.Errorf("the %s client reaches %d of %d tools; missing %s",
				c.Language, c.Covers, s.Counts.Tools, strings.Join(c.Missing, ", "))
		}
	}
}

// TestEveryToolIsReachableFromTheCLI is the global half.
func TestEveryToolIsReachableFromTheCLI(t *testing.T) {
	for _, tool := range sweep(t).Tools {
		if tool.CLI == "" {
			t.Errorf("no CLI command dispatches %s", tool.Name)
		}
	}
}

// TestCLICommandsWithoutAToolAreTheProcessOnes: a command that dispatches no
// tool is either part of running mobium itself or an orphan, and the two look
// identical from outside. Naming the legitimate ones means a new orphan shows
// up as a failure rather than blending in.
func TestCLICommandsWithoutAToolAreTheProcessOnes(t *testing.T) {
	expected := map[string]bool{
		"daemon": true, "daemon start": true, "daemon status": true, "daemon stop": true, "daemon up": true,
		"grid": true, "grid status": true, "grid ui": true,
		"mcp": true, "pipe": true,
		// The test runner calls tools, but from test files through
		// internal/testrun, not by name here; show-report opens a file.
		"test": true, "show-report": true,
		// The inspector calls tools too, from what its page asks for,
		// through internal/inspect.
		"inspect": true,
	}
	for _, cmd := range sweep(t).Extra {
		if !expected[cmd] {
			t.Errorf("the %q command dispatches no tool — if that is deliberate, add it "+
				"to this list with the others that run the process rather than the device", cmd)
		}
	}
}

// TestTheCountsExplainThemselves guards the arithmetic that has been got wrong
// twice in this project: `mobium --help` shows fewer commands than are
// registered, because some are hidden, and more lines than mobium owns,
// because cobra adds `help` and `completion`.
func TestTheCountsExplainThemselves(t *testing.T) {
	s := sweep(t)
	if s.Counts.CLIVisible+len(s.Counts.CLIHidden) != s.Counts.CLIRegistered {
		t.Errorf("visible (%d) + hidden (%d) != registered (%d)",
			s.Counts.CLIVisible, len(s.Counts.CLIHidden), s.Counts.CLIRegistered)
	}
	if s.Counts.CLIConstructors < s.Counts.CLIRegistered {
		t.Errorf("%d constructors cannot register %d commands",
			s.Counts.CLIConstructors, s.Counts.CLIRegistered)
	}
	if s.Counts.Tools != len(s.Tools) {
		t.Errorf("counted %d tools and listed %d", s.Counts.Tools, len(s.Tools))
	}
	if s.Counts.Clients != len(s.Clients) {
		t.Errorf("counted %d clients and listed %d", s.Counts.Clients, len(s.Clients))
	}
}

// TestTheSweepCanSeeEachSurface guards the checker itself. A pattern that
// stopped matching would report a client as covering nothing — which is a loud
// failure — but a *method* pattern that stopped matching would attribute every
// call to no method and report the client as covering nothing either. The one
// dangerous case is a sweep that silently sees a full surface where there is
// none, so each is asserted to have found something.
func TestTheSweepCanSeeEachSurface(t *testing.T) {
	s := sweep(t)
	if len(s.Tools) == 0 {
		t.Fatal("the sweep found no tools at all")
	}
	for _, c := range s.Clients {
		if c.Covers == 0 {
			t.Errorf("the sweep found nothing in the %s client — its patterns have "+
				"stopped matching, and a drift check that cannot see a surface is "+
				"worse than none", c.Language)
		}
	}
	withCLI := 0
	for _, tool := range s.Tools {
		if tool.CLI != "" {
			withCLI++
		}
	}
	if withCLI == 0 {
		t.Error("the sweep parsed no CLI dispatches")
	}
}

func flagSweep(t *testing.T) ([]ArgEntry, []CommandFlags) {
	t.Helper()
	args, cmds, err := CollectFlags(repoRoot)
	if err != nil {
		t.Fatalf("sweeping the flag surface: %v", err)
	}
	return args, cmds
}

// TestNoUndeclaredFlagDrift is the control one level below TestNoUndeclaredDrift.
//
// Every argument a tool declares must be settable from the CLI, and every key
// the CLI sends must be one some tool it dispatches declares — unless the gap
// is written down with a reason. The failure it exists for is silent on the
// surface people use: an undeclared key is refused by an MCP client, because
// every schema is `additionalProperties: false`, and ignored by the CLI,
// because a handler reads the keys it knows by name.
func TestNoUndeclaredFlagDrift(t *testing.T) {
	args, cmds := flagSweep(t)
	for _, p := range CheckFlags(args, cmds) {
		t.Error(p)
	}
}

// TestTheFlagSweepCanFail guards the sweep rather than the repository.
//
// A check that cannot fail reports the same clean result as a repository with
// nothing wrong, and this project has recorded that mistake enough times to
// stop trusting a pass on its own. Feeding it a command that sends a key no
// tool declares must produce a complaint; if this ever goes quiet, the sweep
// above is decoration.
func TestTheFlagSweepCanFail(t *testing.T) {
	args := []ArgEntry{{Tool: "app_map", Name: "device", Global: true, SetBy: []string{"--device"}}}
	cmds := []CommandFlags{{
		Command: "map",
		Tools:   []string{"app_map"},
		Sends:   []string{"speeed"},
		Unknown: []string{"speeed"},
	}}
	if problems := CheckFlags(args, cmds); len(problems) == 0 {
		t.Fatal("a command sending an undeclared key produced no complaint")
	}

	// And the other direction: an argument nothing can set.
	orphan := []ArgEntry{{Tool: "app_map", Name: "nobodySetsThis"}}
	if problems := CheckFlags(orphan, nil); len(problems) == 0 {
		t.Fatal("an argument no command can set produced no complaint")
	}
}

// Every error code has its exception in every client, with the right wire
// string.
func TestEveryClientCoversTheErrorCodes(t *testing.T) {
	problems, err := ErrorCoverage(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for lang, ps := range problems {
		for _, p := range ps {
			t.Errorf("%s: %s", lang, p)
		}
	}
}

// And the sweep can fail: a client with one exception removed, one renamed,
// and one mapped to the wrong code is reported three ways.
func TestTheErrorSweepCanFail(t *testing.T) {
	root := filepath.Join("..", "..")
	sources := map[string]string{}
	for _, s := range errorSources {
		files, _ := filepath.Glob(filepath.Join(root, s.Glob))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			sources[s.Language] += string(b) + "\n"
		}
	}
	py := sources["python"]
	py = strings.Replace(py, "class UnsupportedError(MobiumError):", "class UnsupportedThingError(MobiumError):", 1)
	py = strings.Replace(py, `code = "not_confirmed"`, `code = "not_verified"`, 1)
	py = regexp.MustCompile(`(?s)class InternalError\(MobiumError\):.*?code = "internal"`).ReplaceAllString(py, "")
	sources["python"] = py
	got := strings.Join(errorProblems(sources)["python"], "\n")
	for _, want := range []string{"missing Unsupported", "UnsupportedThing is not a code",
		`NotConfirmed is mapped to "not_verified"`, "missing Internal"} {
		if !strings.Contains(got, want) {
			t.Errorf("sweep did not report %q; it said:\n%s", want, got)
		}
	}
	if len(errorProblems(sources)) != 1 {
		t.Errorf("an untouched client was reported: %v", errorProblems(sources))
	}
}

// Every error Mobium creates has a code. A new fmt.Errorf that wraps nothing
// fails this, with the fix: mobiumerr.New and the code that fits.
func TestNoErrorLeavesWithoutACode(t *testing.T) {
	found, err := UnclassifiedErrors(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range found {
		t.Errorf("%s creates an error with no code — use mobiumerr.New(mobiumerr.<Code>, ...), "+
			"or wrap a classified error with %%w; the codes are in docs/guides/cli.md", at)
	}
}

// The scan can see one: a file with a bare fmt.Errorf is reported.
func TestTheUnclassifiedScanCanFail(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package probe\n\nimport (\n\t\"errors\"\n\t\"fmt\"\n)\n\n" +
		"var a = fmt.Errorf(\"bare %d\", 1)\nvar b = fmt.Errorf(\"wraps: %w\", a)\nvar c = errors.New(\"bare\")\n"
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := UnclassifiedErrors(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Errorf("found %v, want the two bare ones and not the wrapping one", found)
	}
}

func TestTheGoClientReadsOnlyWhatTheDaemonSends(t *testing.T) {
	problems, err := CheckGoWireTypes(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestTheWireTypeCheckCanFail(t *testing.T) {
	// The defect it exists for: a key renamed on the daemon's side and not
	// the client's. Rebuilt in a scratch tree, it must be reported.
	root := t.TempDir()
	for dir, src := range map[string]string{
		"clients/go":     "package mobium\ntype Screen struct { Width int `json:\"width\"` }\n",
		"internal/agent": "package agent\ntype ScreenView struct { WidthPx int `json:\"width_px\"` }\n",
	} {
		os.MkdirAll(filepath.Join(root, dir), 0o755)
		os.WriteFile(filepath.Join(root, dir, "x.go"), []byte(src), 0o644)
	}
	saved := GoWireTypes
	GoWireTypes = map[string][2]string{"Screen": {"internal/agent", "ScreenView"}}
	defer func() { GoWireTypes = saved }()
	problems, err := CheckGoWireTypes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], `reads "width"`) {
		t.Errorf("problems = %v, want the renamed key reported", problems)
	}
}

// A command is listed once per tool, however many times it dispatches it:
// double-tap calls app_tap on two paths, and docs/API.md once read
// "tap, double-tap, double-tap".
func TestEachCommandListedOncePerTool(t *testing.T) {
	for _, e := range sweep(t).Tools {
		seen := map[string]bool{}
		for _, c := range strings.Split(e.CLI, ", ") {
			if c != "" && seen[c] {
				t.Errorf("%s lists %q twice: %q", e.Name, c, e.CLI)
			}
			seen[c] = true
		}
	}
}

// Every command is named by its whole path, and none twice: `daemon status`
// and `grid status` were once both listed as `status`.
func TestCommandsAreNamedByTheirPath(t *testing.T) {
	s := sweep(t)
	seen := map[string]bool{}
	for _, c := range s.Extra {
		if seen[c] {
			t.Errorf("%q is listed twice among the commands that dispatch no tool", c)
		}
		seen[c] = true
	}
	for _, want := range []string{"daemon status", "grid status"} {
		if !seen[want] {
			t.Errorf("%q is not listed; got %v", want, s.Extra)
		}
	}
}
