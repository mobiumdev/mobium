package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/webview"
)

// app_cookies and app_storage: Vibium's cookies and storage commands, carried
// onto a WebView. The command set and the saved-state shape are Vibium's, so a
// state saved by one restores in the other; underneath, it
// is each platform's own inspector protocol for cookies and the page itself
// for web storage (internal/webview/cookies.go says why each).
//
// Both act on the current web context, as app_eval does: the cookies a page
// is sent are a property of its URL, and web storage belongs to its origin.
// An Android app's WebViews share one cookie store, so a cookie set here is
// set for every WebView in the app, and a PWA on Android shares Chrome's.
//
// Every write is read back. WebKit accepts a cookie it then stores expired
// and says nothing, and a tool that reported that as set would be reporting
// what it asked for.

// CookiesView is what app_cookies answers.
type CookiesView struct {
	Action  string           `json:"action"`
	Cookies []webview.Cookie `json:"cookies"`
	// Cleared counts the cookies clear deleted.
	Cleared int    `json:"cleared,omitempty"`
	URL     string `json:"url"`
	Context string `json:"context"`
	Device  string `json:"device"`
}

// StorageState is a page's cookies and web storage, in the shape Vibium
// saves: {cookies, origins: [{origin, localStorage, sessionStorage}]}.
type StorageState struct {
	Cookies []webview.Cookie        `json:"cookies"`
	Origins []webview.OriginStorage `json:"origins"`
}

// StorageView is what app_storage answers.
type StorageView struct {
	Action string       `json:"action"`
	State  StorageState `json:"state"`
	// Skipped names the origins restore did not write, because the page is
	// on another one.
	Skipped []string `json:"skipped,omitempty"`
	Context string   `json:"context"`
	Device  string   `json:"device"`
}

// webPage is the attached page for a tool that needs one, or the refusal
// that says how to get one.
func (h *Handlers) webPage(ctx context.Context, args map[string]interface{}, tool string) (*session, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	if s.web == nil {
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "%s reads a page's cookies and storage, so it needs a "+
			"WebView — this session is on the native shell. Switch with app_context first", tool)
	}
	return s, nil
}

func (h *Handlers) cookiesTool(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.webPage(ctx, args, "app_cookies")
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action")))
	if action == "" {
		action = "get"
	}
	href, _ := s.web.Evaluate(ctx, "location.href")
	view := CookiesView{Action: action, URL: href, Context: s.webCtx, Device: s.dev.Serial}

	switch action {
	case "get":
	case "set":
		want, err := cookiesArg(args["cookies"])
		if err != nil {
			return nil, err
		}
		if err := setCookies(ctx, s.web, want); err != nil {
			return nil, err
		}
	case "clear":
		n, err := clearCookies(ctx, s.web, stringArg(args, "name"))
		if err != nil {
			return nil, err
		}
		view.Cleared = n
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown action %q (want \"get\", \"set\" or \"clear\")", action)
	}

	got, err := s.web.Cookies(ctx)
	if err != nil {
		return nil, err
	}
	view.Cookies = got
	if view.Cookies == nil {
		view.Cookies = []webview.Cookie{}
	}
	return Result(cookiesText(view), view), nil
}

// setCookies sets each cookie and confirms each by reading the store back.
func setCookies(ctx context.Context, jar webview.CookieJar, want []webview.Cookie) error {
	for _, c := range want {
		if err := jar.SetCookie(ctx, c); err != nil {
			return err
		}
	}
	got, err := jar.Cookies(ctx)
	if err != nil {
		return err
	}
	for _, c := range want {
		found := false
		for _, g := range got {
			if g.Name == c.Name && g.Value == c.Value {
				found = true
				break
			}
		}
		if !found {
			hint := ""
			if c.Expires > 0 {
				hint = " — expires is seconds since the epoch, and a time already past stores a cookie expired"
			}
			return mobiumerr.New(mobiumerr.NotConfirmed,
				"cookie %q was accepted and is not in the page's cookies when read back%s; a domain or path "+
					"the page's URL does not match is the other usual cause", c.Name, hint)
		}
	}
	return nil
}

// clearCookies deletes the page's cookies, or those called name, and
// confirms they are gone.
func clearCookies(ctx context.Context, jar webview.CookieJar, name string) (int, error) {
	got, err := jar.Cookies(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range got {
		if name != "" && c.Name != name {
			continue
		}
		if err := jar.DeleteCookie(ctx, c); err != nil {
			return n, err
		}
		n++
	}
	left, err := jar.Cookies(ctx)
	if err != nil {
		return n, err
	}
	for _, c := range left {
		if name == "" || c.Name == name {
			return n, mobiumerr.New(mobiumerr.NotConfirmed, "cookie %q is still there after it was deleted", c.Name)
		}
	}
	return n, nil
}

func cookiesText(v CookiesView) string {
	var lines []string
	switch v.Action {
	case "set":
		lines = append(lines, "set, and read back")
	case "clear":
		lines = append(lines, fmt.Sprintf("cleared %d cookie(s)", v.Cleared))
	}
	if len(v.Cookies) == 0 {
		lines = append(lines, "no cookies for "+v.URL)
	}
	for _, c := range v.Cookies {
		var flags []string
		if c.HTTPOnly {
			flags = append(flags, "HttpOnly")
		}
		if c.Secure {
			flags = append(flags, "Secure")
		}
		if c.SameSite != "" {
			flags = append(flags, "SameSite="+c.SameSite)
		}
		if c.Expires == 0 {
			flags = append(flags, "session")
		}
		lines = append(lines, fmt.Sprintf("%s=%s (%s%s) %s", c.Name, c.Value, c.Domain, c.Path, strings.Join(flags, " ")))
	}
	return strings.Join(lines, "\n")
}

// cookiesArg reads the cookies to set: a list of objects with at least a
// name and a value, as Vibium's setCookies takes them.
func cookiesArg(v interface{}) ([]webview.Cookie, error) {
	if v == nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"set needs cookies: a list of {name, value} objects, with domain, path, expires, httpOnly, secure and sameSite optional")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "cookies could not be read: %v", err)
	}
	var out []webview.Cookie
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "cookies must be a list of {name, value} objects: %v", err)
	}
	if len(out) == 0 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "set needs at least one cookie")
	}
	for _, c := range out {
		if c.Name == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "every cookie needs a name")
		}
		switch strings.ToLower(c.SameSite) {
		case "", "strict", "lax", "none":
		default:
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "sameSite is \"Strict\", \"Lax\" or \"None\", not %q", c.SameSite)
		}
	}
	return out, nil
}

