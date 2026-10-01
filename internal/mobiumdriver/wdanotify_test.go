package mobiumdriver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

func springboardFixture(t *testing.T, name string) (*uitree.Tree, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "uitree", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	return tree, string(raw)
}

// Captured on an iPhone 17 Pro simulator: a banner over Settings, and
// Notification Center open. Each holds MobiumApp's notification, named by
// its display name in capitals, with a title that has a comma in it — read
// from its own element, since the label joins everything with commas.
func TestNotificationsAreReadFromABannerAndFromNotificationCenter(t *testing.T) {
	names := map[string]string{"mobiumapp": "dev.mobium.mobiumapp"}
	for _, c := range []struct {
		file, title, text string
		open              bool
	}{
		{"ios26-notification-banner.xml", "Banner, with a comma", "Banner body 3307", false},
		{"ios26-notification-center.xml", "Banner, with a comma", "Banner body 3307", true},
	} {
		tree, raw := springboardFixture(t, c.file)
		got := notificationsIn(tree, names)
		if len(got) != 1 {
			t.Fatalf("%s: %d notifications, want 1: %+v", c.file, len(got), got)
		}
		if n := got[0]; n.Package != "dev.mobium.mobiumapp" || n.Title != c.title || n.Text != c.text {
			t.Errorf("%s: read %+v", c.file, n)
		}
		if open := strings.Contains(raw, `name="`+coverSheet+`"`); open != c.open {
			t.Errorf("%s: Notification Center read as open=%v", c.file, open)
		}
	}
}

func TestANotificationFromAnUnknownAppKeepsItsName(t *testing.T) {
	if got := notificationApp("SOMEAPP, now, Hi, There", map[string]string{}); got != "SOMEAPP" {
		t.Errorf("got %q", got)
	}
}

// A group expanded in Notification Center holds one notification view each,
// eight here, and every one is read — the first version stopped at the
// first, since Walk ends altogether on false.
func TestAnExpandedGroupIsReadInFull(t *testing.T) {
	tree, _ := springboardFixture(t, "ios26-notification-center-group.xml")
	got := notificationsIn(tree, map[string]string{"mobiumapp": "dev.mobium.mobiumapp"})
	if len(got) != 8 {
		t.Fatalf("read %d notifications, want the 8 in the group", len(got))
	}
	if got[3].Title != "Probe title" || got[3].Text != "Probe body 4711" {
		t.Errorf("the fourth is %+v", got[3])
	}
}
