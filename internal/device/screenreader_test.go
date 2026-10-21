package device

import "testing"

func TestScreenReaderServiceList(t *testing.T) {
	const menu = "com.android.systemui.accessibility.accessibilitymenu/com.android.systemui.accessibility.accessibilitymenu.AccessibilityMenuService"
	cases := []struct {
		was  string
		on   bool
		want string
	}{
		{"null", true, talkBackService},
		{"", true, talkBackService},
		{menu, true, menu + ":" + talkBackService},
		{menu + ":" + talkBackService, true, menu + ":" + talkBackService},
		{menu + ":" + talkBackService, false, menu},
		{talkBackService, false, ""},
		{"null", false, ""},
	}
	for _, c := range cases {
		if got := withService(c.was, talkBackService, c.on); got != c.want {
			t.Errorf("withService(%q, on=%v) = %q, want %q", c.was, c.on, got, c.want)
		}
	}
	if !hasService(menu+":"+talkBackService, talkBackService) || hasService(menu, talkBackService) || hasService("null", talkBackService) {
		t.Error("hasService misread a list")
	}
}
