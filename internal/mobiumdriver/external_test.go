package mobiumdriver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The driver protocol is the one part of Mobium meant to be implemented by
// somebody who cannot read this code, so these tests drive it from the far
// side: testdata/fakedriver imports nothing from this module and knows only
// what docs/decisions/0003 says. If it ever needs something from here, the
// extension point is not open after all.

var driverBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mobium-driver-test")
	if err != nil {
		panic(err)
	}
	driverBinary = filepath.Join(dir, "mobium-driver-fake")
	build := exec.Command("go", "build", "-o", driverBinary, "./testdata/fakedriver")
	if out, err := build.CombinedOutput(); err != nil {
		// Not a skip: a reference driver that does not compile is a broken
		// contract, and a skipped test would hide it.
		panic("could not build the reference driver: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// start brings up the reference driver in a given mode.
func start(t *testing.T, mode string) *External {
	t.Helper()
	t.Setenv("MOBIUM_FAKE_MODE", mode)
	d := NewExternal("fake", driverBinary, "device-1")
	if err := d.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestExternalHandshake(t *testing.T) {
	d := start(t, "")

	if d.Name() != "fake/plain" {
		t.Errorf("name = %q, want the name the driver reported", d.Name())
	}
	for _, c := range []string{CapGestures, CapText, CapApps, CapHealth} {
		if !d.HasCapability(c) {
			t.Errorf("advertised capability %q not recorded", c)
		}
	}
	// Not advertised, so it must not be offered — this is the whole point of
	// the negotiation.
	for _, c := range []string{CapInventory, CapAppearance, CapPermissions, CapPermissionState} {
		if d.HasCapability(c) {
			t.Errorf("capability %q was never advertised but is reported as present", c)
		}
	}
}

// TestCapabilitiesGateTheInterfaces is the one that matters to the layer above.
// The external driver has every method compiled in, so a bare type assertion
// would claim every capability for every driver. The As* helpers are what stop
// that, and this proves they do.
func TestCapabilitiesGateTheInterfaces(t *testing.T) {
	full := start(t, "")
	if _, ok := AsGesturer(full); !ok {
		t.Error("gestures were advertised but AsGesturer refused")
	}
	if _, ok := AsAppInventory(full); ok {
		t.Error("AsAppInventory accepted a driver that never advertised inventory")
	}

	minimal := start(t, "minimal")
	for name, got := range map[string]bool{
		"gestures":   second(AsGesturer(minimal)),
		"text":       second(AsTextEntry(minimal)),
		"apps":       second(AsAppControl(minimal)),
		"appearance": second(AsAppearance(minimal)),
		"inventory":  second(AsAppInventory(minimal)),
	} {
		if got {
			t.Errorf("a driver advertising nothing was still offered %s", name)
		}
	}
	// Starter is not gated: every external driver needs stopping.
	if _, ok := AsStarter(minimal); !ok {
		t.Error("an external driver must always be closeable")
	}
}

func second[T any](_ T, ok bool) bool { return ok }

// TestBuiltInDriversAreUngated guards the other direction: the helpers must not
// change how a compiled-in backend answers, or adding them would have silently
// disabled features on Android.
func TestBuiltInDriversAreUngated(t *testing.T) {
	var d Driver = NewAndroid(nil)
	if _, ok := AsGesturer(d); !ok {
		t.Error("the Android backend lost its gestures")
	}
	if _, ok := AsAppControl(d); !ok {
		t.Error("the Android backend lost app control")
	}
	if _, ok := AsPermissionReader(d); !ok {
		t.Error("the Android backend lost permission reading")
	}
	if _, ok := AsTextEntry(d); ok {
		t.Error("the dump backend must still refuse text entry")
	}
}

func TestExternalSnapshot(t *testing.T) {
	d := start(t, "")
	tree, err := d.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// The driver sent no depth, index or path. They must be here anyway, and
	// numbered the way the XML parsers number them.
	screen := tree.Root.Children[0]
	if screen.Class != "Screen" || screen.Depth != 0 || screen.Path != "0" {
		t.Errorf("root of the driver's tree: class=%q depth=%d path=%q",
			screen.Class, screen.Depth, screen.Path)
	}
	row := screen.Children[0].Children[1]
	if row.Text != "second" {
		t.Fatalf("walked to the wrong node: %+v", row)
	}
	if row.Path != "0/0/1" {
		t.Errorf("path = %q, want 0/0/1", row.Path)
	}
	if row.Depth != 2 {
		t.Errorf("depth = %d, want 2", row.Depth)
	}
	if row.Parent == nil || row.Parent.Class != "List" {
		t.Error("the parent link was not rebuilt")
	}
	if row.Index != 1 {
		t.Errorf("index = %d, want 1", row.Index)
	}
	// Absent in the JSON, so it must default to visible.
	if !row.Displayed {
		t.Error("a node with no displayed field was treated as hidden")
	}
	if row.Bounds.Y1 != 100 || row.Bounds.Y2 != 200 {
		t.Errorf("bounds = %+v", row.Bounds)
	}
}

func TestExternalScreenshot(t *testing.T) {
	png, err := start(t, "").Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !hasPNGMagic(png) {
		t.Errorf("got %d bytes that are not a PNG", len(png))
	}
}

// TestExternalRejectsNonPNG covers the failure every device tool here makes:
// putting an error message where the payload goes and reporting success.
func TestExternalRejectsNonPNG(t *testing.T) {
	_, err := start(t, "bad-png").Screenshot(context.Background())
	if err == nil {
		t.Fatal("accepted a screenshot that is not a PNG")
	}
	if !strings.Contains(err.Error(), "not a PNG") {
		t.Errorf("error = %v", err)
	}
}

// TestArgumentsCrossThePipe checks the far side received what was sent, rather
// than only that the near side did not error. A tap that silently arrives as
// (0,0) would pass any test that trusts the return value.
func TestArgumentsCrossThePipe(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("MOBIUM_FAKE_LOG", log)
	d := start(t, "")

	ctx := context.Background()
	if err := d.Tap(ctx, 120, 640); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if err := d.Swipe(ctx, 1, 2, 3, 4, 250*time.Millisecond); err != nil {
		t.Fatalf("Swipe: %v", err)
	}
	if err := d.Launch(ctx, "com.example.app"); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	// setText addresses a node by the path the driver's own hierarchy implies.
	tree, err := d.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	target := tree.Root.Children[0].Children[0].Children[0]
	if err := d.SetText(ctx, target, "hello"); err != nil {
		t.Fatalf("SetText: %v", err)
	}

	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the driver recorded nothing: %v", err)
	}
	got := string(body)
	for _, want := range []string{
		`tap {"x":120,"y":640}`,
		`"durationMs":250`,
		`launch {"appId":"com.example.app"}`,
		`"path":"0/0/0"`,
		`"text":"hello"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the driver never saw %s\nit saw:\n%s", want, got)
		}
	}
}

// TestDebugOutputOnStdoutDoesNotBreakTheChannel is the mistake every author of
// a stdio protocol makes once. Mobium skips what is not a reply to the request
// in flight, so a stray print is a nuisance rather than a hang.
func TestDebugOutputOnStdoutDoesNotBreakTheChannel(t *testing.T) {
	d := start(t, "chatty")
	if _, err := d.Snapshot(context.Background()); err != nil {
		t.Fatalf("a debug print on stdout broke the channel: %v", err)
	}
	if err := d.Tap(context.Background(), 1, 1); err != nil {
		t.Fatalf("second call after stray output: %v", err)
	}
}

func TestVersionMismatchIsRefused(t *testing.T) {
	t.Setenv("MOBIUM_FAKE_MODE", "wrong-version")
	d := NewExternal("fake", driverBinary, "")
	err := d.Start(context.Background(), nil)
	if err == nil {
		d.Close()
		t.Fatal("a driver speaking another protocol version was accepted")
	}
	if !strings.Contains(err.Error(), "protocol version") {
		t.Errorf("error = %v", err)
	}
}

// TestDriverThatDiesIsReportedWithItsLog: without the stderr tail, the user
// sees "the driver exited" and has nowhere to go. The driver said why.
func TestDriverThatDiesIsReportedWithItsLog(t *testing.T) {
	t.Setenv("MOBIUM_FAKE_MODE", "exit-at-once")
	d := NewExternal("fake", driverBinary, "")
	err := d.Start(context.Background(), nil)
	if err == nil {
		d.Close()
		t.Fatal("a driver that exited immediately was accepted")
	}
	if !strings.Contains(err.Error(), "no such thing") {
		t.Errorf("the driver's own explanation was not passed on: %v", err)
	}
}

func TestSilentDriverTimesOut(t *testing.T) {
	t.Setenv("MOBIUM_FAKE_MODE", "silent")
	d := NewExternal("fake", driverBinary, "")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- d.Start(ctx, nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a driver that never answered was accepted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start hung on a silent driver instead of giving up")
	}
	d.Close()
}

// TestUnhealthyDriverIsNoticed is what lets a cached session drop a driver that
// has lost its device, rather than failing every command from then on.
func TestUnhealthyDriverIsNoticed(t *testing.T) {
	if !start(t, "").Healthy(context.Background()) {
		t.Error("a healthy driver reported unhealthy")
	}
	if start(t, "unhealthy").Healthy(context.Background()) {
		t.Error("an unhealthy driver reported healthy")
	}
}

func TestDeadDriverIsNotHealthy(t *testing.T) {
	d := start(t, "")
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if d.Healthy(context.Background()) {
		t.Error("a driver that has exited reported healthy")
	}
	if _, err := d.Snapshot(context.Background()); err == nil {
		t.Error("a call to a dead driver succeeded")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	d := start(t, "")
	for i := 0; i < 3; i++ {
		if err := d.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
	}
}

// TestDriverErrorsReachTheUser: a driver's message is the whole of the
// diagnosis for a platform Mobium knows nothing about, so it is passed through
// verbatim, attributed.
func TestDriverErrorsReachTheUser(t *testing.T) {
	err := start(t, "").Uninstall(context.Background(), "com.example")
	if err == nil {
		t.Fatal("expected the driver's refusal")
	}
	if !strings.Contains(err.Error(), "inventory was not advertised") {
		t.Errorf("the driver's message did not survive: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "fake/plain:") {
		t.Errorf("the error is not attributed to the driver: %v", err)
	}
}

func TestMalformedSnapshotIsRejected(t *testing.T) {
	_, err := start(t, "broken-snapshot").Snapshot(context.Background())
	if err == nil {
		t.Fatal("accepted a snapshot that is not a hierarchy")
	}
	if !strings.Contains(err.Error(), "hierarchy") {
		t.Errorf("error = %v", err)
	}
}

func TestUnknownCapabilityIsIgnoredNotFatal(t *testing.T) {
	var said []string
	t.Setenv("MOBIUM_FAKE_MODE", "unknown-cap")
	d := NewExternal("fake", driverBinary, "")
	if err := d.Start(context.Background(), func(s string) { said = append(said, s) }); err != nil {
		t.Fatalf("an unrecognised capability should not stop a driver: %v", err)
	}
	defer d.Close()
	if d.HasCapability("teleportation") {
		t.Error("an unrecognised capability was recorded as usable")
	}
	// Ignoring it silently is how a driver author loses an afternoon.
	if !strings.Contains(strings.Join(said, "\n"), "teleportation") {
		t.Errorf("the ignored capability was not mentioned: %v", said)
	}
}

func TestFindDriver(t *testing.T) {
	t.Run("environment override wins", func(t *testing.T) {
		t.Setenv("MOBIUM_DRIVER_ROKU", driverBinary)
		got, err := FindDriver("roku")
		if err != nil || got != driverBinary {
			t.Errorf("FindDriver = %q, %v", got, err)
		}
	})

	t.Run("override pointing nowhere says so", func(t *testing.T) {
		t.Setenv("MOBIUM_DRIVER_ROKU", "/no/such/driver")
		_, err := FindDriver("roku")
		if err == nil || !strings.Contains(err.Error(), "MOBIUM_DRIVER_ROKU") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("found on PATH by convention", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "mobium-driver-tizen")
		if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		got, err := FindDriver("tizen")
		if err != nil || got != exe {
			t.Errorf("FindDriver = %q, %v", got, err)
		}
	})

	t.Run("missing driver explains both halves", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := FindDriver("nowhere")
		if err == nil {
			t.Fatal("expected an error")
		}
		for _, want := range []string{"built-in", "mobium-driver-nowhere", "MOBIUM_DRIVER_NOWHERE"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the message does not mention %q: %v", want, err)
			}
		}
	})
}
