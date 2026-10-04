package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// feedDriver is a list that grows as it is scrolled, as MobiumApp's Feed
// Demo does: ten rows a page, five in view. A swipe that reaches the end
// starts loading the next page, shown as a spinner below the last row, and
// moves nothing until the page arrives a few readings later. After three
// pages it truly ends.
type feedDriver struct {
	t       *testing.T
	rows    int
	first   int
	loading int // readings left until the page arrives; 0 when idle
	swipes  int
}

const feedVisible = 5

func (d *feedDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	if d.loading > 0 {
		d.loading--
		if d.loading == 0 {
			d.rows += 10
		}
	}
	var b strings.Builder
	b.WriteString(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="com.example.feed" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" package="com.example.feed" class="androidx.recyclerview.widget.RecyclerView" ` +
		`scrollable="true" bounds="[0,200][1080,1200]">`)
	for i := 0; i < d.rows; i++ {
		y := 200 + (i-d.first)*200
		fmt.Fprintf(&b, `<node index="%d" text="Row %d" package="com.example.feed" class="android.widget.TextView" `+
			`clickable="true" enabled="true" bounds="[0,%d][1080,%d]" />`, i, i+1, y, y+200)
	}
	if d.loading > 0 {
		y := 200 + (d.rows-d.first)*200
		if y > 1100 {
			y = 1100
		}
		fmt.Fprintf(&b, `<node index="%d" package="com.example.feed" class="android.widget.ProgressBar" `+
			`enabled="true" bounds="[500,%d][580,%d]" />`, d.rows, y, y+80)
	}
	b.WriteString(`</node></node></hierarchy>`)
	tree, err := uitree.ParseAndroid([]byte(b.String()))
	if err != nil {
		d.t.Fatalf("parse feed: %v", err)
	}
	return tree, nil
}
func (d *feedDriver) Screenshot(ctx context.Context) ([]byte, error) { return nil, nil }
func (d *feedDriver) Tap(ctx context.Context, x, y int) error        { return nil }
func (d *feedDriver) Name() string                                   { return "feed-fake" }
func (d *feedDriver) LongPress(ctx context.Context, x, y int, _ time.Duration) error {
	return nil
}
func (d *feedDriver) Swipe(ctx context.Context, x1, y1, x2, y2 int, _ time.Duration) error {
	d.swipes++
	if y2 >= y1 || d.loading > 0 {
		return nil
	}
	end := d.rows - feedVisible
	if d.first >= end {
		if d.rows < 30 {
			d.loading = 3
		}
		return nil
	}
	d.first += feedVisible
	if d.first > end {
		d.first = end
	}
	return nil
}

// A list that loads its next page when its end is reached is not at its end
// while it loads: the swipe that reached it moves nothing, and a spinner
// below the last row says why. scroll-to waits it out and goes on to a row
// two pages down — and at the real end, with no spinner, still says it is the
// end, at once. CHALLENGES 223.
func TestScrollToWaitsForAListToLoadMore(t *testing.T) {
	run := func(target string) (*feedDriver, time.Duration, error) {
		d := &feedDriver{t: t, rows: 10}
		h := NewHandlers()
		h.implicitWait, h.settleWindow = 0, 0
		s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
		start := time.Now()
		_, err := scrollTo(h, s, map[string]interface{}{"target": target, "direction": "down"})
		return d, time.Since(start), err
	}
	if d, _, err := run("text=Row 25"); err != nil {
		t.Errorf("Row 25, two pages down: %v (rows loaded %d, swipes %d)", err, d.rows, d.swipes)
	}
	d, took, err := run("text=Row 40")
	if mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement || !strings.Contains(err.Error(), "end of the list") {
		t.Errorf("Row 40, past the real end: %v", err)
	}
	if d.rows != 30 {
		t.Errorf("the feed held %d rows at the end, want all 30", d.rows)
	}
	if took > loadWait {
		t.Errorf("the real end took %s to report, longer than a load is waited for", took)
	}
}

// A role nothing has is refused rather than matching nothing: the help's
// own `wait role=progressbar --for hidden` was over at once before
// progressbar was a role. CHALLENGES 222.
func TestAnUnknownRoleIsRefused(t *testing.T) {
	for _, l := range []string{"role=nosuchrole", "label=Save,role=buton"} {
		if _, err := uitree.ParseLocator(l); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
			t.Errorf("%s: %v, want refused", l, err)
		}
	}
	for _, l := range []string{"role=progressbar", "role=slider", "label=Save,role=button", "role=password"} {
		if _, err := uitree.ParseLocator(l); err != nil {
			t.Errorf("%s: %v", l, err)
		}
	}
}

// On iOS the spinner below the last row is reported not visible when the
// list stops with that row at its bottom edge, and it still means the next
// page is coming: scroll-to found "the end (7 scrolls)" in two runs of four
// while asking for a visible one. Captured from the Feed Demo mid-load.
func TestASpinnerOutOfViewStillMeansLoading(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/ios26-mobiumapp-feed-loading.xml")
	if err != nil {
		t.Fatal(err)
	}
	for _, visible := range []string{"true", "false"} {
		xml := strings.Replace(string(raw), `type="XCUIElementTypeActivityIndicator" value="1" name="In progress" label="In progress" enabled="true" visible="true"`,
			`type="XCUIElementTypeActivityIndicator" value="1" name="In progress" label="In progress" enabled="true" visible="`+visible+`"`, 1)
		tree, err := uitree.ParseIOS([]byte(xml))
		if err != nil {
			t.Fatal(err)
		}
		c := scrollContainer(tree)
		if c == nil || !busyIn(tree, c) {
			t.Errorf("a spinner inside the list with visible=%s does not read as loading", visible)
		}
	}
	// And a list with no spinner is not loading.
	tree, _ := uitree.ParseIOS([]byte(strings.ReplaceAll(string(raw), "XCUIElementTypeActivityIndicator", "XCUIElementTypeOther")))
	if c := scrollContainer(tree); c != nil && busyIn(tree, c) {
		t.Error("a list with no spinner reads as loading")
	}
}
