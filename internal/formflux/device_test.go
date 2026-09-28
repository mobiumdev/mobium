package formflux

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// The tests above parse captured output. These drive a real emulator, because
// a fixture can establish that the parser reads `wm size` and nothing more —
// it cannot establish that `wm size` does what this package claims, that the
// change is visible to anything, or that reset really puts it back.
//
// This project has 54 recorded defects and 40 of them needed a device.
//
//	MOBIUM_DEVICE_TESTS=1 go test ./internal/formflux/ -run Device -v
//
// It restores the screen on the way out even when it fails, because a
// half-applied profile is a state left on somebody's phone.
func deviceOrSkip(t *testing.T) *device.ADB {
	t.Helper()
	if os.Getenv("MOBIUM_DEVICE_TESTS") != "1" {
		t.Skip("set MOBIUM_DEVICE_TESTS=1 to run tests that change a running device's screen")
	}
	adb, err := device.New(os.Getenv("MOBIUM_DEVICE"))
	if err != nil {
		t.Skipf("no adb: %v", err)
	}
	return adb
}

func TestDeviceAppliesAndResetsEveryAndroidProfile(t *testing.T) {
	adb := deviceOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	before, err := Read(ctx, adb)
	if err != nil {
		t.Fatalf("could not read the screen to begin with: %v", err)
	}
	if before.Overridden() {
		t.Fatalf("the device already has an override in force (%s) — reset it before running this, "+
			"or the physical size recorded here is somebody else's override", before)
	}
	t.Logf("physical screen: %s", before)

	// Always put it back, even if an assertion below stops the test.
	defer func() {
		after, err := Reset(context.Background(), adb)
		if err != nil {
			t.Errorf("could not reset the screen: %v", err)
			return
		}
		if after.WidthPx != before.WidthPx || after.HeightPx != before.HeightPx || after.DPI != before.DPI {
			t.Errorf("the device came back as %s, not the %s it started at", after, before)
		}
	}()

	for _, p := range For(Android) {
		p := p
		t.Run(p.Name, func(t *testing.T) {
			got, err := Apply(ctx, adb, p)
			if err != nil {
				t.Fatalf("%s: %v", p.Name, err)
			}
			t.Logf("%-19s -> %s", p.Name, got)

			// The readback inside Apply is one witness. The physical values
			// surviving underneath is another, and it is the one that proves
			// this is an override rather than a device that has genuinely
			// changed shape.
			if got.PhysicalWidthPx != before.PhysicalWidthPx || got.PhysicalDPI != before.PhysicalDPI {
				t.Errorf("the physical screen changed under an override: %s", got)
			}
			if p.WidthPx != before.WidthPx && !got.SizeOverridden {
				t.Errorf("%s asked for a different size and no override is in force: %s", p.Name, got)
			}
		})
	}
}

// TestDeviceResetIsIdempotent matters because a check that fails halfway will
// call reset on a device that is already clean, and that must not look like a
// failure.
func TestDeviceResetIsIdempotent(t *testing.T) {
	adb := deviceOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	first, err := Reset(ctx, adb)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Reset(ctx, adb)
	if err != nil {
		t.Fatalf("resetting an already-clean device failed: %v", err)
	}
	if first.String() != second.String() {
		t.Errorf("two resets disagreed: %s then %s", first, second)
	}
}

// TestDeviceFindsWhatBreaksWhenTheScreenShrinks is the whole point of the
// package: apply each profile, read the screen, and report what is wrong at
// that size that was not wrong at the baseline.
//
// It reads the hierarchy through `uiautomator dump` rather than a driver,
// because that is the zero-install path and this test should not depend on an
// APK being installed to answer a question about layout. The dump file is a
// serialization of whatever is on screen, so it is deleted after every read —
// on somebody's phone that file is their data.
func TestDeviceFindsWhatBreaksWhenTheScreenShrinks(t *testing.T) {
	adb := deviceOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	app := os.Getenv("MOBIUM_APP")
	if app == "" {
		app = "com.android.settings"
	}

	before, err := Read(ctx, adb)
	if err != nil {
		t.Fatal(err)
	}
	if before.Overridden() {
		t.Fatalf("the device already has an override in force: %s", before)
	}
	defer func() {
		if _, err := Reset(context.Background(), adb); err != nil {
			t.Errorf("could not reset the screen: %v", err)
		}
	}()

	var baseline []Finding
	for i, p := range For(Android) {
		screen, err := Apply(ctx, adb, p)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		if _, err := adb.Shell(ctx, "am", "start", "-W", "-n",
			app+"/.Settings", "-a", "android.intent.action.MAIN"); err != nil {
			t.Logf("%s: could not launch %s: %v", p.Name, app, err)
		}
		time.Sleep(2 * time.Second)

		tree, err := dumpTree(ctx, adb)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		rect := uitree.Rect{X2: screen.WidthPx, Y2: screen.HeightPx}
		found := Inspect(tree, rect, screen.DPI, Android)

		if i == 0 {
			baseline = found
			t.Logf("%-19s %-24s %d findings (baseline)", p.Name, screen.String(), len(found))
			for _, f := range found {
				t.Logf("      %s", f)
			}
			continue
		}
		added := Compare(baseline, found)
		t.Logf("%-19s %-24s %d findings, %d new", p.Name, screen.String(), len(found), len(added))
		for _, f := range added {
			t.Logf("  NEW %s", f)
		}
	}
}

