// Package formflux makes one device impersonate many, so a flow can be run
// against a range of screens and resolutions instead of the one that happens
// to be plugged in.
//
// # Why this is not symmetric
//
// Android and iOS disagree about what a screen is, and the difference is not
// cosmetic.
//
// On Android a screen is a *setting*. `wm size` and `wm density` override the
// display for every app, the hierarchy and screenshots both follow, and both
// report the physical value alongside the override so the change can be read
// back. One emulator can be a cheap 720x1520 phone and then a 1440x3120
// flagship a second later, and `reset` puts it back.
//
// On iOS a screen is a *device*. A simulator's geometry is fixed when it is
// created; nothing resizes a booted one. Covering four screens means creating
// and booting four simulators, which costs tens of seconds each rather than
// seconds, and they cannot be varied along density independently — Apple ships
// the combinations it ships.
//
// So the same idea is one mechanism on one platform and another on the other,
// and this package says so rather than pretending to a uniformity that is not
// there. A Profile carries what it can control and what it cannot.
//
// # The accessibility case is the interesting one
//
// Changing density without changing size is what Android's "display size"
// accessibility setting does, and it is where layouts break that no amount of
// rotating finds: the same pixels, larger everything, so text wraps and
// controls collide. A profile that moves density alone covers it.
package formflux

import (
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"sort"
	"strings"
)

// Platform is which of the two mechanisms a profile needs.
type Platform string

const (
	Android Platform = "android"
	IOS     Platform = "ios"
)

// Profile is one screen to run a flow against.
//
// # Three units, and they are not interchangeable
//
// Confusing them is not hypothetical here: formflux shipped a touch-target
// check that compared Apple's 44pt guideline against pixel bounds and was
// wrong by a factor of three. Every field below therefore says its unit in
// its name.
//
//   - **px** — physical device pixels. What `map` bounds, screenshots and
//     taps are in, on *both* platforms, because mobium normalizes iOS
//     coordinates on the way out.
//   - **dpi** — Android's screen density, pixels per inch against a 160
//     baseline. Android only; iOS has a scale factor instead.
//   - **dp / pt** — the density-independent unit each platform lays out in.
//     Android's dp is px*160/dpi. iOS's point is px/scale.
//
// **The last one is what actually decides a layout.** Android picks resources
// and breakpoints on dp width, never on pixels, so a 1440px flagship and a
// 1080px Pixel 7 are the same 411dp screen and lay out identically. Anything
// reasoning about "a wider screen" has to reason in dp.
type Profile struct {
	Name     string
	Platform Platform

	// WidthPx and HeightPx are physical device pixels.
	WidthPx  int
	HeightPx int

	// DPI is Android's density in dots per inch. Zero on iOS.
	DPI int

	// Scale is iOS's point-to-pixel ratio (2 or 3). Zero on Android, which
	// expresses the same idea as DPI.
	Scale int

	// DeviceType is the simctl identifier, iOS only. Empty on Android.
	DeviceType string

	// Why says what this profile is for. A profile nobody can explain is a
	// profile nobody will keep passing.
	Why string
}

// WidthDP is the width in the unit the platform lays out in: Android's
// density-independent pixels, or iOS's points. Zero when the conversion is
// unknown, rather than a guess.
func (p Profile) WidthDP() int { return p.toLayoutUnits(p.WidthPx) }

// HeightDP is the height in dp on Android, points on iOS.
func (p Profile) HeightDP() int { return p.toLayoutUnits(p.HeightPx) }

// SmallestWidthDP is Android's sw qualifier — the shorter edge in dp, which is
// what `values-sw600dp` and friends switch on. It is the single number that
// best predicts whether a layout changes.
func (p Profile) SmallestWidthDP() int {
	w, h := p.WidthDP(), p.HeightDP()
	if w == 0 || h == 0 {
		return 0
	}
	if w < h {
		return w
	}
	return h
}

func (p Profile) toLayoutUnits(px int) int {
	switch {
	case p.Platform == Android && p.DPI > 0:
		// dp is defined against a 160dpi baseline.
		return px * 160 / p.DPI
	case p.Platform == IOS && p.Scale > 0:
		return px / p.Scale
	}
	return 0
}

// Settable reports whether this profile can be applied to a device already
// running, rather than needing one booted for it.
func (p Profile) Settable() bool { return p.Platform == Android }

func (p Profile) String() string {
	if p.Platform == IOS {
		return fmt.Sprintf("%s (%dx%dpx = %dx%dpt @%dx)",
			p.Name, p.WidthPx, p.HeightPx, p.WidthDP(), p.HeightDP(), p.Scale)
	}
	// The dp figure is printed alongside the pixels because it is the one that
	// decides the layout, and reading only the pixels is how two profiles that
	// lay out identically look different.
	return fmt.Sprintf("%s (%dx%dpx @%ddpi = %dx%ddp, sw%ddp)",
		p.Name, p.WidthPx, p.HeightPx, p.DPI, p.WidthDP(), p.HeightDP(), p.SmallestWidthDP())
}

