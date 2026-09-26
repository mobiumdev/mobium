package device

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// The head of `dumpsys input_method` captured on the Pixel 7 AVD (API 35)
// with a text field focused and the keyboard up.
func TestParseInputShownReadsARealDump(t *testing.T) {
	raw, err := os.ReadFile("testdata/input-method-shown-api35.txt")
	if err != nil {
		t.Fatal(err)
	}
	up, err := parseInputShown(string(raw))
	if err != nil || !up {
		t.Fatalf("up = %v, err = %v; want true", up, err)
	}
	if up, err := parseInputShown("mCurMethodId=x mInputShown=false mSystemReady=true"); err != nil || up {
		t.Errorf("hidden read as %v, %v", up, err)
	}
	// Output that does not say is not taken as "hidden".
	if _, err := parseInputShown("Current Input Method Manager state:"); err == nil {
		t.Error("an answer with no mInputShown was read as hidden")
	}
}

// The input method's touchable region, as `dumpsys window InputMethod` gave it
// on a headed Pixel 7 AVD: Gboard's floating toolbar at the left edge, and a
// strip along the bottom. Its frame is the whole screen below the status
// bar, which covers everything and means nothing.
func TestIMERegions(t *testing.T) {
	out := `  Window #0 Window{d131759 u0 InputMethod}:
    mViewVisibility=0x0 mHaveFrame=true mObscured=false
    touchable region=SkRegion((21,965,169,1525)(0,2337,1080,2400))
    Frames: parent=[0,136][1080,2400] display=[0,136][1080,2400] frame=[0,136][1080,2400]
    isVisible=true`
	got := parseIMERegions(out)
	want := [][4]int{{21, 965, 169, 1525}, {0, 2337, 1080, 2400}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("regions = %v, want %v", got, want)
	}
	hidden := strings.Replace(out, "isVisible=true", "isVisible=false", 1)
	if got := parseIMERegions(hidden); got != nil {
		t.Errorf("a hidden keyboard has regions: %v", got)
	}
}

// The clipboard's preview, from the same dumpsys on the same AVD a moment
// after app_clipboard wrote: its window and, after it, the display's list of
// every visible window, whose "touchableRegion=" is another spelling and
// must not be read as the preview's.
func TestClipboardPreviewRegions(t *testing.T) {
	out := `  Window #0 Window{7f76725 u0 ClipboardOverlay}:
    mTouchableInsets=3 mGivenInsetsPending=false
    touchable region=SkRegion((179,1985,367,2048)(32,2048,367,2121)(-31,2121,654,2336)(253,2336,654,2352))
    isVisible=true
    3 visible windows: [f96b7a9 ScreenDecorOverlay, frame=[Rect(0, 0 - 1080, 136)], touchableRegion=SkRegion((480,0,625,136))]`
	got := parseIMERegions(out)
	want := [][4]int{{179, 1985, 367, 2048}, {32, 2048, 367, 2121}, {-31, 2121, 654, 2336}, {253, 2336, 654, 2352}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("regions = %v, want %v", got, want)
	}
	// With no preview up, dumpsys names no window.
	if got := parseIMERegions("WINDOW MANAGER WINDOWS (dumpsys window windows)\n"); got != nil {
		t.Errorf("no preview has regions: %v", got)
	}
}
