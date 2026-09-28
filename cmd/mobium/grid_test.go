package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/grid"
)

// A device belongs to one run at a time, and the lease on the node is what
// says so: exclusive, renewable by its holder, free again once its holder
// stops renewing, and released only by its holder.
func TestLeases(t *testing.T) {
	t.Setenv("MOBIUM_HOME", t.TempDir())

	if got, err := grid.Take("emulator-5554", "a"); err != nil || !got.OK {
		t.Fatalf("a free device was not leased: %+v, %v", got, err)
	}
	if got, _ := grid.Take("emulator-5554", "b"); got.OK || got.Holder != "a" {
		t.Errorf("a second holder was given a leased device: %+v", got)
	}
	if got, _ := grid.Take("emulator-5554", "a"); !got.OK {
		t.Error("the holder could not renew its own lease")
	}
	if live, _ := grid.Live(); live["emulator-5554"] != "a" {
		t.Errorf("live leases %v", live)
	}

	// Lapsed: nobody renewed it for longer than the TTL.
	f := filepath.Join(os.Getenv("MOBIUM_HOME"), "leases", grid.Key("emulator-5554"))
	old := time.Now().Add(-2 * grid.TTL)
	os.Chtimes(f, old, old)
	if live, _ := grid.Live(); len(live) != 0 {
		t.Errorf("a lapsed lease is still live: %v", live)
	}
	if got, _ := grid.Take("emulator-5554", "b"); !got.OK || got.Holder != "b" {
		t.Errorf("a lapsed lease was not taken over: %+v", got)
	}

	// Only its holder releases it.
	grid.Drop("emulator-5554", "a")
	if live, _ := grid.Live(); live["emulator-5554"] != "b" {
		t.Error("someone else's release freed the device")
	}
	grid.Drop("emulator-5554", "b")
	if live, _ := grid.Live(); len(live) != 0 {
		t.Errorf("the holder's release left %v", live)
	}

	// A serial never reaches the filesystem as a path.
	if k := grid.Key("../../etc/x"); strings.ContainsAny(k, "/\\") {
		t.Errorf("a hostile serial became %q", k)
	}
}

// MOBIUM_GRID_OS matches the start of a word, so a version asks for that
// version and its point releases, not every OS containing its digits.
func TestGridMatchesOSAndModel(t *testing.T) {
	d := func(model, os string) gridDevice {
		g := gridDevice{OS: os}
		g.Model = model
		return g
	}
	for _, c := range []struct {
		model, os, wantModel, wantOS string
		match                        bool
	}{
		{"sdk_gphone64_arm64", "Android 17", "", "17", true},
		{"sdk_gphone64_arm64", "Android 17.1", "", "17", true},
		{"sdk_gphone64_arm64", "Android 15", "", "17", false},
		{"sdk_gphone64_arm64", "Android 15", "", "android", true},
		{"iPhone 17 Pro", "iOS 26.5", "", "iOS 26", true},
		{"iPhone 17 Pro", "iOS 26.5", "pixel", "", false},
		{"Pixel 8 Pro", "Android 17", "PIXEL", "17", true},
	} {
		t.Setenv("MOBIUM_GRID_MODEL", c.wantModel)
		t.Setenv("MOBIUM_GRID_OS", c.wantOS)
		if got := matches(d(c.model, c.os)); got != c.match {
			t.Errorf("model %q os %q against model=%q os=%q: %v, want %v", c.model, c.os, c.wantModel, c.wantOS, got, c.match)
		}
	}
}
