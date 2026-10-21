package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// siriScreen is SpringBoard's tree while Siri answers, cut down to what was
// measured on the iPhone 15 Plus, iOS 27.0.1: a full-screen window named
// siri-interface-window holding Siri's card, and no app.
const siriScreen = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<XCUIElementTypeApplication type="XCUIElementTypeApplication" name=" " label=" " enabled="true" visible="true" ` +
	`accessible="false" x="0" y="0" width="430" height="932" bundleId="com.apple.springboard">` +
	`<XCUIElementTypeWindow type="XCUIElementTypeWindow" name="siri-interface-window" enabled="true" visible="true" ` +
	`accessible="false" x="0" y="0" width="430" height="932">` +
	`<XCUIElementTypeOther type="XCUIElementTypeOther" name="SiriUICardViewAccessibilityIdentifier" enabled="true" ` +
	`visible="true" accessible="false" x="8" y="60" width="414" height="120">` +
	`<XCUIElementTypeButton type="XCUIElementTypeButton" name="Current Time, 6:40 PM" label="Current Time, 6:40 PM" ` +
	`enabled="true" visible="true" accessible="true" x="8" y="60" width="414" height="120"/>` +
	`</XCUIElementTypeOther></XCUIElementTypeWindow></XCUIElementTypeApplication>`

// appScreen is an app's own screen with the button the tap is for.
const appScreen = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="MobiumApp" label="MobiumApp" enabled="true" ` +
	`visible="true" accessible="false" x="0" y="0" width="430" height="932" bundleId="dev.mobium.mobiumapp">` +
	`<XCUIElementTypeWindow type="XCUIElementTypeWindow" enabled="true" visible="true" accessible="false" x="0" y="0" ` +
	`width="430" height="932">` +
	`<XCUIElementTypeButton type="XCUIElementTypeButton" name="Pro" label="Pro" enabled="true" visible="true" ` +
	`accessible="true" x="20" y="300" width="390" height="44"/>` +
	`</XCUIElementTypeWindow></XCUIElementTypeApplication>`

func parseIOS(t *testing.T, xml string) *uitree.Tree {
	t.Helper()
	tree, err := uitree.ParseIOS([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestSiriPanelIsSpringBoardsSiriWindow(t *testing.T) {
	if !uitree.SiriPanel(parseIOS(t, siriScreen)) {
		t.Error("Siri's window was not recognized")
	}
	// The negative control: an app's own screen has no such window.
	if uitree.SiriPanel(parseIOS(t, appScreen)) {
		t.Error("an app's own screen was taken for Siri's panel")
	}
}

// A tap made while Siri's panel is up waits for it to go and lands on the
// app, where it failed after eight seconds saying only that nothing matched.
func TestATapBehindSiriWaitsForThePanelToGo(t *testing.T) {
	siri, app := parseIOS(t, siriScreen), parseIOS(t, appScreen)
	screens := []*uitree.Tree{siri, siri, siri, siri, siri, app}
	h, s, d := withFake(t, screens...)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if _, err := h.tapOn(ctx, s, map[string]interface{}{"target": "label=Pro"}); err != nil {
		t.Fatalf("tap behind Siri's panel: %v", err)
	}
	if len(d.tapped) != 1 {
		t.Errorf("sent %d taps, want one, on the app once Siri had gone", len(d.tapped))
	}
}

// A panel that does not go is refused naming Siri, never scrolled for.
func TestATapBehindSiriThatStaysIsRefusedNamingSiri(t *testing.T) {
	defer func(d time.Duration) { siriPanelTimeout = d }(siriPanelTimeout)
	siriPanelTimeout = 50 * time.Millisecond
	h, s, d := withFake(t, parseIOS(t, siriScreen))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	_, err := h.tapOn(ctx, s, map[string]interface{}{"target": "label=Pro"})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "Siri's panel") ||
		!strings.Contains(err.Error(), "still there after") {
		t.Fatalf("tap with Siri staying answered %v, want device_not_ready naming Siri's panel and the wait", err)
	}
	// A panel that outlasted the wait will not go — on a simulator it never
	// does — so the remedy is Home, never waiting again. CHALLENGES 351.
	if text := mobiumerr.Text(err); !strings.Contains(text, "app_press home") || strings.Contains(text, "next: wait") {
		t.Errorf("the remedy for a panel that stays is not Home: %s", text)
	}
	if len(d.tapped) != 0 {
		t.Errorf("sent %d taps with Siri's panel in front", len(d.tapped))
	}
}
