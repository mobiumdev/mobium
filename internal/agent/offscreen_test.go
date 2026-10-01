package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// offscreenDriver shows one link below the bottom of a 430x932 screen,
// hidden, as Wikipedia's feed did on an iPhone 15 Plus: the rest of an
// extract its card clips. scrolled puts it inside a scroll view instead.
type offscreenDriver struct {
	fakeDriver
	scrolled bool
	tapped   []string
}

func (d *offscreenDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	open, close := `<XCUIElementTypeOther type="XCUIElementTypeOther" x="0" y="0" width="430" height="932" visible="true" enabled="true" accessible="false">`, `</XCUIElementTypeOther>`
	if d.scrolled {
		open, close = `<XCUIElementTypeScrollView type="XCUIElementTypeScrollView" x="0" y="0" width="430" height="932" visible="true" enabled="true" accessible="false">`, `</XCUIElementTypeScrollView>`
	}
	return uitree.ParseIOS([]byte(`<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Wikipedia" ` +
		`bundleId="org.wikimedia.wikipedia" x="0" y="0" width="430" height="932" visible="true" enabled="true" accessible="false">` +
		open + `<XCUIElementTypeLink type="XCUIElementTypeLink" name="computer printers" label="computer printers" ` +
		`x="16" y="921" width="378" height="47" visible="false" enabled="true" accessible="true"/>` + close +
		`</XCUIElementTypeApplication>`))
}

func (d *offscreenDriver) Tap(ctx context.Context, x, y int) error {
	d.tapped = append(d.tapped, fmt.Sprintf("%d,%d", x, y))
	return nil
}

// A link whose center is below the screen, with nothing around it that
// scrolls, was tapped there and reported done. It is refused instead, saying
// where it is, and nothing is touched.
func TestAnElementOffTheScreenWithNothingToScrollIsRefused(t *testing.T) {
	d := &offscreenDriver{}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 10 * time.Millisecond
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}
	h.sessions["fake"] = s
	_, err := tap(h, s, map[string]interface{}{"target": "label=computer printers"})
	if mobiumerr.CodeOf(err) != mobiumerr.ElementNotReachable || !strings.Contains(err.Error(), "off the screen") {
		t.Fatalf("an off-screen element with no scroller gave %v", err)
	}
	if len(d.tapped) != 0 {
		t.Errorf("it was touched anyway, at %v", d.tapped)
	}

	// Inside a scroll view, it is the scroll path's to bring into view, not
	// this refusal's.
	d = &offscreenDriver{scrolled: true}
	s = &session{dev: fakeDevice(), driver: d, backend: BackendDump}
	h.sessions["fake"] = s
	_, err = tap(h, s, map[string]interface{}{"target": "label=computer printers"})
	if err != nil && strings.Contains(err.Error(), "nothing it is inside scrolls") {
		t.Errorf("an element inside a scroll view was refused as having none: %v", err)
	}
}
