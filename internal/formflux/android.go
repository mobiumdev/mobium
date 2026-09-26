package formflux

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"regexp"
	"strconv"
)

// Screen is what a device currently reports, physical and overridden.
//
// Both are kept because the difference is the whole point: an override is a
// state left on somebody's device, and `Overridden` is how a check knows
// whether it has cleaned up after itself.
type Screen struct {
	// All the Px fields are physical device pixels; DPI is dots per inch.
	// The names carry their units because formflux has already shipped one
	// bug from not knowing which unit a number was in.
	PhysicalWidthPx  int
	PhysicalHeightPx int
	PhysicalDPI      int

	// WidthPx, HeightPx and DPI are what is in force — the override where
	// there is one, the physical value otherwise.
	WidthPx  int
	HeightPx int
	DPI      int

	SizeOverridden    bool
	DensityOverridden bool
}

// WidthDP is the width in density-independent pixels, which is the unit
// Android lays out in and selects resources by. Zero if the density is
// unreadable, rather than a guess.
func (s Screen) WidthDP() int { return dp(s.WidthPx, s.DPI) }

// HeightDP is the height in density-independent pixels.
func (s Screen) HeightDP() int { return dp(s.HeightPx, s.DPI) }

// SmallestWidthDP is Android's `sw` qualifier: the shorter edge in dp. It is
// the number that decides whether a different layout is chosen at all, so two
// screens with the same sw value will usually lay out the same however
// different their pixel counts look.
func (s Screen) SmallestWidthDP() int {
	w, h := s.WidthDP(), s.HeightDP()
	if w == 0 || h == 0 {
		return 0
	}
	if w < h {
		return w
	}
	return h
}

func dp(px, dpi int) int {
	if dpi <= 0 {
		return 0
	}
	return px * 160 / dpi
}

// Overridden reports whether anything has been left changed on the device.
func (s Screen) Overridden() bool { return s.SizeOverridden || s.DensityOverridden }

func (s Screen) String() string {
	// The dp figure is printed because it is the one that decides the layout.
	// Reading only the pixels is how 1440x3120 @560 and 1080x2400 @420 look
	// like different screens when they are both 411dp wide.
	out := fmt.Sprintf("%dx%dpx @%ddpi = %dx%ddp (sw%ddp)",
		s.WidthPx, s.HeightPx, s.DPI, s.WidthDP(), s.HeightDP(), s.SmallestWidthDP())
	if s.Overridden() {
		out += fmt.Sprintf(" — overriding %dx%dpx @%ddpi",
			s.PhysicalWidthPx, s.PhysicalHeightPx, s.PhysicalDPI)
	}
	return out
}

// Shell is the slice of ADB this package needs. Narrow on purpose: it makes
// the parsing testable without a device, which is the only part of this that
// a fixture can honestly establish.
type Shell interface {
	Shell(ctx context.Context, rest ...string) ([]byte, error)
}

var (
	sizeRE    = regexp.MustCompile(`(?m)^(Physical|Override) size:\s*(\d+)x(\d+)`)
	densityRE = regexp.MustCompile(`(?m)^(Physical|Override) density:\s*(\d+)`)
)

// Read asks the device what its screen is.
func Read(ctx context.Context, sh Shell) (Screen, error) {
	sizeOut, err := sh.Shell(ctx, "wm", "size")
	if err != nil {
		return Screen{}, fmt.Errorf("read screen size: %w", err)
	}
	densityOut, err := sh.Shell(ctx, "wm", "density")
	if err != nil {
		return Screen{}, fmt.Errorf("read screen density: %w", err)
	}
	return parseScreen(string(sizeOut), string(densityOut))
}

func parseScreen(sizeOut, densityOut string) (Screen, error) {
	var s Screen

	sizes := sizeRE.FindAllStringSubmatch(sizeOut, -1)
	if len(sizes) == 0 {
		return s, mobiumerr.New(mobiumerr.DeviceServer, "could not read a screen size from %q", trim(sizeOut))
	}
	for _, m := range sizes {
		w, _ := strconv.Atoi(m[2])
		h, _ := strconv.Atoi(m[3])
		if m[1] == "Physical" {
			s.PhysicalWidthPx, s.PhysicalHeightPx = w, h
		} else {
			s.WidthPx, s.HeightPx, s.SizeOverridden = w, h, true
		}
	}
	if !s.SizeOverridden {
		s.WidthPx, s.HeightPx = s.PhysicalWidthPx, s.PhysicalHeightPx
	}

	densities := densityRE.FindAllStringSubmatch(densityOut, -1)
	if len(densities) == 0 {
		return s, mobiumerr.New(mobiumerr.DeviceServer, "could not read a screen density from %q", trim(densityOut))
	}
	for _, m := range densities {
		d, _ := strconv.Atoi(m[2])
		if m[1] == "Physical" {
			s.PhysicalDPI = d
		} else {
			s.DPI, s.DensityOverridden = d, true
		}
	}
	if !s.DensityOverridden {
		s.DPI = s.PhysicalDPI
	}
	return s, nil
}

// Apply puts a profile in force and confirms it took.
//
// Confirmed rather than assumed, for the usual reason: `wm` exits 0 for a size
// it did not apply, and this project has been caught by every other device
// tool that does the same. The readback is the result.
func Apply(ctx context.Context, sh Shell, p Profile) (Screen, error) {
	if p.Platform != Android {
		return Screen{}, mobiumerr.New(mobiumerr.DeviceServer,
			"profile %q is an iOS device type, and a booted simulator's screen cannot be changed — boot a simulator of that type instead", p.Name)
	}
	if _, err := sh.Shell(ctx, "wm", "size", fmt.Sprintf("%dx%d", p.WidthPx, p.HeightPx)); err != nil {
		return Screen{}, fmt.Errorf("set screen size for %s: %w", p.Name, err)
	}
	if _, err := sh.Shell(ctx, "wm", "density", strconv.Itoa(p.DPI)); err != nil {
		return Screen{}, fmt.Errorf("set screen density for %s: %w", p.Name, err)
	}

	got, err := Read(ctx, sh)
	if err != nil {
		return Screen{}, err
	}
	if got.WidthPx != p.WidthPx || got.HeightPx != p.HeightPx || got.DPI != p.DPI {
		return got, mobiumerr.New(mobiumerr.DeviceServer,
			"asked for %s but the device reports %s — the profile was not applied", p, got)
	}
	return got, nil
}

// Reset puts the device back to its physical screen.
//
// Always call it, and call it even when Apply failed: a half-applied profile
// leaves somebody's phone at a size they did not choose, and on an emulator
// that survives until the next wipe. Leaving nothing behind is a rule here,
// not a courtesy.
func Reset(ctx context.Context, sh Shell) (Screen, error) {
	if _, err := sh.Shell(ctx, "wm", "size", "reset"); err != nil {
		return Screen{}, fmt.Errorf("reset screen size: %w", err)
	}
	if _, err := sh.Shell(ctx, "wm", "density", "reset"); err != nil {
		return Screen{}, fmt.Errorf("reset screen density: %w", err)
	}
	got, err := Read(ctx, sh)
	if err != nil {
		return Screen{}, err
	}
	if got.Overridden() {
		return got, mobiumerr.New(mobiumerr.DeviceServer, "the device still reports an override after reset: %s", got)
	}
	return got, nil
}

func trim(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
