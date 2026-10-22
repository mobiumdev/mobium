package codegen

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/testrun"
)

// everyShape is one step of every shape the table writes, in the shorthand
// the inspector records, with a label that needs escaping in every one of
// the five languages.
const everyShape = `[
  {"tap": "text=\"Network & internet\""},
  {"tap": {"target": "@e2", "double": true}},
  {"tap": {"x": 540, "y": 1200}},
  {"tap": {"x": 10, "y": 20, "double": true}},
  {"type": {"target": "testid=email", "text": "it's \"quoted\"\\ and\nnew"}},
  {"fill": {"target": "label=Volume", "text": "0.3"}},
  {"long_press": "label=Row 4"},
  {"long_press": {"target": "label=Row 4", "duration_ms": 2000}},
  {"check": "testid=terms"},
  {"check": {"target": "testid=news", "checked": false}},
  {"swipe": {"direction": "up"}},
  {"swipe": {"direction": "left", "target": "label=Card 1"}},
  {"press": "back"},
  {"scroll_to": "text=System"},
  {"scroll_to": {"target": "label=Card 8", "direction": "right"}},
  {"wait_for": "text=Welcome — Добро пожаловать"},
  {"wait_for": {"target": "role=progressbar", "condition": "hidden"}},
  {"wait_for": {"target": "testid=status", "condition": "text", "text": "Sent", "exact": true, "timeout_ms": 5000}},
  {"wait_for": {"target": "testid=terms", "condition": "checked"}},
  {"wait_for": {"target": "testid=status", "condition": "text", "text": "Sending", "not": true}},
  {"wait_for": {"target": "testid=search", "condition": "value", "text": ""}},
  {"context": "WEBVIEW_dev.mobium.mobiumapp"},
  {"keyboard": {"text": "coffee", "key": "enter"}},
  {"keyboard": {"hide": true}},
  {"launch": "com.android.settings"}
]`

func steps(t *testing.T, raw string) []testrun.Step {
	t.Helper()
	var out []testrun.Step
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func program(t *testing.T, lang string) string {
	t.Helper()
	src, err := Program(lang, "dev.mobium.mobiumapp", steps(t, everyShape))
	if err != nil {
		t.Fatalf("%s: %v", lang, err)
	}
	return src
}

// Each program compiles, where this machine has the language: a recording
// that downloads as something that does not run is no use to anyone. Java
// and .NET are compiled by docs/checks/codegen.sh, which builds their
// clients first.
func TestEveryProgramCompiles(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		lang, tool string
		check      func(dir, file string) *exec.Cmd
	}{
		{"python", "python3", func(dir, file string) *exec.Cmd {
			return exec.Command("python3", "-I", "-m", "py_compile", file)
		}},
		{"javascript", "node", func(dir, file string) *exec.Cmd { return exec.Command("node", "--check", file) }},
		{"go", "go", func(dir, file string) *exec.Cmd {
			mod := "module recorded\n\ngo 1.22\n\nrequire github.com/mobiumdev/mobium/clients/go v0.0.0\n\n" +
				"replace github.com/mobiumdev/mobium/clients/go => " + filepath.Join(root, "clients", "go") + "\n"
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "vet", ".")
			cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
			return cmd
		}},
	} {
		t.Run(c.lang, func(t *testing.T) {
			if _, err := exec.LookPath(c.tool); err != nil {
				t.Skipf("no %s here", c.tool)
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "recorded"+Extension[c.lang])
			if err := os.WriteFile(file, []byte(program(t, c.lang)), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := c.check(dir, file)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s does not compile: %v\n%s\n%s", c.lang, err, out, program(t, c.lang))
			}
		})
	}
}

