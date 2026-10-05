package mobiumdriver

import (
	"testing"

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
