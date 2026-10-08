package mobiumdriver

import (
	"context"
	"strings"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// bannerOver reads, in a hierarchy that came back as SpringBoard's, whether
// that is only because a notification banner is showing over an app. It
// returns the app's bundle id and the banner, or "" when SpringBoard is
// really what is in front.
//
// Measured on the iPhone 17 Pro simulator (CHALLENGES 155): for five to eight
// seconds after an app posts a notification while in front, WebDriverAgent
// reads SpringBoard, whose tree holds the banner — a
// `NotificationShortLookView`, visible until it starts to slide away and
// still there while it does — and the app only as a hidden
// `card:<bundle id>:sceneID:…`, while the screen shows the app with a banner
// across its top.
func bannerOver(tree *uitree.Tree) (string, *uitree.Node) {
	if tree == nil || tree.Package() != springboardBundleID {
		return "", nil
	}
	var banner *uitree.Node
	var app string
	tree.Walk(func(n *uitree.Node) bool {
		if banner == nil && n.TestID == "NotificationShortLookView" {
			banner = n
		}
		if app == "" && strings.HasPrefix(n.TestID, "card:") {
			if id, _, ok := strings.Cut(strings.TrimPrefix(n.TestID, "card:"), ":"); ok && id != "" && id != springboardBundleID {
				app = id
			}
		}
		return true
	})
	if banner == nil || app == "" {
		return "", nil
	}
	return app, banner
}

// underBanner reads the app a banner is over, pinned to it for the one read,
// and lays the banner over it as the last thing drawn, so an action aims
// around it, waits for it, or is refused as covered — as for anything else
// drawn over a target (CHALLENGES 115). The hint goes back to what it was.
func (w *WDA) underBanner(ctx context.Context, app string, banner *uitree.Node) (*uitree.Tree, bool) {
	var xml string
	err := w.asApp(ctx, app, func() (err error) {
		xml, err = w.w3c.source(ctx)
		return err
	})
	if err != nil {
		return nil, false
	}
	tree, err := uitree.ParseIOS([]byte(xml))
	if err != nil || tree.Package() != app || tree.Root == nil {
		return nil, false
	}
	// Detached from SpringBoard's tree and hung last on the app's root, so it
	// is drawn over everything; its label carries what the banner says.
	banner.Parent = tree.Root
	banner.Clickable = true
	// Still on screen while it slides away, when SpringBoard already calls
	// it not visible; a tap meant for what it covers would land on it.
	banner.Displayed = true
	if banner.Label == "" {
		banner.Label = "notification banner"
	}
	tree.Root.Children = append(tree.Root.Children, banner)
	return tree, true
}

// asApp runs fn with WebDriverAgent told that app is the active one, and puts
// back what it was told before. A banner makes it believe SpringBoard is
// active, and then both reads and element lookups search SpringBoard.
func (w *WDA) asApp(ctx context.Context, app string, fn func() error) error {
	if err := w.setActiveAppHint(ctx, app); err != nil {
		return err
	}
	w.hintMu.Lock()
	restore := w.expecting
	if restore == "" && w.shadeHint {
		restore = springboardBundleID
	}
	w.hintMu.Unlock()
	if restore == "" {
		restore = "auto"
	}
	defer func() { _ = w.setActiveAppHint(ctx, restore) }()
	return fn()
}

// appOf names the app a node was read from, or "" for a node built by hand.
func appOf(n *uitree.Node) string {
	for ; n != nil; n = n.Parent {
		if n.Package != "" {
			return n.Package
		}
	}
	return ""
}

// appUnderBanner is the app a notification banner is showing over, read from
// what WebDriverAgent reads now, or "" when no banner is the reason it reads
// SpringBoard.
func (w *WDA) appUnderBanner(ctx context.Context) string {
	xml, err := w.w3c.source(ctx)
	if err != nil {
		return ""
	}
	tree, err := uitree.ParseIOS([]byte(xml))
	if err != nil {
		return ""
	}
	app, _ := bannerOver(tree)
	return app
}
