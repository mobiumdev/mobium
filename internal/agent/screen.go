package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"

	"github.com/mobiumdev/mobium/internal/formflux"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// screen is app_screen: read the screen, make the device pretend to be a
// different one, and say what is wrong with the layout at that size.
//
// Worth having for the reason rotation was: it is a genuinely different
// rendering of every screen, and it is where layouts go untested. A flow that
// works on the screen you happen to own is a flow tested once.
//
// The two platforms do not share a mechanism and this tool does not pretend
// they do. On Android a screen is a setting and can be changed under a running
// app. On iOS it is fixed when the simulator is created, so a profile there
// names a simulator to boot rather than a size to apply — and asking for one
// on a running iOS session is refused with what to do instead, rather than
// quietly doing nothing.
func (h *Handlers) screen(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}

	profile := strings.TrimSpace(stringArg(args, "profile"))
	inspect := boolArg(args, "inspect")

	if s.backend == BackendWDA {
		if profile != "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument,
				"an iOS simulator's screen is fixed when it is created and cannot be changed while it runs — "+
					"boot a simulator of the device type you want instead. Known iOS profiles: %s",
				strings.Join(formflux.Names(formflux.IOS), ", "))
		}
		return h.screenReportIOS(ctx, s, inspect)
	}

	adb, err := h.adbFor(ctx, s)
	if err != nil {
		return nil, err
	}

	var applied string
	switch {
	case strings.EqualFold(profile, "reset"):
		if _, err := formflux.Reset(ctx, adb); err != nil {
			return nil, err
		}
		applied = "reset"
	case profile != "":
		p, err := formflux.Lookup(profile)
		if err != nil {
			// Lookup knows every profile; this session can only use the
			// Android ones, and an error that offers "iphone-16e" to an
			// Android device is a remedy that cannot work.
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown screen profile %q — on Android: %s, or \"reset\"",
				profile, strings.Join(formflux.Names(formflux.Android), ", "))
		}
		if p.Platform != formflux.Android {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument,
				"%q is an iOS device type and this session is driving an Android device — "+
					"Android profiles are %s", p.Name, strings.Join(formflux.Names(formflux.Android), ", "))
		}
		if _, err := formflux.Apply(ctx, adb, p); err != nil {
			return nil, err
		}
		applied = p.Name
		// The screen just changed size, so every ref from the last map points
		// at a rectangle that has moved. Same rule as rotation, and the same
		// line of code.
		delete(h.refs, s.dev.Serial)
	}

	now, err := formflux.Read(ctx, adb)
	if err != nil {
		return nil, err
	}

	view := ScreenView{
		WidthPx: now.WidthPx, HeightPx: now.HeightPx, DPI: now.DPI,
		WidthDP: now.WidthDP(), HeightDP: now.HeightDP(),
		SmallestWidthDP:  now.SmallestWidthDP(),
		PhysicalWidthPx:  now.PhysicalWidthPx,
		PhysicalHeightPx: now.PhysicalHeightPx,
		PhysicalDPI:      now.PhysicalDPI, Overridden: now.Overridden(),
		Applied: applied, Profiles: formflux.Names(formflux.Android),
		Device: s.dev.Serial,
	}

	msg := now.String()
	if applied != "" {
		msg = fmt.Sprintf("%s — %s", applied, now)
	}
	if now.Overridden() {
		msg += "\nThis is an override. Call app_screen with profile=\"reset\" to put the device back."
	}

	if inspect {
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		view.Findings = collectFindings(tree,
			uitree.Rect{X2: now.WidthPx, Y2: now.HeightPx}, now.DPI, formflux.Android)
		msg += "\n\n" + describeFindings(view.Findings)
	}
	return Result(msg, view), nil
}

