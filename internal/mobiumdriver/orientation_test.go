package mobiumdriver

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOrientationVocabularyRoundTrips(t *testing.T) {
	for want, name := range map[int]string{
		0: OrientationPortrait,
		1: OrientationLandscape,
		2: OrientationPortraitReverse,
		3: OrientationLandscapeReverse,
	} {
		got, err := OrientationQuarter(name)
		if err != nil || got != want {
			t.Errorf("OrientationQuarter(%q) = %d, %v; want %d", name, got, err, want)
		}
		back, err := OrientationName(want)
		if err != nil || back != name {
			t.Errorf("OrientationName(%d) = %q, %v; want %q", want, back, err, name)
		}
	}
}

func TestOrientationRejectsNonsense(t *testing.T) {
	// "left" and "right" are refused on purpose: which landscape is which
	// depends on whether you mean the device or the image, and the two
	// platforms' own names disagree. Accepting them would mean guessing.
	for _, bad := range []string{"landscape-left", "landscape-right", "sideways", "", "0", "auto"} {
		if _, err := OrientationQuarter(bad); err == nil {
			t.Errorf("accepted %q as an orientation", bad)
		}
	}
	// The message has to list what is valid, since the caller just guessed.
	_, err := OrientationQuarter("landscape-left")
	for _, want := range []string{"portrait", "landscape", "auto"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
	if _, err := OrientationName(4); err == nil {
		t.Error("accepted rotation 4")
	}
}

// fakeRotator stands in for adb so the degrees-to-quarters conversion and the
// lock reporting can be tested without a device.
type fakeRotator struct {
	quarter int
	locked  bool
	setErr  error
	sets    []int
	freed   int
}

func (f *fakeRotator) Rotation(context.Context) (int, error)        { return f.quarter, nil }
func (f *fakeRotator) RotationLocked(context.Context) (bool, error) { return f.locked, nil }
func (f *fakeRotator) SetRotation(_ context.Context, q int) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.sets = append(f.sets, q)
	f.quarter, f.locked = q, true
	return nil
}
func (f *fakeRotator) SetRotationAuto(context.Context) error {
	f.freed++
	f.locked = false
	return nil
}

func TestAndroidOrientationReportsBothFacts(t *testing.T) {
	// A screen that happens to be portrait and one pinned to portrait are
	// different situations: the first can turn under you mid-test.
	f := &fakeRotator{quarter: 1, locked: false}
	mode, locked, err := androidOrientation(context.Background(), f)
	if err != nil || mode != OrientationLandscape || locked {
		t.Errorf("got %q locked=%v err=%v", mode, locked, err)
	}
	f.locked = true
	if _, locked, _ := androidOrientation(context.Background(), f); !locked {
		t.Error("a pinned rotation was reported as following the sensor")
	}
}

func TestAndroidSetOrientation(t *testing.T) {
	f := &fakeRotator{}
	if err := androidSetOrientation(context.Background(), f, OrientationLandscapeReverse); err != nil {
		t.Fatal(err)
	}
	if len(f.sets) != 1 || f.sets[0] != 3 {
		t.Errorf("sent %v, want one lock to quarter 3", f.sets)
	}
	if err := androidSetOrientation(context.Background(), f, OrientationAuto); err != nil {
		t.Fatal(err)
	}
	if f.freed != 1 {
		t.Errorf("auto did not release the lock")
	}
	if err := androidSetOrientation(context.Background(), f, "sideways"); err == nil {
		t.Error("accepted a nonsense orientation")
	}
}

func TestSetOrientationPassesTheDriverError(t *testing.T) {
	// An activity that locks its own orientation cannot be turned from
	// outside. That has to surface rather than read as success.
	f := &fakeRotator{setErr: errors.New("the display is showing 0")}
	err := androidSetOrientation(context.Background(), f, OrientationLandscape)
	if err == nil || !strings.Contains(err.Error(), "showing 0") {
		t.Errorf("err = %v", err)
	}
}
