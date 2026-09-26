package formflux

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The fixtures below are real output, captured from emulator-5554 (a Pixel 7
// AVD on Android 17) on 2026-09-14 — before an override, and after
// `wm size 720x1520` / `wm density 320`. Written down rather than invented,
// because the two-line form is the whole reason this is parseable at all and
// a guessed fixture would have had one line.
const (
	sizePlain      = "Physical size: 1080x2400\n"
	sizeOverridden = "Physical size: 1080x2400\nOverride size: 720x1520\n"
	dpiPlain       = "Physical density: 420\n"
	dpiOverridden  = "Physical density: 420\nOverride density: 320\n"
)

func TestReadsAPlainScreen(t *testing.T) {
	s, err := parseScreen(sizePlain, dpiPlain)
	if err != nil {
		t.Fatal(err)
	}
	if s.WidthPx != 1080 || s.HeightPx != 2400 || s.DPI != 420 {
		t.Errorf("got %s, want 1080x2400 @420dpi", s)
	}
	if s.Overridden() {
		t.Error("a device with no override reported one")
	}
}

func TestAnOverrideWinsAndThePhysicalValueSurvives(t *testing.T) {
	s, err := parseScreen(sizeOverridden, dpiOverridden)
	if err != nil {
		t.Fatal(err)
	}
	// What is in force is the override.
	if s.WidthPx != 720 || s.HeightPx != 1520 || s.DPI != 320 {
		t.Errorf("in force: got %dx%d @%d, want 720x1520 @320", s.WidthPx, s.HeightPx, s.DPI)
	}
	// And the physical value is still readable, which is how a check knows
	// what to put back.
	if s.PhysicalWidthPx != 1080 || s.PhysicalHeightPx != 2400 || s.PhysicalDPI != 420 {
		t.Errorf("physical: got %dx%d @%d, want 1080x2400 @420",
			s.PhysicalWidthPx, s.PhysicalHeightPx, s.PhysicalDPI)
	}
	if !s.SizeOverridden || !s.DensityOverridden {
		t.Error("an overridden device did not say so")
	}
}

func TestOneAxisCanBeOverriddenWithoutTheOther(t *testing.T) {
	// The display-size accessibility case: same pixels, larger everything.
	s, err := parseScreen(sizePlain, dpiOverridden)
	if err != nil {
		t.Fatal(err)
	}
	if s.WidthPx != 1080 || s.DPI != 320 {
		t.Errorf("got %s, want the physical size with an overridden density", s)
	}
	if s.SizeOverridden {
		t.Error("size reported as overridden when only density was")
	}
	if !s.Overridden() {
		t.Error("a density-only override is still an override to clean up")
	}
}

func TestUnreadableOutputIsAnErrorRatherThanZeroes(t *testing.T) {
	// A zero-valued Screen would be taken for a real answer by anything that
	// did not check the error, and 0x0 is not obviously wrong at a glance.
	for _, bad := range []string{"", "error: device offline", "Physical size: wide"} {
		if _, err := parseScreen(bad, dpiPlain); err == nil {
			t.Errorf("parsed %q as a size", bad)
		}
		if _, err := parseScreen(sizePlain, bad); err == nil {
			t.Errorf("parsed %q as a density", bad)
		}
	}
}

// fakeShell records what it was asked and answers from a script.
type fakeShell struct {
	calls []string
	size  string
	dpi   string
	err   error
}

func (f *fakeShell) Shell(_ context.Context, rest ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(rest, " "))
	if f.err != nil {
		return nil, f.err
	}
	switch {
	case len(rest) >= 2 && rest[1] == "size":
		return []byte(f.size), nil
	case len(rest) >= 2 && rest[1] == "density":
		return []byte(f.dpi), nil
	}
	return nil, nil
}

func TestApplyRefusesAnIOSProfile(t *testing.T) {
	p, err := Lookup("iphone-17-pro")
	if err != nil {
		t.Fatal(err)
	}
	sh := &fakeShell{size: sizePlain, dpi: dpiPlain}
	if _, err := Apply(context.Background(), sh, p); err == nil {
		t.Fatal("applied an iOS profile to an Android device")
	} else if !strings.Contains(err.Error(), "boot a simulator") {
		t.Errorf("the refusal does not say what to do instead: %v", err)
	}
	if len(sh.calls) != 0 {
		t.Errorf("it touched the device anyway: %v", sh.calls)
	}
}

