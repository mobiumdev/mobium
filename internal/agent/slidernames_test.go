package agent

import (
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// Two seek bars with one rectangle are a two-thumb control's two ranges, and
// are told apart by their place among those sharing it: MobiumApp's price,
// on the Pixel 8 Pro. A key by rectangle alone named both thumbs the same.
// CHALLENGES 347.
func TestSliderKeysTellTwoThumbsApart(t *testing.T) {
	track := uitree.Rect{X1: 54, Y1: 1104, X2: 954, Y2: 1165}
	other := uitree.Rect{X1: 56, Y1: 868, X2: 960, Y2: 906}
	keys := sliderKeys([]*uitree.Node{{Bounds: other}, {Bounds: track}, {Bounds: track}})
	if keys[1] == keys[2] {
		t.Fatalf("the two thumbs share a key: %v", keys)
	}
	if keys[0] == keys[1] || keys[0] == keys[2] {
		t.Fatalf("a slider elsewhere shares a key with a thumb: %v", keys)
	}
}
