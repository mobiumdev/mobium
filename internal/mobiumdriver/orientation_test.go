package mobiumdriver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
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
	// The request Android keeps, which a refused turn leaves behind.
	askLocked bool
	asked     int
	restored  []string
}

func (f *fakeRotator) UserRotation(context.Context) (bool, int, error) {
	return f.askLocked, f.asked, nil
}
func (f *fakeRotator) RestoreUserRotation(_ context.Context, locked bool, q int) error {
	f.askLocked, f.asked = locked, q
	f.restored = append(f.restored, fmt.Sprintf("%v %d", locked, q))
	return nil
}

func (f *fakeRotator) Rotation(context.Context) (int, error)        { return f.quarter, nil }
func (f *fakeRotator) RotationLocked(context.Context) (bool, error) { return f.locked, nil }
func (f *fakeRotator) SetRotation(_ context.Context, q int) error {
	// The lock is set whether or not the app turns, as on a device.
	f.askLocked, f.asked = true, q
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

// A turn the app refuses leaves the device asked for it — locked, auto-rotate
// off — and the next app opened turned: Settings, after MobiumApp refused
// landscape on the Android 15 emulator. The request is put back as it was,
// and the refusal says where the app stayed. CHALLENGES 308.
func TestARefusedTurnIsTakenBack(t *testing.T) {
	f := &fakeRotator{quarter: 0, askLocked: false, asked: 1,
		setErr: mobiumerr.New(mobiumerr.NotConfirmed, "asked for rotation 1 and the display is still showing 0")}
	err := androidSetOrientation(context.Background(), f, OrientationLandscape)
	if mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed {
		t.Fatalf("err = %v", err)
	}
	if f.askLocked || f.asked != 1 || len(f.restored) != 1 {
		t.Errorf("the request was left %v %d (restores %v), want following the sensor at 1", f.askLocked, f.asked, f.restored)
	}
	e, _ := mobiumerr.As(err)
	if !strings.Contains(err.Error(), "still portrait") || !strings.Contains(err.Error(), "taken back") ||
		e.Details["orientation"] != "portrait" {
		t.Errorf("the refusal does not say where the app stayed and that it was taken back: %v %v", err, e.Details)
	}
	// A turn that happened is not taken back.
	f = &fakeRotator{}
	if err := androidSetOrientation(context.Background(), f, OrientationLandscape); err != nil || len(f.restored) != 0 {
		t.Errorf("a turn that happened: err %v, restores %v", err, f.restored)
	}
}

// What a session changes is put back when it ends, lock and all.
func TestTheSessionUndoPutsTheRotationBack(t *testing.T) {
	f := &fakeRotator{askLocked: false, asked: 0}
	undo, err := androidSaveOrientation(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	_ = androidSetOrientation(context.Background(), f, OrientationLandscape)
	if !f.askLocked || f.asked != 1 {
		t.Fatalf("the turn did not lock to landscape: %v %d", f.askLocked, f.asked)
	}
	if err := undo(context.Background()); err != nil || f.askLocked || f.asked != 0 {
		t.Errorf("after the undo: %v %d, err %v; want following the sensor at 0", f.askLocked, f.asked, err)
	}
}
