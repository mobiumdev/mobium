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
	if w.setActiveAppHint(ctx, app) != nil {
		return nil, false
	}
	xml, err := w.w3c.source(ctx)
	w.hintMu.Lock()
	restore := w.expecting
	w.hintMu.Unlock()
	if restore == "" {
		restore = "auto"
	}
	_ = w.setActiveAppHint(ctx, restore)
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