func (h *Handlers) storageTool(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.webPage(ctx, args, "app_storage")
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action")))
	if action == "" {
		action = "get"
	}
	view := StorageView{Action: action, Context: s.webCtx, Device: s.dev.Serial}
	var text string

	switch action {
	case "get":
	case "restore":
		state, err := stateArg(args["state"])
		if err != nil {
			return nil, err
		}
		here, err := webview.ReadStorage(ctx, s.web)
		if err != nil {
			return nil, err
		}
		if len(state.Cookies) > 0 {
			if err := setCookies(ctx, s.web, state.Cookies); err != nil {
				return nil, err
			}
		}
		for _, o := range state.Origins {
			// Written into this page only if it is this page's origin.
			// Anything else would put one site's storage into another's.
			if o.Origin != here.Origin {
				view.Skipped = append(view.Skipped, o.Origin)
				continue
			}
			if err := webview.WriteStorage(ctx, s.web, o.LocalStorage, o.SessionStorage, false); err != nil {
				return nil, err
			}
			if err := storageHas(ctx, s.web, o); err != nil {
				return nil, err
			}
		}
		text = fmt.Sprintf("restored %d cookie(s)", len(state.Cookies))
		if n := len(state.Origins) - len(view.Skipped); n > 0 {
			text += fmt.Sprintf(" and %s's storage", here.Origin)
		}
		if len(view.Skipped) > 0 {
			text += fmt.Sprintf("; not the storage of %s — the page is on %s, and one origin's storage is written "+
				"only into a page on that origin: open one there and restore again", strings.Join(view.Skipped, ", "), here.Origin)
		}
	case "clear":
		n, err := clearCookies(ctx, s.web, "")
		if err != nil {
			return nil, err
		}
		if err := webview.WriteStorage(ctx, s.web, nil, nil, true); err != nil {
			return nil, err
		}
		text = fmt.Sprintf("cleared %d cookie(s), localStorage and sessionStorage", n)
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown action %q (want \"get\", \"restore\" or \"clear\")", action)
	}

	state, err := readState(ctx, s.web)
	if err != nil {
		return nil, err
	}
	view.State = *state
	if action == "clear" {
		o := state.Origins[0]
		if len(state.Cookies) > 0 || len(o.LocalStorage) > 0 || len(o.SessionStorage) > 0 {
			return nil, mobiumerr.New(mobiumerr.NotConfirmed, "the page still has storage after it was cleared")
		}
	}
	data, _ := json.MarshalIndent(view.State, "", "  ")
	if text == "" {
		text = string(data)
	} else {
		text += "\n" + string(data)
	}
	return Result(text, view), nil
}

// readState is the page's whole storage state.
func readState(ctx context.Context, page webview.Page) (*StorageState, error) {
	cookies, err := page.Cookies(ctx)
	if err != nil {
		return nil, err
	}
	if cookies == nil {
		cookies = []webview.Cookie{}
	}
	o, err := webview.ReadStorage(ctx, page)
	if err != nil {
		return nil, err
	}
	return &StorageState{Cookies: cookies, Origins: []webview.OriginStorage{*o}}, nil
}

// storageHas confirms every item written is in the page's stores.
func storageHas(ctx context.Context, page webview.Page, want webview.OriginStorage) error {
	got, err := webview.ReadStorage(ctx, page)
	if err != nil {
		return err
	}
	index := func(items []webview.StorageItem) map[string]string {
		m := map[string]string{}
		for _, i := range items {
			m[i.Name] = i.Value
		}
		return m
	}
	local, session := index(got.LocalStorage), index(got.SessionStorage)
	var missing []string
	for _, i := range want.LocalStorage {
		if v, ok := local[i.Name]; !ok || v != i.Value {
			missing = append(missing, "localStorage."+i.Name)
		}
	}
	for _, i := range want.SessionStorage {
		if v, ok := session[i.Name]; !ok || v != i.Value {
			missing = append(missing, "sessionStorage."+i.Name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return mobiumerr.New(mobiumerr.NotConfirmed, "written and not read back: %s", strings.Join(missing, ", "))
	}
	return nil
}

// stateArg reads a saved storage state.
func stateArg(v interface{}) (*StorageState, error) {
	if v == nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"restore needs state: {cookies, origins: [{origin, localStorage, sessionStorage}]}, as app_storage get answers")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "state could not be read: %v", err)
	}
	var st StorageState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "state is not a storage state: %v", err)
	}
	for _, c := range st.Cookies {
		if c.Name == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "every cookie in the state needs a name")
		}
	}
	return &st, nil
}
