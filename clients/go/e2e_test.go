package mobium

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// TestEndToEnd drives a booted Android device through the Go client. It is
// skipped unless MOBIUM_E2E_DEVICE names one; docs/checks/clients.sh sets it
// and runs the four other clients' copies of the same flow:
//
//	MOBIUM_E2E_DEVICE=emulator-5554 go test -run EndToEnd ./...
//
// Reading, acting, waiting, scrolling, lifecycle, permissions, the device log
// and crash reports, and one failure that must match its sentinel.
func TestEndToEnd(t *testing.T) {
	serial := os.Getenv("MOBIUM_E2E_DEVICE")
	if serial == "" {
		t.Skip("MOBIUM_E2E_DEVICE is not set")
	}
	const settings = "com.android.settings"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	d, err := Connect(WithDevice(serial), WithBinary(os.Getenv("MOBIUM_BIN_PATH")))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	must := func(err error, what string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	must(d.Terminate(ctx, settings), "terminate")
	must(d.Launch(ctx, settings), "launch")
	if app, _ := d.Current(ctx); app != settings {
		t.Fatalf("launch left %q in front", app)
	}

	found, err := d.WaitFor(ctx, "text=Network & internet", nil)
	must(err, "wait for")
	if found == nil || !strings.HasPrefix(found.Ref, "@e") {
		t.Fatalf("WaitFor returned %+v, want a ref", found)
	}
	must(d.Tap(ctx, found.Ref), "tap the ref")
	// A tap returns when delivered, not when the next screen is up.
	if _, err := d.WaitFor(ctx, "text=Internet", nil); err != nil {
		t.Fatalf("tapping the ref did not open its screen: %v", err)
	}

	must(d.Press(ctx, "back"), "back")
	row, err := d.ScrollTo(ctx, "text=About", "down")
	must(err, "scroll to")
	if row == nil {
		t.Fatal("ScrollTo returned nothing")
	}

	must(d.Grant(ctx, "com.android.chrome", "camera"), "grant")
	must(d.Revoke(ctx, "com.android.chrome", "camera"), "revoke")

	entries, _, err := d.DeviceLogs(ctx, "", "", 5)
	must(err, "device logs")
	if len(entries) == 0 {
		t.Fatal("DeviceLogs read nothing from logcat")
	}
	_, err = d.Crashes(ctx, "", 3)
	must(err, "crashes")

	if err := d.Tap(ctx, "text=Definitely Not Here"); !errors.Is(err, ErrNoSuchElement) {
		t.Fatalf("a missing element gave %v, want ErrNoSuchElement", err)
	}

	must(d.Terminate(ctx, settings), "terminate")
	if app, _ := d.Current(ctx); app == settings {
		t.Fatal("terminate left Settings in front")
	}
}
