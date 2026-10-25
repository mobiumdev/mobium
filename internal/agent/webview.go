package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"sort"
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

// inWebView says whether a native node is part of a WebView's page — a node
// the page drew, which the native tree describes less exactly than the page
// itself does: a read-only field reads as disabled, and a checkbox may carry
// no checked state. A refusal for such a node names the WebView's context,
// where the page answers (CHALLENGES 299, 300).
func inWebView(n *uitree.Node) bool {
	if n == nil {
		return false
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if strings.HasSuffix(p.ShortClass(), webviewClass) {
			return true
		}
	}
	return false
}

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
		// Every window is read where apps are windows over one another, and
		// the largest WebView there was another app's: the browser's page
		// behind MobiumApp's panel, and a tap in MobiumApp's page was refused.
		if tree.Windowed && s.webApp != "" && n.Package != s.webApp {
			continue
		}
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
	return h.listWeb(ctx, s, els, unreached, nil)
}

// findInWeb is app_find inside a WebView: the page's elements a locator
// matches, each by the ref app_map gives it. Every element still gets its
// ref, so a ref find hands out is the one map would. It answered from the
// native screen before, and so listed the app's own Back button.
func (h *Handlers) findInWeb(ctx context.Context, s *session, target string) (*ToolsCallResult, error) {
	els, unreached, err := webview.MapAll(ctx, s.web)
	if err != nil {
		return nil, err
	}
	idx, hint, err := webIndexes(ctx, s, els, target)
	if err != nil {
		return nil, err
	}
	keep := map[int]bool{}
	for _, i := range idx {
		keep[i] = true
	}
	res, err := h.listWeb(ctx, s, els, unreached, keep)
	if err != nil || len(idx) > 0 {
		return res, err
	}
	text := fmt.Sprintf("No element matches %s in %s", target, s.webCtx)
	if hint != "" {
		text += " — " + hint
	}
	return Result(text, res.StructuredContent), nil
}

