package mobium

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The tests drive a fake mobium: the test binary re-executes itself, which is
// the standard way to test os/exec without shipping a script. The child sees
// mobiumFakeEnv in its environment and answers JSON-RPC on stdin instead of
// running tests.
const (
	mobiumFakeEnv   = "MOBIUM_GO_CLIENT_FAKE"
	fakeScenarioEnv = "MOBIUM_GO_CLIENT_SCENARIO"
)

func TestMain(m *testing.M) {
	if os.Getenv(mobiumFakeEnv) != "" {
		fakeMobium()
		return
	}
	os.Exit(m.Run())
}

// fakeMobium answers the handshake and a handful of tools, well enough to
// exercise the transport and the decoding of every result shape.
func fakeMobium() {
	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	scenario := os.Getenv(fakeScenarioEnv)
	if path := os.Getenv(fakeNotifyLogEnv); path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			f.WriteString("session=" + os.Getenv("MOBIUM_SESSION") + "\n")
			f.Close()
		}
	}

	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil || req.ID == nil {
			if path := os.Getenv(fakeNotifyLogEnv); path != "" && req.Method != "" {
				if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
					f.WriteString(req.Method + "\n")
					f.Close()
				}
			}
			continue // a notification
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": *req.ID}
		if scenario == "noid" && req.Method == "tools/call" && req.Params.Name == "app_map" {
			// What mobium pipe answers to a line it cannot parse: an error
			// with no id, as JSON-RPC requires.
			out.WriteString(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error","data":"invalid character 'N' looking for beginning of value"}}` + "\n")
			out.Flush()
			continue
		}
		switch {
		case req.Method == "initialize":
			reply["result"] = map[string]any{"protocolVersion": "2024-11-05"}
		case req.Method == "tools/call":
			reply["result"] = fakeTool(req.Params.Name, req.Params.Arguments, scenario)
		default:
			reply["error"] = map[string]any{"code": -32601, "message": "Method not found"}
		}
		body, _ := json.Marshal(reply)
		out.Write(append(body, '\n'))
		out.Flush()
	}
}

