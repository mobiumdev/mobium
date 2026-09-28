package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// AppView is the result of app_current, app_launch and app_terminate.
type AppView struct {
	App     string `json:"app"`
	Device  string `json:"device"`
	Context string `json:"context,omitempty"`
}

// InstallView is the result of app_install.
type InstallView struct {
	Path   string `json:"path"`
	Device string `json:"device"`
}

// OpenURLView is the result of app_open_url.
type OpenURLView struct {
	URL    string `json:"url"`
	Device string `json:"device"`
	// App is what ended up in the foreground, which for a URL is not
	// something the caller can know in advance.
	App string `json:"app,omitempty"`
}

// settleAfterStart bounds the wait for a launch or a link to take effect.
//
// Both `am start` and simctl return once the request is dispatched, not once
// the app is on screen. Verified twice on the emulator: opening a link and
// immediately asking what was in the foreground answered with the previous
// app, and launching Settings over a running Chrome answered "chrome".
// Waiting here rather than making every caller do it is the same bargain as
// the implicit wait on actions.
const settleAfterStart = 3 * time.Second

// awaitForeground waits for a start request to visibly take effect, and
// reports the app that ended up in front.
//
// When the caller named the app — a launch — that is what it waits for. A URL
// names no app, so it settles for either sign of movement instead: a
// different app in the foreground, which is the cold start worth waiting for,
// or a different screen in the same app, which is what an in-app deep link
// does and which would otherwise pay the whole timeout for nothing.
//
// A request that changes nothing visible is not a failure, so running out of
// time is not an error. The caller gets whatever is actually in front, which
// is the honest answer either way.
func (h *Handlers) awaitForeground(ctx context.Context, s *session, want, wasApp, wasScreen string) string {
	app := wasApp
	// changed and changedAt track a new screen in the same app, which only
	// counts once it has held still for sameAppSettle: see below.
	changed, changedAt := "", time.Time{}
	_ = pollUntil(ctx, settleAfterStart, func(ctx context.Context) (bool, error) {
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			// The hierarchy is briefly unreadable while an app is starting.
			// That is a reason to look again, not to give up.
			return false, nil
		}
		now := tree.Package()
		if now != "" {
			app = now
		}
		if want != "" {
			return now == want, nil
		}
		if now != "" && now != wasApp {
			return true, nil
		}
		// A new screen in the same app is either a deep link that landed in
		// it or the app on its way out for another one — and the second looks
		// like the first until the other app arrives. Measured on an iPhone 17
		// Pro simulator: opening https://example.com over MobiumApp changed
		// its screen within 400-850ms, the answer was taken from that, and
		// Safari came forward 500-900ms later — so every open-url reported
		// MobiumApp. A deep link's screen holds still; a departing app's does
		// not, or is replaced, so the change only counts once it has held.
		fp := fingerprint(tree.Root)
		if fp == wasScreen {
			changed = ""
			return false, nil
		}
		if fp != changed {
			changed, changedAt = fp, time.Now()
			return false, nil
		}
		return time.Since(changedAt) >= sameAppSettle, nil
	})
	return app
}

// sameAppSettle is how long a new screen in the same app must hold still
// before an open-url decides the link landed there, rather than in an app
// still on its way. Safari arrived at most 900ms after MobiumApp's screen first
// changed; the price is up to this much more wait on a same-app deep link.
const sameAppSettle = 1 * time.Second

