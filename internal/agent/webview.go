package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/uitree"
	"github.com/mobiumdev/mobium/internal/webview"
)

// webRef is what a @ref points at inside a WebView.
//
// Native refs resolve through a uitree.Locator against a fresh snapshot; a web
// ref resolves by re-running the page map and matching on the same identity.
// Both re-resolve before acting, for the same reason: the screen moves.
type webRef struct {
	Selector string // CSS selector, when the element has a durable one
	Label    string
	Role     string
	Index    int // position in the page map, the fallback identity
}

// webviewClass is how a WebView appears in the native hierarchy. Its rectangle
// is the origin and scale for everything the page reports.
const webviewClass = "WebView"

// contexts lists the native context plus every attachable web context.
func (h *Handlers) contexts(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	found, err := h.webContexts(ctx, s)
	if err != nil {
		return nil, err
	}

	current := s.webCtx
	if current == "" {
		current = webview.NativeContext
	}
	view := ContextsView{
		Current: current,
		Contexts: []ContextView{{
			ID: webview.NativeContext, Current: s.webCtx == "",
		}},
	}

	lines := []string{webview.NativeContext + "  (current)"}
	if s.webCtx != "" {
		lines[0] = webview.NativeContext
	}
	for _, c := range found {
		line := c.ID
		if c.ID == s.webCtx {
			line += "  (current)"
		}
		if c.Title != "" || c.URL != "" {
			line += fmt.Sprintf("  — %s", strings.TrimSpace(c.Title+" "+c.URL))
		}
		lines = append(lines, line)
		view.Contexts = append(view.Contexts, ContextView{
			ID: c.ID, Title: c.Title, URL: c.URL, Current: c.ID == s.webCtx,
		})
	}
	if len(found) == 0 {
		lines = append(lines, "", noWebViewsHint(s))
	}
	return Result(strings.Join(lines, "\n"), view), nil
}

// switchContext moves the session between the native shell and a WebView.
func (h *Handlers) switchContext(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}

	name := stringArg(args, "context")
	if name == "" {
		current := s.webCtx
		if current == "" {
			current = webview.NativeContext
		}
		return Result(current, ContextView{ID: current, Current: true}), nil
	}

	// Leaving a WebView always tears the CDP attachment down, whether or not
	// the next context opens successfully.
	s.closeWeb()

	if strings.EqualFold(name, webview.NativeContext) || strings.EqualFold(name, "native") {
		return Result("switched to "+webview.NativeContext,
			ContextView{ID: webview.NativeContext, Current: true}), nil
	}

	found, err := h.webContexts(ctx, s)
	if err != nil {
		return nil, err
	}
	for _, c := range found {
		if !strings.EqualFold(c.ID, name) {
			continue
		}
		sess, err := h.attachWeb(ctx, s, c)
		if err != nil {
			return nil, err
		}
		s.web = sess
		s.webCtx = c.ID
		// Start capturing the console now rather than when logs are first
		// read: anything the page logs before the shim is installed is gone,
		// so the earliest possible moment is the only sensible one. A failure
		// here is not worth refusing the context switch over — the page is
		// attached and usable, and app_logs will say so.
		_ = webview.InstallConsole(ctx, sess)
		return Result("switched to "+c.ID,
			ContextView{ID: c.ID, Title: c.Title, URL: c.URL, Current: true}), nil
	}

	var names []string
	for _, c := range found {
		names = append(names, c.ID)
	}
	if len(names) == 0 {
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "no context named %q — only %s is available (see app_contexts)",
			name, webview.NativeContext)
	}
	return nil, mobiumerr.New(mobiumerr.NoSuchContext, "no context named %q — have %s and %s",
		name, webview.NativeContext, strings.Join(names, ", "))
}

// webFrame reconciles the page's CSS pixels with the device screen, using the
// native WebView element as ground truth for origin and scale.
func (h *Handlers) webFrame(ctx context.Context, s *session) (*webview.Frame, error) {
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	var host *uitree.Node
	for _, n := range tree.All() {
		if strings.HasSuffix(n.ShortClass(), webviewClass) && !n.Bounds.Empty() {
			// The largest WebView on screen is the one being driven; a page
			// can contain small nested ones.
			if host == nil || area(n.Bounds) > area(host.Bounds) {
				host = n
			}
		}
	}
	if host == nil {
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "no WebView is visible in the native hierarchy — "+
			"the app may have navigated away; switch back with `app_context NATIVE_APP`")
	}

	metrics, err := s.web.LayoutMetrics(ctx)
	if err != nil {
		return nil, err
	}
	return webview.NewFrame(host.Bounds, metrics)
}