func fakeTool(name string, args map[string]any, scenario string) map[string]any {
	text := func(s string, structured any) map[string]any {
		res := map[string]any{"content": []map[string]any{{"type": "text", "text": s}}}
		if structured != nil {
			res["structuredContent"] = structured
		}
		return res
	}
	button := map[string]any{
		"ref": "@e3", "label": "Sign in", "role": "button",
		"locator": map[string]any{"kind": "text", "value": "Sign in", "exact": true},
		"bounds":  map[string]any{"x1": 100, "y1": 200, "x2": 300, "y2": 280},
	}

	switch {
	case scenario == "toolerror":
		return map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text",
				"text": "no element matches text=Nope on the current screen"}},
			"structuredContent": map[string]any{
				"code": "no_such_element", "message": "no element matches text=Nope on the current screen",
				"remedy": "run app_map again", "retryable": false,
				"details": map[string]any{"locator": "text=Nope"},
			},
		}
	case scenario == "olderror":
		// A daemon from before error codes: text only.
		return map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "something went wrong"}},
		}
	case scenario == "hang":
		// Never answers, for the cancellation test. A bare select{} would be
		// spotted by the runtime as a deadlock and kill the process, which
		// looks to the client like a closed pipe rather than a hang.
		time.Sleep(time.Hour)
	}

	switch name {
	case "app_session":
		// Answers as the daemon does: start names the device it got, and
		// echoes the platform and app it was asked for.
		switch args["action"] {
		case "start":
			platform, _ := args["platform"].(string)
			if platform == "" {
				platform = "android"
			}
			driver := "uiautomator2"
			if platform == "ios" {
				driver = "wda"
			}
			app, _ := args["app"].(string)
			return text("session started", map[string]any{"action": "start", "device": "fake-device",
				"platform": platform, "driver": driver, "app": app, "sessions": []any{}})
		case "end":
			return text("session ended", map[string]any{"action": "end", "device": args["device"], "ended": true, "sessions": []any{}})
		default:
			return text("no session is open", map[string]any{"action": "status", "sessions": []any{}})
		}

	case "app_map", "app_find":
		if args["diff"] == true {
			if path := os.Getenv(fakeArgsLogEnv); path != "" {
				if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
					line, _ := json.Marshal(map[string]any{"tool": name, "arguments": args})
					f.Write(append(line, '\n'))
					f.Close()
				}
			}
			view := map[string]any{"elements": []any{button}, "context": "NATIVE_APP", "device": "emulator-5554"}
			if scenario == "firstmap" {
				view["diff"] = map[string]any{"first": true, "added": []any{}, "removed": []any{}, "changed": []any{}}
				return text("no earlier map of this device to compare with", view)
			}
			renamed := map[string]any{"ref": "@e3", "label": "Log in", "role": "button",
				"bounds": button["bounds"]}
			view["diff"] = map[string]any{
				"since":   "2026-09-29T10:00:00Z",
				"added":   []any{map[string]any{"ref": "@e4", "label": "Remember me", "role": "checkbox", "checked": false, "bounds": map[string]any{"x1": 100, "y1": 300, "x2": 300, "y2": 340}}},
				"removed": []any{map[string]any{"ref": "@e5", "label": "Loading", "bounds": map[string]any{"x1": 0, "y1": 0, "x2": 10, "y2": 10}}},
				"changed": []any{map[string]any{"before": renamed, "after": button, "what": []any{"label"}}},
			}
			return text("+ @e4 Remember me (checkbox)", view)
		}
		return text("@e3 Sign in (button)", map[string]any{
			"elements": []any{button}, "context": "NATIVE_APP", "device": "emulator-5554",
		})
	case "app_wait_for":
		if want, has := args["text"]; args["condition"] == "value" && (!has || want != "") {
			// The empty value is the case a client can drop.
			return map[string]any{"isError": true, "content": []map[string]any{{"type": "text",
				"text": fmt.Sprintf("value arrived as %v, present %v", want, has)}}}
		}
		if args["condition"] == "hidden" {
			// Nothing is left to point at once it is gone.
			return text("hidden", map[string]any{"target": args["target"],
				"condition": "hidden", "waited_ms": 12})
		}
		return text("visible", map[string]any{"target": args["target"],
			"condition": "visible", "waited_ms": 12, "element": button})
	case "app_scroll_to":
		return text("on screen after 2 scrolls", map[string]any{
			"target": args["target"], "direction": args["direction"],
			"scrolls": 2, "element": button})
	case "app_devices":
		return text("emulator-5554", map[string]any{"devices": []any{
			map[string]any{"id": "emulator-5554", "platform": "android",
				"state": "device", "model": "Pixel 7", "emulator": true},
		}})
	case "app_current":
		return text("com.example.shop", map[string]any{"app": "com.example.shop"})
	case "app_open_url":
		return text("opened", map[string]any{"url": args["url"], "app": "com.android.chrome"})
	case "app_install":
		return text("installed", map[string]any{"path": "/abs/app.apk"})
	case "app_text":
		return text("Sign in\nForgot password", map[string]any{"text": "Sign in"})
	case "app_screenshot":
		if _, ok := args["path"]; !ok {
			// "iVBORw0KGgo=" is the start of a PNG, which is all the client
			// needs to prove it decoded the base64 rather than the JSON.
			return map[string]any{"content": []map[string]any{
				{"type": "image", "data": "iVBORw0KGgo=", "mimeType": "image/png"}}}
		}
		return text("saved", map[string]any{"path": args["path"], "bytes": 11})
	case "app_upload", "app_download":
		if path := os.Getenv(fakeArgsLogEnv); path != "" {
			// What arrived, so a test can see which arguments were left out.
			if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
				line, _ := json.Marshal(map[string]any{"tool": name, "arguments": args})
				f.Write(append(line, '\n'))
				f.Close()
			}
		}
		if name == "app_download" && args["name"] == nil {
			return text("the Download folder holds 1 file", map[string]any{
				"device": "emulator-5554", "folder": "the Download folder",
				"files": []any{map[string]any{"name": "report.txt", "bytes": 5,
					"modified": "2026-09-29T10:00:00Z"}}})
		}
		view := map[string]any{"device": "emulator-5554", "name": "report.txt",
			"where": "Download/report.txt", "bytes": 5, "checked": "MediaStore"}
		if app, ok := args["app"]; ok {
			view["app"] = app
		}
		if path, ok := args["path"]; ok {
			view["path"] = path
		} else if name == "app_download" {
			view["data"] = "aGVsbG8=" // "hello"
			if scenario == "short" {
				view["bytes"] = 6
			}
		}
		return text(name+" ok", view)
	case "app_contexts":
		return text("NATIVE_APP", map[string]any{"contexts": []any{
			map[string]any{"id": "NATIVE_APP", "current": true},
			map[string]any{"id": "WEBVIEW_com.example", "url": "https://example.com"},
		}, "current": "NATIVE_APP"})
	default:
		// Every acting tool: prose plus an action view.
		return text(name+" ok", map[string]any{"action": name, "x": 200, "y": 240})
	}
}

