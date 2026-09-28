package agent

import (
	"context"
	"testing"

	"github.com/mobiumdev/mobium/internal/grid"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A device a grid leased to one run is refused to every other daemon on the
// node — the one a run that bypassed the grid would use included — and
// allowed to the run's own, whose session name is the lease's holder.
func TestALeasedDeviceBelongsToItsRun(t *testing.T) {
	t.Setenv("MOBIUM_HOME", t.TempDir())
	t.Setenv("MOBIUM_SESSION", "")

	h, _, _ := withFake(t, screen(t, "OK"))
	ctx := context.Background()
	args := map[string]interface{}{"device": "fake"}

	if _, err := h.sessionFor(ctx, args); err != nil {
		t.Fatalf("an unleased device was refused: %v", err)
	}
	if got, _ := grid.Take("fake", "g1234abcd"); !got.OK {
		t.Fatal("could not lease the device")
	}
	_, err := h.sessionFor(ctx, args)
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Fatalf("another daemon reached a leased device: %v", err)
	}

	t.Setenv("MOBIUM_SESSION", "g1234abcd")
	if _, err := h.sessionFor(ctx, args); err != nil {
		t.Errorf("the run holding the lease was refused its own device: %v", err)
	}

	t.Setenv("MOBIUM_SESSION", "")
	grid.Drop("fake", "g1234abcd")
	if _, err := h.sessionFor(ctx, args); err != nil {
		t.Errorf("a released device is still refused: %v", err)
	}
}
