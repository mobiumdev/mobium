package webview

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeEval stands in for a page, recording what was evaluated and replying
// with whatever the test wants.
type fakeEval struct {
	scripts []string
	replies []string
	err     error
}

func (f *fakeEval) Evaluate(_ context.Context, script string) (string, error) {
	f.scripts = append(f.scripts, script)
	if f.err != nil {
		return "", f.err
	}
	if len(f.replies) == 0 {
		return "", nil
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r, nil
}

// TestInstallScriptDoesNotClobber guards the two decisions that separate this
// from the recipe everyone publishes.
func TestInstallScriptDoesNotClobber(t *testing.T) {
	f := &fakeEval{replies: []string{"installed"}}
	if err := InstallConsole(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	script := f.scripts[0]

	// The original console methods must still be called, or the browser's own
	// devtools go quiet and anyone debugging by hand loses their logs.
	if !strings.Contains(script, "original.apply(console, arguments)") {
		t.Error("the shim swallows console output instead of passing it on")
	}
	// window.onerror must be chained. The published recipe is
	// `window.onerror = console.error.bind(console)`, which discards whatever
	// the app installed — a test tool changing the app under test.
	if !strings.Contains(script, "previous.apply(this, arguments)") {
		t.Error("the shim replaces window.onerror instead of chaining to it")
	}
	// Installing twice must not wrap the console twice, or every message
	// would be recorded once per installation.
	if !strings.Contains(script, `if (window.__mobiumConsole) { return "already"; }`) {
		t.Error("the shim is not idempotent")
	}
	// The buffer lives in the page under test; unbounded growth would be this
	// tool changing the thing it is measuring.
	if !strings.Contains(script, "buf.shift()") {
		t.Error("the buffer is unbounded")
	}
	if strings.Contains(script, "__LIMIT__") {
		t.Error("the buffer limit was not substituted")
	}
}

// TestConsoleFiltersInThePage: filtering after the drain would be shorter and
// would quietly destroy the entries at other levels — asking for errors would
// throw away the warnings and nothing would say so.
func TestConsoleFiltersInThePage(t *testing.T) {
	f := &fakeEval{replies: []string{`[{"level":"error","text":"boom","time":1}]`}}
	got, err := Console(context.Background(), f, "error")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "boom" {
		t.Fatalf("got %+v", got)
	}
	if !strings.Contains(f.scripts[0], `const want = "error"`) {
		t.Error("the level was not passed into the page")
	}
	if !strings.Contains(f.scripts[0], "for (const e of keep) { buf.push(e); }") {
		t.Error("entries at other levels are not put back")
	}
}

// TestConsoleReinstallsAfterNavigation: a navigation replaces the document,
// taking the buffer and the wrapped console with it. Without reinstalling,
// logs would stop after the first link was followed, silently.
func TestConsoleReinstallsAfterNavigation(t *testing.T) {
	f := &fakeEval{replies: []string{"null", "installed"}}
	got, err := Console(context.Background(), f, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("reported %d entries from a page that had none captured", len(got))
	}
	if len(f.scripts) != 2 {
		t.Fatalf("made %d calls, want a drain then an install", len(f.scripts))
	}
	if !strings.Contains(f.scripts[1], "__mobiumConsole = buf") {
		t.Error("the second call was not an install")
	}
}

func TestConsoleSurfacesABrokenBuffer(t *testing.T) {
	f := &fakeEval{replies: []string{"not json at all"}}
	if _, err := Console(context.Background(), f, ""); err == nil {
		t.Error("accepted a console buffer that is not JSON")
	}
	f = &fakeEval{err: errors.New("page is gone")}
	if _, err := Console(context.Background(), f, ""); err == nil {
		t.Error("swallowed an evaluate failure")
	}
}