// fakeNotifyLogEnv names a file the fake appends each notification's method
// to, so a test can see what the client said without a reply to carry it.
const fakeNotifyLogEnv = "MOBIUM_FAKE_NOTIFY_LOG"

// mobium ends the sessions a client started once the client goes away, and a
// crash closes stdin exactly as Close does — so Close says first that it is
// leaving on purpose, or it would quit.
func TestCloseDetachesBeforeItGoes(t *testing.T) {
	log := filepath.Join(t.TempDir(), "notify.log")
	t.Setenv(fakeNotifyLogEnv, log)
	dev := connectFake(t, "")
	if err := dev.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(data)); len(got) != 3 || got[1] != "notifications/initialized" || got[2] != "mobium/detach" {
		t.Errorf("notifications %v, want the handshake's and then mobium/detach", got)
	}
}

// WithSession gives the connection a daemon of its own, which is how two
// parallel runs stop waiting on each other.
func TestWithSessionReachesThePipe(t *testing.T) {
	log := filepath.Join(t.TempDir(), "notify.log")
	t.Setenv(fakeNotifyLogEnv, log)
	t.Setenv(mobiumFakeEnv, "1")
	t.Setenv("MOBIUM_SESSION", "outer")
	dev, err := Connect(WithBinary(os.Args[0]), WithSession("run7"))
	if err != nil {
		t.Fatal(err)
	}
	dev.Close()
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "session=run7\n") {
		t.Errorf("the pipe saw %q, want MOBIUM_SESSION=run7 over the environment's", data)
	}
}

func connectFake(t *testing.T, scenario string) *Device {
	t.Helper()
	t.Setenv(mobiumFakeEnv, "1")
	if scenario != "" {
		t.Setenv(fakeScenarioEnv, scenario)
	}
	dev, err := Connect(WithBinary(os.Args[0]))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { dev.Close() })
	return dev
}

func TestAnElementCarriesItsCheckedState(t *testing.T) {
	var els []Element
	if err := json.Unmarshal([]byte(`[{"ref":"@e1","role":"checkbox","checked":true},`+
		`{"ref":"@e2","role":"switch","checked":false},{"ref":"@e3","role":"button"}]`), &els); err != nil {
		t.Fatal(err)
	}
	if els[0].Checked == nil || !*els[0].Checked || els[1].Checked == nil || *els[1].Checked {
		t.Errorf("checked states lost: %v %v", els[0].Checked, els[1].Checked)
	}
	if els[2].Checked != nil {
		t.Error("a button reported a checked state; nil means it has none")
	}
}

func TestMapDecodesElements(t *testing.T) {
	dev := connectFake(t, "")
	els, err := dev.Map(context.Background())
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if len(els) != 1 {
		t.Fatalf("got %d elements", len(els))
	}
	e := els[0]
	if e.Ref != "@e3" || e.Label != "Sign in" || e.Role != "button" {
		t.Errorf("element = %+v", e)
	}
	if x, y := e.Bounds.Center(); x != 200 || y != 240 {
		t.Errorf("center = (%d, %d), want (200, 240)", x, y)
	}
	if e.Bounds.Width() != 200 || e.Bounds.Height() != 80 {
		t.Errorf("size = %dx%d", e.Bounds.Width(), e.Bounds.Height())
	}
	if e.Locator == nil || e.Locator.String() != "text=Sign in" {
		t.Errorf("locator = %v", e.Locator)
	}
}

