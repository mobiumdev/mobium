package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// A failed tool call carries its code on the wire, beside the unchanged text:
// the structured half is what the clients and --json read.
func TestAFailedCallCarriesItsCode(t *testing.T) {
	h := NewHandlers()
	params, _ := json.Marshal(ToolsCallParams{Name: "app_no_such_tool"})
	out, perr := CallTool(params, h)
	if perr != nil {
		t.Fatalf("a failing tool became a protocol error: %v", perr)
	}
	res := out.(ToolsCallResult)
	if !res.IsError {
		t.Fatal("not reported as an error")
	}
	p, ok := res.StructuredContent.(mobiumerr.Payload)
	if !ok {
		t.Fatalf("no structured payload: %#v", res.StructuredContent)
	}
	if p.Code != mobiumerr.InvalidArgument {
		t.Errorf("code = %s, want invalid_argument", p.Code)
	}
	// The text an MCP agent reads is the message, unchanged.
	if res.Content[0].Text != p.Message || p.Message == "" {
		t.Errorf("text %q and message %q disagree", res.Content[0].Text, p.Message)
	}
}

// The three locator outcomes are three codes, because they want three
// different responses: scroll, narrow the locator, or scroll with a direction.
func TestLocatorFailuresAreDistinguishable(t *testing.T) {
	tree := loadLauncher(t)
	cases := map[mobiumerr.Code]uitree.Locator{
		mobiumerr.NoSuchElement:    {Kind: uitree.KindText, Value: "Nowhere at all", Exact: true},
		mobiumerr.AmbiguousLocator: {Kind: uitree.KindText, Value: "Gmail"},
	}
	for want, loc := range cases {
		_, err := pickOne(loc, tree)
		if got := mobiumerr.CodeOf(err); got != want {
			t.Errorf("%s: code %s, want %s (%v)", loc, got, want, err)
		}
		e, _ := mobiumerr.As(err)
		if e == nil || e.Remedy == "" || e.Details["locator"] == nil {
			t.Errorf("%s: no remedy or locator detail: %+v", loc, e)
		}
	}
	off, err := uitree.ParseIOS([]byte(offscreenRowIOS))
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := uitree.ParseLocator("label=Legal & Regulatory,role=button")
	if _, err := pickOne(loc, off); mobiumerr.CodeOf(err) != mobiumerr.ElementNotReachable {
		t.Errorf("an off-screen row is %s, want element_not_reachable", mobiumerr.CodeOf(err))
	}
}

