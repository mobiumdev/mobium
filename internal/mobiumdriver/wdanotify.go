package mobiumdriver

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// Notifications on an iOS simulator, through Notification Center.
//
// iOS has no list of what is posted that can be asked from outside, as
// Android's dumpsys is; what there is is Notification Center, which is
// SpringBoard's, drawn over the app in front. WebDriverAgent goes on reading
// the app under it, so it is read as SpringBoard. Measured on an iPhone 17
// Pro simulator, iOS 26.5: a notification is a NotificationShortLookView
// whose label starts with the app's name in capitals, holding a StaticText
// NotificationTitle and, under NotificationBody, a TextContent.Primary; and
// SBCoverSheetWindow is in SpringBoard's tree only while Notification Center
// is open, which is how opening and closing it are confirmed.
//
// A swipe down from the top edge opens it and a swipe up from the bottom
// edge closes it, back to the app that was in front. A notification is
// posted with simctl push, as an app — the one in front, since iOS has no
// shell to post as — and shows only if that app may notify. Its tag is the
// thread iOS groups it under.
//
// Simulator only. A real iPhone's Notification Center is its owner's, and
// reading it is not built.

const coverSheet = "SBCoverSheetWindow"

// shadeWait bounds the wait for Notification Center to open or close.
const shadeWait = 3 * time.Second

