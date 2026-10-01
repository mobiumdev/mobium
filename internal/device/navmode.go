package device

import (
	"context"
	"strings"
)

// Android's navigation modes, as `settings get secure navigation_mode` stores
// them. The mode decides what an edge swipe is: back in gesture navigation,
// and nothing at all with buttons, where the measured swipe in from either
// edge changed nothing in MobiumApp or in Settings. docs/BACK.md.
const (
	NavGestures    = "gestures"
	NavThreeButton = "three-button"
	NavTwoButton   = "two-button"
)

// NavigationMode reads how the device is navigated, or "" when the setting
// says nothing this knows: unset reads "null", and a value added later reads
// as itself, neither of which is a mode to act on.
func (a *ADB) NavigationMode(ctx context.Context) (string, error) {
	out, err := a.Shell(ctx, "settings", "get", "secure", "navigation_mode")
	if err != nil {
		return "", err
	}
	return navigationMode(string(out)), nil
}

func navigationMode(v string) string {
	switch strings.TrimSpace(v) {
	case "0":
		return NavThreeButton
	case "1":
		return NavTwoButton
	case "2":
		return NavGestures
	}
	return ""
}
