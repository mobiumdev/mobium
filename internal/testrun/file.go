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
	"regexp"
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
	// Each runs the test once per case, a test over its data:
	// ${key} in its name and steps is that case's value. See expandEach.
	Each []map[string]interface{} `json:"each,omitempty"`
}

// Step is either a tool call — exactly an app_batch step — or an expect.
type Step struct {
	Name        string                 `json:"name,omitempty"`
	Arguments   map[string]interface{} `json:"arguments,omitempty"`
	Expect      *Expect                `json:"expect,omitempty"`
	Description string                 `json:"description,omitempty"`
	// Soft marks an assertion whose failure is recorded and the test goes
	// on, so one run reports every check that failed. Only an assertion can
	// be soft: an action that failed leaves nothing after it to check.
	Soft bool `json:"soft,omitempty"`
}

// UnmarshalJSON reads a step in its long form, or in the shorthand: one key
// that is a tool's name without "app_", whose value is the tool's arguments
// or, as a string, its main one —
//
//	{"tap": "label=Login Demo"}
//	{"fill": {"target": "testid=username", "text": "mobium"}}
//
// The shorthand is only a spelling: it becomes the long form here, and is
// checked as the long form is. Any key the step does not have is refused,
// as DisallowUnknownFields would have — "step" for "steps" still says so.
func (s *Step) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	type long Step
	var l long
	short := map[string]json.RawMessage{}
	own := map[string]bool{"name": true, "arguments": true, "expect": true, "description": true, "soft": true}
	for k, v := range raw {
		if !own[k] {
			short[k] = v
			delete(raw, k)
		}
	}
	rest, _ := json.Marshal(raw)
	dec := json.NewDecoder(bytes.NewReader(rest))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return err
	}
	*s = Step(l)
	if len(short) == 0 {
		return nil
	}
	if len(short) > 1 || s.Name != "" || s.Expect != nil || s.Arguments != nil {
		keys := make([]string, 0, len(short))
		for k := range short {
			keys = append(keys, fmt.Sprintf("%q", k))
		}
		sort.Strings(keys)
		return mobiumerr.New(mobiumerr.InvalidArgument, "a step is one tool call: %s beside name, arguments or another shorthand", strings.Join(keys, ", "))
	}
	for k, v := range short {
		tool := "app_" + k
		schema, ok := toolSchema(tool)
		if !ok {
			return mobiumerr.New(mobiumerr.InvalidArgument, "unknown field %q: not a step's own field, and %s is not a tool", k, tool)
		}
		s.Name = tool
		var str string
		if json.Unmarshal(v, &str) == nil {
			arg, err := mainArgument(tool, schema)
			if err != nil {
				return err
			}
			s.Arguments = map[string]interface{}{arg: str}
			return nil
		}
		var args map[string]interface{}
		if err := json.Unmarshal(v, &args); err != nil {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%q takes a string or an object of %s's arguments", k, tool)
		}
		s.Arguments = args
	}
	return nil
}

// label names a step in a trace or a debugger: its tool, or the expect's.
func (s Step) label() string {
	if s.Expect != nil {
		return "expect " + s.Expect.Tool
	}
	return s.Name
}

// longForm is the step as its long form spells it.
func (s Step) longForm() interface{} {
	if s.Expect != nil {
		return map[string]interface{}{"expect": s.Expect}
	}
	return map[string]interface{}{"name": s.Name, "arguments": s.argumentsOrEmpty()}
}

func (s Step) argumentsOrEmpty() map[string]interface{} {
	if s.Arguments == nil {
		return map[string]interface{}{}
	}
	return s.Arguments
}

