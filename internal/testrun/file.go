// Package testrun is `mobium test`: JSON test files whose steps are app_batch
// steps and whose assertions are app_wait_for and expect, run against the
// devices a config names, with retries, workers and reports. It is a client
// of the tools, as the language clients are, and calls them through whatever
// the caller hands it. See docs/decisions/0006.
package testrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// File is one *.test.json: tests that share an app and a beforeEach.
type File struct {
	// Path is where it was read from, relative to the working directory
	// when it could be made so.
	Path        string `json:"-"`
	Description string `json:"description,omitempty"`
	// App is launched fresh before each test: terminated, then launched.
	App        string `json:"app,omitempty"`
	BeforeEach []Step `json:"beforeEach,omitempty"`
	Tests      []Test `json:"tests"`
}

// Test is one test: a name, and steps run in order.
type Test struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Steps       []Step `json:"steps"`
}

// Step is either a tool call — exactly an app_batch step — or an expect.
type Step struct {
	Name        string                 `json:"name,omitempty"`
	Arguments   map[string]interface{} `json:"arguments,omitempty"`
	Expect      *Expect                `json:"expect,omitempty"`
	Description string                 `json:"description,omitempty"`
}

// Expect retries a read-only tool until one field of its answer matches.
type Expect struct {
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	// Field is a dotted path into the tool's structured answer: "state",
	// "settings.bold_text".
	Field  string      `json:"field"`
	Equals interface{} `json:"equals"`
	// Not waits for the field to be anything but Equals.
	Not       bool `json:"not,omitempty"`
	TimeoutMs int  `json:"timeout_ms,omitempty"`
}

// Title names a test in reports and for -g: "login.test.json › wrong password".
func (f *File) Title(t Test) string { return filepath.Base(f.Path) + " › " + t.Name }

// LoadFile reads and checks one test file. Everything is checked before
// anything runs, as app_batch checks every step: a typo found halfway
// through a suite would leave the device mid-flow with nothing to say where.
func LoadFile(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "cannot read the test file %s: %v", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	// A key the format does not have is a mistake the author wants to hear
	// about: "step" for "steps" would otherwise run a test with none.
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not a test file: %v", path, err)
	}
	f.Path = path
	if len(f.Tests) == 0 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s has no tests", path)
	}
	seen := map[string]bool{}
	for i, s := range f.BeforeEach {
		if err := s.check(fmt.Sprintf("%s beforeEach step %d", path, i+1)); err != nil {
			return nil, err
		}
	}
	for _, t := range f.Tests {
		if strings.TrimSpace(t.Name) == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s has a test with no name", path)
		}
		if seen[t.Name] {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s has two tests named %q", path, t.Name)
		}
		seen[t.Name] = true
		if len(t.Steps) == 0 {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s › %s has no steps", path, t.Name)
		}
		for i, s := range t.Steps {
			if err := s.check(fmt.Sprintf("%s › %s step %d", path, t.Name, i+1)); err != nil {
				return nil, err
			}
		}
	}
	return &f, nil
}

// check refuses a step that could not run: a tool call app_batch would
// refuse, or an expect on a call that acts — retried until it matched, a
// call that taps would tap until the timeout.
func (s Step) check(where string) error {
	switch {
	case s.Expect != nil && s.Name != "":
		return mobiumerr.New(mobiumerr.InvalidArgument, "%s is both a tool call and an expect — one per step", where)
	case s.Expect != nil:
		e := s.Expect
		if e.Tool == "" || e.Field == "" {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%s: an expect needs tool and field", where)
		}
		if err := agent.CheckStep(1, e.Tool, e.Arguments); err != nil {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%s: %v", where, err)
		}
		if !agent.IsReadCall(e.Tool, e.Arguments) {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%s: an expect retries its call until it matches, and "+
				"%s with these arguments changes the device — only a call that reads can be expected on", where, e.Tool)
		}
		if e.TimeoutMs < 0 {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%s: timeout_ms must not be negative", where)
		}
		return nil
	case s.Name != "":
		if err := agent.CheckStep(1, s.Name, s.Arguments); err != nil {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%s: %v", where, err)
		}
		return nil
	}
	return mobiumerr.New(mobiumerr.InvalidArgument, "%s has neither a tool name nor an expect", where)
}