// listWeb records a ref for each of a page's elements and lists them, or,
// with keep, only those.
func (h *Handlers) listWeb(ctx context.Context, s *session, els []webview.Element, unreached int, keep map[int]bool) (*ToolsCallResult, error) {
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
		if keep != nil && !keep[i] {
			continue
		}

		label := e.Label
		if label == "" {
			label = e.Tag
		}
		line := fmt.Sprintf("%s %s (%s)", ref, label, role)
		if e.Value != "" {
			line = fmt.Sprintf("%s %s (%s, %s)", ref, label, role, e.Value)
		}
		if e.Frame != "" {
			// Which frame it is in: MobiumApp's three frames each hold a
			// "Tap me", and without this the three lines read the same.
			line += " in " + e.Frame
		}
		lines = append(lines, line)

		// Bounds are reported in device pixels, like everything else, so a
		// web element and a native one are directly comparable.
		item := ElementView{Ref: ref, Label: label, Role: role, Context: s.webCtx, Value: e.Value}
		if frameErr == nil {
			item.Bounds = boundsView(frame.ToDevice(e.X, e.Y, e.W, e.H))
		}
		if sel := e.Selector(); sel != "" {
			item.Locator = &LocatorView{Kind: "css", Value: sel, Exact: true}
		} else {
			item.Locator = deriveWebLocator(els, i)
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

// deriveWebLocator is, for a page element with no id and no test id, the
// locator its whole text gives it, narrowed by role where the text alone
// names several, checked against the page as a locator would be resolved,
// or nil when neither names it alone. Without it map gave such an element
// only a ref, and a recording wrote the ref into a test that ran on another
// screen: most of a real page's links have no id. A native element's
// locator is derived the same way (uitree.Derive).
func deriveWebLocator(els []webview.Element, i int) *LocatorView {
	label := strings.TrimSpace(els[i].Label)
	if label == "" {
		return nil
	}
	names := func(loc uitree.Locator) bool {
		var idx []int
		for j, e := range els {
			if webMatches(loc, e) {
				idx = append(idx, j)
			}
		}
		idx = preferWhole(loc, els, idx)
		return len(idx) == 1 && idx[0] == i
	}
	loc := uitree.Locator{Kind: uitree.KindText, Value: label, Whole: true}
	if names(loc) {
		return &LocatorView{Kind: string(uitree.KindText), Value: label, Exact: true}
	}
	loc.Role = els[i].NeutralRole()
	if loc.Role != "" && names(loc) {
		return &LocatorView{Kind: string(uitree.KindText), Value: label, Exact: true, Role: loc.Role}
	}
	return nil
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
	// A locator, not a ref: matched against the page's map now, as a ref's
	// identity is. It was refused — "locators do not work inside a WebView"
	// — so a test file, which has no refs, could not touch a page at all
	// (ROADMAP, locators item 4).
	if !strings.HasPrefix(target, "@") {
		return h.findWebByLocator(ctx, s, target)
	}
	table, ok := h.refs[s.dev.Serial]
	if !ok || table.web == nil {
		return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.InvalidArgument, "no map for %s yet — run app_map first", s.webCtx)
	}
	ref, ok := table.web[target]
	if !ok {
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
	return h.webFill(ctx, s, target, func(ctx context.Context) (webview.Element, error) {
		_, el, _, err := h.findWeb(ctx, s, target)
		return el, err
	}, text, replace)
}

// webFill is webType with the element found by find, re-run on every pass:
// a ref from the page's map for app_type in a WebView, or the page element
// under a native target for typeThroughPage.
func (h *Handlers) webFill(ctx context.Context, s *session, target string,
	find func(context.Context) (webview.Element, error), text string, replace bool) (*ToolsCallResult, error) {
	// Empty text clears for app_type as for app_fill, natively and here.
	appendText := !replace && text != ""
	deadline := time.Now().Add(h.implicitWait)
	for {
		el, err := find(ctx)
		if err != nil {
			return nil, err
		}
		// A range is a slider, filled with a position as a native one is,
		// and not typed into: fill 0.2 typed "0.2" and the page clamped it
		// to a step, which read as "the page rewrote or refused the value".
		// The page gives its min, max and step, so the move lands exactly
		// (CHALLENGES 319).
		aria := el.Type != "range" && el.NeutralRole() == "slider"
		if el.Type == "range" || aria {
			if !replace {
				return nil, failedCheck(mobiumerr.InvalidArgument, target, checkEditable, "it is a slider, not a text field",
					"app_fill moves a slider to a position from 0 to 1").
					WithRemedy("app_fill with a position from 0 (the start of its track) to 1 (the end)")
			}
			pos, err := sliderPosition(text)
			if err != nil {
				return nil, err
			}
			set := webview.SetRange
			if aria {
				set = webview.SetARIASlider
			}
			r, err := set(ctx, s.web, el, pos)
			if err != nil {
				return nil, err
			}
			switch {
			case r.Status == "failed" && r.Check == "settable":
				return nil, failedCheck(mobiumerr.Unsupported, target, "settable", r.Reason,
					"a slider the page draws itself is moved as a finger moves it").
					WithRemedy("app_swipe from its thumb along its track, with x1, y1, x2, y2; then app_map to read its value")
			case r.Status == "not_found":
				return nil, mobiumerr.New(mobiumerr.NoSuchElement, "%s is no longer on the page — the content changed, run app_map again", target)
			case r.Status == "ok" && !r.Matches && aria:
				return nil, mobiumerr.New(mobiumerr.NotConfirmed, "moved %s toward position %s, which is %s on its "+
					"track, by its arrow keys, and the page stopped it at %s — another of its thumbs, or its own "+
					"rule, holds it there", target, formatPosition(pos), r.Want, r.Value)
			case r.Status == "ok" && !r.Matches:
				return nil, mobiumerr.New(mobiumerr.NotConfirmed, "moved %s to position %s, which is %s on its "+
					"track, and the page reads %s — the page rewrote the value", target, formatPosition(pos), r.Want, r.Value)
			case r.Status == "ok":
				how := ""
				if aria {
					how = " by its arrow keys"
				}
				if r.Nearest {
					how += fmt.Sprintf(", the closest its keys reach to %s", r.Want)
				}
				return Result(fmt.Sprintf("moved %s to position %s in %s%s — it reads %q, was %q", target,
					formatPosition(pos), s.webCtx, how, r.Value, r.Was),
					ActionView{Action: "fill", Target: target, Context: s.webCtx, Value: r.Value, Position: &pos}), nil
			}
			if time.Now().After(deadline) {
				return nil, webCheckFailed(target, &webview.Actionability{Status: r.Status, Check: r.Check, Reason: r.Reason}, h.implicitWait)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
			continue
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

// typeThroughPage is app_type and app_fill for a native target that is part
// of a WebView's page, when the page can be reached: the text goes in
// through the page, as webType puts it there, which asks the page whether a
// person could type there first. Typed natively, a read-only field took the
// text on Android 17 — the native tree reads it as enabled, and the WebView
// applies a native set-text whatever the attribute says (CHALLENGES 301).
//
// ok is false when no page was reached or no element of it is the target —
// a shipped app that has not opened its WebView to debugging publishes no
// page at all — and the caller then types natively, the only way into such
// a page. The session is native again when this returns.
func (h *Handlers) typeThroughPage(ctx context.Context, s *session, target string, node *uitree.Node,
	text string, replace bool) (res *ToolsCallResult, ok bool, err error) {
	found, lerr := h.webContexts(ctx, s)
	if lerr != nil {
		return nil, false, nil
	}
	for _, c := range found {
		if c.Behind {
			continue
		}
		page, aerr := h.attachWeb(ctx, s, c)
		if aerr != nil {
			continue
		}
		pctx, cancel := context.WithTimeout(ctx, pageAnswerTimeout)
		_, perr := page.Evaluate(pctx, "1")
		cancel()
		if perr != nil {
			_ = page.Close()
			continue
		}
		s.web, s.webCtx, s.webApp = page, c.ID, c.App
		if s.insp != nil {
			s.webAppID, _, _ = strings.Cut(c.Socket, "/")
		}
		find := func(ctx context.Context) (webview.Element, error) { return h.pageElementAt(ctx, s, node.Bounds) }
		if _, ferr := find(ctx); ferr != nil {
			s.closeWeb()
			continue
		}
		res, err = h.webFill(ctx, s, target, find, text, replace)
		s.closeWeb()
		if res != nil && len(res.Content) > 0 {
			res.Content[0].Text += " — through its page"
		}
		return res, true, err
	}
	return nil, false, nil
}

// pageElementAt is the one element of the attached page whose rectangle on
// screen is the native node's: the best overlap, at least two thirds of
// their union, and clearly better than the next. Anything less is not the
// same element, and an ambiguous match is worse than none.
func (h *Handlers) pageElementAt(ctx context.Context, s *session, want uitree.Rect) (webview.Element, error) {
	frame, err := h.webFrame(ctx, s)
	if err != nil {
		return webview.Element{}, err
	}
	els, err := s.web.Map(ctx)
	if err != nil {
		return webview.Element{}, err
	}
	best, second, at := 0.0, 0.0, -1
	for i, e := range els {
		o := overlap(frame.ToDevice(e.X, e.Y, e.W, e.H), want)
		switch {
		case o > best:
			best, second, at = o, best, i
		case o > second:
			second = o
		}
	}
	if at < 0 || best < 2.0/3 || second > best*0.8 {
		return webview.Element{}, mobiumerr.New(mobiumerr.NoSuchElement, "no element of %s is at %v", s.webCtx, want)
	}
	return els[at], nil
}

// overlap is the intersection of two rectangles over their union: 1 for the
// same rectangle, 0 for two that do not touch.
func overlap(a, b uitree.Rect) float64 {
	ix := min(a.X2, b.X2) - max(a.X1, b.X1)
	iy := min(a.Y2, b.Y2) - max(a.Y1, b.Y1)
	if ix <= 0 || iy <= 0 {
		return 0
	}
	inter := float64(ix * iy)
	return inter / (float64(area(a)+area(b)) - inter)
}

// findWebByLocator finds the one element of the page's map a locator names.
//
// css= takes a CSS selector, with a frame's path before it as map prints one
// ("iframe#f >> button"). The other kinds match what the map reads of each
// element — text and label its accessible name, testid its data-testid or id,
// role its role, class its tag — by the rules a native locator follows: a
// part in any case, the whole when quoted, a whole match preferred among
// several, and ,nth= counted down the page. Only the map's elements can
// match, since every web action picks its element from that list; one match
// goes on through the checks every web action makes, and none or several is
// refused saying what was found.
func (h *Handlers) findWebByLocator(ctx context.Context, s *session, target string) (uitree.Rect, webview.Element, *webview.Frame, error) {
	frame, err := h.webFrame(ctx, s)
	if err != nil {
		return uitree.Rect{}, webview.Element{}, nil, err
	}
	els, err := s.web.Map(ctx)
	if err != nil {
		return uitree.Rect{}, webview.Element{}, nil, err
	}
	idx, hint, err := webIndexes(ctx, s, els, target)
	if err != nil {
		return uitree.Rect{}, webview.Element{}, nil, err
	}
	switch {
	case len(idx) == 0 && hint != "":
		return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.NoSuchElement, "no element of the page "+
			"in %s matches %s: %s", s.webCtx, target, hint).
			WithRemedy("the selector with the frame's path first, as app_map prints it")
	case len(idx) == 0:
		return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.NoSuchElement, "no element of the page "+
			"in %s matches %s — app_map lists what it has", s.webCtx, target).
			WithRemedy("app_map for the page's elements, and a locator or ref for one of them")
	case len(idx) > 1:
		var names []string
		for _, i := range idx {
			if len(names) == 5 {
				names = append(names, "…")
				break
			}
			// The frame tells apart what reads the same: MobiumApp's three
			// frames each hold a "Tap me".
			name := fmt.Sprintf("%q (%s)", els[i].Label, els[i].NeutralRole())
			if els[i].Frame != "" {
				name += " in " + els[i].Frame
			}
			names = append(names, name)
		}
		return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.AmbiguousLocator, "%s matches %d elements "+
			"of the page: %s", target, len(idx), strings.Join(names, ", ")).
			WithRemedy("narrow it: a role (,role=button), the whole text in quotes, ,nth= for one of them, a css= selector, or a ref from app_map").
			WithDetail("matches", len(idx))
	}
	e := els[idx[0]]
	rect := frame.ToDevice(e.X, e.Y, e.W, e.H)
	if rect.Empty() {
		return uitree.Rect{}, webview.Element{}, nil, mobiumerr.New(mobiumerr.ElementNotReachable, "%s has no on-screen area", target)
	}
	return rect, e, frame, nil
}

// webIndexes is which of a page's elements a locator names, by their index
// in els: a css= selector, or a locator matched by webMatches with a whole
// match preferred and ,nth= counted down the page. Several is not refused
// here; app_find lists them and an action refuses them. When a css= selector
// matches nothing where it looks but does inside a frame, hint names the
// path that reaches it.
func webIndexes(ctx context.Context, s *session, els []webview.Element, target string) (idx []int, hint string, err error) {
	if sel, ok := strings.CutPrefix(target, "css="); ok {
		var elsewhere []int
		if idx, elsewhere, err = webview.MatchCSS(ctx, s.web, els, sel); err != nil {
			return nil, "", err
		}
		hint = framedHint(els, elsewhere, sel)
	} else {
		loc, err := uitree.ParseLocator(target)
		if err != nil {
			return nil, "", err
		}
		if loc.Within != nil || loc.Kind == uitree.KindPath {
			return nil, "", mobiumerr.New(mobiumerr.Unsupported, "%s cannot name an element "+
				"inside a WebView: within and path are for native screens — a css= selector says what is inside what", target).
				WithRemedy("a css= selector, as \"css=li.order:nth-child(3) button\", or a ref from app_map")
		}
		for i, e := range els {
			if webMatches(loc, e) {
				idx = append(idx, i)
			}
		}
		idx = preferWhole(loc, els, idx)
		if loc.Nth > 0 {
			sort.SliceStable(idx, func(a, b int) bool {
				ea, eb := els[idx[a]], els[idx[b]]
				if ea.Y != eb.Y {
					return ea.Y < eb.Y
				}
				return ea.X < eb.X
			})
			if loc.Nth > len(idx) {
				idx = nil
			} else {
				idx = idx[loc.Nth-1 : loc.Nth]
			}
		}
	}
	return idx, hint, nil
}

// framedHint names, for a selector that matched only inside frames, the path
// that reaches each of up to three of those elements.
func framedHint(els []webview.Element, elsewhere []int, selector string) string {
	if len(elsewhere) == 0 {
		return ""
	}
	sel := selector
	if i := strings.LastIndex(sel, " >> "); i >= 0 {
		sel = sel[i+len(" >> "):]
	}
	sel = strings.TrimSpace(sel)
	var paths []string
	for _, i := range elsewhere {
		if len(paths) == 3 {
			paths = append(paths, "…")
			break
		}
		if f := els[i].Frame; f != "" {
			paths = append(paths, "css="+f+" >> "+sel)
		} else {
			paths = append(paths, "css="+sel)
		}
	}
	return "it matches in another frame, and a selector names a frame's element with the frame's path first: " +
		strings.Join(paths, ", ")
}

// webMatches says whether a page element is one a locator names.
func webMatches(loc uitree.Locator, e webview.Element) bool {
	if loc.Role != "" && loc.Kind != uitree.KindRole && e.NeutralRole() != strings.ToLower(loc.Role) {
		return false
	}
	switch loc.Kind {
	case uitree.KindText, uitree.KindLabel:
		return loc.MatchesString(e.Label)
	case uitree.KindTestID:
		return loc.MatchesString(e.TestID) || loc.MatchesString(e.ID)
	case uitree.KindRole:
		return e.NeutralRole() == strings.ToLower(loc.Value)
	case uitree.KindClass:
		return loc.MatchesString(e.Tag)
	}
	return false
}

// preferWhole keeps the one whole match among several, as a native locator
// does (CHALLENGES 185): text=Save over "Save" and "Save draft".
func preferWhole(loc uitree.Locator, els []webview.Element, idx []int) []int {
	if len(idx) < 2 || loc.Whole || loc.Exact {
		return idx
	}
	var whole []int
	for _, i := range idx {
		v := els[i].Label
		if loc.Kind == uitree.KindTestID {
			v = els[i].TestID
		}
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(loc.Value)) {
			whole = append(whole, i)
		}
	}
	if len(whole) == 1 {
		return whole
	}
	return idx
}

// sliderNameCache holds what the page said of the nameless web sliders on a
// screen, for a moment: their names and values, by sliderKey.
type sliderNameCache struct {
	key   string
	at    time.Time
	names map[string][2]string
}

// sliderNamesFor is how long what the page said is used again: a locator
// polls the screen several times a second while it waits.
const sliderNamesFor = 2 * time.Second

// sliderKeys names each node by its rectangle and its place among the nodes
// sharing that rectangle: the two thumbs of a two-thumb control share one.
func sliderKeys(nodes []*uitree.Node) []string {
	seen := map[uitree.Rect]int{}
	keys := make([]string, len(nodes))
	for i, n := range nodes {
		keys[i] = fmt.Sprintf("%s#%d", n.Bounds, seen[n.Bounds])
		seen[n.Bounds]++
	}
	return keys
}

// nameWebSliders names, from the page, each seek bar a WebView drew with no
// name of its own and none from a label before it. WebView 155, on the Pixel
// 8 Pro, keeps an aria-label slider's name and value only in the page:
// MobiumApp's "Minimum price" and "9:00, Start hour" mapped as "SeekBar" and
// "h0", and a locator by either name found nothing (CHALLENGES 347). Only
// when such a seek bar is on screen, so every other read costs nothing;
// never while the session is switched into a page, whose reads are its own.
func (h *Handlers) nameWebSliders(ctx context.Context, s *session, tree *uitree.Tree) {
	if s.web != nil || tree == nil || s.backend == BackendWDA {
		return
	}
	var nameless []*uitree.Node
	tree.Walk(func(n *uitree.Node) bool {
		if uitree.HasClassRole(n, "slider") && inWebView(n) && n.Text == "" && n.Label == "" && !n.Bounds.Empty() {
			nameless = append(nameless, n)
		}
		return true
	})
	if len(nameless) == 0 {
		return
	}
	keys := sliderKeys(nameless)
	all := strings.Join(keys, ",")
	c := &s.sliderNames
	if c.key != all || time.Since(c.at) > sliderNamesFor {
		c.key, c.at, c.names = all, time.Now(), h.sliderNamesFromPage(ctx, s, tree.Package(), nameless, keys)
	}
	for i, n := range nameless {
		if nv, ok := c.names[keys[i]]; ok {
			n.Text, n.Value, n.ValueSteps = nv[0], nv[1], false
		}
	}
}

// sliderNamesFromPage reads the names and values of the nameless seek bars
// from the front app's own page. Only its pages are listed: listing every
// WebView on the device took 20 of a 22-second map on the Pixel 8 Pro, with
// seven Chrome tabs open behind MobiumApp, each asked in turn.
func (h *Handlers) sliderNamesFromPage(ctx context.Context, s *session, front string, nodes []*uitree.Node,
	keys []string) map[string][2]string {
	adb, err := h.adbFor(ctx, s)
	if err != nil || front == "" {
		return nil
	}
	found, err := webview.ContextsOf(ctx, adb, front)
	if err != nil {
		return nil
	}
	for _, c := range found {
		page, aerr := h.attachWeb(ctx, s, c)
		if aerr != nil {
			continue
		}
		s.web, s.webCtx, s.webApp = page, c.ID, c.App
		names := h.pairSliders(ctx, s, nodes, keys)
		s.closeWeb()
		if len(names) > 0 {
			return names
		}
	}
	return nil
}

// pairSliders matches the attached page's sliders — ranges and ARIA sliders
// — to the nameless seek bars by rectangle, reading the page once: at least
// two thirds of their union, as pageElementAt asks. Seek bars sharing one
// rectangle are a two-thumb control, its ranges laid on one track —
// MobiumApp's price was two seek bars with the same bounds, which
// pageElementAt rightly refuses as ambiguous — and are paired in order with
// the page's sliders there, only when there are as many of each. Anything
// else is left nameless rather than named by a guess.
func (h *Handlers) pairSliders(ctx context.Context, s *session, nodes []*uitree.Node, keys []string) map[string][2]string {
	pctx, cancel := context.WithTimeout(ctx, pageAnswerTimeout)
	defer cancel()
	frame, err := h.webFrame(pctx, s)
	if err != nil {
		return nil
	}
	els, err := s.web.Map(pctx)
	if err != nil {
		return nil
	}
	byRect := map[uitree.Rect][]webview.Element{}
	for _, e := range els {
		if e.Type != "range" && e.NeutralRole() != "slider" {
			continue
		}
		at := frame.ToDevice(e.X, e.Y, e.W, e.H)
		for _, n := range nodes {
			if overlap(at, n.Bounds) >= 2.0/3 {
				byRect[n.Bounds] = append(byRect[n.Bounds], e)
				break
			}
		}
	}
	count := map[uitree.Rect]int{}
	for _, n := range nodes {
		count[n.Bounds]++
	}
	names := map[string][2]string{}
	seen := map[uitree.Rect]int{}
	for i, n := range nodes {
		on := byRect[n.Bounds]
		k := seen[n.Bounds]
		seen[n.Bounds]++
		if len(on) != count[n.Bounds] || on[k].Label == "" {
			continue
		}
		names[keys[i]] = [2]string{on[k].Label, on[k].Value}
	}
	return names
}
