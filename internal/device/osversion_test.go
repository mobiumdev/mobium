package device

import "testing"

func TestOSVersion(t *testing.T) {
	for in, want := range map[string]string{
		"15\n35\n":    "Android 15 (API 35)",
		"9\r\n28\r\n": "Android 9 (API 28)",
		"":            "",
		"15\n":        "",
	} {
		if got := osVersion(in); got != want {
			t.Errorf("osVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
