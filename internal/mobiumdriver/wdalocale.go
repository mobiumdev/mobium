package mobiumdriver

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Per-app language on iOS, as launch arguments.
//
// Android stores a language for an app and the device applies it. iOS has
// no such setting that can be made from outside — Settings offers one only
// for an app that ships more than one language, and only by hand — but an
// app reads its languages from its defaults, and launch arguments are the
// first place those are looked up: `-AppleLanguages (ja) -AppleLocale ja_JP`.
// That is what Appium's language and locale capabilities do too.
//
// So the language lives in the session and goes with every launch Mobium
// makes: it lasts until it is cleared or the session ends, and an app opened
// any other way — by hand, or by a link — opens in the device's language.
// Measured on an iPhone 15 Plus, iOS 26.6.2: Clock launched this way read
// 編集, 開始 and タイマー, and the next plain launch was English again, so
// nothing is left on the device.

// localeTag is a BCP 47 tag of the shape both platforms take: a language,
// then optional script and region subtags.
var localeTag = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z]{4})?(-([A-Za-z]{2}|[0-9]{3}))?$`)

func (w *WDA) pinnedLocale(appID string) []string {
	w.localeMu.Lock()
	defer w.localeMu.Unlock()
	return append([]string(nil), w.locales[appID]...)
}

// AppLocales reports the language the session launches an app in; empty means
// the device's.
func (w *WDA) AppLocales(ctx context.Context, appID string) ([]string, error) {
	return w.pinnedLocale(appID), nil
}

// SetAppLocales pins an app's language for the session, or clears it with an
// empty list. An app that is running is launched again in it now, since a
// launch is the only time it is read; one that is not running takes it on
// its next launch from here.
func (w *WDA) SetAppLocales(ctx context.Context, appID string, tags []string) error {
	for _, t := range tags {
		if !localeTag.MatchString(t) {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%q is not a language tag — give one like \"ja\", "+
				"\"ja-JP\" or \"zh-Hant-TW\"", t)
		}
	}
	w.localeMu.Lock()
	if w.locales == nil {
		w.locales = map[string][]string{}
	}
	if len(tags) == 0 {
		delete(w.locales, appID)
	} else {
		w.locales[appID] = append([]string(nil), tags...)
	}
	w.localeMu.Unlock()

	running, err := w.appRunning(ctx, appID)
	if err != nil || !running {
		return err
	}
	// Stopped through WebDriverAgent on both a phone and a simulator: the
	// launch that follows goes through it too.
	_ = w.phoneTerminate(ctx, appID)
	return w.Launch(ctx, appID)
}

// DeviceLocale reports the device's own locale, which an app follows when it
// is not pinned.
func (w *WDA) DeviceLocale(ctx context.Context) (string, error) {
	var resp struct {
		Value struct {
			CurrentLocale string `json:"currentLocale"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, w.w3c.sessionPath("/wda/device/info"), nil, &resp); err != nil {
		return "", fmt.Errorf("read the device's locale: %w", err)
	}
	return strings.ReplaceAll(resp.Value.CurrentLocale, "_", "-"), nil
}

// appRunning asks WebDriverAgent whether an app is running, in front or not.
// Its states are XCUIApplication's: 1 not running, 2 suspended, 3 in the
// background, 4 in front.
func (w *WDA) appRunning(ctx context.Context, appID string) (bool, error) {
	var resp struct {
		Value int `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/apps/state"),
		map[string]interface{}{"bundleId": appID}, &resp); err != nil {
		return false, err
	}
	return resp.Value >= 2, nil
}

// launchArguments are the arguments that start an app in tags: its languages
// in order, and the first one as its locale.
func launchArguments(tags []string) []string {
	return []string{
		"-AppleLanguages", "(" + strings.Join(tags, ", ") + ")",
		"-AppleLocale", strings.ReplaceAll(tags[0], "-", "_"),
	}
}

// launchWith launches an app through WebDriverAgent with its language as
// launch arguments and the session's time zone as TZ; devicectl and simctl
// would take them too, but only WebDriverAgent returns once the app is
// running.
func (w *WDA) launchWith(ctx context.Context, appID string, tags []string, zone string) error {
	args := []string{}
	if len(tags) > 0 {
		args = launchArguments(tags)
	}
	env := map[string]string{}
	if zone != "" {
		env["TZ"] = zone
	}
	w.expectApp(ctx, appID)
	err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/apps/launch"), map[string]interface{}{
		"bundleId": appID, "arguments": args, "environment": env,
	}, nil)
	if err != nil {
		w.clearExpected(ctx)
		return fmt.Errorf("launch %s with %v %v: %w", appID, args, env, err)
	}
	return nil
}