func TestApplyFailsWhenTheDeviceDisagrees(t *testing.T) {
	// wm exits 0 for a size it did not apply, so the readback is the result.
	// This is the check that would have caught it.
	p, _ := Lookup("small-phone")
	sh := &fakeShell{size: sizePlain, dpi: dpiPlain} // reports no override
	_, err := Apply(context.Background(), sh, p)
	if err == nil {
		t.Fatal("Apply believed a device that had not changed")
	}
	if !strings.Contains(err.Error(), "not applied") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestApplySucceedsWhenTheDeviceAgrees(t *testing.T) {
	p, _ := Lookup("small-phone")
	sh := &fakeShell{size: sizeOverridden, dpi: dpiOverridden}
	got, err := Apply(context.Background(), sh, p)
	if err != nil {
		t.Fatal(err)
	}
	if got.WidthPx != p.WidthPx || got.DPI != p.DPI {
		t.Errorf("got %s, want %s", got, p)
	}
	// The order matters: size before density, then a read of each.
	want := []string{"wm size 720x1520", "wm density 320", "wm size", "wm density"}
	if strings.Join(sh.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls were %v, want %v", sh.calls, want)
	}
}

func TestResetFailsWhenAnOverrideSurvives(t *testing.T) {
	// Reset reporting success while the device is still overridden would
	// leave somebody's phone at a size they did not choose.
	sh := &fakeShell{size: sizeOverridden, dpi: dpiOverridden}
	if _, err := Reset(context.Background(), sh); err == nil {
		t.Fatal("Reset reported success with an override still in force")
	}
}

func TestResetSucceedsWhenTheDeviceIsClean(t *testing.T) {
	sh := &fakeShell{size: sizePlain, dpi: dpiPlain}
	got, err := Reset(context.Background(), sh)
	if err != nil {
		t.Fatal(err)
	}
	if got.Overridden() {
		t.Error("a clean device reported an override")
	}
}

func TestAShellFailureIsNotSilent(t *testing.T) {
	sh := &fakeShell{err: errors.New("device offline")}
	if _, err := Read(context.Background(), sh); err == nil {
		t.Fatal("a failing shell read as a screen")
	}
}

func TestLookupNamesTheAlternativesWhenItFails(t *testing.T) {
	_, err := Lookup("nokia-3310")
	if err == nil {
		t.Fatal("found a profile that does not exist")
	}
	// An error that does not say what is valid makes the caller go and read
	// the source, which is the thing the catalog exists to avoid.
	if !strings.Contains(err.Error(), "small-phone") {
		t.Errorf("the error lists no alternatives: %v", err)
	}
}

func TestEveryProfileExplainsItself(t *testing.T) {
	for _, p := range catalog {
		if strings.TrimSpace(p.Why) == "" {
			t.Errorf("profile %q has no reason to exist", p.Name)
		}
		if p.Platform == Android && (p.WidthPx == 0 || p.HeightPx == 0 || p.DPI == 0) {
			t.Errorf("android profile %q is missing a dimension", p.Name)
		}
		if p.Platform == IOS && p.DeviceType == "" {
			t.Errorf("ios profile %q has no simctl device type, so nothing can boot it", p.Name)
		}
		if p.Platform == IOS && p.Settable() {
			t.Errorf("ios profile %q claims it can be set on a running device", p.Name)
		}
	}
}

func TestTheBaselineComesFirst(t *testing.T) {
	// A run that fails on the first profile has failed before it varied
	// anything, which is a different problem from a layout that only breaks
	// when narrow. That only reads clearly if the baseline is first.
	android := For(Android)
	if len(android) == 0 || android[0].Name != "pixel-7" {
		t.Errorf("the first android profile is %v, want pixel-7", android)
	}
}

// -- units ---------------------------------------------------------------
//
// These exist because formflux has already shipped one bug from not knowing
// which unit a number was in: a touch-target check that compared Apple's 44pt
// guideline against pixel bounds and was wrong by the scale factor. The three
// units are px, dpi and dp/pt, and only the last one decides a layout.

func TestPixelsAreNotTheLayoutWidth(t *testing.T) {
	// The claim worth asserting: flagship has 78% more pixels than pixel-7 and
	// is the *same* screen as far as any layout is concerned. If somebody
	// "fixes" the catalog by rounding these numbers, this test is the alarm.
	baseline, _ := Lookup("pixel-7")
	flagship, _ := Lookup("flagship")

	if flagship.WidthPx <= baseline.WidthPx {
		t.Fatalf("flagship should have more pixels: %d vs %d", flagship.WidthPx, baseline.WidthPx)
	}
	if got, want := flagship.WidthDP(), baseline.WidthDP(); got != want {
		t.Errorf("flagship is %ddp wide and pixel-7 is %ddp — they are supposed to be the same "+
			"layout width, which is the whole point of the profile", got, want)
	}
}

func TestTheAccessibilityProfileIsTheNarrowestOne(t *testing.T) {
	// display-size-large has the most pixels of any phone profile here and the
	// narrowest layout, and that inversion is why it finds bugs nothing else
	// does. Stated as a test so it cannot quietly stop being true.
	large, _ := Lookup("display-size-large")
	small, _ := Lookup("small-phone")

	if large.WidthPx <= small.WidthPx {
		t.Fatalf("display-size-large should have more pixels than small-phone")
	}
	if large.SmallestWidthDP() >= small.SmallestWidthDP() {
		t.Errorf("display-size-large is sw%ddp and small-phone is sw%ddp — the accessibility "+
			"profile is supposed to be the narrower of the two",
			large.SmallestWidthDP(), small.SmallestWidthDP())
	}
}

func TestEveryProfileKnowsItsOwnLayoutUnit(t *testing.T) {
	for _, p := range catalog {
		if p.WidthDP() == 0 || p.HeightDP() == 0 {
			t.Errorf("%s cannot convert its pixels to a layout unit: dpi=%d scale=%d",
				p.Name, p.DPI, p.Scale)
		}
		switch p.Platform {
		case Android:
			if p.Scale != 0 {
				t.Errorf("%s is Android and should have no iOS scale", p.Name)
			}
			if want := p.WidthPx * 160 / p.DPI; p.WidthDP() != want {
				t.Errorf("%s: %ddp, want %ddp", p.Name, p.WidthDP(), want)
			}
		case IOS:
			if p.DPI != 0 {
				t.Errorf("%s is iOS and should have no dpi — it has a scale factor instead", p.Name)
			}
			if p.Scale != 2 && p.Scale != 3 {
				t.Errorf("%s has an implausible scale of %d", p.Name, p.Scale)
			}
			if want := p.WidthPx / p.Scale; p.WidthDP() != want {
				t.Errorf("%s: %dpt, want %dpt", p.Name, p.WidthDP(), want)
			}
		}
	}
}

func TestAScreenReadBackConvertsTheSameWay(t *testing.T) {
	// The live reading and the catalog must agree, or a profile would report
	// one layout width when asked for and another once applied.
	p, _ := Lookup("small-phone")
	s, err := parseScreen(sizeOverridden, dpiOverridden)
	if err != nil {
		t.Fatal(err)
	}
	if s.WidthDP() != p.WidthDP() {
		t.Errorf("the device reads %ddp and the profile says %ddp", s.WidthDP(), p.WidthDP())
	}
	if s.SmallestWidthDP() != 360 {
		t.Errorf("small-phone should be sw360dp, exactly on Android's own qualifier, got sw%d",
			s.SmallestWidthDP())
	}
}

func TestNoDensityMeansNoConversionRatherThanAGuess(t *testing.T) {
	if got := dp(1080, 0); got != 0 {
		t.Errorf("with no density there is no dp value, got %d", got)
	}
	var s Screen
	if s.SmallestWidthDP() != 0 {
		t.Error("an empty screen produced a layout width")
	}
}
