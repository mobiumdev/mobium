package device

import (
	"context"
	"strconv"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Accessibility settings, one vocabulary for every platform that has them.
//
// Each is written the way the platform stores it and confirmed by reading it
// back, and each change comes with an undo that restores the exact raw values
// found — an unset key is deleted again rather than written as a default,
// since on a freshly booted emulator most of these are unset, not off. What
// each write reaches was measured, not assumed: on a Pixel 7 AVD (API 35)
// every Android one was confirmed by Android's own Accessibility screens and
// by MobiumApp's Accessibility Demo, and on an iPhone 17 Pro simulator every
// iOS one by the Settings app and the demo, live, on 2026-09-26.
const (
	AXReduceMotion              = "reduce_motion"
	AXBoldText                  = "bold_text"
	AXIncreaseContrast          = "increase_contrast"
	AXReduceTransparency        = "reduce_transparency"
	AXButtonShapes              = "button_shapes"
	AXDifferentiateWithoutColor = "differentiate_without_color"
	AXInvertColors              = "invert_colors"
	AXGrayscale                 = "grayscale"
	// AXTextSize is iOS's Dynamic Type, a named category.
	AXTextSize = "text_size"
	// AXTextScale is Android's font scale, a number. The two are not the
	// same thing under two names: iOS picks from categories and Android
	// multiplies, and mapping one onto the other would be a guess.
	AXTextScale = "text_scale"
)

// AXSettings is every setting, in the order they are reported.
var AXSettings = []string{
	AXReduceMotion, AXBoldText, AXIncreaseContrast, AXReduceTransparency, AXButtonShapes,
	AXDifferentiateWithoutColor, AXInvertColors, AXGrayscale, AXTextSize, AXTextScale,
}

// AXOn and AXOff are the values of every setting that is a switch.
const (
	AXOn  = "on"
	AXOff = "off"
)

// IOSTextSizes are the categories iOS offers, smallest first; "large" is the
// default.
var IOSTextSizes = []string{
	"extra-small", "small", "medium", "large", "extra-large", "extra-extra-large",
	"extra-extra-extra-large", "accessibility-medium", "accessibility-large",
	"accessibility-extra-large", "accessibility-extra-extra-large",
	"accessibility-extra-extra-extra-large",
}

// AXUndo puts a setting back exactly as it was found.
type AXUndo func(context.Context) error

// switchValue reads "on" or "off" from a caller, refusing anything else.
func switchValue(name, value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AXOn, "true", "1", "yes":
		return true, nil
	case AXOff, "false", "0", "no":
		return false, nil
	}
	return false, mobiumerr.New(mobiumerr.InvalidArgument, "%s is a switch: give %q or %q, not %q", name, AXOn, AXOff, value)
}

func onOff(b bool) string {
	if b {
		return AXOn
	}
	return AXOff
}

// ---- Android ---------------------------------------------------------------

// androidKey is one row of `settings`.
type androidKey struct{ namespace, key string }

var (
	animationScales = []androidKey{
		{"global", "animator_duration_scale"},
		{"global", "transition_animation_scale"},
		{"global", "window_animation_scale"},
	}
	boldTextKey  = androidKey{"secure", "font_weight_adjustment"}
	contrastKey  = androidKey{"secure", "high_text_contrast_enabled"}
	inversionKey = androidKey{"secure", "accessibility_display_inversion_enabled"}
	daltonOnKey  = androidKey{"secure", "accessibility_display_daltonizer_enabled"}
	daltonKey    = androidKey{"secure", "accessibility_display_daltonizer"}
	fontScaleKey = androidKey{"system", "font_scale"}
)

// boldTextWeight is what Android's own Bold text switch writes.
const boldTextWeight = "300"

// androidLacks names what Android has no setting for, and what to use
// instead where there is something.
var androidLacks = map[string]string{
	AXReduceTransparency:        "Android has no Reduce Transparency setting",
	AXButtonShapes:              "Android has no Button Shapes setting",
	AXDifferentiateWithoutColor: "Android has no Differentiate Without Color setting",
	AXTextSize:                  "Android sizes text by a scale, not a category — use text_scale, such as 1.3",
}