// Every front door reports a failure through ErrorResult, so every one
// carries the code. `mobium pipe` once built its own result without it, and
// all five clients raised their base exception for every failure.
func TestErrorResultCarriesTheCode(t *testing.T) {
	r := ErrorResult(fmt.Errorf("app_tap: %w", mobiumerr.New(mobiumerr.NoSuchElement, "no element matches x")))
	if !r.IsError || len(r.Content) != 1 || !strings.Contains(r.Content[0].Text, "no element matches x") {
		t.Errorf("result = %+v", r)
	}
	raw, err := json.Marshal(r.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"code":"no_such_element"`) {
		t.Errorf("structured content %s lacks the code", raw)
	}
}

// A target under a dialog is refused rather than tapped: iOS keeps it in the
// tree, not visible, and a tap on it landed on the dialog while reporting
// success. The dialog's own buttons still resolve. Captured from MobiumApp.
func TestATargetUnderADialogIsRefused(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/ios-save-password.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	under, _ := uitree.ParseLocator("testid=logoutBtn")
	_, err = pickOne(under, tree)
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "Save Password?") {
		t.Errorf("a button under the sheet: %v", err)
	}
	if e, _ := mobiumerr.As(err); e == nil || e.Remedy == "" {
		t.Error("no remedy")
	}
	// Not a miss: nothing should scroll looking for it.
	if matchedNothing(err) {
		t.Error("a covered target reads as a miss, and would be scrolled for")
	}
	on, _ := uitree.ParseLocator("label=Not Now")
	if n, err := pickOne(on, tree); err != nil || n.Label != "Not Now" {
		t.Errorf("the sheet's own button: %v", err)
	}
}

// So is a target behind a screen the app put in front itself, which is no
// Alert or Sheet: Ice Cubes' Timeline tab, behind its image viewer, took a
// tap that landed on the viewer and was reported done. The viewer's own
// Close still resolves. CHALLENGES 217.
func TestATargetBehindAnotherScreenIsRefused(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/ios26-icecubes-image-viewer.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	behind, _ := uitree.ParseLocator("label=Timeline")
	_, err = pickOne(behind, tree)
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "another screen") {
		t.Errorf("the tab behind the viewer: %v", err)
	}
	if e, _ := mobiumerr.As(err); e == nil || !strings.Contains(e.Remedy, "app_map") {
		t.Errorf("the remedy does not say where the close control is: %v", err)
	}
	if matchedNothing(err) {
		t.Error("a covered target reads as a miss, and would be scrolled for")
	}
	front, _ := uitree.ParseLocator("label=Close")
	if n, err := pickOne(front, tree); err != nil || n.Label != "Close" {
		t.Errorf("the viewer's own Close: %v", err)
	}
}

// Behind a menu, which has no close control, the refusal names a point
// outside it; behind the image viewer, which has one, it names none.
// CHALLENGES 221.
func TestARefusalBehindAMenuNamesAPointOutsideIt(t *testing.T) {
	for _, c := range []struct {
		file  string
		point bool
	}{{"ios26-icecubes-post-menu.xml", true}, {"ios26-icecubes-long-press-menu.xml", true},
		{"ios26-icecubes-image-viewer.xml", false}} {
		raw, err := os.ReadFile("../uitree/testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := uitree.ParseIOS(raw)
		if err != nil {
			t.Fatal(err)
		}
		behind, _ := uitree.ParseLocator("label=Trending")
		_, err = pickOne(behind, tree)
		if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
			t.Errorf("%s: the title behind was %v", c.file, err)
			continue
		}
		if got := strings.Contains(err.Error(), "app_tap at x"); got != c.point {
			t.Errorf("%s: offers a point outside: %v, want %v — %v", c.file, got, c.point, err)
		}
	}
}

// Behind an Android popup menu — a window of its own, as a dialog is — the
// refusal names back, which closes a menu or a dialog that can be canceled;
// its items are actions, and tapping one is not closing it. iOS has no
// back, and its refusal does not name one. CHALLENGES 225.
func TestARefusalBehindAnAndroidMenuNamesBack(t *testing.T) {
	loc, _ := uitree.ParseLocator("label=Stopwatch")
	err := appDialogOver("an untitled dialog", loc, true)
	e, _ := mobiumerr.As(err)
	if e == nil || !strings.Contains(err.Error(), "app_press back") || !strings.Contains(e.Remedy, "app_press back") {
		t.Errorf("Android: %v (remedy %q) does not name back", err, e.Remedy)
	}
	err = appDialogOver("Discard changes?", loc, false)
	if strings.Contains(err.Error(), "back") {
		t.Errorf("iOS, which has no back, named it: %v", err)
	}
}

// A near miss is the same words, whole, under another kind — not a longer
// text that contains them. MobiumApp's Pager shows "Card 8 starts off screen
// to the right." while Card 8 itself is off screen; label=Card 8 named that
// note as the locator that works, and scroll-to stopped instead of swiping
// to the card. CHALLENGES 226.
func TestANearMissIsTheWholeWords(t *testing.T) {
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1008,2077]" enabled="true">` +
		`<node index="0" class="android.widget.TextView" text="Card 8 starts off screen to the right." ` +
		`enabled="true" bounds="[36,810][972,853]" />` +
		`<node index="1" class="android.widget.TextView" text="URL" enabled="true" bounds="[36,900][972,950]" />` +
		`</node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	card, _ := uitree.ParseLocator("label=Card 8")
	if alt := nearMiss(card, tree); alt != nil {
		t.Errorf("label=Card 8 was offered %s, a note that only contains the words", alt)
	}
	url, _ := uitree.ParseLocator("label=URL")
	if alt := nearMiss(url, tree); alt == nil || alt.Kind != uitree.KindText {
		t.Errorf("label=URL, whose words are a field's whole text, was offered %v", alt)
	}
}

// The keyboard refusal names both ways to hide it, and both must exist: an
// iPhone's keyboard has no hide key, so enter is named too. A remedy that
// names an argument the tool does not take is obeyed and fails.
func TestTheKeyboardRemedyNamesRealArguments(t *testing.T) {
	loc, _ := uitree.ParseLocator("testid=coveredBtn")
	for _, covered := range []bool{true, false} {
		err := keyboardOver(loc, covered)
		for _, want := range []string{"mobium keyboard --hide", "app_keyboard with hide", `key "enter"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("covered=%v: the remedy lacks %q: %v", covered, want, err)
			}
		}
	}
	// Covered is a refusal, and not scrolled for; a miss stays a miss.
	if c := mobiumerr.CodeOf(keyboardOver(loc, true)); c != mobiumerr.DeviceNotReady {
		t.Errorf("covered: %s", c)
	}
	if c := mobiumerr.CodeOf(keyboardOver(loc, false)); c != mobiumerr.NoSuchElement {
		t.Errorf("a miss: %s", c)
	}
	for _, tool := range GetToolSchemas() {
		if tool.Name != "app_keyboard" {
			continue
		}
		props := tool.InputSchema["properties"].(map[string]interface{})
		if _, ok := props["hide"]; !ok {
			t.Error("app_keyboard no longer takes hide, which the remedy names")
		}
		if _, ok := props["key"]; !ok {
			t.Error("app_keyboard no longer takes key, which the remedy names")
		}
	}
}