// catalog is the built-in set. Every Android entry is a real device's real
// geometry rather than a round number, because a layout that breaks does so at
// a real width — 1008 and 360 below are a Pixel 8 Pro measured here, not the
// 1080/420 an emulator defaults to.
// catalog is the built-in set. Every Android geometry is a real device's, not
// a round number, because a layout breaks at a real width.
//
// The `sw` figure in each comment is the shorter edge in dp, and it is the
// number that decides whether Android picks a different layout at all. Reading
// only the pixel column is misleading: flagship has 78% more pixels than
// pixel-7 and is the *same* 411dp screen.
var catalog = []Profile{
	{
		Name: "pixel-7", Platform: Android, WidthPx: 1080, HeightPx: 2400, DPI: 420,
		Why: "sw411dp. The mobium-test AVD's own geometry, and the baseline everything else is compared against",
	},
	{
		Name: "pixel-8-pro", Platform: Android, WidthPx: 1008, HeightPx: 2244, DPI: 360,
		Why: "sw448dp. Measured on the real Pixel 8 Pro: fewer pixels than the emulator and a *wider* layout, because it is less dense. The clearest demonstration that pixels do not predict the layout",
	},
	{
		Name: "small-phone", Platform: Android, WidthPx: 720, HeightPx: 1520, DPI: 320,
		Why: "sw360dp, which sits exactly on Android's own sw360dp resource qualifier — a boundary worth testing on rather than near. The low end still sold in volume",
	},
	{
		Name: "flagship", Platform: Android, WidthPx: 1440, HeightPx: 3120, DPI: 560,
		Why: "sw411dp — the *same layout width* as pixel-7 with 78% more pixels. It tests rendering at high density, bitmap assets and touch targets in pixels, not a wider layout. Said explicitly because the obvious reading of 1440 is wrong",
	},
	{
		Name: "tablet", Platform: Android, WidthPx: 1600, HeightPx: 2560, DPI: 320,
		Why: "sw800dp, past both the sw600dp and sw720dp qualifiers, so it is the only profile here that can select a genuinely different layout. Phone layouts stretched across it look wrong in ways portrait phones never show",
	},
	{
		Name: "fold-open", Platform: Android, WidthPx: 2076, HeightPx: 2152, DPI: 390,
		Why: "sw851dp, and nearly square — the Pixel 9 Pro Fold's inner screen, as its emulator profile gives it. Past both tablet qualifiers like tablet, but with no long edge: a layout that assumes a tablet is landscape or a phone is tall is wrong here in both directions",
	},
	{
		Name: "fold-closed", Platform: Android, WidthPx: 1080, HeightPx: 2424, DPI: 390,
		Why: "sw443dp — the same device folded: its outer screen, a phone. The pair is one device crossing from Android's tablet layout class to its phone one and back, which a folding user does mid-task",
	},
	{
		Name: "display-size-large", Platform: Android, WidthPx: 1080, HeightPx: 2400, DPI: 560,
		Why: "sw308dp — the **narrowest** profile in this set, narrower than small-phone, on a screen with the most pixels of any phone here. That is Android's display-size accessibility setting, and that inversion is exactly why it breaks layouts nothing else finds",
	},
	{
		Name: "iphone-17-pro", Platform: IOS, WidthPx: 1206, HeightPx: 2622, Scale: 3,
		DeviceType: "com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro",
		Why:        "402x874pt. The verified iOS baseline",
	},
	{
		Name: "iphone-17-pro-max", Platform: IOS, WidthPx: 1320, HeightPx: 2868, Scale: 3,
		DeviceType: "com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro-Max",
		Why:        "440x956pt. The largest iPhone, and 38pt wider than the baseline",
	},
	{
		Name: "ipad-mini", Platform: IOS, WidthPx: 1488, HeightPx: 2266, Scale: 2,
		DeviceType: "com.apple.CoreSimulator.SimDeviceType.iPad-mini-A17-Pro",
		Why:        "744x1133pt. The smallest iPad, and the only iOS profile here in the regular width size class, where an iPhone is compact: the nearest iOS has to a foldable's jump from phone to tablet layout, as a second device rather than a transition. Measured on the iPad mini (A17 Pro) simulator, whose runner needed sending to the background before it would start (CHALLENGES 147)",
	},
	{
		Name: "ipad-pro-13", Platform: IOS, WidthPx: 2064, HeightPx: 2752, Scale: 2,
		DeviceType: "com.apple.CoreSimulator.SimDeviceType.iPad-Pro-13-inch-M5-12GB",
		Why:        "1032x1376pt. The largest iOS screen, regular width like ipad-mini but 288pt wider: the pair brackets the regular size class the way the iPhones bracket the compact one. Measured on the iPad Pro 13-inch (M5) simulator",
	},
	{
		Name: "iphone-16e", Platform: IOS, WidthPx: 1170, HeightPx: 2532, Scale: 3,
		DeviceType: "com.apple.CoreSimulator.SimDeviceType.iPhone-16e",
		Why:        "390x844pt. The narrowest current iPhone, and 12pt narrower than the baseline — iPhone widths vary far less than Android's, which is why one iOS profile finds much less than one Android one",
	},
}

// Lookup returns a profile by name.
func Lookup(name string) (Profile, error) {
	for _, p := range catalog {
		if strings.EqualFold(p.Name, name) {
			return p, nil
		}
	}
	return Profile{}, mobiumerr.New(mobiumerr.InvalidArgument, "unknown screen profile %q — known ones are %s",
		name, strings.Join(Names(""), ", "))
}

// Names lists the profiles for a platform, or all of them when platform is
// empty.
func Names(platform Platform) []string {
	var out []string
	for _, p := range catalog {
		if platform == "" || p.Platform == platform {
			out = append(out, p.Name)
		}
	}
	sort.Strings(out)
	return out
}

// For returns every profile for a platform, in catalog order — which is
// deliberate: the baseline comes first, so a run that fails on the first
// profile has failed before it varied anything, and that is a different
// problem from a layout that only breaks when narrow.
func For(platform Platform) []Profile {
	var out []Profile
	for _, p := range catalog {
		if p.Platform == platform {
			out = append(out, p)
		}
	}
	return out
}
