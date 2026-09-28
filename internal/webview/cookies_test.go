package webview

import (
	"net/url"
	"testing"
)

func TestCookieConversions(t *testing.T) {
	// CDP marks a session cookie with -1; Mobium, with 0.
	if got := (cdpCookie{Name: "a", Expires: -1, Session: true}).cookie(); got.Expires != 0 {
		t.Errorf("a CDP session cookie expires %v, want 0", got.Expires)
	}
	if got := (cdpCookie{Name: "a", Expires: 1.9e9}).cookie(); got.Expires != 1.9e9 {
		t.Errorf("a CDP cookie's expiry changed to %v", got.Expires)
	}
	// WebKit has no unset SameSite; a browser given none treats it as Lax.
	for in, want := range map[string]string{"": "Lax", "lax": "Lax", "STRICT": "Strict", "None": "None"} {
		if got := wkSameSite(in); got != want {
			t.Errorf("wkSameSite(%q) = %q, want %q", in, got, want)
		}
	}
	if got := wkSeconds(1790555306123); got != 1790555306.123 {
		t.Errorf("wkSeconds = %v, want 1790555306.123", got)
	}
	page, _ := url.Parse("http://example.com/a/b")
	for _, c := range []struct {
		cookie Cookie
		want   string
	}{
		{Cookie{Name: "a"}, "http://example.com/"},
		{Cookie{Name: "a", Domain: ".example.com", Path: "/x", Secure: true}, "https://example.com/x"},
	} {
		if got := cookieURL(c.cookie, page); got != c.want {
			t.Errorf("cookieURL(%+v) = %q, want %q", c.cookie, got, c.want)
		}
	}
}
