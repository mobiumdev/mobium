package webview

import (
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

func TestSocketRegexFindsBothKinds(t *testing.T) {
	// /proc/net/unix is dense; only the devtools sockets should be picked out,
	// and each only once however many times the kernel lists it.
	procNetUnix := `Num       RefCount Protocol Flags    Type St Inode Path
0000000000000000: 00000002 00000000 00010000 0001 01 45361 @webview_devtools_remote_1508
0000000000000000: 00000002 00000000 00010000 0001 01 45362 @webview_devtools_remote_1508
0000000000000000: 00000002 00000000 00010000 0001 01 45363 @chrome_devtools_remote
0000000000000000: 00000003 00000000 00000000 0001 03 12345 /dev/socket/logdw
0000000000000000: 00000002 00000000 00010000 0001 01 45364 @webview_devtools_remote_4257`

	var names []string
	seen := map[string]bool{}
	for _, m := range socketRe.FindAllStringSubmatch(procNetUnix, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		names = append(names, m[1])
	}
	want := []string{"webview_devtools_remote_1508", "chrome_devtools_remote", "webview_devtools_remote_4257"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("found %v, want %v", names, want)
	}
}

func TestFrameConversion(t *testing.T) {
	// A WebView occupying the lower part of a 1080-wide screen, showing a page
	// whose visual viewport is 412 CSS px across.
	host := uitree.Rect{X1: 0, Y1: 283, X2: 1080, Y2: 2337}
	frame, err := NewFrame(host, &Metrics{CSSWidth: 412, CSSHeight: 783})
	if err != nil {
		t.Fatalf("NewFrame: %v", err)
	}

	if frame.OriginX != 0 || frame.OriginY != 283 {
		t.Errorf("origin = (%d,%d)", frame.OriginX, frame.OriginY)
	}
	want := 1080.0 / 412.0
	if diff := frame.Scale - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("scale = %v, want %v", frame.Scale, want)
	}

	// A CSS rect converts by scale, then shifts by the host origin.
	got := frame.ToDevice(100, 50, 200, 20)
	wantRect := uitree.Rect{
		X1: int(100 * want),
		Y1: 283 + int(50*want),
		X2: int(100*want) + int(200*want),
		Y2: 283 + int(50*want) + int(20*want),
	}
	if got != wantRect {
		t.Errorf("ToDevice = %v, want %v", got, wantRect)
	}

	// The scale is what the visual-viewport bug got wrong: using the layout
	// viewport of the same page (1648 CSS px) instead of its visual viewport
	// puts every point at about a quarter of the right offset.
	wrong, _ := NewFrame(host, &Metrics{CSSWidth: 1648})
	if wrong.Scale >= frame.Scale {
		t.Fatal("precondition: the layout viewport should give a smaller scale")
	}
	rightY := frame.ToDevice(0, 44, 10, 10).Y1
	wrongY := wrong.ToDevice(0, 44, 10, 10).Y1
	if rightY-wrongY < 50 {
		t.Errorf("the two viewports differ by only %dpx; this test is not "+
			"exercising the bug it describes", rightY-wrongY)
	}
}

func TestFrameOffsetsByHostOrigin(t *testing.T) {
	// A WebView that does not start at the top of the screen: everything in
	// the page must shift down by the host's origin, which is the whole
	// reason the native node is consulted at all.
	top := uitree.Rect{X1: 0, Y1: 0, X2: 400, Y2: 800}
	inset := uitree.Rect{X1: 40, Y1: 200, X2: 440, Y2: 1000}

	a, _ := NewFrame(top, &Metrics{CSSWidth: 400})
	b, _ := NewFrame(inset, &Metrics{CSSWidth: 400})

	ra := a.ToDevice(10, 10, 100, 20)
	rb := b.ToDevice(10, 10, 100, 20)
	if rb.X1-ra.X1 != 40 || rb.Y1-ra.Y1 != 200 {
		t.Errorf("inset webview did not shift by its origin: %v vs %v", ra, rb)
	}
}

func TestNewFrameRejectsUnusableInputs(t *testing.T) {
	good := uitree.Rect{X1: 0, Y1: 0, X2: 400, Y2: 800}
	if _, err := NewFrame(uitree.Rect{}, &Metrics{CSSWidth: 400}); err == nil {
		t.Error("a zero-area webview was accepted")
	}
	if _, err := NewFrame(good, &Metrics{CSSWidth: 0}); err == nil {
		t.Error("a zero-width viewport was accepted")
	}
	if _, err := NewFrame(good, nil); err == nil {
		t.Error("missing metrics were accepted")
	}
}

func TestNeutralRole(t *testing.T) {
	tests := []struct {
		el   Element
		want string
	}{
		{Element{Tag: "a"}, "link"},
		{Element{Tag: "button"}, "button"},
		{Element{Tag: "input", Type: "text"}, "input"},
		{Element{Tag: "input", Type: "checkbox"}, "checkbox"},
		{Element{Tag: "input", Type: "radio"}, "radio"},
		{Element{Tag: "input", Type: "submit"}, "button"},
		{Element{Tag: "textarea"}, "input"},
		{Element{Tag: "select"}, "input"},
		{Element{Tag: "div", Role: "button"}, "button"},
		{Element{Tag: "span", Role: "checkbox"}, "checkbox"},
		// An explicit role that mobium has no equivalent for falls back to
		// the tag rather than inventing a vocabulary entry.
		{Element{Tag: "a", Role: "menuitem"}, "link"},
	}
	for _, tc := range tests {
		if got := tc.el.NeutralRole(); got != tc.want {
			t.Errorf("%+v NeutralRole = %q, want %q", tc.el, got, tc.want)
		}
	}
}