func TestMapDiffSendsDiffAndDecodesWhatChanged(t *testing.T) {
	dev, sent := sentArgs(t, "")
	d, err := dev.MapDiff(context.Background())
	if err != nil {
		t.Fatalf("map diff: %v", err)
	}
	if calls := sent(); len(calls) != 1 || len(calls[0]) != 1 || calls[0]["diff"] != true {
		t.Errorf("sent %v, want diff=true alone", calls)
	}
	if d.First {
		t.Error("first on a map that had one to compare with")
	}
	if want := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC); !d.Since.Equal(want) {
		t.Errorf("since = %v, want %v", d.Since, want)
	}
	if len(d.Added) != 1 || d.Added[0].Ref != "@e4" || d.Added[0].Checked == nil || *d.Added[0].Checked {
		t.Errorf("added = %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].Label != "Loading" {
		t.Errorf("removed = %+v", d.Removed)
	}
	if len(d.Changed) != 1 {
		t.Fatalf("changed = %+v", d.Changed)
	}
	c := d.Changed[0]
	if c.Before.Label != "Log in" || c.After.Label != "Sign in" || len(c.What) != 1 || c.What[0] != "label" {
		t.Errorf("change = %+v", c)
	}
	if c.After.Locator == nil || c.After.Locator.String() != "text=Sign in" {
		t.Errorf("after's locator = %v", c.After.Locator)
	}
}

func TestMapDiffSaysWhenThereWasNothingToCompare(t *testing.T) {
	dev := connectFake(t, "firstmap")
	d, err := dev.MapDiff(context.Background())
	if err != nil {
		t.Fatalf("map diff: %v", err)
	}
	if !d.First || !d.Since.IsZero() {
		t.Errorf("first = %v, since = %v; want first and no since", d.First, d.Since)
	}
	if d.Added == nil || d.Removed == nil || d.Changed == nil ||
		len(d.Added)+len(d.Removed)+len(d.Changed) != 0 {
		t.Errorf("a first map listed changes, or sent null lists: %+v", d)
	}
}

func TestWaitForReturnsTheElementAndHiddenReturnsNone(t *testing.T) {
	dev := connectFake(t, "")
	ctx := context.Background()

	el, err := dev.WaitFor(ctx, "text=Sign in", nil)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if el == nil || el.Ref != "@e3" {
		t.Fatalf("element = %+v", el)
	}

	// Nothing is left to point at once it is gone, and that is not an error.
	gone, err := dev.WaitFor(ctx, "text=Loading", &WaitOptions{Condition: Hidden})
	if err != nil {
		t.Fatalf("wait hidden: %v", err)
	}
	if gone != nil {
		t.Errorf("a hidden element came back as %+v", gone)
	}
}

// An empty value is a real question — has the field been cleared — so it has
// to reach the tool, where an empty Text is otherwise left out.
func TestWaitForAnEmptyValueSendsIt(t *testing.T) {
	dev := connectFake(t, "")
	if _, err := dev.WaitFor(context.Background(), "testid=search", &WaitOptions{Condition: HasValue}); err != nil {
		t.Fatalf("wait for an empty value: %v", err)
	}
}

func TestWaitOptionsReachTheTool(t *testing.T) {
	dev := connectFake(t, "")
	var got struct {
		Target    string `json:"target"`
		Condition string `json:"condition"`
	}
	err := dev.Call(context.Background(), "app_wait_for", map[string]any{
		"target": "@e1", "condition": Hidden}, &got)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got.Target != "@e1" || got.Condition != "hidden" {
		t.Errorf("tool saw %+v", got)
	}
}

func TestToolFailureIsAnError(t *testing.T) {
	// A failing tool answers with isError and its reason in the content, not
	// with a protocol error, so the reason has to be lifted out.
	dev := connectFake(t, "toolerror")
	_, err := dev.Map(context.Background())
	if err == nil {
		t.Fatal("a failing tool reported success")
	}
	var me *Error
	if !errors.As(err, &me) {
		t.Fatalf("error is %T, want *mobium.Error", err)
	}
	if me.Tool != "app_map" {
		t.Errorf("error does not name the tool: %+v", me)
	}
	if !strings.Contains(me.Error(), "no element matches") {
		t.Errorf("error %q lost the tool's reason", me)
	}
	// The code travels: a caller branches on it rather than on the text.
	if !errors.Is(err, ErrNoSuchElement) || errors.Is(err, ErrTimedOut) {
		t.Errorf("code = %q; errors.Is did not match it alone", me.Code)
	}
	if me.Remedy == "" || me.Details["locator"] != "text=Nope" {
		t.Errorf("remedy or details lost: %+v", me)
	}
}

