package mobium

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
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
			continue // a notification
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": *req.ID}
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
	case "app_map", "app_find":
		return text("@e3 Sign in (button)", map[string]any{
			"elements": []any{button}, "context": "NATIVE_APP", "device": "emulator-5554",
		})
	case "app_wait_for":
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
		"replace":    func() error { return dev.Replace(ctx, "@e3", "hi") },
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

func TestCancellingEndsTheConnection(t *testing.T) {
	// There is one pipe and replies are told apart only by id, so a call
	// abandoned half-way cannot be resynchronised. Canceling has to end the
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

func TestFindBinaryRejectsSomethingThatIsNotOne(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindBinary(dir + "/nothing-here"); err == nil {
		t.Error("accepted a path with no binary at it")
	}
	if _, err := FindBinary(dir); err == nil {
		t.Error("accepted a directory as the binary")
	}
}

func TestConnectFailsWhenMobiumIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MOBIUM_BIN_PATH", "")
	// Away from the repo, where the ./bin/mobium fallbacks would find one.
	t.Chdir(t.TempDir())
	if _, err := Connect(); err == nil {
		t.Error("connected with no mobium anywhere")
	} else if !strings.Contains(err.Error(), "MOBIUM_BIN_PATH") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}
