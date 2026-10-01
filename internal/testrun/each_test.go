package testrun

import (
	"strings"
	"testing"
)

// A test with "each" is one test per case, Playwright's parameterized test:
// ${key} in its steps is the case's value, a step argument that is nothing
// but ${key} keeps the value's type, and with no ${key} in the name each
// case is numbered rather than named after its values.
func TestEachCaseIsATest(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "login.test.json", `{"app": "dev.mobium.mobiumapp", "tests": [
	  {"name": "a wrong sign-in is refused", "each": [
	    {"user": "mobium", "pass": "wrong", "rows": 3},
	    {"user": "nobody", "pass": "hunter2", "rows": 4}
	  ], "steps": [
	    {"fill": {"target": "testid=username", "text": "${user}"}, "description": "as ${user}"},
	    {"fill": {"target": "testid=password", "text": "${pass}"}},
	    {"wait_for": {"target": "role=input", "condition": "count", "count": "${rows}"}, "soft": true},
	    {"expect": {"tool": "app_text", "arguments": {"target": "testid=loginResult"}, "field": "text", "equals": "no ${user}"}},
	    {"tap": "text=costs $${price}"}
	  ]},
	  {"name": "signs in as ${user}", "each": [{"user": "mobium"}, {"user": "admin"}], "steps": [
	    {"fill": {"target": "testid=username", "text": "${user}"}}
	  ]},
	  {"name": "a plain test with a literal", "steps": [{"tap": "text=$${not a key}"}]}
	]}`)
	f, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tt := range f.Tests {
		names = append(names, tt.Name)
	}
	want := "a wrong sign-in is refused [1]|a wrong sign-in is refused [2]|signs in as mobium|signs in as admin|a plain test with a literal"
	if got := strings.Join(names, "|"); got != want {
		t.Fatalf("tests\n  %s\nwant\n  %s", got, want)
	}
	second := f.Tests[1]
	if got := second.Steps[0].Arguments["text"]; got != "nobody" {
		t.Errorf("${user} in case 2 is %v", got)
	}
	if second.Steps[0].Description != "as nobody" {
		t.Errorf("the description was not filled in: %q", second.Steps[0].Description)
	}
	if got, ok := second.Steps[2].Arguments["count"].(float64); !ok || got != 4 || !second.Steps[2].Soft {
		t.Errorf("a count given as ${rows} is %#v, soft %v — want the number 4, still soft", second.Steps[2].Arguments["count"], second.Steps[2].Soft)
	}
	if e := second.Steps[3].Expect; e == nil || e.Equals != "no nobody" || e.Arguments["target"] != "testid=loginResult" {
		t.Errorf("the expect was not filled in: %+v", e)
	}
	if got := second.Steps[4].Arguments["target"]; got != "text=costs ${price}" {
		t.Errorf("$${ was not a literal ${: %v", got)
	}
	if got := f.Tests[4].Steps[0].Arguments["target"]; got != "text=${not a key}" {
		t.Errorf("$${ in a plain test was not a literal ${: %v", got)
	}
}

// Every mistake is refused before anything runs, saying which.
func TestEachMistakesAreRefused(t *testing.T) {
	for name, c := range map[string]struct{ body, says string }{
		"a key a case lacks": {`{"tests": [{"name": "x", "each": [{"user": "a"}, {"name": "b"}],
			"steps": [{"tap": "text=${user}"}]}]}`, "case 2 uses ${user}, which the case does not have (it has name)"},
		"a key with no each": {`{"tests": [{"name": "x", "steps": [{"tap": "text=${user}"}]}]}`, `has no "each"`},
		"no cases":           {`{"tests": [{"name": "x", "each": [], "steps": [{"tap": "@e1"}]}]}`, "no cases"},
		"two cases, one name": {`{"tests": [{"name": "as ${u}", "each": [{"u": "a"}, {"u": "a"}],
			"steps": [{"tap": "@e1"}]}]}`, `two tests named "as a"`},
		"a case's step that could not run": {`{"tests": [{"name": "x", "each": [{"t": "app_tap"}],
			"steps": [{"expect": {"tool": "${t}", "field": "x", "equals": 1}}]}]}`, ""},
	} {
		p := write(t, t.TempDir(), "x.test.json", c.body)
		_, err := LoadFile(p)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