func area(r uitree.Rect) int { return r.Width() * r.Height() }

// mapWeb maps the current WebView and records its refs.
func (h *Handlers) mapWeb(ctx context.Context, s *session) (*ToolsCallResult, error) {
	els, err := s.web.Map(ctx)
	if err != nil {
		return nil, err
	}

	// Bounds are a bonus here, not a precondition. If the page and its host
	// element disagree about geometry — mobile Safari, where the WebView
	// covers the chrome too — the refs, labels and roles are still exactly
	// right and worth having. Failing the whole map because a rectangle
	// cannot be computed would withhold the useful part to protect the
	// part that is only advisory; acting on a ref re-derives the frame and
	// refuses there, which is where it matters.
	frame, frameErr := h.webFrame(ctx, s)

	table := &refTable{entries: map[string]uitree.Locator{}, web: map[string]webRef{}}
	view := MapView{Elements: []ElementView{}, Context: s.webCtx, Device: s.dev.Serial}
	var lines []string
	for i, e := range els {
		ref := fmt.Sprintf("@e%d", i+1)
		role := e.NeutralRole()
		table.web[ref] = webRef{Selector: e.Selector(), Label: e.Label, Role: role, Index: i}

		label := e.Label
		if label == "" {
			label = e.Tag
		}
		lines = append(lines, fmt.Sprintf("%s %s (%s)", ref, label, role))

		// Bounds are reported in device pixels, like everything else, so a
		// web element and a native one are directly comparable.
		item := ElementView{Ref: ref, Label: label, Role: role, Context: s.webCtx}
		if frameErr == nil {
			item.Bounds = boundsView(frame.ToDevice(e.X, e.Y, e.W, e.H))
		}
		if sel := e.Selector(); sel != "" {
			item.Locator = &LocatorView{Kind: "css", Value: sel, Exact: true}
		}
		view.Elements = append(view.Elements, item)
	}
	h.refs[s.dev.Serial] = table

	if len(lines) == 0 {
		return Result("No actionable elements found in "+s.webCtx, view), nil
	}
	if frameErr != nil {
		lines = append(lines, "",
			"These elements cannot be tapped: "+firstSentence(frameErr.Error()))
	}
	return Result(strings.Join(lines, "\n"), view), nil
}

// firstSentence trims a long explanation down for a footnote, keeping the
// sentence that says what is wrong.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}

// resolveWeb re-maps the page and finds the element a ref names, returning its
// rectangle in device pixels.
//
// Matching prefers a CSS selector, then the label and role, then the position
// in the map. Anything cached from the previous map would be a coordinate that
// the page may have scrolled out from under.
func (h *Handlers) resolveWeb(ctx context.Context, s *session, target string) (uitree.Rect, error) {
	table, ok := h.refs[s.dev.Serial]
	if !ok || table.web == nil {
		return uitree.Rect{}, mobiumerr.New(mobiumerr.InvalidArgument, "no map for %s yet — run app_map first", s.webCtx)
	}
	ref, ok := table.web[target]
	if !ok {
		// A locator is not a ref, and telling someone to re-map will never
		// help them: the web map hands out refs and matches on the CSS
		// selector behind each one, so there is nothing for `text=…` to
		// resolve against. Found driving Wikipedia's article WebView, where
		// `tap text=Charles Babbage` reported "unknown ref" and reading it
		// literally sent you back to app_map forever.
		if !strings.HasPrefix(target, "@") {
			return uitree.Rect{}, mobiumerr.New(mobiumerr.InvalidArgument,
				"%q is a locator, and locators do not work inside a WebView — "+
					"use a @ref from app_map (you are in %s; `app_context NATIVE_APP` "+
					"switches back to the app shell, where locators do work)",
				target, s.webCtx)
		}
		return uitree.Rect{}, mobiumerr.New(mobiumerr.InvalidArgument, "unknown ref %s in %s — run app_map again", target, s.webCtx)
	}

	frame, err := h.webFrame(ctx, s)
	if err != nil {
		return uitree.Rect{}, err
	}
	els, err := s.web.Map(ctx)
	if err != nil {
		return uitree.Rect{}, err
	}

	match := -1
	for i, e := range els {
		if ref.Selector != "" && e.Selector() == ref.Selector {
			match = i
			break
		}
		if ref.Selector == "" && e.Label == ref.Label && e.NeutralRole() == ref.Role {
			match = i
			break
		}
	}
	if match < 0 {
		// Fall back to position only when the page still has an element
		// there, and say so rather than silently tapping the wrong one.
		if ref.Index < len(els) {
			return uitree.Rect{}, mobiumerr.New(mobiumerr.NoSuchElement,
				"%s (%q) is no longer on the page — the content changed, run app_map again",
				target, ref.Label)
		}
		return uitree.Rect{}, mobiumerr.New(mobiumerr.NoSuchElement, "%s (%q) is no longer on the page", target, ref.Label)
	}

	e := els[match]
	rect := frame.ToDevice(e.X, e.Y, e.W, e.H)
	if rect.Empty() {
		return uitree.Rect{}, mobiumerr.New(mobiumerr.ElementNotReachable, "%s has no on-screen area", target)
	}
	return rect, nil
}

