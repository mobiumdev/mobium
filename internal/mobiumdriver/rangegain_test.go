package mobiumdriver

import (
	"context"
	"math"
	"os"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// fakeRange is Morpho Market's price range as a slider that moves: a drag
// moves the thumb nearer its press by gain times the finger's travel past
// the slop, held between the track's ends and the other thumb.
type fakeRange struct {
	t           *testing.T
	gain        float64
	slop, inset int
	start, end  int // the thumbs' centers
	track       uitree.Rect
	half        int
	drags       int
	hideMet     bool    // the start thumb leaves the tree when the two meet
	maxGain     float64 // the drive's, 1 unless set
	trueSlop    int     // the slop the slider has, where it is not the one assumed
	steps       int     // the slider's steps, where it has them
}

const fakeSlop, fakeInset = 21, 5

func newFakeRange(t *testing.T, gain float64, start, end float64) *fakeRange {
	s, e := rangeFixture(t)
	track, _, _ := uitree.RangeThumb(e)
	f := &fakeRange{t: t, gain: gain, slop: fakeSlop, inset: fakeInset, track: track, half: e.Bounds.Width() / 2, maxGain: 1}
	if gain > 1 {
		f.maxGain = 3 // Horizon OS's drive
	}
	f.start, f.end = sliderAlong(track, fakeInset, start), sliderAlong(track, fakeInset, end)
	_ = s
	return f
}

func (f *fakeRange) drag(x1, _, x2, _ int) error {
	f.drags++
	d := x2 - x1
	eff := 0.0
	slop := f.slop
	if f.trueSlop != 0 {
		slop = f.trueSlop
	}
	if a := abs(d) - slop; a > 0 {
		eff = math.Copysign(float64(a)*f.gain, float64(d))
	}
	lo, hi := f.track.X1+f.inset, f.track.X2-f.inset
	snap := func(x int) int {
		if f.steps == 0 {
			return x
		}
		w := float64(hi-lo) / float64(f.steps)
		return lo + int(math.Round(math.Round(float64(x-lo)/w)*w))
	}
	// Material's RangeSlider moves the thumb nearer the press, and of two
	// on one point the one the drag can move.
	if ds, de := abs(x1-f.start), abs(x1-f.end); ds < de || ds == de && d < 0 {
		f.start = max(lo, min(f.end, snap(f.start+int(math.Round(eff)))))
	} else {
		f.end = max(f.start, min(hi, snap(f.end+int(math.Round(eff)))))
	}
	return nil
}

func (f *fakeRange) snapshot(context.Context) (*uitree.Tree, error) {
	data, err := os.ReadFile("../uitree/testdata/android-compose-range-slider-uia2.xml")
	if err != nil {
		f.t.Fatal(err)
	}
	tree, err := uitree.ParseAndroid(data)
	if err != nil {
		f.t.Fatal(err)
	}
	if f.hideMet && abs(f.start-f.end) <= 1 {
		tree.Walk(func(n *uitree.Node) bool {
			if n.Label == "Range start" && n.Parent != nil {
				kids := n.Parent.Children[:0]
				for _, c := range n.Parent.Children {
					if c != n {
						kids = append(kids, c)
					}
				}
				n.Parent.Children = kids
				return false
			}
			return true
		})
	}
	tree.Walk(func(n *uitree.Node) bool {
		switch n.Label {
		case "Range start":
			n.Bounds.X1, n.Bounds.X2 = f.start-f.half, f.start+f.half
		case "Range end":
			n.Bounds.X1, n.Bounds.X2 = f.end-f.half, f.end+f.half
		}
		return true
	})
	return tree, nil
}

func (f *fakeRange) thumb(label string) *uitree.Node {
	tree, _ := f.snapshot(context.Background())
	var found *uitree.Node
	tree.Walk(func(n *uitree.Node) bool {
		if n.Label == label {
			found = n
			return false
		}
		return true
	})
	return found
}

func (f *fakeRange) fill(label string, position float64) error {
	return dragThumb(context.Background(), f.thumb(label), position, thumbDrive{
		// No edge: a panel's track is away from the screen's sides, as the
		// Quest's was (416 to 1648 of 2064); the edge has its own test.
		inset: func(*uitree.Node) int { return f.inset }, slop: f.slop, edge: 0, maxGain: f.maxGain,
		drag: f.drag, snapshot: f.snapshot,
	})
}

// A thumb lands where it was sent whether a drag moves it as far as the
// finger, as on a phone, or 2.48 times as far, as a Compose app's on a
// Horizon OS panel (CHALLENGES 367) — and further, or less far.
func TestAThumbLandsWhateverTheDragsGain(t *testing.T) {
	for _, gain := range []float64{1, 2.48, 0.5} {
		for _, slop := range []int{0, 4, -2} {
			testThumbLands(t, gain, slop)
		}
	}
}

// testThumbLands fills each thumb on a $0–$60 slider of $1 steps, its true
// slop trueSlop pixels where it is not 0 (-2: a thumb that runs ahead).
func testThumbLands(t *testing.T, gain float64, trueSlop int) {
	{
		for _, c := range []struct {
			label    string
			position float64
		}{{"Range end", 0.25}, {"Range end", 0.6}, {"Range start", 0.4}} {
			f := newFakeRange(t, gain, 0, 1)
			f.trueSlop, f.steps = trueSlop, 60
			if trueSlop == 0 {
				f.steps = 0
			}
			if err := f.fill(c.label, c.position); err != nil {
				t.Errorf("gain %g, slop %d, %s to %g: %v", gain, trueSlop, c.label, c.position, err)
				continue
			}
			got := f.end
			if c.label == "Range start" {
				got = f.start
			}
			if want := sliderAlong(f.track, f.inset, c.position); abs(got-want) > max(1, f.track.Width()/200) {
				t.Errorf("gain %g, slop %d, %s to %g: stopped at x %d, not %d, after %d drags", gain, trueSlop, c.label, c.position, got, want, f.drags)
			}
		}
	}
}

// A drag that moves nothing is refused, never reported as a move.
func TestAThumbThatDoesNotMoveIsRefused(t *testing.T) {
	f := newFakeRange(t, 0, 0, 1)
	err := f.fill("Range end", 0.25)
	if mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed {
		t.Fatalf("a thumb that never moved: %v", err)
	}
	if f.drags != 5 {
		t.Errorf("%d drags, want 5", f.drags)
	}
}

// Carried onto the other thumb, a Compose thumb on a Horizon OS panel hid
// it, and what was left was no two-thumb slider: refused, not reported as
// moved, and a first drag of unknown gain does not get there.
func TestAThumbIsNotCarriedOntoTheOther(t *testing.T) {
	f := newFakeRange(t, 2.48, 0, 1)
	f.hideMet = true
	if err := f.fill("Range end", 0.25); err != nil {
		t.Errorf("end to 0.25 at gain 2.48: %v", err)
	}
	f = newFakeRange(t, 1, 0, 1)
	f.hideMet = true
	if err := f.fill("Range end", 0.25); err != nil || f.drags > 3 {
		t.Errorf("end to 0.25 at gain 1: %v after %d drags", err, f.drags)
	}
	// Met all the same, the miss is refused.
	f = newFakeRange(t, 1, 0, 0)
	f.hideMet = true
	f.end = f.start
	if err := f.fill("Range end", 0.5); mobiumerr.CodeOf(err) == "" && f.end != sliderAlong(f.track, f.inset, 0.5) {
		t.Errorf("a thumb lost from the tree was taken for moved")
	}
}

// Sent past the other thumb, a thumb stops against it, and that is where
// it can go. Stopped against it short of the point, it went too far.
func TestAThumbStopsAgainstTheOther(t *testing.T) {
	f := newFakeRange(t, 1, 0, 0.5)
	if err := f.fill("Range start", 0.9); err != nil {
		t.Errorf("sent past the other thumb: %v", err)
	}
	if abs(f.start-f.end) > f.half {
		t.Errorf("start at %d, end at %d", f.start, f.end)
	}
}

// On a phone a drag carries a thumb as far as the finger, and a fill takes
// one drag, as it did before any gain was learned.
func TestAPhoneFillTakesOneDrag(t *testing.T) {
	f := newFakeRange(t, 1, 0, 1)
	for _, c := range []struct {
		label    string
		position float64
	}{{"Range end", 0.25}, {"Range start", 0.1}, {"Range end", 0.75}, {"Range start", 0.5}} {
		f.drags = 0
		if err := f.fill(c.label, c.position); err != nil || f.drags != 1 {
			t.Errorf("%s to %g: %d drags, %v", c.label, c.position, f.drags, err)
		}
	}
}