// A target under the keyboard is refused on iOS, where it is still in the
// tree: before this, the tap landed on a key and reported success. Captured
// from MobiumApp's Dialog Demo.
func TestATargetUnderTheKeyboardIsRefused(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/ios-keyboard-over-button.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := uitree.ParseLocator("testid=coveredBtn")
	_, err = pickOne(loc, tree)
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "keyboard") {
		t.Errorf("a button under the keyboard: %v", err)
	}
	if matchedNothing(err) {
		t.Error("a covered target reads as a miss, and would be scrolled for")
	}
	back, _ := uitree.ParseLocator("testid=backBtn")
	if _, err := pickOne(back, tree); err != nil {
		t.Errorf("a button above the keyboard: %v", err)
	}
}

// regionDriver reports where the Android keyboard takes touches.
type regionDriver struct {
	fakeDriver
	regions []uitree.Rect
	asked   int
}

func (r *regionDriver) KeyboardRegions(ctx context.Context) ([]uitree.Rect, error) {
	r.asked++
	return r.regions, nil
}

// On Android the keyboard is another window, so its region is asked for —
// only while something has focus — and a target under it is refused. The
// region is the one a headed Pixel 7 AVD reported: Gboard's floating
// toolbar at the left edge, which covers the left of the screen and nothing
// at the bottom.
func TestAnAndroidTargetUnderTheKeyboardIsRefused(t *testing.T) {
	field := &uitree.Node{Focused: true, Displayed: true, Bounds: uitree.Rect{X1: 0, Y1: 300, X2: 1080, Y2: 400}}
	left := &uitree.Node{Displayed: true, TestID: "left", Bounds: uitree.Rect{X1: 30, Y1: 1000, X2: 150, Y2: 1100}}
	middle := &uitree.Node{Displayed: true, TestID: "middle", Bounds: uitree.Rect{X1: 400, Y1: 1000, X2: 700, Y2: 1100}}
	root := &uitree.Node{Children: []*uitree.Node{field, left, middle}, Bounds: uitree.Rect{X2: 1080, Y2: 2400}}
	for _, c := range root.Children {
		c.Parent = root
	}
	tree := &uitree.Tree{Root: root}
	d := &regionDriver{regions: []uitree.Rect{{X1: 21, Y1: 965, X2: 169, Y2: 1525}, {X1: 0, Y1: 2337, X2: 1080, Y2: 2400}}}
	h := NewHandlers()
	s := &session{dev: fakeDevice(), driver: d}

	err := h.keyboardOverTarget(context.Background(), s, left, tree, "testid=left")
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "keyboard") {
		t.Errorf("a target under the toolbar: %v", err)
	}
	if err := h.keyboardOverTarget(context.Background(), s, middle, tree, "testid=middle"); err != nil {
		t.Errorf("a target beside it was refused: %v", err)
	}
	field.Focused = false
	asked := d.asked
	if err := h.keyboardOverTarget(context.Background(), s, left, tree, "testid=left"); err != nil || d.asked != asked {
		t.Errorf("with nothing focused the device was asked (%d) or it refused: %v", d.asked-asked, err)
	}
}

// previewDriver reports Android's clipboard preview for its first `up`
// asks, then nothing, as the preview does when it closes by itself.
type previewDriver struct {
	fakeDriver
	regions []uitree.Rect
	up      int // how many asks still see it
	asked   int
}

func (p *previewDriver) ClipboardPreviewRegions(ctx context.Context) ([]uitree.Rect, error) {
	p.asked++
	if p.up == 0 {
		return nil, nil
	}
	p.up--
	return p.regions, nil
}

