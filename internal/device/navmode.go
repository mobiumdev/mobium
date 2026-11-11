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
	// NavCar is a car's: its system bars and a back button laid over the
	// app. navigation_mode reads 0 there, which is no three buttons, and a
	// swipe in from the left edge changed nothing (CHALLENGES 403).
	NavCar = "car bars"
)

// NavigationMode reads how the device is navigated, or "" when the setting
// says nothing this knows: unset reads "null", and a value added later reads
// as itself, neither of which is a mode to act on.
func (a *ADB) NavigationMode(ctx context.Context) (string, error) {
	if car, err := a.Automotive(ctx); err != nil {
		return "", err
	} else if car {
		return NavCar, nil
	}
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
