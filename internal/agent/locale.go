package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// locale is app_locale: read or pin the language one app runs in.
//
// Per-app rather than device-wide, which is both the smaller hammer and the
// better question. "Does this screen work in Japanese" is answered by running
// that app in Japanese; moving the whole device there means a framework
// restart and disturbs everything else on it.
func (h *Handlers) locale(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.localeOn(ctx, s, args)
}

// localeOn is app_locale once the device is resolved.
func (h *Handlers) localeOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsLocalization(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapLocalization, "read or change an app's language")
	}

	app := stringArg(args, "app")
	if app == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_locale needs an app, e.g. \"com.example.shop\" — "+
			"the language is set per app, not for the device")
	}

	device, err := ctrl.DeviceLocale(ctx)
	if err != nil {
		return nil, err
	}

	raw, hasArg := args["locale"]
	if !hasArg {
		tags, err := ctrl.AppLocales(ctx, app)
		if err != nil {
			return nil, err
		}
		return Result(describeLocale(s, app, tags, device),
			LocaleView{App: app, Locales: tags, Device: device, Serial: s.dev.Serial}), nil
	}

	want := parseLocaleArg(raw)
	before, err := ctrl.AppLocales(ctx, app)
	if err != nil {
		return nil, err
	}
	if err := ctrl.SetAppLocales(ctx, app, want); err != nil {
		return nil, err
	}

	// The app re-renders in the new language only when it next lays out, and
	// every ref from before names a screen in the old one.
	delete(h.refs, s.dev.Serial)

	view := LocaleView{App: app, Locales: want, Device: device,
		Previous: before, Serial: s.dev.Serial}
	if len(want) == 0 {
		return Result(fmt.Sprintf("%s now follows the device (%s)", app, device), view), nil
	}
	if s.backend == BackendWDA {
		return Result(fmt.Sprintf("%s set to %s — iOS stores no per-app language that can be set from "+
			"outside, so it is a launch argument: every launch from this session is in it, until it is "+
			"cleared or the session ends, and the app was launched again now if it was running. Whether %s "+
			"has that translation shows only on the screen", app, strings.Join(want, ","), app), view), nil
	}
	// Said plainly because the distinction matters and the platform will not
	// make it: `zz-ZZ` is stored exactly as willingly as `ja-JP`, and an app
	// with no translation for a tag renders in its default language while the
	// setting reads back as asked for.
	return Result(fmt.Sprintf("%s set to %s — the device stored it; whether %s has that "+
		"translation is not something Android reports, so check the screen",
		app, strings.Join(want, ","), app), view), nil
}

// parseLocaleArg accepts a string or a list, since "two languages in order of
// preference" is a real thing an app honors.
func parseLocaleArg(raw interface{}) []string {
	var out []string
	switch v := raw.(type) {
	case string:
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				out = append(out, t)
			}
		}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

func describeLocale(s *session, app string, tags []string, device string) string {
	if len(tags) == 0 {
		return fmt.Sprintf("%s follows the device (%s)", app, device)
	}
	if s.backend == BackendWDA {
		return fmt.Sprintf("%s is launched in %s by this session (the device is %s)",
			app, strings.Join(tags, ","), device)
	}
	return fmt.Sprintf("%s is pinned to %s (the device is %s)",
		app, strings.Join(tags, ","), device)
}

// LocaleView is the result of app_locale.
type LocaleView struct {
	App string `json:"app"`
	// Locales is empty when the app follows the device.
	Locales []string `json:"locales"`
	// Device is what an unpinned app follows.
	Device string `json:"deviceLocale"`
	// Previous is set only when the call changed something.
	Previous []string `json:"previous,omitempty"`
	Serial   string   `json:"device"`
}