// rawGet reads one setting, "null" when it is unset.
func (a *ADB) rawGet(ctx context.Context, k androidKey) (string, error) {
	out, err := a.Shell(ctx, "settings", "get", k.namespace, k.key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *ADB) rawPut(ctx context.Context, k androidKey, v string) error {
	if v == "null" {
		_, err := a.Shell(ctx, "settings", "delete", k.namespace, k.key)
		return err
	}
	_, err := a.Shell(ctx, "settings", "put", k.namespace, k.key, shellQuote(v))
	return err
}

// snapshot reads keys as they are, for an undo that restores them exactly.
func (a *ADB) snapshot(ctx context.Context, keys []androidKey) (AXUndo, error) {
	was := make([]string, len(keys))
	for i, k := range keys {
		v, err := a.rawGet(ctx, k)
		if err != nil {
			return nil, err
		}
		was[i] = v
	}
	return func(ctx context.Context) error {
		for i, k := range keys {
			if err := a.rawPut(ctx, k, was[i]); err != nil {
				return err
			}
		}
		return nil
	}, nil
}

func isZero(v string) bool {
	f, err := strconv.ParseFloat(v, 64)
	return err == nil && f == 0
}

// AccessibilitySetting reads one setting in the shared vocabulary.
func (a *ADB) AccessibilitySetting(ctx context.Context, name string) (string, error) {
	if reason, ok := androidLacks[name]; ok {
		return "", mobiumerr.New(mobiumerr.Unsupported, "%s", reason)
	}
	switch name {
	case AXReduceMotion:
		// Android's own Remove animations switch sets all three to zero, and
		// an app asking whether motion is reduced reads the animator scale.
		for _, k := range animationScales {
			v, err := a.rawGet(ctx, k)
			if err != nil {
				return "", err
			}
			if !isZero(v) {
				return AXOff, nil
			}
		}
		return AXOn, nil
	case AXBoldText:
		v, err := a.rawGet(ctx, boldTextKey)
		if err != nil {
			return "", err
		}
		n, _ := strconv.Atoi(v)
		return onOff(n > 0), nil
	case AXIncreaseContrast, AXInvertColors:
		k := contrastKey
		if name == AXInvertColors {
			k = inversionKey
		}
		v, err := a.rawGet(ctx, k)
		if err != nil {
			return "", err
		}
		return onOff(v == "1"), nil
	case AXGrayscale:
		on, err := a.rawGet(ctx, daltonOnKey)
		if err != nil {
			return "", err
		}
		mode, err := a.rawGet(ctx, daltonKey)
		if err != nil {
			return "", err
		}
		// Grayscale is color correction switched on in its monochrome mode,
		// 0; the other modes correct for a kind of color blindness instead.
		return onOff(on == "1" && mode == "0"), nil
	case AXTextScale:
		v, err := a.rawGet(ctx, fontScaleKey)
		if err != nil {
			return "", err
		}
		if v == "null" {
			return "1.0", nil
		}
		return v, nil
	}
	return "", unknownAXSetting(name)
}

// SetAccessibilitySetting changes one setting, confirms it by reading it
// back, and returns how to put it back.
//
// Bold text and the text scale are configuration changes on Android: a
// running app's activity is recreated, which on MobiumApp meant returning
// to its home screen. A caller's refs are stale afterwards either way.
func (a *ADB) SetAccessibilitySetting(ctx context.Context, name, value string) (AXUndo, error) {
	if reason, ok := androidLacks[name]; ok {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "%s", reason)
	}
	var keys []androidKey
	var writes map[androidKey]string
	var want string
	switch name {
	case AXReduceMotion:
		on, err := switchValue(name, value)
		if err != nil {
			return nil, err
		}
		keys, writes = animationScales, map[androidKey]string{}
		for _, k := range animationScales {
			writes[k] = map[bool]string{true: "0", false: "1.0"}[on]
		}
		want = onOff(on)
	case AXBoldText:
		on, err := switchValue(name, value)
		if err != nil {
			return nil, err
		}
		if sdk, err := a.sdkLevel(ctx); err == nil && sdk < 31 {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "Bold text arrived in Android 12 (API 31); this device is API %d", sdk)
		}
		keys = []androidKey{boldTextKey}
		writes = map[androidKey]string{boldTextKey: map[bool]string{true: boldTextWeight, false: "0"}[on]}
		want = onOff(on)
	case AXIncreaseContrast, AXInvertColors:
		on, err := switchValue(name, value)
		if err != nil {
			return nil, err
		}
		k := contrastKey
		if name == AXInvertColors {
			k = inversionKey
		}
		keys = []androidKey{k}
		writes = map[androidKey]string{k: map[bool]string{true: "1", false: "0"}[on]}
		want = onOff(on)
	case AXGrayscale:
		on, err := switchValue(name, value)
		if err != nil {
			return nil, err
		}
		keys = []androidKey{daltonOnKey, daltonKey}
		writes = map[androidKey]string{daltonOnKey: "0"}
		if on {
			writes = map[androidKey]string{daltonOnKey: "1", daltonKey: "0"}
		}
		want = onOff(on)
	case AXTextScale:
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		// Android's own slider runs 0.85 to 2.0; outside a wider margin
		// than that is a typo rather than a test.
		if err != nil || f < 0.5 || f > 3 {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "text_scale is a number from 0.5 to 3, such as 1.3 — got %q", value)
		}
		want = strconv.FormatFloat(f, 'f', -1, 64)
		keys = []androidKey{fontScaleKey}
		writes = map[androidKey]string{fontScaleKey: want}
	default:
		return nil, unknownAXSetting(name)
	}

	undo, err := a.snapshot(ctx, keys)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if v, ok := writes[k]; ok {
			if err := a.rawPut(ctx, k, v); err != nil {
				return undo, err
			}
		}
	}
	got, err := a.AccessibilitySetting(ctx, name)
	if err != nil {
		return undo, err
	}
	if !sameAXValue(name, got, want) {
		return undo, mobiumerr.New(mobiumerr.NotConfirmed, "asked for %s %s and the device reports %s", name, want, got)
	}
	return undo, nil
}

