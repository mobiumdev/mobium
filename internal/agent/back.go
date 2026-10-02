package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// gestureBack is back as the user's finger gives it: a swipe in from the left
// edge, which every device measured on 2026-10-01 took for back where the app
// handles it (docs/BACK.md). It is not the key — predictive back and an iOS
// navigation stack see a gesture, not a keycode — so it is asked for by name
// rather than sent in place of one.
//
// It returns where the swipe started, for the result to say.
func (h *Handlers) gestureBack(ctx context.Context, s *session, tree *uitree.Tree) (string, error) {
	gest, ok := mobiumdriver.AsGesturer(s.driver)
	if !ok {
		return "", cannot(s, mobiumdriver.CapGestures, "swipe back")
	}
	if tree == nil || tree.Root == nil || tree.Root.Bounds.Empty() {
		return "", mobiumerr.New(mobiumerr.DeviceNotReady, "the screen could not be read, so there is no edge to swipe from")
	}
	r := tree.Root.Bounds
	y, from := r.Y1+r.Height()/2, "the left edge, halfway down"

	if s.backend == BackendWDA {
		// A row with swipe actions takes a horizontal swipe for itself, and
		// on the iPhone a swipe from the edge across one did nothing. The
		// navigation bar has none, so the swipe starts there when there is
		// one; otherwise high on the screen, above where lists usually are.
		if bar := navigationBar(tree); bar != nil {
			y, from = bar.Bounds.Y1+bar.Bounds.Height()/2, "the left edge, in the navigation bar"
		} else {
			y, from = r.Y1+r.Height()/8, "the left edge, high on the screen — there is no navigation bar"
		}
	} else {
		adb, err := h.adbFor(ctx, s)
		if err != nil {
			return "", err
		}
		mode, err := adb.NavigationMode(ctx)
		if err != nil {
			return "", err
		}
		if mode != device.NavGestures {
			if mode == "" {
				mode = "an unknown"
			}
			return "", mobiumerr.New(mobiumerr.Unsupported, "this device uses %s navigation, where a swipe in "+
				"from the edge is not back — measured: it changed nothing, in an app or in Settings. Its "+
				"back button is the user's back: `mobium press back`, app_press back without gesture", mode).
				WithRemedy("app_press back without gesture: the back button is the user's back here").
				WithDetail("navigation", mode)
		}
	}
	if err := gest.Swipe(ctx, r.X1, y, r.X1+r.Width()*65/100, y, backSwipe); err != nil {
		return "", err
	}
	return from, nil
}

// backSwipe is the swipe's duration: what the measurements used.
const backSwipe = 250 * time.Millisecond

// backSettle and backBudget bound the wait for what a back did. Leaving an app
// handed focus to the next in about 0.1s on the API 35 AVD, four runs out of
// four, so a back that left is seen at once; a new screen in the same app
// counts once it has held still for backSettle; and a back that changed
// nothing is answered after backBudget. Opening a link waits longer, because
// a departing app's screen changes before the next app arrives — app_launch's
// one-second settle — and that made every back inside an app take 2.4s.
const (
	backSettle = 400 * time.Millisecond
	backBudget = 1500 * time.Millisecond
)

// awaitBack waits for the outcome of a back: the app in front afterwards.
func (h *Handlers) awaitBack(ctx context.Context, s *session, wasApp, wasScreen string) string {
	app := wasApp
	changed, changedAt := "", time.Time{}
	_ = pollUntil(ctx, backBudget, func(ctx context.Context) (bool, error) {
		tree, err := s.driver.Snapshot(ctx)
		if err != nil || tree == nil {
			return false, nil
		}
		now := tree.Package()
		if now != "" {
			app = now
		}
		if now != "" && now != wasApp {
			return true, nil
		}
		fp := fingerprint(tree.Root)
		if fp == wasScreen {
			changed = ""
			return false, nil
		}
		if fp != changed {
			changed, changedAt = fp, time.Now()
			return false, nil
		}
		return time.Since(changedAt) >= backSettle, nil
	})
	return app
}

// taskOwner names the app in front by its task where a browser draws it — an
// installed web app, a Trusted Web Activity, an app's custom tab — and
// otherwise returns the package it was given. Android only; elsewhere, and
// when the activities cannot be read, the package stands.
func (h *Handlers) taskOwner(ctx context.Context, s *session, pkg string) string {
	if pkg == "" || s.backend == BackendWDA {
		return pkg
	}
	adb, err := h.adbFor(ctx, s)
	if err != nil {
		return pkg
	}
	if hd, ok := adb.HostedInFront(ctx); ok && hd.Host == pkg {
		return hd.Owner
	}
	return pkg
}

// navigationBar is the iOS navigation bar on screen — the visible one, since a
// sheet leaves the bar beneath it in the tree, hidden.
func navigationBar(tree *uitree.Tree) *uitree.Node {
	for _, n := range tree.All() {
		if n.ShortClass() == "NavigationBar" && n.Displayed && !n.Bounds.Empty() {
			return n
		}
	}
	return nil
}

// navigationTitle is what the navigation bar says, which on iOS is the clearest
// sign of which screen of an app is up: a back that worked changes it.
func navigationTitle(tree *uitree.Tree) string {
	if tree == nil {
		return ""
	}
	if bar := navigationBar(tree); bar != nil {
		if bar.TestID != "" {
			return bar.TestID
		}
		return bar.Label
	}
	return ""
}

// backOutcome says what back did, from what was in front before and after:
// the app it left, or that the app is still in front — and on iOS, where back
// never leaves an app, what the navigation bar says now.
func backOutcome(how, beforeApp, afterApp, beforeTitle, afterTitle string) (string, bool) {
	switch {
	case afterApp == "":
		return fmt.Sprintf("%s — what is in front afterwards could not be read", how), false
	case beforeApp != "" && afterApp != beforeApp:
		return fmt.Sprintf("%s — it left %s; %s is in the foreground", how, beforeApp, afterApp), true
	case beforeTitle != afterTitle && afterTitle != "":
		return fmt.Sprintf("%s — %s is still in the foreground, and its navigation bar now says %q",
			how, afterApp, afterTitle), true
	case beforeTitle != "" && beforeTitle == afterTitle:
		return fmt.Sprintf("%s — %s is still in the foreground, and its navigation bar still says %q: "+
			"the app did not go back", how, afterApp, afterTitle), true
	}
	return fmt.Sprintf("%s — %s is still in the foreground; app_map shows which screen", how, afterApp), true
}