func TestSelectorPrefersTestID(t *testing.T) {
	if got := (Element{TestID: "buy", ID: "x"}).Selector(); got != `[data-testid="buy"]` {
		t.Errorf("selector = %q", got)
	}
	if got := (Element{ID: "checkout"}).Selector(); got != "#checkout" {
		t.Errorf("selector = %q", got)
	}
	// Nothing durable: the empty selector signals that the ref must fall back
	// to matching by label and role.
	if got := (Element{Label: "Buy"}).Selector(); got != "" {
		t.Errorf("selector = %q, want empty", got)
	}
}

func TestMapScriptFiltersInvisibleAndZeroSized(t *testing.T) {
	// The script is sent to a real page, so its intent is asserted here
	// rather than its behavior: both filters must be present, since a
	// zero-sized or hidden element has no tappable point.
	for _, want := range []string{"r.width <= 0", "visibility", "display", "opacity"} {
		if !strings.Contains(mapScript, want) {
			t.Errorf("map script is missing the %q filter", want)
		}
	}
}

// TestFrameRefusesAHostThatIsNotTheContent guards the iOS geometry trap.
//
// Mobile Safari's XCUIElementTypeWebView covers the whole window, chrome
// included, while the page's viewport is 160 CSS points shorter — and the page
// cannot see its own inset: screenY, visualViewport.offsetTop and pageTop are
// all zero. Measured on an iPhone 17 Pro simulator: the page put "Learn more"
// at CSS y=254, the native tree had it at device y=948, and trusting the
// host's origin placed it at 762 — 186 device pixels too high. Enough to hit a
// different link, not enough to look wrong.
func TestFrameRefusesAHostThatIsNotTheContent(t *testing.T) {
	// Safari, as measured. Screen 1206x2622 device px at 3x; viewport 402x714.
	_, err := NewFrame(uitree.Rect{X1: 0, Y1: 0, X2: 1206, Y2: 2622},
		&Metrics{CSSWidth: 402, CSSHeight: 714})
	if err == nil {
		t.Fatal("accepted a host 160 CSS pixels taller than the page it hosts")
	}
	for _, want := range []string{"874", "714", "app_text"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

// TestFrameAcceptsAnEmbeddedWebView is the case this feature is actually for:
// a WKWebView or Android WebView placed by an app, whose frame is its content.
func TestFrameAcceptsAnEmbeddedWebView(t *testing.T) {
	// A 402x714 CSS page at 3x, inset below a 62-point app bar.
	f, err := NewFrame(uitree.Rect{X1: 0, Y1: 186, X2: 1206, Y2: 186 + 2142},
		&Metrics{CSSWidth: 402, CSSHeight: 714})
	if err != nil {
		t.Fatalf("refused a WebView whose frame is its content: %v", err)
	}
	if f.Scale != 3 {
		t.Errorf("scale = %v, want 3", f.Scale)
	}
	// The element the real page reported, converted, must land where the
	// native tree said it was.
	got := f.ToDevice(80.40625, 254.171875, 82, 20)
	if got.X1 != 241 || got.Y1 != 948 {
		t.Errorf("converted to %v, want x1=241 y1=948", got)
	}
}

// TestFrameToleratesASmallDisagreement: a scrollbar or a rounded density moves
// this by a pixel or two, and refusing on that would make the check useless.
func TestFrameToleratesASmallDisagreement(t *testing.T) {
	for _, off := range []int{-9, -3, 0, 3, 9} {
		host := uitree.Rect{X1: 0, Y1: 0, X2: 1206, Y2: 2142 + off}
		_, err := NewFrame(host, &Metrics{CSSWidth: 402, CSSHeight: 714})
		// off is in device pixels; the tolerance is 4 CSS pixels = 12 device.
		if err != nil {
			t.Errorf("refused a %d device-pixel disagreement, which is within a rounding: %v", off, err)
		}
	}
}

// TestFrameStillWorksWithoutAReportedHeight keeps the Android path unchanged:
// an older WebView that reports no viewport height cannot be cross-checked,
// and must not be refused for it.
func TestFrameStillWorksWithoutAReportedHeight(t *testing.T) {
	f, err := NewFrame(uitree.Rect{X1: 0, Y1: 0, X2: 1080, Y2: 2400},
		&Metrics{CSSWidth: 360})
	if err != nil {
		t.Fatalf("refused a page that reported no height: %v", err)
	}
	if f.Scale != 3 {
		t.Errorf("scale = %v, want 3", f.Scale)
	}
}

// With the iOS keyboard up over an app's WebView the page's viewport shrinks
// and the WebView does not, so the frame is the WebView's, by its width, and
// is not refused as NewFrame refuses mobile Safari's: MobiumApp's Web form
// reported 571 CSS pixels in a 584-pixel WebView. CHALLENGES 230.
func TestAFrameUnderTheKeyboardIsTheWebViews(t *testing.T) {
	host := uitree.Rect{X1: 0, Y1: 768, X2: 1206, Y2: 2520}
	m := &Metrics{CSSWidth: 402, CSSHeight: 571}
	if _, err := NewFrame(host, m); err == nil {
		t.Fatal("NewFrame took the shrunken viewport, so this test cannot tell the two apart")
	}
	f, err := NewFrameUnderKeyboard(host, m)
	if err != nil {
		t.Fatal(err)
	}
	if r := f.ToDevice(16, 164, 370, 38); r.X1 != 48 || r.Y1 != 768+492 {
		t.Errorf("the email field maps to %v, want its top-left at (48, 1260)", r)
	}
	if _, err := NewFrameUnderKeyboard(uitree.Rect{}, m); err == nil {
		t.Error("a WebView with no area was given a frame")
	}
}
