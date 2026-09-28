package mobiumdriver

import (
	"os"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// SpringBoard's hierarchy as WebDriverAgent returned it on the iPhone 17 Pro
// simulator, iOS 26.5, two seconds after MobiumApp posted a notification
// while in front — the code replaced with a fixed one.
func TestBannerOverNamesTheAppUnderneath(t *testing.T) {
	raw, err := os.ReadFile("testdata/springboard-banner-ios26.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	app, banner := bannerOver(tree)
	if app != "dev.mobium.mobiumapp" {
		t.Errorf("app = %q, want MobiumApp", app)
	}
	if banner == nil || !strings.Contains(banner.Label, "Your MobiumApp code is 123456") {
		t.Errorf("banner = %+v, want the notification's text in its label", banner)
	}
	if !uitree.IsControl(banner) {
		t.Error("the banner is not a control, so nothing would aim around it")
	}
}

// The home screen, or SpringBoard with no banner, is SpringBoard, and is
// read as it is.
func TestBannerOverLeavesSpringBoardAlone(t *testing.T) {
	home := `<?xml version="1.0" encoding="UTF-8"?><XCUIElementTypeApplication type="XCUIElementTypeApplication" name=" " bundleId="com.apple.springboard" x="0" y="0" width="402" height="874" visible="true" enabled="true"><XCUIElementTypeIcon type="XCUIElementTypeIcon" name="Settings" label="Settings" x="20" y="100" width="60" height="60" visible="true" enabled="true"/></XCUIElementTypeApplication>`
	tree, err := uitree.ParseIOS([]byte(home))
	if err != nil {
		t.Fatal(err)
	}
	if app, _ := bannerOver(tree); app != "" {
		t.Errorf("the home screen was read as a banner over %q", app)
	}
}