// A target under the clipboard's preview is waited on until the preview
// goes, and refused only if it outlasts its budget. The region is the
// one a Pixel 7 AVD reported after app_clipboard, and the target is where
// MobiumApp's Dialog Demo button sits on Home — the tap that opened Quick
// Share.
func TestAnAndroidTargetUnderTheClipboardPreviewWaits(t *testing.T) {
	preview := []uitree.Rect{{X1: 179, Y1: 1985, X2: 367, Y2: 2048}, {X1: -31, Y1: 2121, X2: 654, Y2: 2336}}
	under := &uitree.Node{Displayed: true, Bounds: uitree.Rect{X1: 0, Y1: 2160, X2: 1080, Y2: 2290}}
	above := &uitree.Node{Displayed: true, Bounds: uitree.Rect{X1: 0, Y1: 600, X2: 1080, Y2: 700}}
	h := NewHandlers()
	defer func(t time.Duration) { clipboardPreviewTimeout = t }(clipboardPreviewTimeout)
	clipboardPreviewTimeout = 2 * time.Second

	d := &previewDriver{regions: preview, up: 2}
	s := &session{dev: fakeDevice(), driver: d}
	if err := h.waitOutClipboardPreview(context.Background(), s, under, "label=Dialog Demo"); err != nil {
		t.Errorf("a preview that went was not waited out: %v", err)
	}
	if d.asked != 3 {
		t.Errorf("asked %d times, want 3: twice while it was up and once to see it gone", d.asked)
	}

	d = &previewDriver{regions: preview, up: 1}
	s.driver = d
	if err := h.waitOutClipboardPreview(context.Background(), s, above, "label=Top"); err != nil || d.asked != 1 {
		t.Errorf("a target clear of the preview waited (%d asks) or was refused: %v", d.asked, err)
	}

	d = &previewDriver{regions: preview, up: 1 << 30}
	s.driver = d
	clipboardPreviewTimeout = 400 * time.Millisecond
	err := h.waitOutClipboardPreview(context.Background(), s, under, "label=Dialog Demo")
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "clipboard preview") {
		t.Errorf("a preview that stayed: %v", err)
	}
}

// A real iPhone's clear-data refusal sends the caller to path, and the CLI's
// to --bundle: both must exist, or the remedy is obeyed and fails.
func TestTheClearDataRemedyNamesRealArguments(t *testing.T) {
	found := false
	for _, tool := range GetToolSchemas() {
		if tool.Name != "app_clear_data" {
			continue
		}
		found = true
		props := tool.InputSchema["properties"].(map[string]interface{})
		if _, ok := props["path"]; !ok {
			t.Error("app_clear_data no longer takes path, which a phone's refusal names")
		}
	}
	if !found {
		t.Fatal("no app_clear_data tool")
	}
	if !strings.Contains(string(mustRead(t, "../../docs/FLAGS.md")), "--bundle") {
		t.Error("the CLI no longer has --bundle, which a phone's refusal names")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The iOS refusal of back names app_press's gesture argument as the way to
// swipe back; a remedy naming an argument the tool lost would be obeyed and
// refused.
func TestTheBackRemedyNamesARealArgument(t *testing.T) {
	err := (&mobiumdriver.WDA{}).Press(context.Background(), mobiumdriver.ButtonBack)
	e, ok := mobiumerr.As(err)
	if !ok || !strings.Contains(e.Remedy, "app_press back with gesture") {
		t.Fatalf("the iOS back refusal does not name the gesture: %v", err)
	}
	for _, tool := range GetToolSchemas() {
		if tool.Name != "app_press" {
			continue
		}
		props := tool.InputSchema["properties"].(map[string]interface{})
		if _, ok := props["gesture"]; !ok {
			t.Error("app_press no longer takes gesture, which the remedy names")
		}
		return
	}
	t.Error("no app_press tool")
}

// CHALLENGES 294: Android's keyboard over an app's WebView shrank the page's
// viewport and not the WebView, and every tap with it up was refused. The
// frame is the WebView's while the keyboard reaches into it — asked only
// while something has focus.
func TestKeyboardOverWebViewIsReadFromItsRegion(t *testing.T) {
	host := uitree.Rect{X1: 0, Y1: 300, X2: 1008, Y2: 2100}
	tree := func(focus bool) *uitree.Tree {
		field := &uitree.Node{Focused: focus, Displayed: true, Bounds: uitree.Rect{X1: 40, Y1: 600, X2: 960, Y2: 700}}
		root := &uitree.Node{Children: []*uitree.Node{field}, Bounds: host}
		field.Parent = root
		return &uitree.Tree{Root: root}
	}
	focused, idle := tree(true), tree(false)
	for _, c := range []struct {
		name    string
		regions []uitree.Rect
		tree    *uitree.Tree
		want    bool
		asked   int
	}{
		{"the keyboard over the WebView", []uitree.Rect{{X1: 0, Y1: 1300, X2: 1008, Y2: 2244}}, focused, true, 1},
		{"the keyboard below it", []uitree.Rect{{X1: 0, Y1: 2150, X2: 1008, Y2: 2244}}, focused, false, 1},
		{"nothing focused, nothing asked", []uitree.Rect{{X1: 0, Y1: 1300, X2: 1008, Y2: 2244}}, idle, false, 0},
	} {
		d := &regionDriver{regions: c.regions}
		s := &session{driver: d}
		if got := keyboardOverWebView(context.Background(), s, c.tree, host); got != c.want || d.asked != c.asked {
			t.Errorf("%s: %v, asked %d times", c.name, got, d.asked)
		}
	}
}
