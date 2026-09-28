package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/webview"
)

// jarPage is a web page with a cookie store and web storage in memory. With
// expireOnSet it behaves as WebKit does given an expiry in seconds: the set
// is accepted, and the cookie is not there afterwards.
type jarPage struct {
	webview.Page
	origin      string
	jar         []webview.Cookie
	local       map[string]string
	session     map[string]string
	expireOnSet bool
}

func (p *jarPage) Evaluate(ctx context.Context, expr string) (string, error) {
	switch {
	case expr == "location.href":
		return p.origin + "/", nil
	case strings.Contains(expr, "JSON.stringify({origin"):
		return storageJSON(p), nil
	case strings.Contains(expr, "setItem"):
		if strings.Contains(expr, `"clear":true`) {
			p.local, p.session = map[string]string{}, map[string]string{}
		}
		for _, kv := range between(expr, `"local":[`, `]`) {
			p.local[kv[0]] = kv[1]
		}
		return "ok", nil
	}
	return "", errors.New("unexpected script")
}

func (p *jarPage) Cookies(ctx context.Context) ([]webview.Cookie, error) {
	return append([]webview.Cookie(nil), p.jar...), nil
}
func (p *jarPage) SetCookie(ctx context.Context, c webview.Cookie) error {
	if !p.expireOnSet {
		p.jar = append(p.jar, c)
	}
	return nil
}
func (p *jarPage) DeleteCookie(ctx context.Context, c webview.Cookie) error {
	var keep []webview.Cookie
	for _, k := range p.jar {
		if k.Name != c.Name {
			keep = append(keep, k)
		}
	}
	p.jar = keep
	return nil
}

func storageJSON(p *jarPage) string {
	var b strings.Builder
	b.WriteString(`{"origin":"` + p.origin + `","localStorage":[`)
	first := true
	for k, v := range p.local {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(`{"name":"` + k + `","value":"` + v + `"}`)
	}
	b.WriteString(`],"sessionStorage":[]}`)
	return b.String()
}

// between reads {"name":"k","value":"v"} pairs out of the write script's
// data, enough for these tests.
func between(s, start, end string) [][2]string {
	i := strings.Index(s, start)
	if i < 0 {
		return nil
	}
	s = s[i+len(start):]
	s = s[:strings.Index(s, end)]
	var out [][2]string
	for _, part := range strings.Split(s, "},") {
		n := strings.Split(part, `"name":"`)
		v := strings.Split(part, `"value":"`)
		if len(n) > 1 && len(v) > 1 {
			out = append(out, [2]string{n[1][:strings.Index(n[1], `"`)], v[1][:strings.Index(v[1], `"`)]})
		}
	}
	return out
}

func pageSession(p *jarPage) (*Handlers, *session) {
	h := NewHandlers()
	s := &session{dev: &device.Device{Serial: "SIM-1"}, driver: &fakeDriver{}, backend: BackendWDA, web: p, webCtx: "WEBVIEW_x"}
	h.sessions["SIM-1"] = s
	return h, s
}

func TestCookiesAreSetAndReadBack(t *testing.T) {
	p := &jarPage{origin: "https://example.com", local: map[string]string{}, session: map[string]string{}}
	h, _ := pageSession(p)
	res, err := h.cookiesTool(context.Background(), map[string]interface{}{"device": "SIM-1", "action": "set",
		"cookies": []interface{}{map[string]interface{}{"name": "session", "value": "abc", "httpOnly": true}}})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(CookiesView)
	if len(v.Cookies) != 1 || v.Cookies[0].Name != "session" || !v.Cookies[0].HTTPOnly {
		t.Errorf("after set, cookies = %+v", v.Cookies)
	}
}

// WebKit, given an expiry in seconds, accepts the cookie and stores it
// already expired. The set is read back, so that is a failure, not a success.
func TestACookieTheBrowserDidNotKeepIsNotReportedSet(t *testing.T) {
	p := &jarPage{origin: "https://example.com", expireOnSet: true, local: map[string]string{}, session: map[string]string{}}
	h, _ := pageSession(p)
	_, err := h.cookiesTool(context.Background(), map[string]interface{}{"device": "SIM-1", "action": "set",
		"cookies": []interface{}{map[string]interface{}{"name": "session", "value": "abc", "expires": 1.9e9}}})
	if mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed {
		t.Fatalf("an unkept cookie answered %v, want not_confirmed", err)
	}
}

func TestClearingCookiesByName(t *testing.T) {
	p := &jarPage{origin: "https://example.com", jar: []webview.Cookie{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}},
		local: map[string]string{}, session: map[string]string{}}
	h, _ := pageSession(p)
	res, err := h.cookiesTool(context.Background(), map[string]interface{}{"device": "SIM-1", "action": "clear", "name": "a"})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(CookiesView)
	if v.Cleared != 1 || len(v.Cookies) != 1 || v.Cookies[0].Name != "b" {
		t.Errorf("clear a: cleared %d, left %+v", v.Cleared, v.Cookies)
	}
}

// Vibium's restore wrote every origin's storage into whatever page was open.
// Here one origin's storage goes only into a page on that origin, and the
// rest are named as skipped.
func TestRestoreWritesOnlyThisOriginsStorage(t *testing.T) {
	p := &jarPage{origin: "https://example.com", local: map[string]string{}, session: map[string]string{}}
	h, _ := pageSession(p)
	state := map[string]interface{}{
		"cookies": []interface{}{map[string]interface{}{"name": "c", "value": "1"}},
		"origins": []interface{}{
			map[string]interface{}{"origin": "https://example.com", "localStorage": []interface{}{map[string]interface{}{"name": "k", "value": "v"}}, "sessionStorage": []interface{}{}},
			map[string]interface{}{"origin": "https://other.example", "localStorage": []interface{}{map[string]interface{}{"name": "x", "value": "y"}}, "sessionStorage": []interface{}{}},
		},
	}
	res, err := h.storageTool(context.Background(), map[string]interface{}{"device": "SIM-1", "action": "restore", "state": state})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(StorageView)
	if p.local["k"] != "v" || p.local["x"] != "" {
		t.Errorf("local storage = %v, want only this origin's k=v", p.local)
	}
	if len(v.Skipped) != 1 || v.Skipped[0] != "https://other.example" {
		t.Errorf("skipped = %v, want the other origin", v.Skipped)
	}
	if !strings.Contains(res.Content[0].Text, "not the storage of https://other.example") {
		t.Errorf("the result does not say what it skipped: %q", res.Content[0].Text)
	}
}

func TestCookiesAndStorageNeedAWebContext(t *testing.T) {
	h := NewHandlers()
	h.sessions["SIM-1"] = &session{dev: &device.Device{Serial: "SIM-1"}, driver: &fakeDriver{}, backend: BackendWDA}
	for _, tool := range []func(context.Context, map[string]interface{}) (*ToolsCallResult, error){h.cookiesTool, h.storageTool} {
		if _, err := tool(context.Background(), map[string]interface{}{"device": "SIM-1"}); mobiumerr.CodeOf(err) != mobiumerr.NoSuchContext {
			t.Errorf("on the native shell: %v, want no_such_context", err)
		}
	}
}
