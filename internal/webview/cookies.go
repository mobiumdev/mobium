package webview

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Cookies and web storage, for app_cookies and app_storage.
//
// Cookies go through each transport's own cookie commands, because the
// page's document.cookie cannot see an HttpOnly one — which is what a session
// cookie usually is. Web storage goes through the page, the same script on
// both platforms, as Map and Text do: it is page state, and the page is the
// one place both transports agree on.
//
// What each transport answers was measured on 2026-09-27
// (cookie_probe_test.go): on Android, Chrome and an app's WebView both answer
// Network.getCookies, setCookie and deleteCookies; on iOS, Safari answers
// Page.getCookies, setCookie and deleteCookie. Two traps came out of it and
// are handled here. Storage.getCookies answers in Chrome with every cookie in
// the browser, other sites' included, so only the page-scoped
// Network.getCookies is used. And WebKit takes a cookie's expiry in
// milliseconds: given seconds, it accepts the cookie, answers {}, and stores
// it already expired — so every write is read back by the caller.

// Cookie is one cookie, the same on both platforms. The JSON keys are
// Playwright's and Vibium's rather than Mobium's usual snake_case, so a
// storage state saved by one tool can be restored by another.
type Cookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain,omitempty"`
	Path   string `json:"path,omitempty"`
	// Expires is seconds since the epoch; 0 is a session cookie, which ends
	// with the browser.
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	// SameSite is "Strict", "Lax" or "None"; empty leaves the browser's own.
	SameSite string `json:"sameSite,omitempty"`
}

// CookieJar is the cookie store as a page's transport reaches it: the
// cookies that would be sent to the page's URL.
type CookieJar interface {
	Cookies(ctx context.Context) ([]Cookie, error)
	SetCookie(ctx context.Context, c Cookie) error
	DeleteCookie(ctx context.Context, c Cookie) error
}

// pageURL is where the page is, which is what a cookie with no domain is
// set for.
func pageURL(ctx context.Context, e evaluator) (*url.URL, error) {
	raw, err := e.Evaluate(ctx, "location.href")
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"this page is at %q, which has no host a cookie can belong to — cookies need an http or https page; "+
				"navigate the WebView to one first", raw)
	}
	return u, nil
}

// cookieURL is a URL that a cookie would be sent to, for the transports that
// name a cookie by URL rather than by domain and path.
func cookieURL(c Cookie, page *url.URL) string {
	host := strings.TrimPrefix(c.Domain, ".")
	if host == "" {
		host = page.Host
	}
	path := c.Path
	if path == "" {
		path = "/"
	}
	scheme := page.Scheme
	if c.Secure {
		scheme = "https"
	}
	return scheme + "://" + host + path
}

// --- Android: the Chrome DevTools Protocol ------------------------------

type cdpCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	Session  bool    `json:"session"`
	SameSite string  `json:"sameSite"`
}

func (c cdpCookie) cookie() Cookie {
	out := Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
		HTTPOnly: c.HTTPOnly, Secure: c.Secure, SameSite: c.SameSite}
	// CDP marks a session cookie with -1 and session: true.
	if !c.Session && c.Expires > 0 {
		out.Expires = c.Expires
	}
	return out
}

// Cookies lists the cookies the page's URL would be sent, HttpOnly ones
// included.
//
// The URL is the page's own, from location.href, and named in the call.
// With none, Chrome answers for the frame's URL as it recorded it, which is
// not always where the page is: a page loaded as HTML with a base URL — an
// app's WebView given a string and an origin, MobiumApp's Web storage page —
// is recorded as about:blank, so its cookies, which the page itself could
// read, came back as none. CHALLENGES 168.
func (s *Session) Cookies(ctx context.Context) ([]Cookie, error) {
	params := map[string]interface{}{}
	if u, err := pageURL(ctx, s); err == nil {
		params["urls"] = []string{u.String()}
	}
	raw, err := s.call(ctx, "Network.getCookies", params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Cookies []cdpCookie `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "Network.getCookies answered %s: %w", raw, err)
	}
	out := make([]Cookie, 0, len(resp.Cookies))
	for _, c := range resp.Cookies {
		out = append(out, c.cookie())
	}
	return out, nil
}

// SetCookie sets one cookie; with no domain, for the page's own host.
func (s *Session) SetCookie(ctx context.Context, c Cookie) error {
	page, err := pageURL(ctx, s)
	if err != nil {
		return err
	}
	params := map[string]interface{}{"name": c.Name, "value": c.Value, "url": cookieURL(c, page),
		"httpOnly": c.HTTPOnly, "secure": c.Secure}
	if c.Domain != "" {
		params["domain"] = c.Domain
	}
	if c.Path != "" {
		params["path"] = c.Path
	}
	if c.SameSite != "" {
		params["sameSite"] = c.SameSite
	}
	if c.Expires > 0 {
		params["expires"] = c.Expires
	}
	raw, err := s.call(ctx, "Network.setCookie", params)
	if err != nil {
		return err
	}
	var resp struct {
		Success *bool `json:"success"`
	}
	if json.Unmarshal(raw, &resp) == nil && resp.Success != nil && !*resp.Success {
		return mobiumerr.New(mobiumerr.InvalidArgument, "the WebView refused cookie %q for %s", c.Name, params["url"])
	}
	return nil
}

// DeleteCookie deletes one cookie, named by its name, domain and path as
// Cookies reported them.
func (s *Session) DeleteCookie(ctx context.Context, c Cookie) error {
	params := map[string]interface{}{"name": c.Name}
	if c.Domain != "" {
		params["domain"] = c.Domain
	}
	if c.Path != "" {
		params["path"] = c.Path
	}
	if c.Domain == "" {
		page, err := pageURL(ctx, s)
		if err != nil {
			return err
		}
		params["url"] = cookieURL(c, page)
	}
	_, err := s.call(ctx, "Network.deleteCookies", params)
	return err
}

// --- iOS: WebKit's Remote Web Inspector ---------------------------------

type wkCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"` // milliseconds since the epoch
	Session  bool    `json:"session"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

// wkSameSite is WebKit's spelling of SameSite, which has no "unset": its
// setCookie wants one of the three, and a browser that is given none treats a
// cookie as Lax.
func wkSameSite(s string) string {
	switch strings.ToLower(s) {
	case "strict":
		return "Strict"
	case "none":
		return "None"
	}
	return "Lax"
}

// wkSeconds is a WebKit expiry, milliseconds, in seconds.
func wkSeconds(ms float64) float64 { return math.Round(ms) / 1000 }

// Cookies lists the cookies the page's URL would be sent, HttpOnly ones
// included.
func (s *IOSSession) Cookies(ctx context.Context) ([]Cookie, error) {
	raw, err := s.call(ctx, "Page.getCookies", map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Cookies []wkCookie `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "Page.getCookies answered %s: %w", raw, err)
	}
	out := make([]Cookie, 0, len(resp.Cookies))
	for _, c := range resp.Cookies {
		ck := Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
			HTTPOnly: c.HTTPOnly, Secure: c.Secure, SameSite: c.SameSite}
		if !c.Session && c.Expires > 0 {
			ck.Expires = wkSeconds(c.Expires)
		}
		out = append(out, ck)
	}
	return out, nil
}

