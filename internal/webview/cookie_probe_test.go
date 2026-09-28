//go:build probe

package webview

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
)

// Which cookie and storage commands each transport answers, measured
// 2026-09-27 on an Android 15 emulator (Chrome and MobiumApp's WebView, each
// on https://example.com) and an iPhone 17 Pro simulator (Safari). Kept, as
// docs/probes are, so the measurement can be repeated; it needs those pages
// open, and the build tag keeps it out of every ordinary run:
//
//	go test -tags probe ./internal/webview/ -run Probe -v
//
// What it found is in docs/ROADMAP.md, "Cookies and web storage".

func show(label string, raw []byte, err error) {
	s := string(raw)
	if len(s) > 220 {
		s = s[:220] + "…"
	}
	fmt.Printf("  %-44s err=%v  %s\n", label, err, s)
}

func TestProbeCookiesAndroid(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	adb, _, err := device.Select(ctx, "emulator-5554")
	if err != nil {
		t.Fatal(err)
	}
	cs, err := Contexts(ctx, adb)
	if err != nil {
		t.Fatal(err)
	}
	var c *Context
	for i := range cs {
		if strings.HasPrefix(cs[i].URL, "https://example.com/?inapp") {
			c = &cs[i]
			break
		}
	}
	if c == nil {
		t.Fatalf("no https://example.com page among %d contexts", len(cs))
	}
	fmt.Println("android page:", c.ID, c.URL)
	s, err := Attach(ctx, adb, *c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, m := range []struct {
		method string
		params map[string]interface{}
	}{
		{"Network.getCookies", map[string]interface{}{}},
		{"Network.setCookie", map[string]interface{}{"name": "mobium_probe", "value": "1", "url": "https://example.com/", "httpOnly": true}},
		{"Network.getCookies", map[string]interface{}{}},
		{"Storage.getCookies", map[string]interface{}{}},
		{"Network.deleteCookies", map[string]interface{}{"name": "mobium_probe", "url": "https://example.com/"}},
		{"Network.getCookies", map[string]interface{}{}},
		{"DOMStorage.enable", map[string]interface{}{}},
		{"DOMStorage.getDOMStorageItems", map[string]interface{}{"storageId": map[string]interface{}{"securityOrigin": "https://example.com", "isLocalStorage": true}}},
		{"Storage.clearDataForOrigin", map[string]interface{}{"origin": "https://example.com", "storageTypes": "local_storage"}},
	} {
		raw, err := s.call(ctx, m.method, m.params)
		show(m.method, raw, err)
	}
	v, err := s.Evaluate(ctx, "document.cookie")
	fmt.Printf("  document.cookie after (HttpOnly hidden): %q %v\n", v, err)
}

func errorsJoin(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func TestProbeCleanIOS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sim, _ := device.NewSimctl("457C7DC2-C706-45D9-8D68-1D26953E28B1")
	insp, err := OpenInspector(ctx, sim)
	if err != nil {
		t.Fatal(err)
	}
	defer insp.Close()
	cs, _ := insp.Contexts(ctx)
	for _, c := range cs {
		if !strings.HasPrefix(c.URL, "https://example.com") {
			continue
		}
		s, err := AttachIOS(ctx, insp, c)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"from_page", "via_example.com", "mobium_probe"} {
			_, _ = s.call(ctx, "Page.deleteCookie", map[string]any{"cookieName": n, "url": "https://example.com/"})
		}
		raw, err := s.call(ctx, "Page.getCookies", map[string]any{})
		show("after cleanup", raw, err)
		s.Close()
		return
	}
}

func TestProbeCookiesIOS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sim, err := device.NewSimctl("457C7DC2-C706-45D9-8D68-1D26953E28B1")
	if err != nil {
		t.Fatal(err)
	}
	insp, err := OpenInspector(ctx, sim)
	if err != nil {
		t.Fatal(err)
	}
	defer insp.Close()
	cs, err := insp.Contexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var c *Context
	for i := range cs {
		if strings.HasPrefix(cs[i].URL, "https://example.com") {
			c = &cs[i]
			break
		}
	}
	if c == nil {
		t.Fatalf("no https://example.com page among %d contexts", len(cs))
	}
	fmt.Println("ios page:", c.ID, c.URL)
	s, err := AttachIOS(ctx, insp, *c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.Evaluate(ctx, `(document.cookie = "from_page=1; path=/", document.cookie)`)
	fmt.Printf("  control: document.cookie set from the page -> %q %v\n", v, err)
	raw, err := s.call(ctx, "Page.getCookies", map[string]any{})
	show("Page.getCookies after the page set one", raw, err)
	for _, dom := range []string{"example.com", ".example.com"} {
		_, err := s.call(ctx, "Page.setCookie", map[string]any{"cookie": map[string]any{"name": "via_" + strings.TrimPrefix(dom, "."), "value": "1", "domain": dom, "path": "/", "expires": float64(time.Now().Add(time.Hour).UnixMilli()), "httpOnly": false, "secure": false, "session": false, "sameSite": "None"}})
		raw, gerr := s.call(ctx, "Page.getCookies", map[string]any{})
		show("setCookie domain="+dom+" (ms expiry) then get", raw, errorsJoin(err, gerr))
	}
	for _, m := range []struct {
		method string
		params map[string]any
	}{
		{"Page.getCookies", map[string]any{}},
		{"Page.setCookie", map[string]any{"cookie": map[string]any{"name": "mobium_probe", "value": "1", "domain": "example.com", "path": "/", "expires": float64(time.Now().Add(time.Hour).Unix()), "httpOnly": true, "secure": true, "session": false, "sameSite": "Lax"}}},
		{"Page.getCookies", map[string]any{}},
		{"Page.deleteCookie", map[string]any{"cookieName": "mobium_probe", "url": "https://example.com/"}},
		{"Page.getCookies", map[string]any{}},
		{"DOMStorage.enable", map[string]any{}},
		{"DOMStorage.getDOMStorageItems", map[string]any{"storageId": map[string]any{"securityOrigin": "https://example.com", "isLocalStorage": true}}},
	} {
		raw, err := s.call(ctx, m.method, m.params)
		show(m.method, raw, err)
	}
}
