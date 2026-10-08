package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"time"

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
	// A page whose app is not in front is not on screen, and a tap into it
	// would be aimed through the WebView the front app shows. It is named
	// rather than listed, so it is neither offered nor unexplained.
	var front []webview.Context
	for _, c := range found {
		if c.Behind {
			view.Behind = append(view.Behind, c.ID)
			continue
		}
		front = append(front, c)
	}
	for _, c := range front {
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
	if len(view.Behind) > 0 {
		lines = append(lines, "", "Also open, in apps not in front: "+strings.Join(view.Behind, ", ")+
			" — app_launch its app to reach it.")
	}
	// The hint is for a device with nothing inspectable. With pages behind,
	// its remedies — opting the app in, enabling Web Inspector — would be
	// followed for nothing.
	if len(found) == 0 {
		lines = append(lines, "", noWebViewsHint(s))
	}
	return Result(strings.Join(lines, "\n"), view), nil
}

// switchContext moves the session between the native shell and a WebView.
// pageAnswerTimeout is how long a page just attached to has to answer before
// the switch into it is refused.
const pageAnswerTimeout = 10 * time.Second

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
		if c.Behind {
			return nil, mobiumerr.New(mobiumerr.DeviceNotReady,
				"%s belongs to %s, which is not in front, so its page is not on screen and a tap into it "+
					"would land on whatever is", c.ID, c.App).
				WithRemedy(fmt.Sprintf("app_launch %s to bring it forward, then app_context again", c.App)).
				WithDetail("app", c.App)
		}
		// WebKit names no app in front on the home screen, and then cannot
		// say a page is behind: Safari's page, left open, was attached with
		// SpringBoard in front and its cookies read (CHALLENGES 188). The
		// native side knows what is in front; ask it only then, since an
		// in-app browser's pages belong to another process than the app
		// hosting them, which WebKit's own answer accounts for.
		if s.insp != nil && !s.insp.FrontKnown() {
			if tree, terr := s.driver.Snapshot(ctx); terr == nil {
				if front := tree.Package(); front != "" && !strings.EqualFold(front, c.App) {
					return nil, mobiumerr.New(mobiumerr.DeviceNotReady,
						"%s belongs to %s, which is not in front (%s is), so its page is not on screen and a tap "+
							"into it would land on whatever is", c.ID, c.App, front).
						WithRemedy(fmt.Sprintf("app_launch %s to bring it forward, then app_context again", c.App)).
						WithDetail("app", c.App)
				}
			}
		}
		sess, err := h.attachWeb(ctx, s, c)
		if err != nil {
			return nil, err
		}
		// A page can be listed and attachable and still never answer:
		// Android keeps a background tab whose renderer has gone in the
		// listing, and every call into it waited out its whole budget, the
		// switch itself included (CHALLENGES 203). So the page is asked
		// first, briefly.
		pctx, cancel := context.WithTimeout(ctx, pageAnswerTimeout)
		_, perr := sess.Evaluate(pctx, "1")
		cancel()
		if perr != nil {
			_ = sess.Close()
			return nil, mobiumerr.New(mobiumerr.DeviceNotReady, "%s is listed but its page did not answer within %s — "+
				"a page whose renderer has gone, as Android leaves a tab unloaded in the background, is still listed "+
				"and reaches nothing: %v", c.ID, pageAnswerTimeout, perr).
				WithRemedy(fmt.Sprintf("bring the page to the front in %s — for a browser tab, choose it in the tab "+
					"switcher, which reloads it — then app_context %s again", c.App, c.ID)).
				WithDetail("app", c.App)
		}
		s.web = sess
		s.webCtx = c.ID
		s.webApp = c.App
		if s.insp != nil {
			s.webAppID, _, _ = strings.Cut(c.Socket, "/")
		}
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
//
// It refuses once the attached page's app has left the screen: the page is
// still attached and still answers, but the WebView found below is the front
// app's, so a point computed from it lands on whatever is there. CHALLENGES 144.
func (h *Handlers) webFrame(ctx context.Context, s *session) (*webview.Frame, error) {
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if front := tree.Package(); s.pageBehind(front) {
		return nil, mobiumerr.New(mobiumerr.DeviceNotReady,
			"%s belongs to %s, which is no longer in front (%s is), so its page is not on screen and a tap "+
				"into it would land on whatever is", s.webCtx, s.webApp, front).
			WithRemedy(fmt.Sprintf("app_launch %s to bring it back, then app_context %s again, since launching "+
				"detaches the page; or app_context %s", s.webApp, s.webCtx, webview.NativeContext)).
			WithDetail("app", s.webApp)
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
		// A dialog in front hides the WebView as well, and says so: Chrome's
		// "Chrome notifications make things easier" over example.com on the
		// Android 15 emulator drew this refusal, which blamed installed web
		// apps, while chrome.sh's page was in a plain tab under a prompt
		// that a tap of "No thanks" would close. CHALLENGES 268.
		if text := appDialogText(tree); text != "" {
			return nil, webDialogOver(text, s.webCtx)
		}
		if a, ok := mobiumdriver.AsAlerts(s.driver); ok {
			if text, aerr := a.AlertText(ctx); aerr == nil && strings.TrimSpace(text) != "" {
				return nil, webDialogOver(text, s.webCtx)
			}
		}
		// Two causes look the same from here, and only the first has a
		// remedy: the app moved to a screen with no WebView, or it still
		// shows the page and has stopped putting the WebView in the
		// accessibility tree — Chrome's installed web apps do when opened
		// while Chrome is already running, within about five seconds, and
		// then nothing native can say where the page is.
		// CHALLENGES 200.
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "%s is in front but its accessibility tree has no "+
			"WebView, so where %s sits on screen cannot be measured and a tap is refused. Either the app "+
			"moved to a screen without one, or it still shows the page and stopped reporting its WebView — "+
			"Chrome's installed web apps do this when opened while Chrome is running. Reading the page still works: "+
			"app_text, app_map and app_eval", tree.Package(), s.webCtx).
			WithRemedy("if the app moved on, `app_context NATIVE_APP`; if it still shows the page, no tap " +
				"inside it can be placed until the app reports its WebView again")
	}

	metrics, err := s.web.LayoutMetrics(ctx)
	if err != nil {
		return nil, err
	}
	// The iOS keyboard over an app's WebView changes the page's viewport and
	// not the WebView, so the two heights no longer say where the page sits
	// (CHALLENGES 230). Not in Safari, whose chrome is what the comparison
	// is for, keyboard or not.
	if k := tree.Keyboard(); k != nil && k.Bounds.Y1 < host.Bounds.Y2 && tree.Package() != safariBundleID {
		return webview.NewFrameUnderKeyboard(host.Bounds, metrics)
	}
	// Android's keyboard is the same case in another window, missing from
	// the tree: on the Pixel 8 Pro it left MobiumApp's WebView 770 CSS pixels
	// tall over a 401-pixel viewport, and every tap with it up was refused.
	// Its touchable region is read as the keyboard check reads it — only
	// while something has focus, since it costs a dumpsys. CHALLENGES 294.
	if keyboardOverWebView(ctx, s, tree, host.Bounds) {
		return webview.NewFrameUnderKeyboard(host.Bounds, metrics)
	}
	frame, err := webview.NewFrame(host.Bounds, metrics)
	if err != nil && host.Bounds.Width() > 0 && metrics != nil && metrics.CSSWidth > 0 {
		// A WebView taller than its page: where the page starts is read from
		// text both trees can see, or the refusal stands (CHALLENGES 248).
		if anchored, aerr := webview.AnchorFrame(ctx, s.web, host, metrics); aerr == nil {
			return anchored, nil
		}
	}
	return frame, err
}