// adbFor recovers the adb handle for a session's device. Only the Android
// half of the WebView work goes through it; see webContexts for the split.
func (h *Handlers) adbFor(ctx context.Context, s *session) (*device.ADB, error) {
	if s.backend == BackendWDA {
		return nil, mobiumerr.New(mobiumerr.Internal, "this session is driving an iOS simulator, which has no adb")
	}
	adb, _, err := device.Select(ctx, s.dev.Serial)
	return adb, err
}

// webContexts lists the attachable web contexts for whichever platform this
// session is on.
//
// The two protocols could hardly be less alike — CDP over a forwarded abstract
// socket against Remote Web Inspector over a Unix socket carrying binary
// property lists — and this is the only place above the transport that knows
// there are two. Everything after it works on `webview.Page`.
func (h *Handlers) webContexts(ctx context.Context, s *session) ([]webview.Context, error) {
	if s.backend == BackendWDA {
		insp, err := h.inspectorFor(ctx, s)
		if err != nil {
			return nil, err
		}
		return insp.Contexts(ctx)
	}
	adb, err := h.adbFor(ctx, s)
	if err != nil {
		return nil, err
	}
	return webview.Contexts(ctx, adb)
}

// inspectorFor returns the session's web inspector connection, opening it on
// first use. It lives as long as the session for the reason given on the
// field itself: a second connection costs ten seconds before it says anything.
func (h *Handlers) inspectorFor(ctx context.Context, s *session) (*webview.Inspector, error) {
	if s.insp != nil {
		return s.insp, nil
	}
	target, err := device.SelectIOS(ctx, s.dev.Serial)
	if err != nil {
		return nil, err
	}
	// A simulator's inspector is a Unix socket on this Mac (decisions/0002);
	// a phone's is a service on the device, reached through usbmuxd and
	// lockdown. The protocol after that is the same.
	var insp *webview.Inspector
	if target.Phone != nil {
		insp, err = webview.OpenPhoneInspector(ctx, target.Phone.Phone.UDID)
	} else {
		insp, err = webview.OpenInspector(ctx, target.Sim)
	}
	if err != nil {
		return nil, err
	}
	s.insp = insp
	return insp, nil
}

// attachWeb opens one web context.
func (h *Handlers) attachWeb(ctx context.Context, s *session, c webview.Context) (webview.Page, error) {
	if s.backend == BackendWDA {
		insp, err := h.inspectorFor(ctx, s)
		if err != nil {
			return nil, err
		}
		return webview.AttachIOS(ctx, insp, c)
	}
	adb, err := h.adbFor(ctx, s)
	if err != nil {
		return nil, err
	}
	return webview.Attach(ctx, adb, c)
}

// noWebViewsHint explains an empty context list, which means something
// different on each platform and is the first thing anyone hits.
func noWebViewsHint(s *session) string {
	// A phone's Safari is switched on in Settings, not with a defaults write:
	// the simulator's remedy, suggested on an iPhone, was a command that
	// cannot reach it.
	if w, ok := s.driver.(interface{ OnPhone() bool }); ok && w.OnPhone() {
		return "No WebViews are attachable. A WKWebView is only inspectable when the app " +
			"allows it: on iOS 16.4 and later the app must set " +
			"`webView.isInspectable = true`. For Safari, turn on Settings > Apps > Safari > " +
			"Advanced > Web Inspector on the phone.\n" +
			"A page must also be open: an app showing no web content publishes no targets."
	}
	if s.backend == BackendWDA {
		return "No WebViews are attachable. A WKWebView is only inspectable when the app " +
			"allows it: on iOS 16.4 and later the app must set " +
			"`webView.isInspectable = true`, and Safari needs Web Inspector enabled —\n" +
			"  xcrun simctl spawn booted defaults write com.apple.mobilesafari " +
			"WebKitDeveloperExtrasEnabledPreferenceKey -bool true\n" +
			"A page must also be open: an app showing no web content publishes no targets."
	}
	return "No WebViews are attachable. A debuggable WebView is required: apps must " +
		"call WebView.setWebContentsDebuggingEnabled(true), which release builds " +
		"usually do not."
}