// springboardTree reads SpringBoard rather than the app it is drawn over.
func (w *WDA) springboardTree(ctx context.Context) (*uitree.Tree, string, error) {
	var xml string
	err := w.asApp(ctx, springboardBundleID, func() (err error) {
		xml, err = w.w3c.source(ctx)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	t, err := uitree.ParseIOS([]byte(xml))
	return t, xml, err
}

func (w *WDA) shadeIsOpen(ctx context.Context) (bool, *uitree.Tree, error) {
	t, xml, err := w.springboardTree(ctx)
	if err != nil {
		return false, nil, err
	}
	return strings.Contains(xml, `name="`+coverSheet+`"`), t, nil
}

// SetShade opens or closes Notification Center and confirms it did.
func (w *WDA) SetShade(ctx context.Context, open bool) error {
	if err := w.simOnly(CapNotifications); err != nil {
		return err
	}
	is, _, err := w.shadeIsOpen(ctx)
	if err != nil {
		return err
	}
	if is == open {
		w.pointReadsAtShade(ctx, open)
		return nil
	}
	size, err := w.windowSize(ctx)
	if err != nil {
		return err
	}
	x := size.Width / 2
	from, to := 1, size.Height*2/3
	if !open {
		from, to = size.Height-2, size.Height/3
	}
	if err := w.w3c.pointerSequence(ctx, dragActions(x, from, x, to, 400*time.Millisecond)); err != nil {
		return err
	}
	deadline := time.Now().Add(shadeWait)
	for time.Now().Before(deadline) {
		if is, _, err := w.shadeIsOpen(ctx); err == nil && is == open {
			w.pointReadsAtShade(ctx, open)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	state := "open"
	if !open {
		state = "closed"
	}
	return mobiumerr.New(mobiumerr.NotConfirmed, "swiped to leave Notification Center %s, and it is not", state)
}

// Notifications lists what Notification Center shows, and any banner on
// screen. A banner is in SpringBoard's tree as a notification is, and it
// sits where the swipe that opens Notification Center starts, so the swipe
// lands on it instead: a banner is read where it is, its leaving is waited
// for, and then Notification Center is opened for the read, and closed after
// if it was closed.
func (w *WDA) Notifications(ctx context.Context) ([]device.Notification, error) {
	if err := w.simOnly(CapNotifications); err != nil {
		return nil, err
	}
	names := w.appNames(ctx)
	was, t, err := w.shadeIsOpen(ctx)
	if err != nil {
		return nil, err
	}
	if was {
		return notificationsIn(t, names), nil
	}
	seen := notificationsIn(t, names)
	deadline := time.Now().Add(bannerWait)
	for len(notificationsIn(t, names)) > 0 && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if _, t, err = w.shadeIsOpen(ctx); err != nil {
			return nil, err
		}
	}
	if err := w.SetShade(ctx, true); err != nil {
		return nil, err
	}
	defer func() { _ = w.SetShade(ctx, false) }()
	if _, t, err = w.shadeIsOpen(ctx); err != nil {
		return nil, err
	}
	out := notificationsIn(t, names)
	have := map[string]bool{}
	for _, n := range out {
		have[n.Key] = true
	}
	for _, n := range seen {
		if !have[n.Key] {
			out = append(out, n)
		}
	}
	return out, nil
}

// bannerWait bounds the wait for a banner to leave the screen; iOS takes it
// away after about five seconds.
const bannerWait = 8 * time.Second

// notificationsIn reads the notifications a SpringBoard tree shows, in
// Notification Center or as a banner.
func notificationsIn(t *uitree.Tree, names map[string]string) []device.Notification {
	var out []device.Notification
	t.Walk(func(n *uitree.Node) bool {
		if n.TestID != "NotificationShortLookView" {
			return true
		}
		note := device.Notification{Package: notificationApp(n.Label, names)}
		walkUnder(n, func(c *uitree.Node) {
			switch c.TestID {
			case "NotificationTitle":
				note.Title = c.Text
			case "TextContent.Primary":
				note.Text = c.Text
			}
		})
		note.Key = note.Package + "|" + note.Title + "|" + note.Text
		out = append(out, note)
		// true: Walk stops altogether on false, and a group expanded holds
		// one of these per notification.
		return true
	})
	return out
}

// notificationApp turns the name a notification shows — the app's display
// name in capitals, before the first comma of its label — into its bundle
// id, or keeps the name when no installed app has it.
func notificationApp(label string, names map[string]string) string {
	name, _, _ := strings.Cut(label, ", ")
	if id, ok := names[strings.ToLower(name)]; ok {
		return id
	}
	return name
}

// appNames maps each installed app's display name, lowercased, to its id.
func (w *WDA) appNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	apps, err := w.ListApps(ctx, true)
	if err != nil {
		return out
	}
	for _, a := range apps {
		if a.Name != "" {
			out[strings.ToLower(a.Name)] = a.ID
		}
	}
	return out
}

func walkUnder(n *uitree.Node, fn func(*uitree.Node)) {
	for _, c := range n.Children {
		fn(c)
		walkUnder(c, fn)
	}
}

// PostNotification posts as the app in front, with simctl push, and confirms
// it by finding it in Notification Center.
func (w *WDA) PostNotification(ctx context.Context, tag, title, text string) error {
	if err := w.simOnly(CapNotifications); err != nil {
		return err
	}
	front, err := w.Snapshot(ctx)
	if err != nil {
		return err
	}
	app := front.Package()
	if app == "" || app == springboardBundleID {
		return mobiumerr.New(mobiumerr.InvalidArgument, "a notification on iOS is posted as an app, the one in "+
			"front, and the home screen is in front — launch the app it should come from first")
	}
	// The tag is the thread iOS groups a notification under, the nearest it
	// has to Android's tag; iOS has no way to replace one by it.
	aps := map[string]interface{}{"alert": map[string]string{"title": title, "body": text}}
	if tag != "" {
		aps["thread-id"] = tag
	}
	payload, _ := json.Marshal(map[string]interface{}{"aps": aps})
	dir, err := os.MkdirTemp("", "mobium-push-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "push.json")
	if err := os.WriteFile(file, payload, 0o600); err != nil {
		return err
	}
	if _, err := w.sim.Run(ctx, "push", w.sim.UDID, app, file); err != nil {
		return err
	}
	// Its banner is what confirms it: it comes within a second, and reading
	// it needs no swipe.
	names := w.appNames(ctx)
	deadline := time.Now().Add(shadeWait)
	for time.Now().Before(deadline) {
		if t, _, err := w.springboardTree(ctx); err == nil {
			for _, n := range notificationsIn(t, names) {
				if n.Package == app && n.Title == title && n.Text == text {
					return nil
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return mobiumerr.New(mobiumerr.NotConfirmed, "posted %q as %s and it is not in Notification Center — "+
		"the app may not be allowed to notify, which it asks for itself and Settings > Notifications shows",
		title, app)
}

// windowSize is the screen in points, as WebDriverAgent's gestures take it.
func (w *WDA) windowSize(ctx context.Context) (struct{ Width, Height int }, error) {
	var resp struct {
		Value struct {
			Width, Height float64
		} `json:"value"`
	}
	var out struct{ Width, Height int }
	if err := w.w3c.do(ctx, http.MethodGet, w.w3c.sessionPath("/window/size"), nil, &resp); err != nil {
		return out, err
	}
	out.Width, out.Height = int(resp.Value.Width), int(resp.Value.Height)
	return out, nil
}

// pointReadsAtShade points every read at SpringBoard while Notification
// Center is open, so map shows it and what it holds can be tapped, as on
// Android; WebDriverAgent otherwise goes on reading the app under it.
func (w *WDA) pointReadsAtShade(ctx context.Context, open bool) {
	app := "auto"
	if open {
		app = springboardBundleID
	}
	if w.setActiveAppHint(ctx, app) == nil {
		w.hintMu.Lock()
		w.shadeHint = open
		w.hintMu.Unlock()
	}
}

// dropShadeHint takes reads off SpringBoard once Notification Center, opened
// here, is no longer in what was read, and says whether it did.
func (w *WDA) dropShadeHint(ctx context.Context, xml string) bool {
	w.hintMu.Lock()
	hinted := w.shadeHint
	w.hintMu.Unlock()
	if !hinted || strings.Contains(xml, `name="`+coverSheet+`"`) {
		return false
	}
	w.pointReadsAtShade(ctx, false)
	return true
}
