package mobiumdriver

import (
	"bytes"
	"strings"
	"testing"
)

// The prefix is what a Fire TV (Fire OS 7.7.1.7) printed ahead of every
// capture, read off the TV with od on 2026-10-02.
func TestPNGInSkipsAVendorLine(t *testing.T) {
	png := append(append([]byte(nil), pngMagic...), "\x00\x00\x00\rIHDR"...)
	vendor := []byte("Init wrapper sys mutex successful. Pid:15573\n")
	cases := []struct {
		name string
		in   []byte
		ok   bool
	}{
		{"a plain capture", png, true},
		{"the Fire TV's line first", append(append([]byte(nil), vendor...), png...), true},
		{"no PNG at all", []byte("error: something\n"), false},
		// Not a line of text: a corrupt capture, refused rather than
		// searched for a signature in its middle.
		{"binary ahead of it", append([]byte("\x00\x01garbage\n"), png...), false},
		{"text not ending a line", append([]byte("no newline"), png...), false},
		{"a prefix too long to be a vendor line", append([]byte(strings.Repeat("x", maxVendorPrefix)+"\n"), png...), false},
	}
	for _, c := range cases {
		got, ok := pngIn(c.in)
		if ok != c.ok {
			t.Errorf("%s: ok=%v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && !bytes.Equal(got, png) {
			t.Errorf("%s: got %q, want the PNG alone", c.name, got)
		}
	}
}