// SetCookie sets one cookie; with no domain, for the page's own host.
func (s *IOSSession) SetCookie(ctx context.Context, c Cookie) error {
	page, err := pageURL(ctx, s)
	if err != nil {
		return err
	}
	domain, path := c.Domain, c.Path
	if domain == "" {
		domain = page.Hostname()
	}
	if path == "" {
		path = "/"
	}
	wk := map[string]any{"name": c.Name, "value": c.Value, "domain": domain, "path": path,
		"httpOnly": c.HTTPOnly, "secure": c.Secure, "sameSite": wkSameSite(c.SameSite),
		"session": c.Expires <= 0, "expires": float64(0)}
	if c.Expires > 0 {
		// Milliseconds. In seconds WebKit stores the cookie expired and says
		// nothing.
		wk["expires"] = c.Expires * 1000
	}
	_, err = s.call(ctx, "Page.setCookie", map[string]any{"cookie": wk})
	return err
}

// DeleteCookie deletes one cookie, named by its name and a URL it would be
// sent to — WebKit's form of the same question.
func (s *IOSSession) DeleteCookie(ctx context.Context, c Cookie) error {
	page, err := pageURL(ctx, s)
	if err != nil {
		return err
	}
	_, err = s.call(ctx, "Page.deleteCookie", map[string]any{"cookieName": c.Name, "url": cookieURL(c, page)})
	return err
}

// --- Web storage, through the page --------------------------------------

// StorageItem is one key of localStorage or sessionStorage.
type StorageItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// OriginStorage is one origin's web storage, in the shape Playwright and
// Vibium save it.
type OriginStorage struct {
	Origin         string        `json:"origin"`
	LocalStorage   []StorageItem `json:"localStorage"`
	SessionStorage []StorageItem `json:"sessionStorage"`
}

// storageScript reads the page's origin and both stores. Reading a store on
// a page with no origin of its own — about:blank, or an app's inline HTML —
// throws a SecurityError, which is answered as that rather than as an empty
// store.
const storageScript = `(() => {
  const dump = (s) => { const o = []; for (let i = 0; i < s.length; i++) { const k = s.key(i); o.push({name: k, value: s.getItem(k)}); } return o; };
  try {
    return JSON.stringify({origin: location.origin, localStorage: dump(localStorage), sessionStorage: dump(sessionStorage)});
  } catch (e) {
    return JSON.stringify({error: String(e), origin: location.origin, href: location.href});
  }
})()`

// ReadStorage reads the page's web storage.
func ReadStorage(ctx context.Context, e evaluator) (*OriginStorage, error) {
	raw, err := e.Evaluate(ctx, storageScript)
	if err != nil {
		return nil, err
	}
	var got struct {
		OriginStorage
		Error string `json:"error"`
		Href  string `json:"href"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the page answered the storage read with %q: %w", raw, err)
	}
	if got.Error != "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"this page (%s) cannot hold web storage: %s — an app's inline HTML and about:blank have no origin of "+
				"their own; navigate the WebView to an http or https page first", got.Href, got.Error)
	}
	if got.LocalStorage == nil {
		got.LocalStorage = []StorageItem{}
	}
	if got.SessionStorage == nil {
		got.SessionStorage = []StorageItem{}
	}
	return &got.OriginStorage, nil
}

// WriteStorage sets every item given, into the page's own stores. clear
// empties both stores first.
func WriteStorage(ctx context.Context, e evaluator, local, session []StorageItem, clear bool) error {
	data, err := json.Marshal(map[string]interface{}{"local": local, "session": session, "clear": clear})
	if err != nil {
		return err
	}
	script := fmt.Sprintf(`((d) => {
  try {
    if (d.clear) { localStorage.clear(); sessionStorage.clear(); }
    for (const i of d.local || []) localStorage.setItem(i.name, i.value);
    for (const i of d.session || []) sessionStorage.setItem(i.name, i.value);
    return "ok";
  } catch (e) { return String(e); }
})(%s)`, data)
	out, err := e.Evaluate(ctx, script)
	if err != nil {
		return err
	}
	if out != "ok" {
		return mobiumerr.New(mobiumerr.InvalidArgument, "the page refused the storage write: %s", out)
	}
	return nil
}