func TestAnOlderDaemonStillFailsWithAnError(t *testing.T) {
	dev := connectFake(t, "olderror")
	_, err := dev.Map(context.Background())
	var me *Error
	if !errors.As(err, &me) || me.Code != CodeError || me.Reason != "something went wrong" {
		t.Fatalf("got %#v", err)
	}
	if errors.Is(err, ErrNoSuchElement) {
		t.Error("an unclassified error matched a code")
	}
}

func TestScreenshotDecodesTheImage(t *testing.T) {
	dev := connectFake(t, "")
	png, err := dev.Screenshot(context.Background(), "")
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if len(png) < 4 || png[0] != 0x89 || string(png[1:4]) != "PNG" {
		t.Errorf("got %v, which is not the start of a PNG", png)
	}
}

func TestListsAndScalars(t *testing.T) {
	dev := connectFake(t, "")
	ctx := context.Background()

	devices, err := dev.Devices(ctx)
	if err != nil || len(devices) != 1 || devices[0].Model != "Pixel 7" {
		t.Fatalf("devices = %+v, %v", devices, err)
	}
	if app, err := dev.Current(ctx); err != nil || app != "com.example.shop" {
		t.Errorf("current = %q, %v", app, err)
	}
	if app, err := dev.OpenURL(ctx, "https://example.com"); err != nil || app != "com.android.chrome" {
		t.Errorf("openURL = %q, %v", app, err)
	}
	if path, err := dev.Install(ctx, "app.apk"); err != nil || path != "/abs/app.apk" {
		t.Errorf("install = %q, %v", path, err)
	}
	if text, err := dev.Text(ctx, ""); err != nil || !strings.Contains(text, "Sign in") {
		t.Errorf("text = %q, %v", text, err)
	}
	names, err := dev.Contexts(ctx)
	if err != nil || len(names) != 2 || names[0] != "NATIVE_APP" {
		t.Errorf("contexts = %v, %v", names, err)
	}
}

