// Package codegen turns a test's steps into a program in one of the five
// clients' languages: the inspector's recording, downloaded as code someone
// can paste where their tests live.
//
// Every step is one tool call and every client has every tool, so this is
// one table from step to call, written once per language — not a second
// recorder. A step it cannot write exactly is refused, naming the step,
// rather than written as something that does a different thing: a dropped
// argument would replay as a different test that still passes.
// docs/guides/inspector.md, "Download it as code".
package codegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/testrun"
)

// Languages are the clients a recording can be written for, as the
// inspector's download names them.
var Languages = []string{"python", "javascript", "go", "java", "dotnet"}

// Extension is the file name ending each language's program is saved with.
var Extension = map[string]string{
	"python": ".py", "javascript": ".mjs", "go": ".go", "java": ".java", "dotnet": ".cs",
}

// call is one step as a client call: the method, by each client's spelling,
// and its arguments in that call's order.
type call struct {
	tool string
	args map[string]interface{}
}

// Program writes steps as a program in lang that starts from the app
// launched fresh, as `mobium test` starts each test, when app is not "".
func Program(lang, app string, steps []testrun.Step) (string, error) {
	w, ok := writers[lang]
	if !ok {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "no client language %q: one of %s", lang,
			strings.Join(Languages, ", "))
	}
	var calls []call
	if app != "" {
		calls = append(calls, call{"app_terminate", map[string]interface{}{"app": app}},
			call{"app_launch", map[string]interface{}{"app": app}})
	}
	for i, s := range steps {
		if s.Expect != nil || s.Soft {
			return "", unsupported(i, s, "an expect is not written as code yet; record the check as a wait")
		}
		if err := known(s); err != nil {
			return "", unsupported(i, s, err.Error())
		}
		calls = append(calls, call{s.Name, s.Arguments})
	}
	var b builder
	if err := w(&b, calls); err != nil {
		return "", err
	}
	return b.String(), nil
}

// unsupported names a step the language table cannot write exactly.
func unsupported(i int, s testrun.Step, why string) error {
	return mobiumerr.New(mobiumerr.Unsupported, "step %d (%s) cannot be written as code: %s", i+1,
		strings.TrimPrefix(s.Name, "app_"), why).
		WithRemedy("download the recording as a test file and run it with mobium test")
}

// shapes are the arguments each tool may be written with, and the one set
// of them each shape needs. Anything else on a step is refused: a client
// call that dropped it would be a different step.
var shapes = map[string][][]string{
	"app_tap":        {{"target"}, {"target", "double"}, {"x", "y"}, {"x", "y", "double"}},
	"app_type":       {{"target", "text"}},
	"app_fill":       {{"target", "text"}},
	"app_long_press": {{"target"}, {"target", "duration_ms"}},
	"app_set":        {{"target"}, {"target", "value"}},
	"app_swipe":      {{"direction"}, {"direction", "target"}},
	"app_press":      {{"button"}},
	"app_scroll_to":  {{"target"}, {"target", "direction"}},
	"app_wait_for":   nil, // target with any of waitOptions; see known
	"app_launch":     {{"app"}},
	"app_terminate":  {{"app"}},
	"app_context":    {{"context"}},
	"app_keyboard":   nil, // any of text, key, hide; see known
}

// waitOptions are what a wait may carry beyond its target, and
// waitConditions the conditions a client names: count needs a number
// that no recording makes, so it is not one of them.
var waitOptions = map[string]bool{"condition": true, "text": true, "timeout_ms": true, "exact": true, "not": true}

var waitConditions = map[string]bool{"visible": true, "hidden": true, "text": true, "value": true,
	"enabled": true, "disabled": true, "checked": true, "unchecked": true, "selected": true,
	"unselected": true, "focused": true}

// known reports whether a step is one the table writes, with arguments of
// the right types.
func known(s testrun.Step) error {
	allowed, ok := shapes[s.Name]
	if !ok {
		return mobiumerr.New(mobiumerr.Unsupported, "%s is not in the table of calls a recording can hold", s.Name)
	}
	keys := sortedKeys(s.Arguments)
	switch s.Name {
	case "app_wait_for":
		if _, ok := s.Arguments["target"].(string); !ok {
			return mobiumerr.New(mobiumerr.Unsupported, "a wait needs a target")
		}
		for _, k := range keys {
			if k != "target" && !waitOptions[k] {
				return mobiumerr.New(mobiumerr.Unsupported, "a wait's %s has no client spelling here", k)
			}
		}
		cond, _ := s.Arguments["condition"].(string)
		if cond == "" {
			cond = "visible"
		}
		if !waitConditions[cond] {
			return mobiumerr.New(mobiumerr.Unsupported, "a wait for %q has no client spelling here", cond)
		}
		if _, hasText := s.Arguments["text"]; hasText != (cond == "text" || cond == "value") {
			return mobiumerr.New(mobiumerr.Unsupported, "a wait's text goes with the text and value conditions, and they need it")
		}
	case "app_keyboard":
		if len(keys) == 0 {
			return mobiumerr.New(mobiumerr.Unsupported, "a keyboard step with nothing to do")
		}
		for _, k := range keys {
			if k != "text" && k != "key" && k != "hide" {
				return mobiumerr.New(mobiumerr.Unsupported, "a keyboard step's %s has no client spelling here", k)
			}
		}
	default:
		matched := false
		for _, shape := range allowed {
			if strings.Join(sorted(shape), ",") == strings.Join(keys, ",") {
				matched = true
			}
		}
		if !matched {
			return mobiumerr.New(mobiumerr.Unsupported, "its arguments %s are not a shape the table writes", strings.Join(keys, ", "))
		}
	}
	for k, v := range s.Arguments {
		switch k {
		case "target", "text", "direction", "button", "app", "context", "condition", "key":
			if _, ok := v.(string); !ok {
				return mobiumerr.New(mobiumerr.Unsupported, "%s must be a string", k)
			}
		case "x", "y", "duration_ms", "timeout_ms":
			if _, ok := wholeNumber(v); !ok {
				return mobiumerr.New(mobiumerr.Unsupported, "%s must be a whole number", k)
			}
		case "double", "checked", "exact", "not", "hide":
			if _, ok := v.(bool); !ok {
				return mobiumerr.New(mobiumerr.Unsupported, "%s must be true or false", k)
			}
		}
	}
	if d, ok := s.Arguments["double"].(bool); ok && !d {
		return mobiumerr.New(mobiumerr.Unsupported, "double false is a plain tap: leave it out")
	}
	return nil
}

func wholeNumber(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

func sortedKeys(m map[string]interface{}) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

// builder is a program being written, line by line.
type builder struct {
	strings.Builder
	indent string
}

func (b *builder) line(format string, a ...interface{}) {
	if format == "" {
		b.WriteString("\n")
		return
	}
	b.WriteString(b.indent)
	fmt.Fprintf(b, format, a...)
	b.WriteString("\n")
}

func str(a map[string]interface{}, k string) string { s, _ := a[k].(string); return s }
func num(a map[string]interface{}, k string) int    { n, _ := wholeNumber(a[k]); return n }
func flag(a map[string]interface{}, k string) bool  { b, _ := a[k].(bool); return b }
func has(a map[string]interface{}, k string) bool   { _, ok := a[k]; return ok }

// writers write a whole program, one per language.
var writers = map[string]func(*builder, []call) error{
	"python":     writePython,
	"javascript": writeJavaScript,
	"go":         writeGo,
	"java":       writeJava,
	"dotnet":     writeDotnet,
}

// header is the first line of every program.
const header = "Recorded with mobium inspect."