// lockedInstead turns "started, but something else is in front" into the
// refusal it is when the device is locked.
//
// Measured on a Pixel 8 Pro with its screen asleep: `am start` succeeded, no
// activity was resumed, the foreground was the lock screen, and the launch
// reported success — its structured result naming the app that was not on
// screen. Every call after it then acted on the lock screen. A phone left
// idle during a run gets there by itself, so this is the ordinary case, not
// an edge; the iPhone met the same thing as defect 80. Only a lock is turned
// into an error: anything else in front — a permission dialog, a chooser —
// is the app's own doing and stays a note.
//
// The remedy works in both cases it can meet: `lock unlock` wakes the screen
// and dismisses a lock with no credential, and says so when there is one.
// Its spelling was checked against the schema — the first draft named a
// `lock off` command and a `locked` argument, neither of which exists.
func (h *Handlers) lockedInstead(ctx context.Context, s *session, did string) error {
	ctrl, ok := mobiumdriver.AsScreenLock(s.driver)
	if !ok {
		return nil
	}
	locked, err := ctrl.ScreenLocked(ctx)
	if err != nil || !locked {
		return nil
	}
	return mobiumerr.New(mobiumerr.DeviceNotReady, "%s, but the device is locked, so it is behind the lock screen "+
		"and nothing after this would reach it — run `mobium lock unlock` (app_lock with state \"unlock\"), which "+
		"unlocks a device with no PIN, pattern or password, or unlock it by hand", did)
}

// screenNow reads the foreground app and a fingerprint of the screen, so a
// wait afterwards has something to compare against. A device that cannot be
// read right now yields empty strings rather than an error: this is only ever
// used as the "before" half of a comparison.
func (h *Handlers) screenNow(ctx context.Context, s *session) (app, screen string) {
	if tree, err := s.driver.Snapshot(ctx); err == nil {
		return tree.Package(), fingerprint(tree.Root)
	}
	return "", ""
}

// appControl returns the lifecycle interface for a session, or an error naming
// the backend that lacks it.
func (h *Handlers) appControl(s *session) (mobiumdriver.AppControl, error) {
	ctrl, ok := mobiumdriver.AsAppControl(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapApps, "manage apps")
	}
	return ctrl, nil
}

// appID reads the package or bundle id an app tool acts on.
func appID(args map[string]interface{}) (string, error) {
	id := stringArg(args, "app")
	if id == "" {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "this tool needs an app id — a package name on Android "+
			"(\"com.example.shop\") or a bundle id on iOS (\"com.example.Shop\")")
	}
	return id, nil
}

func (h *Handlers) launchApp(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.launchAppOn(ctx, s, args)
}

// launchAppOn is app_launch once the device is resolved.
func (h *Handlers) launchAppOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, err := h.appControl(s)
	if err != nil {
		return nil, err
	}
	id, err := appID(args)
	if err != nil {
		return nil, err
	}

	// Leaving a WebView context attached across an app change would leave the
	// session pointing at a page that is no longer on screen.
	s.closeWeb()

	wasApp, wasScreen := h.screenNow(ctx, s)

	if err := ctrl.Launch(ctx, id); err != nil {
		return nil, err
	}
	// A launch invalidates every ref from the previous screen.
	delete(h.refs, s.dev.Serial)

	// Wait for it to actually be in front. Without this, launching an app
	// over a running one and immediately asking what is in the foreground
	// answered with the app being replaced.
	app := h.awaitForeground(ctx, s, id, wasApp, wasScreen)
	if app != id {
		if err := h.lockedInstead(ctx, s, "launched "+id); err != nil {
			return nil, err
		}
	}
	msg := fmt.Sprintf("launched %s", id)
	if app != "" && app != id {
		// Worth saying rather than hiding: the launch was accepted but
		// something else is on screen — a permission dialog, a chooser, or an
		// app whose UI runs under a different package.
		msg = fmt.Sprintf("launched %s, but %s is in the foreground", id, app)
	}
	return Result(msg, AppView{App: id, Device: s.dev.Serial}), nil
}

func (h *Handlers) terminateApp(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	ctrl, err := h.appControl(s)
	if err != nil {
		return nil, err
	}
	id, err := appID(args)
	if err != nil {
		return nil, err
	}

	s.closeWeb()
	if err := ctrl.Terminate(ctx, id); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)

	return Result(fmt.Sprintf("terminated %s", id),
		AppView{App: id, Device: s.dev.Serial}), nil
}