// Shorthand is a tool call spelled as the step shorthand reads it: the tool's
// name without "app_", and its main argument as a string when that is all
// the call has, or its arguments. What `mobium inspect` records, so a test it
// writes reads the way one written by hand does.
func Shorthand(name string, args map[string]interface{}) map[string]interface{} {
	short := strings.TrimPrefix(name, "app_")
	if len(args) == 1 {
		if schema, ok := toolSchema(name); ok {
			if main, err := mainArgument(name, schema); err == nil {
				if v, ok := args[main].(string); ok {
					return map[string]interface{}{short: v}
				}
			}
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	return map[string]interface{}{short: args}
}

// toolSchema is a tool's input schema.
func toolSchema(name string) (map[string]interface{}, bool) {
	for _, t := range agent.GetToolSchemas() {
		if t.Name == name {
			return t.InputSchema, true
		}
	}
	return nil, false
}

// targetNeedsMore are the tools whose target is not enough alone: a swipe
// on an element needs its direction too, so {"swipe": "@e5"} would be
// refused only once it ran, and {"swipe": "up"} would look for an element
// called "up". They have no shorthand, as before they took a target.
var targetNeedsMore = map[string]bool{"app_swipe": true}

// mainArgument is what a string in the shorthand fills: target when the tool
// has one, else its one required string, else its only argument — read from
// the schema, so a tool added later needs nothing here. A tool with no single
// answer is refused, saying what to write instead.
func mainArgument(tool string, schema map[string]interface{}) (string, error) {
	props, _ := schema["properties"].(map[string]interface{})
	var own []string
	for k := range props {
		if k != "device" && k != "driver" {
			own = append(own, k)
		}
	}
	sort.Strings(own)
	if _, ok := props["target"]; ok && !targetNeedsMore[tool] {
		return "target", nil
	}
	var strs []string
	if req, ok := schema["required"].([]interface{}); ok {
		for _, r := range req {
			if name, _ := r.(string); name != "" {
				if p, _ := props[name].(map[string]interface{}); p != nil && p["type"] == "string" {
					strs = append(strs, name)
				}
			}
		}
	}
	if req, ok := schema["required"].([]string); ok {
		for _, name := range req {
			if p, _ := props[name].(map[string]interface{}); p != nil && p["type"] == "string" {
				strs = append(strs, name)
			}
		}
	}
	short := strings.TrimPrefix(tool, "app_")
	switch {
	case len(strs) == 1:
		return strs[0], nil
	case len(own) == 1:
		if p, _ := props[own[0]].(map[string]interface{}); p != nil && p["type"] == "string" {
			return own[0], nil
		}
	case len(own) == 0:
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%q takes no arguments; write {%q: {}}", short, short)
	}
	return "", mobiumerr.New(mobiumerr.InvalidArgument, "%q has no one main argument (it takes %s); write {%q: {...}} with them named",
		short, strings.Join(own, ", "), short)
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
	// Cases become tests before anything is checked, so each is checked as
	// any test is — its name unique, every step one that could run.
	tests, err := expandEach(path, f.Tests)
	if err != nil {
		return nil, err
	}
	f.Tests = tests
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
	if s.Soft && s.Expect == nil && s.Name != "app_wait_for" {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%s is soft, and only an assertion can be — "+
			"a wait_for or an expect; an action that failed leaves nothing after it to check", where)
	}
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
	// to it unless absolute — see Resolve.
	Dir       string `json:"-"`
	TestDir   string `json:"testDir,omitempty"`
	OutputDir string `json:"outputDir,omitempty"`
	TimeoutMs int    `json:"timeout,omitempty"`
	Retries   int    `json:"retries,omitempty"`
	// Trace is --trace's default: on, off or retain-on-failure.
	Trace    string    `json:"trace,omitempty"`
	Projects []Project `json:"projects,omitempty"`
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

// Resolve is a path from the config, as the config means it: relative to
// the config's own folder, or as it stands when absolute. Joined blindly, an
// absolute testDir was looked for inside the config's folder.
func (c *Config) Resolve(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.Dir, p)
}

// Discover lists the test files a run covers: those named, directories
// walked for *.test.json, or the config's test directory when nothing is
// named. Sorted, so a run is the same order every time.
func Discover(cfg *Config, paths []string) ([]string, error) {
	if len(paths) == 0 {
		root := cfg.Dir
		if cfg.TestDir != "" {
			root = cfg.Resolve(cfg.TestDir)
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

// placeholderRe is ${key} in a test's name or steps; $${ is a literal ${.
var placeholderRe = regexp.MustCompile(`\$\$\{|\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEach makes a test with "each" one test per case, as a test in a
// loop over its data is. ${key} anywhere in a step's strings is
// the case's value, and a string that is nothing but ${key} takes the
// value's own type, so a count stays a number. The name is the case's too:
// with a ${key} in it, substituted, and without one, numbered — "[1]",
// "[2]" — rather than spelled out from the values, which may be a password.
// A key a case does not have is refused here, before anything runs.
func expandEach(path string, tests []Test) ([]Test, error) {
	var out []Test
	for _, t := range tests {
		if t.Each == nil {
			if err := noPlaceholders(path, t); err != nil {
				return nil, err
			}
			// What is left is $${, the escape, which is ${ as sent.
			if raw, _ := json.Marshal(t.Steps); !strings.Contains(string(raw), "$${") {
				out = append(out, t)
				continue
			}
			t.Each = []map[string]interface{}{{}}
			cases, err := expandEach(path, []Test{t})
			if err != nil {
				return nil, err
			}
			cases[0].Name = t.Name
			out = append(out, cases[0])
			continue
		}
		if len(t.Each) == 0 {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s › %s has \"each\" with no cases", path, t.Name)
		}
		// Every field of every step — its call, description, soft — as the
		// long form, which Step reads back as it reads a file.
		raw, err := json.Marshal(t.Steps)
		if err != nil {
			return nil, err
		}
		var generic interface{}
		if err := json.Unmarshal(raw, &generic); err != nil {
			return nil, err
		}
		named := placeholderRe.MatchString(strings.ReplaceAll(t.Name, "$${", ""))
		for i, c := range t.Each {
			where := fmt.Sprintf("%s › %s, case %d", path, t.Name, i+1)
			steps, err := substitute(generic, c, where)
			if err != nil {
				return nil, err
			}
			b, err := json.Marshal(steps)
			if err != nil {
				return nil, err
			}
			var ct Test
			if err := json.Unmarshal(b, &ct.Steps); err != nil {
				return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s: %v", where, err)
			}
			name, err := substituteString(t.Name, c, where, false)
			if err != nil {
				return nil, err
			}
			ct.Name = fmt.Sprint(name)
			if !named {
				ct.Name = fmt.Sprintf("%s [%d]", t.Name, i+1)
			}
			ct.Description = t.Description
			out = append(out, ct)
		}
	}
	return out, nil
}

// substitute replaces every ${key} in v's strings with the case's values.
func substitute(v interface{}, c map[string]interface{}, where string) (interface{}, error) {
	switch x := v.(type) {
	case string:
		return substituteString(x, c, where, true)
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			s, err := substitute(e, c, where)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, e := range x {
			s, err := substitute(e, c, where)
			if err != nil {
				return nil, err
			}
			out[k] = s
		}
		return out, nil
	}
	return v, nil
}

// substituteString replaces ${key} in s; when s is nothing but one ${key}
// and typed is set, the value itself, of whatever type, is the answer.
func substituteString(s string, c map[string]interface{}, where string, typed bool) (interface{}, error) {
	if m := placeholderRe.FindStringSubmatch(s); typed && m != nil && m[0] == s && m[1] != "" {
		v, ok := c[m[1]]
		if !ok {
			return nil, missingKey(where, m[1], c)
		}
		return v, nil
	}
	var missing string
	out := placeholderRe.ReplaceAllStringFunc(s, func(p string) string {
		if p == "$${" {
			return "${"
		}
		key := p[2 : len(p)-1]
		v, ok := c[key]
		if !ok {
			if missing == "" {
				missing = key
			}
			return p
		}
		return fmt.Sprint(v)
	})
	if missing != "" {
		return nil, missingKey(where, missing, c)
	}
	return out, nil
}

func missingKey(where, key string, c map[string]interface{}) error {
	var have []string
	for k := range c {
		have = append(have, k)
	}
	sort.Strings(have)
	return mobiumerr.New(mobiumerr.InvalidArgument, "%s uses ${%s}, which the case does not have (it has %s)",
		where, key, strings.Join(have, ", "))
}

// noPlaceholders refuses ${key} in a test with no "each": it would be sent
// as written, and a step that types "${user}" is a test that cannot fail
// the way its author meant.
func noPlaceholders(path string, t Test) error {
	raw, _ := json.Marshal(t.Steps)
	for _, s := range []string{t.Name, string(raw)} {
		for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
			if m[1] != "" {
				return mobiumerr.New(mobiumerr.InvalidArgument, "%s › %s uses ${%s} and has no \"each\" to fill it "+
					"in — add \"each\": [{%q: ...}], or write $${ for a literal ${", path, t.Name, m[1], m[1])
			}
		}
	}
	return nil
}