// dumpTree reads the hierarchy over adb and leaves nothing behind.
func dumpTree(ctx context.Context, adb *device.ADB) (*uitree.Tree, error) {
	const remote = "/data/local/tmp/formflux-dump.xml"
	if _, err := adb.Shell(ctx, "uiautomator", "dump", remote); err != nil {
		return nil, fmt.Errorf("uiautomator dump: %w", err)
	}
	data, err := adb.ExecOut(ctx, "cat", remote)
	if err != nil {
		return nil, fmt.Errorf("read the dump: %w", err)
	}
	// Deleted whatever happened above: the file is a copy of the screen.
	defer func() { _, _ = adb.Shell(context.Background(), "rm", "-f", remote) }()
	return uitree.ParseAndroid(data)
}

// TestDeviceCatchesThePlantedTargets is the positive control the Settings
// run above cannot be: it asserts. MobiumApp's Layout Demo plants two touch
// targets whose verdict is known at every profile — a 24dp square, too small
// everywhere, and a bar an eighth of the window wide, too narrow below 384dp
// — so formflux must report the first at every size and the second exactly
// where the screen is that narrow. A run that reported nothing used to read
// the same as one that could see nothing (CHALLENGES 146).
//
// The screen is opened again after each profile, because a density change
// restarts MobiumApp's activity and drops it back to its home list.
func TestDeviceCatchesThePlantedTargets(t *testing.T) {
	adb := deviceOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	const app = "dev.mobium.mobiumapp"
	if out, _ := adb.Shell(ctx, "pm", "path", app); !strings.Contains(string(out), "package:") {
		t.Skip("MobiumApp is not installed; its Layout Demo is this test's control (mobiumdev/mobium-app)")
	}

	before, err := Read(ctx, adb)
	if err != nil {
		t.Fatal(err)
	}
	if before.Overridden() {
		t.Fatalf("the device already has an override in force: %s", before)
	}
	defer func() {
		if _, err := Reset(context.Background(), adb); err != nil {
			t.Errorf("could not reset the screen: %v", err)
		}
		_, _ = adb.Shell(context.Background(), "am", "force-stop", app)
	}()

	for _, p := range For(Android) {
		screen, err := Apply(ctx, adb, p)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		tree, err := openLayoutDemo(ctx, adb, app)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		found := Inspect(tree, uitree.Rect{X2: screen.WidthPx, Y2: screen.HeightPx}, screen.DPI, Android)
		tiny, narrow := false, false
		for _, f := range found {
			if f.Kind == KindTinyTarget {
				tiny = tiny || f.Locator == "testid=tinyTarget"
				narrow = narrow || f.Locator == "testid=narrowTarget"
			}
		}
		wantNarrow := p.WidthDP() < 384
		t.Logf("%-19s sw%-4d tiny=%v narrow=%v (want narrow=%v)", p.Name, p.SmallestWidthDP(), tiny, narrow, wantNarrow)
		if !tiny {
			t.Errorf("%s: the 24dp target was not reported — the check cannot see a target that is too small", p.Name)
		}
		if narrow != wantNarrow {
			t.Errorf("%s at %ddp wide: narrow target reported=%v, want %v", p.Name, p.WidthDP(), narrow, wantNarrow)
		}
	}
}