// screenReportIOS answers the read-only half on a simulator or an iPhone,
// where the screen is whatever the device is.
func (h *Handlers) screenReportIOS(ctx context.Context, s *session, inspect bool) (*ToolsCallResult, error) {
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	// The root element's frame is the screen. These are **device pixels**,
	// not points: mobium normalizes iOS coordinates on the way out, so an
	// iPhone 17 Pro reports 1206x2622 rather than 402x874. Saying "points"
	// here was wrong and made a touch-target threshold wrong with it.
	rect := tree.Root.Bounds
	// No scale is reported here, so no point conversion is offered. Inventing
	// one would repeat the mistake that disabled the iOS touch-target check.
	view := ScreenView{
		WidthPx: rect.Width(), HeightPx: rect.Height(),
		PhysicalWidthPx: rect.Width(), PhysicalHeightPx: rect.Height(),
		Profiles: formflux.Names(formflux.IOS), Device: s.dev.Serial,
	}
	msg := fmt.Sprintf("%dx%d device pixels (an iOS device's screen is fixed; on a simulator, boot another device "+
		"type to change it)", rect.Width(), rect.Height())
	if inspect {
		// Touch targets are judged in points, which needs the device's scale;
		// without it they are not judged, and the report says so rather than
		// reading as a clean screen.
		var scale float64
		if p, ok := mobiumdriver.AsPointScaler(s.driver); ok {
			scale = p.PointScale()
		}
		view.Findings = collectIOSFindings(tree, rect, scale)
		msg += "\n\n" + describeFindings(view.Findings)
		if scale <= 0 {
			msg += "\nTouch targets were not checked: this driver gives no point scale."
		}
	}
	return Result(msg, view), nil
}

func collectFindings(tree *uitree.Tree, screen uitree.Rect, dpi int, platform formflux.Platform) []FindingView {
	return findingViews(formflux.Inspect(tree, screen, dpi, platform))
}

func collectIOSFindings(tree *uitree.Tree, screen uitree.Rect, scale float64) []FindingView {
	return findingViews(formflux.InspectIOS(tree, screen, scale))
}

func findingViews(fs []formflux.Finding) []FindingView {
	var out []FindingView
	for _, f := range fs {
		out = append(out, FindingView{
			Kind: string(f.Kind), Label: f.Label, Detail: f.Detail,
			Locator: f.Locator, Path: f.Path,
			Bounds: BoundsView{X1: f.Bounds.X1, Y1: f.Bounds.Y1, X2: f.Bounds.X2, Y2: f.Bounds.Y2},
		})
	}
	return out
}

func describeFindings(fs []FindingView) string {
	if len(fs) == 0 {
		// Saying nothing here would read as "not checked".
		return "No layout findings at this screen."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d layout finding(s) at this screen:", len(fs))
	for _, f := range fs {
		where := f.Locator
		if where == "" && f.Path != "" {
			where = "at " + f.Path
		}
		fmt.Fprintf(&b, "\n  %-11s %s — %s", f.Kind, f.Label, f.Detail)
		if where != "" {
			fmt.Fprintf(&b, "  [%s]", where)
		}
	}
	return b.String()
}

// ScreenView is the result of app_screen.
type ScreenView struct {
	// WidthPx and HeightPx are physical device pixels, on both platforms:
	// mobium normalizes iOS coordinates on the way out, so an iPhone reports
	// pixels here and not points. Every field says its unit because getting
	// that wrong already cost this package a bug.
	WidthPx  int `json:"width_px"`
	HeightPx int `json:"height_px"`

	// DPI is Android's density in dots per inch. Zero on iOS, which has a
	// scale factor instead and does not report one here.
	DPI int `json:"dpi,omitempty"`

	// WidthDP and HeightDP are the unit the platform lays out in — dp on
	// Android, points on iOS. **This is the pair that decides a layout.**
	// Android selects resources on dp, never on pixels, so a 1440px screen at
	// 560dpi and a 1080px screen at 420dpi are both 411dp and lay out
	// identically. Zero when the conversion is unknown rather than guessed.
	WidthDP  int `json:"width_dp,omitempty"`
	HeightDP int `json:"height_dp,omitempty"`

	// SmallestWidthDP is Android's `sw` resource qualifier: the shorter edge
	// in dp, and the single number that best predicts whether a different
	// layout is chosen.
	SmallestWidthDP int `json:"smallest_width_dp,omitempty"`

	PhysicalWidthPx  int `json:"physical_width_px"`
	PhysicalHeightPx int `json:"physical_height_px"`
	PhysicalDPI      int `json:"physical_dpi,omitempty"`

	// Overridden says the device is pretending, which is a state somebody has
	// to put back.
	Overridden bool `json:"overridden"`
	// Applied names what this call did, empty when it only read.
	Applied string `json:"applied,omitempty"`

	Profiles []string      `json:"profiles"`
	Findings []FindingView `json:"findings,omitempty"`
	Device   string        `json:"device"`
}

// FindingView is one thing wrong with a layout.
type FindingView struct {
	Kind    string     `json:"kind"`
	Label   string     `json:"label"`
	Detail  string     `json:"detail"`
	Locator string     `json:"locator,omitempty"`
	Path    string     `json:"path,omitempty"`
	Bounds  BoundsView `json:"bounds"`
}