// safariBundleID is mobile Safari, whose WebView is under its own chrome.
const safariBundleID = "com.apple.mobilesafari"

func area(r uitree.Rect) int { return r.Width() * r.Height() }

// mapWeb maps the current WebView and records its refs.
func (h *Handlers) mapWeb(ctx context.Context, s *session) (*ToolsCallResult, error) {
	els, unreached, err := webview.MapAll(ctx, s.web)
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
		line := fmt.Sprintf("%s %s (%s)", ref, label, role)
		if e.Frame != "" {
			// Which frame it is in: MobiumApp's three frames each hold a
			// "Tap me", and without this the three lines read the same.
			line += " in " + e.Frame
		}
		lines = append(lines, line)

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

	// A cross-origin frame is closed to the page's scripts and mapped through
	// its own execution context (CHALLENGES 229); one that could not be
	// paired with a context is said, rather than left to read as an empty
	// frame. The platform's accessibility reaches into it: MobiumApp's
	// cross-origin frame's button is in NATIVE_APP's map (CHALLENGES 228).
	if n := unreached; n > 0 {
		note := "1 cross-origin frame on this page could not be reached through its own context, and its " +
			"elements are not listed here; app_context NATIVE_APP maps what the platform's accessibility reaches inside it"
		if n > 1 {
			note = fmt.Sprintf("%d cross-origin frames on this page could not be reached through their own contexts, "+
				"and their elements are not listed here; app_context NATIVE_APP maps what the platform's "+
				"accessibility reaches inside them", n)
		}
		lines = append(lines, "", note)
	}

	if len(view.Elements) == 0 {
		return Result(strings.Join(append([]string{"No actionable elements found in " + s.webCtx}, lines...), "\n"), view), nil
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
	rect, _, _, err := h.findWeb(ctx, s, target)
	return rect, err
}

// findWeb resolves a web ref to its element: the rectangle on screen, the
// element's index among the page's map candidates, and the frame that
// converts page pixels to device pixels.
func (h *Handlers) findWeb(ctx context.Context, s *session, target string) (uitree.Rect, webview.Element, *webview.Frame, error) {
	table, ok := h.refs[s.dev.Serial]
	if !ok || table.web == nil {
		return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.InvalidArgument, "no map for %s yet — run app_map first", s.webCtx)
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
			return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.InvalidArgument,
				"%q is a locator, and locators do not work inside a WebView — "+
					"use a @ref from app_map (you are in %s; `app_context NATIVE_APP` "+
					"switches back to the app shell, where locators do work)",
				target, s.webCtx)
		}
		return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown ref %s in %s — run app_map again", target, s.webCtx)
	}

	frame, err := h.webFrame(ctx, s)
	if err != nil {
		return uitree.Rect{}, webview.Element{}, nil, err
	}
	els, err := s.web.Map(ctx)
	if err != nil {
		return uitree.Rect{}, webview.Element{}, nil, err
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
			return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.NoSuchElement,
				"%s (%q) is no longer on the page — the content changed, run app_map again",
				target, ref.Label)
		}
		return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.NoSuchElement, "%s (%q) is no longer on the page", target, ref.Label)
	}

	e := els[match]
	rect := frame.ToDevice(e.X, e.Y, e.W, e.H)
	if rect.Empty() {
		return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.ElementNotReachable, "%s has no on-screen area", target)
	}
	return rect, e, frame, nil
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