// sdkLevel is the device's API level.
func (a *ADB) sdkLevel(ctx context.Context) (int, error) {
	out, err := a.Shell(ctx, "getprop", "ro.build.version.sdk")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// ---- iOS simulator ---------------------------------------------------------

// iosAXKeys are the switches kept in the simulator's com.apple.Accessibility
// defaults. Found by flipping each in the simulator's Settings app and
// diffing the domain; a write reached a running app live.
var iosAXKeys = map[string]string{
	AXReduceMotion:              "ReduceMotionEnabled",
	AXBoldText:                  "EnhancedTextLegibilityEnabled",
	AXReduceTransparency:        "EnhancedBackgroundContrastEnabled",
	AXButtonShapes:              "ButtonShapesEnabled",
	AXDifferentiateWithoutColor: "DifferentiateWithoutColor",
}

// simulatorLacks names what the simulator will not do here, and why.
var simulatorLacks = map[string]string{
	AXInvertColors: "Smart Invert is not set on a simulator: the Settings app writes a display filter " +
		"in a second domain as well as the switch, and no screenshot shows a display filter, so " +
		"nothing could confirm the screen inverted — turn it on in the simulator's Settings > " +
		"Accessibility > Display & Text Size",
	AXGrayscale: "the iOS simulator has no Color Filters, so there is no grayscale to set",
	AXTextScale: "iOS sizes text by a category, not a scale — use text_size, such as accessibility-large",
}

const accessibilityDomain = "com.apple.Accessibility"

// contrastKeys are what `simctl ui increase_contrast` writes.
var contrastKeys = []string{"DarkenSystemColors", "PointerIncreasedContrastEnabled"}

// snapshotDefaults reads boolean keys as they are, absent included, for an
// undo that puts each back exactly.
func (s *Simctl) snapshotDefaults(ctx context.Context, keys []string) (AXUndo, error) {
	type was struct{ value, present bool }
	found := make([]was, len(keys))
	for i, k := range keys {
		v, present, err := s.defaultsBool(ctx, k)
		if err != nil {
			return nil, err
		}
		found[i] = was{v, present}
	}
	return func(ctx context.Context) error {
		for i, k := range keys {
			var err error
			if !found[i].present {
				_, err = s.Run(ctx, "spawn", s.UDID, "defaults", "delete", accessibilityDomain, k)
			} else {
				_, err = s.Run(ctx, "spawn", s.UDID, "defaults", "write", accessibilityDomain, k, "-bool", strconv.FormatBool(found[i].value))
			}
			if err != nil {
				return err
			}
		}
		return nil
	}, nil
}

// defaultsBool reads one switch, absent meaning off.
func (s *Simctl) defaultsBool(ctx context.Context, key string) (value, present bool, err error) {
	out, err := s.Run(ctx, "spawn", s.UDID, "defaults", "read", accessibilityDomain, key)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return false, false, nil
		}
		return false, false, err
	}
	return strings.TrimSpace(string(out)) == "1", true, nil
}

