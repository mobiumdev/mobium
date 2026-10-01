package device

import "testing"

// The values measured on 2026-10-01: 2 on the Pixel 8 Pro and both AVDs,
// 0 on the API 35 AVD with the three-button overlay enabled.
func TestNavigationMode(t *testing.T) {
	for in, want := range map[string]string{
		"2\n": NavGestures, "0\r\n": NavThreeButton, "1": NavTwoButton,
		"null\n": "", "": "", "3": "",
	} {
		if got := navigationMode(in); got != want {
			t.Errorf("navigation_mode %q read as %q, want %q", in, got, want)
		}
	}
}