func (h *Handlers) installApp(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	ctrl, err := h.appControl(s)
	if err != nil {
		return nil, err
	}

	path := stringArg(args, "path")
	// The app as content, from a caller whose disk is not the daemon's: the
	// CLI and pipe send it in place of the path, a .apk as itself and a .app,
	// which is a directory, as a .tar.gz. Written to a directory of its own
	// and removed after the install either way.
	if content := stringArg(args, "content"); content != "" {
		p, cleanup, err := materializeApp(content, stringArg(args, "name"))
		if err != nil {
			return nil, err
		}
		defer cleanup()
		path = p
	}
	if path == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_install needs a path to a .apk or .app")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	// Fail on a missing file here rather than letting adb or simctl report it
	// in their own vocabulary.
	if _, err := os.Stat(abs); err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "no app bundle at %s", abs)
	}

	if err := ctrl.Install(ctx, abs); err != nil {
		return nil, err
	}
	return Result(fmt.Sprintf("installed %s", abs),
		InstallView{Path: abs, Device: s.dev.Serial}), nil
}

func (h *Handlers) openURL(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.openURLOn(ctx, s, args)
}

// openURLOn is app_open_url once the device is resolved. Split out so the
// wait for the link to take effect can be tested without a device.
func (h *Handlers) openURLOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, err := h.appControl(s)
	if err != nil {
		return nil, err
	}

	url := stringArg(args, "url")
	if url == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_open_url needs a url or deep link")
	}

	s.closeWeb()

	wasApp, wasScreen := h.screenNow(ctx, s)
	before := h.tabsBefore(ctx, s)

	if err := ctrl.OpenURL(ctx, url); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)

	app := h.awaitForeground(ctx, s, "", wasApp, wasScreen)
	h.trackOpenedTabs(ctx, s, app, before)
	if app == wasApp {
		if err := h.lockedInstead(ctx, s, "opened "+url); err != nil {
			return nil, err
		}
	}
	msg := fmt.Sprintf("opened %s", url)
	if app != "" && app != wasApp {
		msg += " in " + app
	}
	return Result(msg, OpenURLView{URL: url, Device: s.dev.Serial, App: app}), nil
}

// currentApp reports the foreground app.
//
// It reads the hierarchy that a snapshot fetches anyway — Android puts the
// package on every node, iOS a bundleId on the application element — so it
// costs no extra device call.
func (h *Handlers) currentApp(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	pkg := tree.Package()
	if pkg == "" {
		return Result("could not determine the foreground app",
			AppView{Device: s.dev.Serial, Context: s.webCtx}), nil
	}
	return Result(pkg, AppView{App: pkg, Device: s.dev.Serial, Context: s.webCtx}), nil
}

// materializeApp writes an app sent as base64 content to a temporary
// directory and returns its path there. name is the file's own name — a .apk,
// or a .app's directory name with .tar.gz appended when it came archived.
func materializeApp(content, name string) (string, func(), error) {
	raw, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return "", nil, mobiumerr.New(mobiumerr.InvalidArgument, "the app's content is not base64: %w", err)
	}
	base := filepath.Base(name)
	if name == "" || base == "." || base == "/" {
		return "", nil, mobiumerr.New(mobiumerr.InvalidArgument, "the app's content came without its file name")
	}
	dir, err := os.MkdirTemp("", "mobium-install-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	file := filepath.Join(dir, base)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	if !strings.HasSuffix(base, ".tar.gz") {
		return file, cleanup, nil
	}
	if err := device.UntarGz(file, dir); err != nil {
		cleanup()
		return "", nil, mobiumerr.New(mobiumerr.InvalidArgument, "the app's archive could not be unpacked: %w", err)
	}
	app := filepath.Join(dir, strings.TrimSuffix(base, ".tar.gz"))
	if _, err := os.Stat(app); err != nil {
		cleanup()
		return "", nil, mobiumerr.New(mobiumerr.InvalidArgument, "the app's archive holds no %s", filepath.Base(app))
	}
	return app, cleanup, nil
}
