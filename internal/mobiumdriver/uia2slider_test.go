package mobiumdriver

import (
	"os"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// A seek bar is moved by touching its track, the bar less 16dp at each end:
// on the Pixel 8 Pro, at 360dpi, MobiumApp's Volume bar was [36,542][972,583]
// and its inset 36 pixels. 0.2, 0.5 and 0.8 of it read 20, 50 and 80; the
// bar's last pixel took no touch. CHALLENGES 227.
func TestASliderIsTouchedOnItsTrack(t *testing.T) {
	bar := uitree.Rect{X1: 36, Y1: 542, X2: 972, Y2: 583}
	for _, c := range []struct {
		pos  float64
		want int
	}{{0, 72}, {0.5, 504}, {1, 936}} {
		if x, y := sliderTouch(bar, 36, c.pos); x != c.want || y != 562 {
			t.Errorf("position %g touched (%d, %d), want (%d, 562)", c.pos, x, y, c.want)
		}
	}
	// No inset: the end is still kept off the last pixel.
	if x, _ := sliderTouch(bar, 0, 1); x != 970 {
		t.Errorf("position 1 with no inset touched x %d, want 970", x)
	}
	// An inset larger than a quarter of a short bar is cut down to it.
	short := uitree.Rect{X1: 0, Y1: 0, X2: 100, Y2: 20}
	if x, _ := sliderTouch(short, 60, 0); x != 25 {
		t.Errorf("a short bar's start touched x %d, want 25", x)
	}
}

func rangeFixture(t *testing.T) (start, end *uitree.Node) {
	t.Helper()
	data, err := os.ReadFile("../uitree/testdata/android-compose-range-slider-uia2.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseAndroid(data)
	if err != nil {
		t.Fatal(err)
	}
	tree.Walk(func(n *uitree.Node) bool {
		switch n.Label {
		case "Range start":
			start = n
		case "Range end":
			end = n
		}
		return true
	})
	if start == nil || end == nil {
		t.Fatal("the fixture has no Range start and Range end")
	}
	return start, end
}

// Material's RangeSlider is two seek bars the size of their thumbs under
// the view that is the track: Morpho Market's price range, $0 to $60, on
// the Android 15 emulator at 420dpi.
func TestARangeSliderThumbHasTheTrackOfItsParent(t *testing.T) {
	start, end := rangeFixture(t)
	track, other, ok := uitree.RangeThumb(end)
	if !ok || other != start || track != (uitree.Rect{X1: 32, Y1: 662, X2: 1048, Y2: 788}) {
		t.Errorf("rangeThumb = %v, %v, %v", track, other, ok)
	}
	// Both thumbs' centers are the track's ends, 5.5 pixels in: 2dp.
	if x := sliderAlong(track, 5, 0); x != 37 {
		t.Errorf("position 0 at x %d", x)
	}
	if x := sliderAlong(track, 5, 1); x != 1043 {
		t.Errorf("position 1 at x %d", x)
	}
	// One seek bar alone is no thumb.
	lone := &uitree.Node{Class: "android.widget.SeekBar", Bounds: uitree.Rect{X2: 900, Y2: 40}}
	lone.Parent = &uitree.Node{Bounds: uitree.Rect{X2: 1000, Y2: 40}, Children: []*uitree.Node{lone}}
	if _, _, ok := uitree.RangeThumb(lone); ok {
		t.Error("a lone seek bar was taken for a thumb")
	}
}

// With gesture navigation a drag from the screen's side is the back
// gesture, and a full range puts both thumbs there: the drag takes hold
// 32dp in, beside the thumb, which the slider still takes for that thumb.
func TestAThumbAtTheEdgeIsHeldAwayFromIt(t *testing.T) {
	start, end := rangeFixture(t)
	if x, _, err := thumbGrip(start, end, 84); err != nil || x != 84 {
		t.Errorf("the start thumb is held at x %d, %v", x, err)
	}
	if x, _, err := thumbGrip(end, start, 84); err != nil || x != 1080-84 {
		t.Errorf("the end thumb is held at x %d, %v", x, err)
	}
	// Moved in, the hold would be nearer the other thumb: refused, not the
	// wrong thumb dragged.
	near := *start
	near.Bounds = uitree.Rect{X1: 60, Y1: 667, X2: 123, Y2: 783}
	near.Parent = start.Parent
	if _, _, err := thumbGrip(start, &near, 84); mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Errorf("a hold nearer the other thumb: %v", err)
	}
}