// AccessibilitySetting reads one setting in the shared vocabulary.
func (s *Simctl) AccessibilitySetting(ctx context.Context, name string) (string, error) {
	if reason, ok := simulatorLacks[name]; ok {
		return "", mobiumerr.New(mobiumerr.Unsupported, "%s", reason)
	}
	switch name {
	case AXIncreaseContrast:
		out, err := s.Run(ctx, "ui", s.UDID, "increase_contrast")
		if err != nil {
			return "", err
		}
		return onOff(strings.TrimSpace(string(out)) == "enabled"), nil
	case AXTextSize:
		out, err := s.Run(ctx, "ui", s.UDID, "content_size")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	key, ok := iosAXKeys[name]
	if !ok {
		return "", unknownAXSetting(name)
	}
	v, _, err := s.defaultsBool(ctx, key)
	if err != nil {
		return "", err
	}
	return onOff(v), nil
}

// SetAccessibilitySetting changes one setting, confirms it by reading it
// back, and returns how to put it back.
func (s *Simctl) SetAccessibilitySetting(ctx context.Context, name, value string) (AXUndo, error) {
	if reason, ok := simulatorLacks[name]; ok {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "%s", reason)
	}
	var undo AXUndo
	var want string
	switch name {
	case AXIncreaseContrast:
		on, err := switchValue(name, value)
		if err != nil {
			return nil, err
		}
		was, err := s.AccessibilitySetting(ctx, name)
		if err != nil {
			return nil, err
		}
		set := func(ctx context.Context, v string) error {
			_, err := s.Run(ctx, "ui", s.UDID, "increase_contrast", map[string]string{AXOn: "enabled", AXOff: "disabled"}[v])
			return err
		}
		// simctl stores Increase Contrast in two keys, and turning it off
		// writes both as 0 where they had been absent: the setting reads the
		// same, the domain does not. They are put back raw after simctl has
		// switched it, which is what tells a running app.
		raw, err := s.snapshotDefaults(ctx, contrastKeys)
		if err != nil {
			return nil, err
		}
		undo = func(ctx context.Context) error {
			if err := set(ctx, was); err != nil {
				return err
			}
			return raw(ctx)
		}
		want = onOff(on)
		if err := set(ctx, want); err != nil {
			return undo, err
		}
	case AXTextSize:
		want = strings.ToLower(strings.TrimSpace(value))
		known := false
		for _, c := range IOSTextSizes {
			known = known || c == want
		}
		if !known {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "text_size is one of %s — got %q",
				strings.Join(IOSTextSizes, ", "), value)
		}
		was, err := s.AccessibilitySetting(ctx, name)
		if err != nil {
			return nil, err
		}
		undo = func(ctx context.Context) error {
			_, err := s.Run(ctx, "ui", s.UDID, "content_size", was)
			return err
		}
		if _, err := s.Run(ctx, "ui", s.UDID, "content_size", want); err != nil {
			return undo, err
		}
	default:
		key, ok := iosAXKeys[name]
		if !ok {
			return nil, unknownAXSetting(name)
		}
		on, err := switchValue(name, value)
		if err != nil {
			return nil, err
		}
		was, present, err := s.defaultsBool(ctx, key)
		if err != nil {
			return nil, err
		}
		undo = func(ctx context.Context) error {
			if !present {
				_, err := s.Run(ctx, "spawn", s.UDID, "defaults", "delete", accessibilityDomain, key)
				return err
			}
			_, err := s.Run(ctx, "spawn", s.UDID, "defaults", "write", accessibilityDomain, key, "-bool", strconv.FormatBool(was))
			return err
		}
		want = onOff(on)
		if _, err := s.Run(ctx, "spawn", s.UDID, "defaults", "write", accessibilityDomain, key, "-bool", strconv.FormatBool(on)); err != nil {
			return undo, err
		}
	}
	got, err := s.AccessibilitySetting(ctx, name)
	if err != nil {
		return undo, err
	}
	if !sameAXValue(name, got, want) {
		return undo, mobiumerr.New(mobiumerr.NotConfirmed, "asked for %s %s and the simulator reports %s", name, want, got)
	}
	return undo, nil
}

// sameAXValue compares a read-back with what was asked for: numerically for
// a scale, since Android may store 1.30 for 1.3.
func sameAXValue(name, got, want string) bool {
	if name == AXTextScale {
		g, e1 := strconv.ParseFloat(got, 64)
		w, e2 := strconv.ParseFloat(want, 64)
		return e1 == nil && e2 == nil && g == w
	}
	return got == want
}

func unknownAXSetting(name string) error {
	return mobiumerr.New(mobiumerr.InvalidArgument, "unknown accessibility setting %q — one of %s",
		name, strings.Join(AXSettings, ", "))
}