// Every method a program calls is one the client defines, read from the
// client's source, so a client renaming one breaks this and not a user's
// pasted test.
func TestEveryCallIsAClientMethod(t *testing.T) {
	for lang, c := range map[string]struct {
		source  string
		call    *regexp.Regexp
		defines func(name string) *regexp.Regexp
	}{
		"python": {"../../clients/python/mobium/_device.py", regexp.MustCompile(`device\.(\w+)\(`),
			func(n string) *regexp.Regexp { return regexp.MustCompile(`def ` + n + `\(`) }},
		"javascript": {"../../clients/javascript/index.js", regexp.MustCompile(`device\.(\w+)\(`),
			func(n string) *regexp.Regexp { return regexp.MustCompile(`(?m)^  (async )?` + n + `\(`) }},
		"go": {"../../clients/go/mobium.go", regexp.MustCompile(`dev\.(\w+)\(`),
			func(n string) *regexp.Regexp { return regexp.MustCompile(`func \(d \*Device\) ` + n + `\(`) }},
		"java": {"../../clients/java/src/main/java/dev/mobium/Mobium.java", regexp.MustCompile(`device\.(\w+)\(`),
			func(n string) *regexp.Regexp { return regexp.MustCompile(`public [^(=]*\b` + n + `\(`) }},
		"dotnet": {"../../clients/dotnet/Mobium/Device.cs", regexp.MustCompile(`device\.(\w+)\(`),
			func(n string) *regexp.Regexp { return regexp.MustCompile(`public [^(=]*\b` + n + `\(`) }},
	} {
		src, err := os.ReadFile(c.source)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, m := range c.call.FindAllStringSubmatch(program(t, lang), -1) {
			seen[m[1]] = true
		}
		if len(seen) < 12 {
			t.Errorf("%s: only %d methods found in the program — the pattern is wrong", lang, len(seen))
		}
		for name := range seen {
			if !c.defines(name).Match(src) {
				t.Errorf("%s: the program calls %s, which %s does not define", lang, name, c.source)
			}
		}
	}
}

// A recording made fresh starts the way a test does: the app stopped and
// launched again, before the first recorded step.
func TestAProgramStartsFromTheAppFresh(t *testing.T) {
	src := program(t, "python")
	term := strings.Index(src, `device.terminate("dev.mobium.mobiumapp")`)
	launch := strings.Index(src, `device.launch("dev.mobium.mobiumapp")`)
	first := strings.Index(src, `device.tap(`)
	if term < 0 || launch < term || first < launch {
		t.Errorf("not terminate, launch, then the steps:\n%s", src)
	}
}

// Text a person typed reaches the program as typed: quotes, a backslash, a
// new line and Cyrillic, in each language's own literal.
func TestTextIsQuotedForEachLanguage(t *testing.T) {
	for lang, want := range map[string]string{
		"python":     `device.type("testid=email", "it's \"quoted\"\\ and\nnew")`,
		"javascript": `await device.type('testid=email', 'it\'s "quoted"\\ and\nnew')`,
		"go":         `dev.Type(ctx, "testid=email", "it's \"quoted\"\\ and\nnew")`,
		"java":       `device.type("testid=email", "it's \"quoted\"\\ and\nnew");`,
		"dotnet":     `device.Type("testid=email", "it's \"quoted\"\\ and\nnew");`,
	} {
		src := program(t, lang)
		if !strings.Contains(src, want) {
			t.Errorf("%s has no %s:\n%s", lang, want, src)
		}
		if !strings.Contains(src, "Добро пожаловать") {
			t.Errorf("%s did not keep the Cyrillic as it is", lang)
		}
	}
}

// A step the table cannot write exactly is refused, naming it, rather than
// written as a different step.
func TestWhatCannotBeWrittenIsRefused(t *testing.T) {
	for name, raw := range map[string]string{
		"a tool no recording holds":  `[{"screenshot": {"path": "x.png"}}]`,
		"an argument dropped":        `[{"tap": {"target": "@e1", "fingers": 2}}]`,
		"a wait for a count":         `[{"wait_for": {"target": "role=row", "condition": "count", "count": 3}}]`,
		"text without its condition": `[{"wait_for": {"target": "testid=x", "text": "Sent"}}]`,
		"an expect":                  `[{"name": "app_state", "arguments": {"app": "x"}, "expect": {"tool": "app_state", "field": "state", "equals": "foreground"}}]`,
		"double false":               `[{"tap": {"target": "@e1", "double": false}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, lang := range Languages {
				_, err := Program(lang, "", steps(t, raw))
				if mobiumerr.CodeOf(err) != mobiumerr.Unsupported || !strings.Contains(err.Error(), "step 1") {
					t.Errorf("%s: %v", lang, err)
				}
			}
		})
	}
	if _, err := Program("ruby", "", nil); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("an unknown language: %v", err)
	}
}