// Config is mobium.config.json.
type Config struct {
	// Dir is where the config was found; TestDir and OutputDir are relative
	// to it.
	Dir       string    `json:"-"`
	TestDir   string    `json:"testDir,omitempty"`
	OutputDir string    `json:"outputDir,omitempty"`
	TimeoutMs int       `json:"timeout,omitempty"`
	Retries   int       `json:"retries,omitempty"`
	Projects  []Project `json:"projects,omitempty"`
}

// Project is a device to run on. An empty Device is the only one attached;
// on a grid, Platform asks for any free device of that platform, which the
// grid's MOBIUM_GRID_MODEL and MOBIUM_GRID_OS narrow.
type Project struct {
	Name     string `json:"name"`
	Device   string `json:"device,omitempty"`
	Driver   string `json:"driver,omitempty"`
	Platform string `json:"platform,omitempty"`
	// unset names the environment variables its device came from that are
	// not set. Refused when the project is run, not when the config is read,
	// so `--project android` works without the iOS project's variable.
	unset []string
}

// ConfigName is the file FindConfig looks for.
const ConfigName = "mobium.config.json"

// FindConfig reads mobium.config.json from dir or the nearest directory
// above it. None found is a config of defaults rooted at dir.
func FindConfig(dir string) (*Config, error) {
	for d := dir; ; d = filepath.Dir(d) {
		p := filepath.Join(d, ConfigName)
		if raw, err := os.ReadFile(p); err == nil {
			return parseConfig(raw, p, d)
		}
		if filepath.Dir(d) == d {
			return &Config{Dir: dir}, nil
		}
	}
}

// LoadConfig reads a config named explicitly.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "cannot read the config %s: %v", path, err)
	}
	return parseConfig(raw, path, filepath.Dir(path))
}

func parseConfig(raw []byte, path, dir string) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not a Mobium config: %v", path, err)
	}
	c.Dir = dir
	names := map[string]bool{}
	for i, p := range c.Projects {
		// A device can come from the environment: a simulator's id is this
		// Mac's, and a phone's is somebody's, so neither belongs in a file
		// that is checked in. An unset variable is refused by name rather
		// than read as "any device", which is a different project.
		if strings.Contains(p.Device, "$") {
			var missing []string
			c.Projects[i].Device = os.Expand(p.Device, func(v string) string {
				val, ok := os.LookupEnv(v)
				if !ok || val == "" {
					missing = append(missing, v)
				}
				return val
			})
			c.Projects[i].unset = missing
		}
		if p.Name == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s has a project with no name", path)
		}
		if names[p.Name] {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s has two projects named %q", path, p.Name)
		}
		if p.Platform != "" && p.Platform != "android" && p.Platform != "ios" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s: project %q has platform %q — android or ios",
				path, p.Name, p.Platform)
		}
		names[p.Name] = true
	}
	if c.TimeoutMs < 0 || c.Retries < 0 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s: timeout and retries must not be negative", path)
	}
	return &c, nil
}

// Discover lists the test files a run covers: those named, directories
// walked for *.test.json, or the config's test directory when nothing is
// named. Sorted, so a run is the same order every time.
func Discover(cfg *Config, paths []string) ([]string, error) {
	if len(paths) == 0 {
		root := cfg.Dir
		if cfg.TestDir != "" {
			root = filepath.Join(cfg.Dir, cfg.TestDir)
		}
		paths = []string{root}
	}
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "no test file or directory %s", p)
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) && path != p {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".test.json") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "no *.test.json files in %s", strings.Join(paths, ", "))
	}
	return files, nil
}
