package webview

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Console output from a page, captured without subscribing to protocol events.
//
// CDP has `Log.entryAdded` and WebKit has its own equivalent, and using either
// would mean this package growing an event loop and a subscription model for
// one feature. It does not need one: a script can buffer console output in the
// page and a second script can read the buffer back, which is the same
// evaluate-and-read shape as `Map` and `Text`. One implementation serves both
// platforms, where the platforms' own routes are two mechanisms with two
// different data shapes — chromedriver's `browser` log type on Android, a non-standard
// `safariConsole` on iOS whose entries are themselves JSON.

// consoleBufferLimit bounds what the page holds. A chatty app can log
// thousands of lines a minute, and this buffer lives in the page under test:
// growing it without limit would be this tool changing the behavior of the
// thing it is measuring.
const consoleBufferLimit = 500

// installConsoleScript starts capturing. It is idempotent — running it twice
// must not wrap the console twice, or every message would be recorded once per
// installation.
//
// Two things here are deliberate and were the whole point of reading how other
// tools do it:
//
//   - **The original console methods are still called.** Capturing output by
//     swallowing it would make the browser's own devtools go quiet, and anyone
//     debugging by hand would find the tool had taken their logs away.
//   - **`window.onerror` is chained, not replaced.** The usual published
//     recipe is `window.onerror = console.error.bind(console)`, which discards
//     whatever handler the app installed. An app that reports its own errors
//     would silently stop doing so — a test tool changing the behavior of the
//     thing under test, which is the one thing it must never do.
const installConsoleScript = `(() => {
  if (window.__mobiumConsole) { return "already"; }
  const buf = [];
  window.__mobiumConsole = buf;
  const push = (level, text) => {
    try {
      buf.push({ level: level, text: String(text), time: Date.now() });
      if (buf.length > __LIMIT__) { buf.shift(); }
    } catch (e) { /* never let logging break the page */ }
  };
  const render = (args) => Array.prototype.map.call(args, (a) => {
    try {
      if (a instanceof Error) { return a.stack || (a.name + ": " + a.message); }
      if (typeof a === "object" && a !== null) { return JSON.stringify(a); }
      return String(a);
    } catch (e) { return String(a); }
  }).join(" ");
  ["log", "info", "warn", "error", "debug"].forEach((level) => {
    const original = console[level];
    console[level] = function () {
      push(level, render(arguments));
      if (original) { try { original.apply(console, arguments); } catch (e) {} }
    };
  });
  const previous = window.onerror;
  window.onerror = function (message, source, line) {
    push("error", message + " (" + (source || "?") + ":" + (line || "?") + ")");
    if (typeof previous === "function") { return previous.apply(this, arguments); }
    return false;
  };
  window.addEventListener("unhandledrejection", (e) => {
    push("error", "Unhandled promise rejection: " + ((e && e.reason) || "?"));
  });
  return "installed";
})()`

// drainConsoleScript returns everything buffered and empties the buffer, so
// each read reports what happened since the last one. That is the semantics
// every other tool in this space uses, and it means a test can assert "nothing
// was logged during this step" rather than only "nothing was ever logged".
const drainConsoleScript = `(() => {
  const buf = window.__mobiumConsole;
  if (!buf) { return "null"; }
  const want = "__LEVEL__";
  const out = [];
  const keep = [];
  for (const e of buf) {
    if (want === "" || e.level === want) { out.push(e); } else { keep.push(e); }
  }
  buf.length = 0;
  for (const e of keep) { buf.push(e); }
  return JSON.stringify(out);
})()`

// ConsoleEntry is one line the page logged.
type ConsoleEntry struct {
	// Level is "log", "info", "warn", "error" or "debug". Uncaught errors and
	// unhandled promise rejections arrive as "error".
	Level string `json:"level"`
	Text  string `json:"text"`
	// Time is milliseconds since the epoch, as the page saw it.
	Time int64 `json:"time"`
}

// InstallConsole starts capturing console output in a page.
//
// Called when a context is entered rather than when logs are first read,
// because anything logged before the shim is installed is gone. It still
// cannot catch what happened before that — a page that threw during load has
// already thrown — and callers are told so rather than left to assume
// otherwise.
func InstallConsole(ctx context.Context, e evaluator) error {
	// Substituted rather than formatted: this is a JavaScript program, and
	// running fmt.Sprintf over it would break the day somebody writes a
	// modulo or a percent sign into the script.
	script := strings.ReplaceAll(installConsoleScript, "__LIMIT__",
		strconv.Itoa(consoleBufferLimit))
	_, err := e.Evaluate(ctx, script)
	return err
}

// Console returns what the page has logged since the last call, installing the
// capture first if it is not there.
//
// Reinstalling matters more than it looks: a navigation replaces the document,
// taking the buffer and the wrapped console with it. Without this, logs would
// simply stop after the first link was followed, silently.
//
// A level filter is applied **in the page**, so entries at other levels stay
// buffered for the next read. Filtering after the drain would have been two
// lines shorter and would quietly destroy them: asking for errors would throw
// away the warnings, and nothing would say so.
func Console(ctx context.Context, e evaluator, level string) ([]ConsoleEntry, error) {
	script := strings.ReplaceAll(drainConsoleScript, "__LEVEL__", level)
	raw, err := e.Evaluate(ctx, script)
	if err != nil {
		return nil, err
	}
	if raw == "null" || raw == "" {
		if err := InstallConsole(ctx, e); err != nil {
			return nil, err
		}
		// Nothing was captured before this moment, and saying so is more
		// useful than an empty list that looks like "the page logged nothing".
		return nil, nil
	}
	var entries []ConsoleEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("could not read the page's console buffer: %w", err)
	}
	return entries, nil
}