// openLayoutDemo launches MobiumApp and opens its Layout Demo, scrolling the
// home list to find the entry, and returns the screen once the demo is on it.
func openLayoutDemo(ctx context.Context, adb *device.ADB, app string) (*uitree.Tree, error) {
	if _, err := adb.Shell(ctx, "am", "force-stop", app); err != nil {
		return nil, err
	}
	if out, err := adb.Shell(ctx, "am", "start", "-W", "-n", app+"/.MainActivity"); err != nil ||
		strings.Contains(string(out), "Error:") {
		return nil, fmt.Errorf("could not launch %s: %v %s", app, err, out)
	}
	for attempt := 0; attempt < 6; attempt++ {
		tree, err := dumpTree(ctx, adb)
		if err != nil {
			return nil, err
		}
		for _, n := range tree.All() {
			if n.TestID == "layoutWidth" {
				return tree, nil
			}
		}
		var entry *uitree.Node
		for _, n := range tree.All() {
			if n.Label == "Layout Demo" && n.Clickable && !n.Bounds.Empty() {
				entry = n
			}
		}
		if entry != nil {
			x, y := entry.Bounds.Center()
			_, _ = adb.Shell(ctx, "input", "tap", fmt.Sprint(x), fmt.Sprint(y))
		} else {
			w, h := tree.Root.Bounds.Width(), tree.Root.Bounds.Height()
			_, _ = adb.Shell(ctx, "input", "swipe", fmt.Sprint(w/2), fmt.Sprint(h*3/4), fmt.Sprint(w/2), fmt.Sprint(h/4), "300")
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return nil, fmt.Errorf("MobiumApp's Layout Demo never came up — is this build older than the screen?")
}

// TestDeviceEnsuresAnIOSSimulator drives the other mechanism: a screen that is
// a device rather than a setting.
//
//	MOBIUM_SIM_TESTS=1 go test ./internal/formflux/ -run DeviceEnsures -v
//
// Separate from MOBIUM_DEVICE_TESTS because it can create and delete
// simulators, which is a bigger thing to consent to than resizing one.
func TestDeviceEnsuresAnIOSSimulator(t *testing.T) {
	if os.Getenv("MOBIUM_SIM_TESTS") != "1" {
		t.Skip("set MOBIUM_SIM_TESTS=1 to run tests that boot, and may create, simulators")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	// Deliberately not iphone-17-pro: that one is usually already booted
	// here, and Ensure would return it untouched — passing without ever
	// exercising the boot path, which is the part that can be wrong.
	// MOBIUM_SIM_PROFILE overrides it.
	name := os.Getenv("MOBIUM_SIM_PROFILE")
	if name == "" {
		name = "iphone-16e"
	}
	p, err := Lookup(name)
	if err != nil {
		t.Fatal(err)
	}

	sim, err := Ensure(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := Release(context.Background(), sim); err != nil {
			t.Errorf("release: %v", err)
		}
	}()

	t.Logf("%s -> %s (%s, %s) created=%v wasBooted=%v",
		p.Name, sim.UDID, sim.Name, sim.Runtime, sim.Created, sim.WasBooted)

	if sim.UDID == "" {
		t.Fatal("no udid")
	}
	// Booted is the claim; check it against simctl rather than against the
	// absence of an error, because `simctl boot` returns before the boot is
	// finished and this is exactly where that bites.
	out, err := simctl(ctx, "list", "devices", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), sim.UDID) {
		t.Fatalf("simctl does not list %s at all", sim.UDID)
	}
	devs, err := devicesOfType(ctx, p.DeviceType)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range devs {
		if d.UDID == sim.UDID {
			found = true
			if !d.booted() {
				t.Errorf("Ensure returned before the simulator was booted: state is %q", d.State)
			}
		}
	}
	if !found {
		t.Errorf("the simulator is not of the device type it was asked for")
	}
}

// TestDeviceRefusesToApplyAProfileToTheWrongPlatform is the seam between the
// two mechanisms, asserted against the real tools rather than a fake.
func TestDeviceRefusesToApplyAProfileToTheWrongPlatform(t *testing.T) {
	adb := deviceOrSkip(t)
	ios, _ := Lookup("iphone-17-pro")
	if _, err := Apply(context.Background(), adb, ios); err == nil {
		t.Error("an iOS profile was applied to an Android device")
	}
	android, _ := Lookup("small-phone")
	if _, err := Ensure(context.Background(), android); err == nil {
		t.Error("an Android profile was handed to the simulator path")
	}
}