func TestActionsReturnOnlyAnError(t *testing.T) {
	dev := connectFake(t, "")
	ctx := context.Background()
	for name, act := range map[string]func() error{
		"tap":        func() error { return dev.Tap(ctx, "@e3") },
		"tap point":  func() error { return dev.TapPoint(ctx, 10, 20) },
		"type":       func() error { return dev.Type(ctx, "@e3", "hi") },
		"fill":       func() error { return dev.Fill(ctx, "@e3", "hi") },
		"swipe":      func() error { return dev.Swipe(ctx, "up") },
		"long press": func() error { return dev.LongPress(ctx, "@e3", time.Second) },
		"launch":     func() error { return dev.Launch(ctx, "com.example.shop") },
		"terminate":  func() error { return dev.Terminate(ctx, "com.example.shop") },
	} {
		if err := act(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCancelingEndsTheConnection(t *testing.T) {
	// There is one pipe and replies are told apart only by id, so a call
	// abandoned half-way cannot be resynchronized. Canceling has to end the
	// connection, and the next call has to say so rather than quietly
	// returning the previous call's answer.
	dev := connectFake(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if _, err := dev.Map(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context deadline", err)
	}
	_, err := dev.Map(context.Background())
	if err == nil {
		t.Fatal("the connection was reused after being canceled")
	}
	if !strings.Contains(err.Error(), "no longer usable") {
		t.Errorf("error %q does not explain that the connection is gone", err)
	}
}

func TestAnAnswerWithNoIDFailsTheCallInFlight(t *testing.T) {
	// mobium answers a request it cannot parse with an error that has no id.
	// Skipped as a notification would be, it left the call waiting forever.
	dev := connectFake(t, "noid")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := dev.Map(ctx)
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInvalidArgument {
		t.Fatalf("err = %v, want an invalid_argument error, not a wait", err)
	}
	if !strings.Contains(err.Error(), "could not read the request") {
		t.Errorf("error %q does not say the request was unreadable", err)
	}
	// The pipe answered exactly that request, so the connection is still in step.
	if _, err := dev.Current(context.Background()); err != nil {
		t.Errorf("the next call failed: %v", err)
	}
}

func TestStartOpensTheSessionAndQuitEndsIt(t *testing.T) {
	t.Setenv(mobiumFakeEnv, "1")
	ctx := context.Background()
	dev, err := Start(ctx, WithBinary(os.Args[0]), WithPlatform("ios"), WithApp("com.apple.Preferences"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	s := dev.Session()
	if s == nil || s.Device != "fake-device" || s.Platform != "ios" || s.Driver != "wda" || s.App != "com.apple.Preferences" {
		t.Fatalf("session = %+v, want the device, platform, driver and app start was given", s)
	}
	if err := dev.Quit(ctx); err != nil {
		t.Fatalf("quit: %v", err)
	}
	// A deferred Quit after an explicit one does nothing.
	if err := dev.Quit(ctx); err != nil {
		t.Errorf("a second quit failed: %v", err)
	}
	if _, err := dev.Map(ctx); err == nil {
		t.Error("a call after quit succeeded")
	}
}

func TestADeviceFromConnectHasNoSession(t *testing.T) {
	dev := connectFake(t, "")
	if dev.Session() != nil {
		t.Error("Connect reported a session it did not start")
	}
}

func TestFindBinaryRejectsSomethingThatIsNotOne(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindBinary(dir + "/nothing-here"); err == nil {
		t.Error("accepted a path with no binary at it")
	}
	if _, err := FindBinary(dir); err == nil {
		t.Error("accepted a directory as the binary")
	}
}

// plantMobium puts an executable named mobium in dir, the kind of file a
// library must never run just because it sits beside a test.
func plantMobium(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Named as Windows names a program, or there the negative half of the
	// test below would pass by finding nothing at all.
	p := filepath.Join(dir, "mobium"+exeSuffix)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

var exeSuffix = map[bool]string{true: ".exe"}[runtime.GOOS == "windows"]

func TestFindBinaryNeverSearchesTheCurrentDirectory(t *testing.T) {
	t.Setenv("MOBIUM_BIN_PATH", "")
	dir := t.TempDir()
	plantMobium(t, dir)
	plantMobium(t, filepath.Join(dir, "bin"))
	t.Chdir(dir)
	for _, path := range []string{".", "bin", "./bin", ""} {
		t.Setenv("PATH", path)
		if found, err := FindBinary(""); err == nil {
			t.Errorf("PATH=%q: found %s, a binary in the current directory", path, found)
		}
	}
	// The positive control: the same file, on PATH by its absolute directory.
	t.Setenv("PATH", filepath.Join(dir, "bin"))
	if found, err := FindBinary(""); err != nil || found != filepath.Join(dir, "bin", "mobium"+exeSuffix) {
		t.Errorf("an absolute PATH entry was not searched: %q, %v", found, err)
	}
}

func TestConnectFailsWhenMobiumIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MOBIUM_BIN_PATH", "")
	// Beside a mobium it must not find: the current directory is never
	// searched, in ./bin or through a relative PATH entry.
	dir := t.TempDir()
	plantMobium(t, dir)
	plantMobium(t, filepath.Join(dir, "bin"))
	t.Setenv("PATH", ".")
	t.Chdir(dir)
	if _, err := Connect(); err == nil {
		t.Error("connected with no mobium anywhere")
	} else if !strings.Contains(err.Error(), "MOBIUM_BIN_PATH") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}

// fakeArgsLogEnv names a file the fake appends each file tool's arguments to.
const fakeArgsLogEnv = "MOBIUM_FAKE_ARGS_LOG"

// sentArgs connects to the fake with its argument log on, and returns a
// function that reads back every call's arguments so far.
func sentArgs(t *testing.T, scenario string) (*Device, func() []map[string]any) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "args.log")
	t.Setenv(fakeArgsLogEnv, log)
	dev := connectFake(t, scenario)
	return dev, func() []map[string]any {
		data, _ := os.ReadFile(log)
		var calls []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var c struct {
				Arguments map[string]any `json:"arguments"`
			}
			if line != "" && json.Unmarshal([]byte(line), &c) == nil {
				calls = append(calls, c.Arguments)
			}
		}
		return calls
	}
}

func TestUploadSendsOnlyWhatIsSet(t *testing.T) {
	dev, sent := sentArgs(t, "")
	ctx := context.Background()
	up, err := dev.Upload(ctx, "report.txt", nil)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if up.Where != "Download/report.txt" || up.Bytes != 5 || up.Checked != "MediaStore" ||
		up.Path != "report.txt" || up.App != "" || up.Device != "emulator-5554" {
		t.Errorf("upload = %+v", up)
	}
	up, err = dev.Upload(ctx, "local.txt", &TransferOptions{Name: "report.txt", App: "dev.mobium.app"})
	if err != nil {
		t.Fatalf("upload with options: %v", err)
	}
	if up.App != "dev.mobium.app" {
		t.Errorf("app = %q, want the one named", up.App)
	}
	calls := sent()
	if len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}
	if len(calls[0]) != 1 || calls[0]["path"] != "report.txt" {
		t.Errorf("with no options sent %v, want the path alone", calls[0])
	}
	if len(calls[1]) != 3 || calls[1]["name"] != "report.txt" || calls[1]["app"] != "dev.mobium.app" {
		t.Errorf("with options sent %v", calls[1])
	}
}

func TestDownloadSavesAndReportsWhere(t *testing.T) {
	dev, sent := sentArgs(t, "")
	ctx := context.Background()
	got, err := dev.Download(ctx, "report.txt", "/tmp/out.txt", "")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if got.Path != "/tmp/out.txt" || got.Name != "report.txt" || got.Bytes != 5 || got.App != "" {
		t.Errorf("download = %+v", got)
	}
	if got, err := dev.Download(ctx, "report.txt", "/tmp/out.txt", "dev.mobium.app"); err != nil || got.App != "dev.mobium.app" {
		t.Errorf("download for an app = %+v, %v", got, err)
	}
	calls := sent()
	if len(calls) != 2 || len(calls[0]) != 2 || calls[0]["path"] != "/tmp/out.txt" || calls[1]["app"] != "dev.mobium.app" {
		t.Errorf("sent %v", calls)
	}
	// With no name the tool lists the folder instead, and with no path it
	// answers in base64, so Download requires both and sends nothing without.
	for _, c := range [][2]string{{"", "/tmp/out.txt"}, {"report.txt", ""}} {
		if _, err := dev.Download(ctx, c[0], c[1], ""); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("download(%q, %q) = %v, want invalid_argument", c[0], c[1], err)
		}
	}
	if n := len(sent()); n != 2 {
		t.Errorf("%d calls reached the tool, want the refused ones kept back", n)
	}
}

func TestDownloadBytesDecodesTheFile(t *testing.T) {
	dev, sent := sentArgs(t, "")
	raw, err := dev.DownloadBytes(context.Background(), "report.txt", "")
	if err != nil {
		t.Fatalf("download bytes: %v", err)
	}
	if string(raw) != "hello" {
		t.Errorf("got %q, want the base64 decoded", raw)
	}
	if calls := sent(); len(calls) != 1 || len(calls[0]) != 1 || calls[0]["name"] != "report.txt" {
		t.Errorf("sent %v, want the name alone", calls)
	}
	if _, err := dev.DownloadBytes(context.Background(), "", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("no name = %v, want invalid_argument", err)
	}
}

// A file whose size does not match what the device reported is not the file.
func TestDownloadBytesRefusesAShortFile(t *testing.T) {
	dev := connectFake(t, "short")
	if _, err := dev.DownloadBytes(context.Background(), "report.txt", ""); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("err = %v, want not_confirmed", err)
	}
}

func TestDownloadsListsTheFolder(t *testing.T) {
	dev, sent := sentArgs(t, "")
	ctx := context.Background()
	files, err := dev.Downloads(ctx, "")
	if err != nil {
		t.Fatalf("downloads: %v", err)
	}
	want := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	if len(files) != 1 || files[0].Name != "report.txt" || files[0].Bytes != 5 || !files[0].Modified.Equal(want) {
		t.Errorf("files = %+v", files)
	}
	if _, err := dev.Downloads(ctx, "dev.mobium.app"); err != nil {
		t.Fatal(err)
	}
	calls := sent()
	if len(calls) != 2 || len(calls[0]) != 0 || len(calls[1]) != 1 || calls[1]["app"] != "dev.mobium.app" {
		t.Errorf("sent %v, want nothing, then the app alone", calls)
	}
}
