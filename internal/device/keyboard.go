package device

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// KeyboardShown reports whether the soft keyboard is up.
//
// `dumpsys input_method` states it as mInputShown, measured on a Pixel 7 AVD
// (API 35): false with nothing focused, true the moment a text field took
// focus, false again after the keyboard was hidden. The keyboard's own keys
// never appear in the hierarchy on Android, so nothing else would say so.
func (a *ADB) KeyboardShown(ctx context.Context) (bool, error) {
	out, err := a.Shell(ctx, "dumpsys", "input_method")
	if err != nil {
		return false, err
	}
	return parseInputShown(string(out))
}

func parseInputShown(out string) (bool, error) {
	for _, field := range strings.Fields(out) {
		if v, ok := strings.CutPrefix(field, "mInputShown="); ok {
			return v == "true", nil
		}
	}
	return false, mobiumerr.New(mobiumerr.DeviceServer, "dumpsys input_method did not say whether the keyboard is shown")
}

// imeRegionRe reads the input method window's touchable region, a list of
// rectangles in screen pixels: "touchable region=SkRegion((0,1500,1080,2400))".
var imeRegionRe = regexp.MustCompile(`touchable region=SkRegion\(((?:\(-?\d+,-?\d+,-?\d+,-?\d+\))+)\)`)
var imeRectRe = regexp.MustCompile(`\((-?\d+),(-?\d+),(-?\d+),(-?\d+)\)`)

// KeyboardRegions reports where the input method is on screen and takes
// touches, as [x1, y1, x2, y2] rectangles in pixels, or none when it is not
// visible.
//
// Not the keyboard's frame, which is the whole screen below the status bar,
// and not "shown", which is true of the toolbar Gboard puts up in place of a
// keyboard when a hardware one is attached — a pill at the left edge on a
// headed emulator, covering the left of whatever is under it and nothing at
// the bottom. The touchable region is what a tap would land on instead of
// the app: the full keyboard, or just the pill. Measured on a Pixel 7 AVD.
func (a *ADB) KeyboardRegions(ctx context.Context) ([][4]int, error) {
	return a.windowRegions(ctx, "InputMethod")
}

// ClipboardPreviewRegions reports where the clipboard's preview takes
// touches, or none when it is not up. Android 13 and later put it up at the
// bottom left for a few seconds after anything writes the clipboard —
// app_clipboard included — as another window, so it is not in the hierarchy
// and a tap aimed at the app lands on it instead: on its share chip, it
// opened Quick Share. It closed by itself in about seven seconds, and back
// does not close it — the key goes to the app. Measured on a Pixel 7 AVD
// (API 35). CHALLENGES 113.
func (a *ADB) ClipboardPreviewRegions(ctx context.Context) ([][4]int, error) {
	return a.windowRegions(ctx, "ClipboardOverlay")
}

// windowRegions reads a named window's touchable region: `dumpsys window`
// with a name reports only the windows whose name contains it, in about
// 20ms, and 75 bytes when there is none.
func (a *ADB) windowRegions(ctx context.Context, window string) ([][4]int, error) {
	out, err := a.Shell(ctx, "dumpsys", "window", window)
	if err != nil {
		return nil, err
	}
	return parseIMERegions(string(out)), nil
}

func parseIMERegions(out string) [][4]int {
	if !strings.Contains(out, "isVisible=true") {
		return nil
	}
	m := imeRegionRe.FindStringSubmatch(out)
	if m == nil {
		return nil
	}
	var rects [][4]int
	for _, r := range imeRectRe.FindAllStringSubmatch(m[1], -1) {
		var v [4]int
		for i := range v {
			v[i], _ = strconv.Atoi(r[i+1])
		}
		if v[2] > v[0] && v[3] > v[1] {
			rects = append(rects, v)
		}
	}
	return rects
}