// pageBehind says the attached page's app is not the one in front: on iOS by
// WebKit's own active flag, as contexts decides it (CHALLENGES 138), and on
// Android by the foreground package, since Android has no such flag. Unknown
// on either side is not behind: nothing is refused on a guess.
func (s *session) pageBehind(front string) bool {
	if s.insp != nil && s.webAppID != "" {
		return s.insp.AppBehind(s.webAppID)
	}
	return s.webApp != "" && front != "" && s.webApp != front
}

// webContexts lists the attachable web contexts for whichever platform this
// session is on.
//
// The two protocols could hardly be less alike — CDP over a forwarded abstract
// socket against Remote Web Inspector over a Unix socket carrying binary
// property lists — and this is the only place above the transport that knows
// there are two. Everything after it works on `webview.Page`.
func (h *Handlers) webContexts(ctx context.Context, s *session) ([]webview.Context, error) {
	var found []webview.Context
	var err error
	if s.backend == BackendWDA {
		insp, ierr := h.inspectorFor(ctx, s)
		if ierr != nil {
			return nil, ierr
		}
		found, err = insp.Contexts(ctx)
	} else {
		adb, aerr := h.adbFor(ctx, s)
		if aerr != nil {
			return nil, aerr
		}
		found, err = webview.Contexts(ctx, adb)
		if err == nil {
			// Android lists every debuggable page on the device and says
			// nothing of which is on screen, so a page is behind when its
			// package is not the one in front — the same answer iOS gets
			// from WebKit. A custom tab is Chrome's page and Chrome's window.
			if tree, terr := s.driver.Snapshot(ctx); terr == nil {
				if front := tree.Package(); front != "" {
					for i := range found {
						found[i].Behind = found[i].App != "" && found[i].App != front
					}
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	s.nameContexts(found)
	return found, nil
}

// nameContexts gives each page the name it was first given in this session,
// and a new page the first name nobody has had: WEBVIEW_<app>, then _1, _2.
//
// Both transports number pages by their position in a listing, which moves:
// a Safari tab opening between two calls renumbered the rest, and a name
// taken from one listing attached another page (2026-09-27). A page's own
// identity — its devtools socket and CDP target on Android, its application
// and page on iOS — holds still, so names are kept by that for as long as the
// page is listed. Once it is not, its name is free again: an app relaunched
// gets a new devtools socket, and its WebView should come back under the
// name it had, not as _1. CHALLENGES 137.
func (s *session) nameContexts(found []webview.Context) {
	if s.ctxNames == nil {
		s.ctxNames = map[string]string{}
	}
	key := func(c webview.Context) string { return c.Socket + "|" + c.TargetID }
	listed := map[string]bool{}
	for _, c := range found {
		listed[key(c)] = true
	}
	// Names of pages that have gone are free; the rest are held first, so a
	// page arriving in this listing cannot take a name a listed page has.
	taken := map[string]bool{}
	for k, name := range s.ctxNames {
		if listed[k] {
			taken[name] = true
		} else {
			delete(s.ctxNames, k)
		}
	}
	for i := range found {
		c := &found[i]
		if c.Base == "" {
			continue
		}
		if name, ok := s.ctxNames[key(*c)]; ok {
			c.ID = name
			continue
		}
		name := c.Base
		for n := 1; taken[name]; n++ {
			name = fmt.Sprintf("%s_%d", c.Base, n)
		}
		s.ctxNames[key(*c)], taken[name] = name, true
		c.ID = name
	}
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
	// A simulator's inspector is a Unix socket on this Mac;
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

// webStableGap is how long apart two readings of a web target's position
// must agree before it counts as still: Vibium's figure.
const webStableGap = 50 * time.Millisecond

// aimWeb resolves a web ref and decides where to touch it, after the page
// has said the element can be touched: visible and in view, enabled, holding
// still, and not covered — or covered only at its center, in which case the
// clear point nearest it is used (webview.CheckActionable). A check that
// fails is waited out within the implicit wait, since a slide, a spinner or a
// banner on its way out is the ordinary case, and then refused in Vibium's
// words: which check, and why.
func (h *Handlers) aimWeb(ctx context.Context, s *session, target string) (int, int, *CoverView, error) {
	deadline := time.Now().Add(h.implicitWait)
	var last *webview.Actionability
	for {
		_, el, frame, err := h.findWeb(ctx, s, target)
		if err != nil {
			return 0, 0, nil, err
		}
		a, err := webview.CheckActionable(ctx, s.web, el)
		if err != nil {
			return 0, 0, nil, err
		}
		if a.Status == "ok" {
			time.Sleep(webStableGap)
			b, err := webview.CheckActionable(ctx, s.web, el)
			if err != nil {
				return 0, 0, nil, err
			}
			if b.Status == "ok" && b.X == a.X && b.Y == a.Y && b.W == a.W && b.H == a.H {
				p := frame.ToDevice(b.PX, b.PY, 0, 0)
				var cover *CoverView
				if b.Moved {
					cover = &CoverView{Label: b.Cover, Control: true}
				}
				return p.X1, p.Y1, cover, nil
			}
			a = &webview.Actionability{Status: "failed", Check: "stable", Reason: "the element is moving or resizing"}
			if b.Status != "ok" {
				a = b
			}
		}
		last = a
		if a.Status == "not_found" {
			return 0, 0, nil, mobiumerr.New(mobiumerr.NoSuchElement, "%s is no longer on the page — the content changed, run app_map again", target)
		}
		if time.Now().After(deadline) {
			return 0, 0, nil, webCheckFailed(target, last, h.implicitWait)
		}
		select {
		case <-ctx.Done():
			return 0, 0, nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// webCheckFailed is the refusal for a web target that never became
// touchable, in Vibium's shape — "failed check X: reason" — with a code that
// says what kind of failure it is.
func webCheckFailed(target string, a *webview.Actionability, waited time.Duration) error {
	code, remedy := mobiumerr.ElementNotReachable, "wait for it to become touchable, or scroll or dismiss what is in the way"
	switch a.Check {
	case "enabled":
		code, remedy = mobiumerr.Timeout, "do what enables it first"
	case "stable":
		code, remedy = mobiumerr.Timeout, "wait for it to stop moving"
	}
	return failedCheck(code, target, a.Check, a.Reason, fmt.Sprintf("still so after %s", waited)).
		WithRemedy(remedy)
}

// webType is app_type and app_fill inside a WebView: Vibium's fill, and its
// type as the same setter with what the field held kept in front. The page is asked
// whether the field can take text — visible and in view, enabled, editable —
// and then its value is set the way a framework's controlled input hears it,
// with the input and change events typing would have caused, and read back.
// Visibility and enablement are waited out within the implicit wait, as for a
// tap; a field that is not a text field at all is refused at once, as native
// app_type refuses one. A password is never echoed.
func (h *Handlers) webType(ctx context.Context, s *session, target, text string, replace bool) (*ToolsCallResult, error) {
	// Empty text clears for app_type as for app_fill, natively and here.
	appendText := !replace && text != ""
	deadline := time.Now().Add(h.implicitWait)
	for {
		_, el, _, err := h.findWeb(ctx, s, target)
		if err != nil {
			return nil, err
		}
		f, err := webview.Fill(ctx, s.web, el, text, appendText)
		if err != nil {
			return nil, err
		}
		switch {
		case f.Status == "not_found":
			return nil, mobiumerr.New(mobiumerr.NoSuchElement, "%s is no longer on the page — the content changed, run app_map again", target)
		case f.Status == "failed" && f.Check == "editable":
			return nil, failedCheck(mobiumerr.InvalidArgument, target, checkEditable, f.Reason,
				"app_type types into a text field; to press anything else, use app_tap").
				WithRemedy("app_tap to press it; app_type for a text field")
		case f.Status == "ok" && !f.Matches:
			return nil, mobiumerr.New(mobiumerr.NotConfirmed, "set %s and read it back different — the page "+
				"rewrote or refused the value", target)
		case f.Status == "ok":
			if f.Password {
				return Result(fmt.Sprintf("typed %d characters into %s in %s, a password field — not echoed",
					len([]rune(text)), target, s.webCtx), ActionView{Action: "type", Target: target, Context: s.webCtx}), nil
			}
			verb := fmt.Sprintf("typed %q into", text)
			if text == "" {
				verb = "cleared"
			}
			return Result(fmt.Sprintf("%s %s in %s", verb, target, s.webCtx),
				ActionView{Action: "type", Target: target, Context: s.webCtx}), nil
		}
		if time.Now().After(deadline) {
			return nil, webCheckFailed(target, &webview.Actionability{Status: f.Status, Check: f.Check, Reason: f.Reason}, h.implicitWait)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// webDialogOver is the refusal for a tap into a page that a dialog covers.
func webDialogOver(dialog, ctxName string) error {
	dialog = strings.TrimSpace(strings.SplitN(dialog, "\n", 2)[0])
	return mobiumerr.New(mobiumerr.DeviceNotReady, "a dialog is over the app — %q — so the page of %s is "+
		"underneath it and a tap into the page would land on the dialog", dialog, ctxName).
		WithRemedy("answer the dialog first: app_context NATIVE_APP and tap one of its buttons from app_map, "+
			"or declare an answer with app_dialogs").
		WithDetail("dialog", dialog)
}

// keyboardOverWebView reports whether Android's keyboard, another window,
// reaches into the WebView's area. Not knowing is no reason to change the
// frame.
func keyboardOverWebView(ctx context.Context, s *session, tree *uitree.Tree, host uitree.Rect) bool {
	kr, ok := mobiumdriver.AsKeyboardRegioner(s.driver)
	if !ok || tree == nil {
		return false
	}
	focused := false
	tree.Walk(func(x *uitree.Node) bool {
		if x.Focused {
			focused = true
		}
		return !focused
	})
	if !focused {
		return false
	}
	regions, err := kr.KeyboardRegions(ctx)
	if err != nil {
		return false
	}
	for _, r := range regions {
		if r.X1 < host.X2 && r.X2 > host.X1 && r.Y1 < host.Y2 && r.Y2 > host.Y1 {
			return true
		}
	}
	return false
}
