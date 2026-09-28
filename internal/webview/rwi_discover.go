package webview

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
)

// iosSocketPattern matches the path launchd publishes for the simulator's
// Remote Web Inspector. It changes every boot, so it is discovered rather than
// hardcoded — the single most important fact in getting here at all.
var iosSocketPattern = regexp.MustCompile(`/private/var/tmp/\S*webinspectord_sim\.socket`)

// InspectorSocket asks a booted simulator's launchd where its Remote Web
// Inspector is listening.
//
// The host's own com.apple.webinspectord publishes Mach endpoints and no
// socket, which is where the belief that this was unreachable came from. The
// *simulator's* copy publishes RWI_LISTEN_SOCKET, an ordinary Unix socket.
func InspectorSocket(ctx context.Context, sim *device.Simctl) (string, error) {
	if p := os.Getenv("MOBIUM_RWI_SOCKET"); p != "" {
		return p, nil
	}
	out, err := sim.Run(ctx, "spawn", sim.UDID, "launchctl", "print", "system/com.apple.webinspectord")
	if err != nil {
		return "", fmt.Errorf("could not ask the simulator where its web inspector is listening: %w", err)
	}
	match := iosSocketPattern.FindString(string(out))
	if match == "" {
		return "", mobiumerr.New(mobiumerr.DeviceNotReady, "the simulator published no RWI_LISTEN_SOCKET, so no WebView can be "+
			"inspected. Web Inspector must be enabled for the app — for Safari:\n"+
			"  xcrun simctl spawn booted defaults write com.apple.mobilesafari "+
			"WebKitDeveloperExtrasEnabledPreferenceKey -bool true")
	}
	return match, nil
}

// iosPage is one inspectable page, plus the two identifiers needed to talk to
// it. Both are opaque strings from WebKit and are meaningless outside the
// connection that learned them.
type iosPage struct {
	App    string // WIRApplicationIdentifierKey
	Page   any    // WIRPageIdentifierKey — a number, kept as sent
	Title  string
	URL    string
	Bundle string
	Behind bool // its application is not in front; see behind
}

// appActive is WIRIsApplicationActiveKey's value for the application in
// front. Measured on an iPhone 15 Plus, iOS 26.6.2: 2 in front, 1 while
// leaving or arriving, 0 in the background — and Safari, left for an app,
// went from 2 to 1 and stayed there, its page still published.
const appActive = 2

// appState is what webinspectord says about an application beyond its
// bundle: whether it is in front, and for a proxy — an in-app browser, whose
// pages another process draws — the application hosting it.
type appState struct {
	active int
	known  bool // the application reported an active flag at all
	host   string
}

// statesIn reads the active flag and host out of either message shape, as
// applicationsIn reads the bundle.
func statesIn(arg map[string]any) map[string]appState {
	out := map[string]appState{}
	add := func(id string, info map[string]any) {
		var st appState
		st.active, st.known = plistInt(info["WIRIsApplicationActiveKey"])
		st.host, _ = info["WIRHostApplicationIdentifierKey"].(string)
		out[id] = st
	}
	if apps, ok := arg["WIRApplicationDictionaryKey"].(map[string]any); ok {
		for id, v := range apps {
			if info, ok := v.(map[string]any); ok {
				add(id, info)
			}
		}
	}
	if id, ok := arg["WIRApplicationIdentifierKey"].(string); ok {
		add(id, arg)
	}
	return out
}

func plistInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		return int(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// behind reports whether webinspectord says an application is not the one in
// front: it reported an active flag, the flag is not appActive, and it is not
// hosted by the application that is. Only once something is in front at all —
// mid-switch every application can read 1, and an application that reports
// no flag is not hidden on a guess. The host rule is WebKit's relation for an
// in-app browser; it has not been driven on a device here. CHALLENGES 138.
func behind(id string, states map[string]appState) bool {
	front := false
	for _, st := range states {
		if st.known && st.active == appActive {
			front = true
			break
		}
	}
	st, ok := states[id]
	if !front || !ok || !st.known || st.active == appActive {
		return false
	}
	if h, ok := states[st.host]; ok && h.known && h.active == appActive {
		return false
	}
	return true
}

// announceAndLearnApps introduces us and waits for the application list.
//
// webinspectord sends `_rpc_reportConnectedApplicationList:` **once per
// connection**, right after the announce. Re-announcing on a connection that
// has already done this produces nothing, so the app set is learned once and
// kept.
func (in *inspector) announceAndLearnApps(ctx context.Context) (map[string]string, error) {
	msgs, unsubscribe := in.subscribe()
	defer unsubscribe()

	if err := in.announce(); err != nil {
		return nil, err
	}

	bundles := map[string]string{}
	// The first message is not prompt: on a second connection to the same
	// socket webinspectord waits ten seconds before saying anything, so the
	// quiet window cannot start until something arrives. A nil channel blocks
	// forever, which is how that is expressed.
	const quiet = 600 * time.Millisecond
	deadline := time.After(rwiSetupTimeout)
	idle := time.NewTimer(quiet)
	if !idle.Stop() {
		<-idle.C
	}
	var idleC <-chan time.Time
	defer idle.Stop()

	for {
		select {
		case <-ctx.Done():
			return bundles, ctx.Err()
		case <-deadline:
			if len(bundles) == 0 {
				return nil, mobiumerr.New(mobiumerr.NoSuchContext, "the simulator's web inspector never listed any "+
					"applications; nothing on it is inspectable")
			}
			return bundles, nil
		case <-idleC:
			return bundles, nil
		case msg, ok := <-msgs:
			if !ok {
				return bundles, in.failure("learning which applications are inspectable")
			}
			switch msg.Selector {
			case "_rpc_reportConnectedApplicationList:", "_rpc_applicationConnected:":
			default:
				continue
			}
			for id, bundle := range applicationsIn(msg.Argument) {
				bundles[id] = bundle
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(quiet)
			idleC = idle.C
		}
	}
}

// listPages asks every known application what pages it has.
//
// Unlike the announce, this *is* close to a request and reply: one
// `_rpc_forwardGetListing:` per application, one `_rpc_applicationSentListing:`
// back. It still is not quite, because the replies are separate messages that
// may interleave with unrelated chatter, so they are matched by application id
// rather than by arrival order.
//
// Waiting for every application stalled whenever one never answers. Changing
// an accessibility setting on an iPhone brought com.apple.AppStore.Widgets
// into the set, and it answers no listing: every app_contexts then took the
// whole setup timeout, and an attach twice that, while the six other
// applications had answered in no measurable time (CHALLENGES 116). So the
// first answer may take the setup timeout, since a cold WebView can be slow,
// but once one has come the rest get listingGrace from the last; and an
// application in quiet — one that stayed silent before — is still asked and
// not waited for. What did not answer is returned, for the caller to remember.
func (in *inspector) listPages(ctx context.Context, apps map[string]string, quiet map[string]bool) ([]iosPage, []string, error) {
	msgs, unsubscribe := in.subscribe()
	defer unsubscribe()

	for id := range apps {
		if err := in.send("_rpc_forwardGetListing:", map[string]any{
			"WIRConnectionIdentifierKey":  in.id,
			"WIRApplicationIdentifierKey": id,
		}); err != nil {
			return nil, nil, err
		}
	}
	if len(apps) == 0 {
		return nil, nil, nil
	}

	answered := map[string]bool{}
	seen := map[string]bool{}
	var pages []iosPage
	unanswered := func() []string {
		var out []string
		for id := range apps {
			if !answered[id] {
				out = append(out, id)
			}
		}
		sort.Strings(out)
		return out
	}
	// done is every application not known to be silent having answered.
	done := func() bool {
		for id := range apps {
			if !answered[id] && !quiet[id] {
				return false
			}
		}
		return true
	}

	deadline := time.After(rwiSetupTimeout)
	var graceC <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return pages, unanswered(), ctx.Err()
		case <-deadline:
			return pages, unanswered(), nil
		case <-graceC:
			return pages, unanswered(), nil
		case msg, ok := <-msgs:
			if !ok {
				return pages, unanswered(), in.failure("listing pages")
			}
			if msg.Selector != "_rpc_applicationSentListing:" {
				continue
			}
			app, _ := msg.Argument["WIRApplicationIdentifierKey"].(string)
			if _, known := apps[app]; !known {
				continue
			}
			answered[app] = true
			// The WebContent process answers with an empty listing, so an
			// empty one means that application has no pages — not that the
			// conversation is unfinished.
			listing, _ := msg.Argument["WIRListingKey"].(map[string]any)
			for _, v := range listing {
				entry, _ := v.(map[string]any)
				if entry == nil || entry["WIRPageIdentifierKey"] == nil {
					continue
				}
				p := iosPage{App: app, Page: entry["WIRPageIdentifierKey"], Bundle: apps[app]}
				p.Title, _ = entry["WIRTitleKey"].(string)
				p.URL, _ = entry["WIRURLKey"].(string)
				key := fmt.Sprintf("%s/%v", p.App, p.Page)
				if seen[key] {
					continue
				}
				seen[key] = true
				pages = append(pages, p)
			}
			if done() {
				return pages, unanswered(), nil
			}
			graceC = time.After(listingGrace)
		}
	}
}

// applicationsIn reads the application dictionary out of either message shape
// webinspectord uses to report them.
func applicationsIn(arg map[string]any) map[string]string {
	out := map[string]string{}
	add := func(id string, info map[string]any) {
		bundle, _ := info["WIRApplicationBundleIdentifierKey"].(string)
		out[id] = bundle
	}
	if apps, ok := arg["WIRApplicationDictionaryKey"].(map[string]any); ok {
		for id, v := range apps {
			if info, ok := v.(map[string]any); ok {
				add(id, info)
			}
		}
	}
	// _rpc_applicationConnected: reports a single application inline.
	if id, ok := arg["WIRApplicationIdentifierKey"].(string); ok {
		add(id, arg)
	}
	return out
}

// failure explains why the connection stopped, quoting the read error when
// there is one.
func (in *inspector) failure(doing string) error {
	if in.readErr != nil {
		return fmt.Errorf("the web inspector connection ended while %s: %w", doing, in.readErr)
	}
	return mobiumerr.New(mobiumerr.DeviceServer, "the web inspector closed the connection while %s", doing)
}

// Inspector is a live connection to a simulator's Remote Web Inspector, held
// open for the life of a session.
//
// It is held rather than reopened because **webinspectord answers only the
// first connection promptly**. Measured on an iPhone 17 Pro simulator: the
// first connection receives its first byte in 0.17s, and every connection
// after it waits 10.2 seconds before webinspectord says anything at all —
// consistently, to the tenth of a second. Reopening per command would put a
// ten-second floor under `app_contexts` and every context switch.
//
// **And the application set is kept current for as long as it is held.**
// webinspectord announces an application once, when it connects, and a
// relaunched app is a new process with a new identifier. Learning the set
// only at setup kept asking the old process for pages after an app was
// terminated and launched again, so its WebView disappeared from
// `app_contexts` until the daemon restarted — found by a check that relaunches
// MobiumApp between sections, on an iPhone 17 Pro simulator. CHALLENGES 83.
//
// **And an application that never answers a listing is remembered**, so it
// is asked each time and not waited for (CHALLENGES 116).
type Inspector struct {
	in *inspector

	mu     sync.Mutex
	apps   map[string]string
	states map[string]appState
	silent map[string]bool
	// heard counts each application's listings as the watch hears them, so a
	// listing can tell whether one that missed it answered afterwards.
	heard map[string]int
}

// openWith announces on a fresh connection, learns the applications, and
// keeps learning them. The watch is subscribed before the announce, so nothing
// said in between is missed; the announce's own messages arrive on it too, and
// adding an application twice is harmless.
func openWith(ctx context.Context, in *inspector) (*Inspector, error) {
	msgs, unsubscribe := in.subscribe()
	apps, err := in.announceAndLearnApps(ctx)
	if err != nil {
		unsubscribe()
		in.Close()
		return nil, err
	}
	i := &Inspector{in: in, apps: apps, silent: map[string]bool{}}
	go i.watch(msgs, unsubscribe)
	return i, nil
}

// watch applies webinspectord's application announcements to the set until
// the connection ends, which closes msgs.
func (i *Inspector) watch(msgs <-chan rwiMessage, unsubscribe func()) {
	defer unsubscribe()
	for msg := range msgs {
		switch msg.Selector {
		case "_rpc_reportConnectedApplicationList:", "_rpc_applicationConnected:", "_rpc_applicationUpdated:":
			i.mu.Lock()
			for id, bundle := range applicationsIn(msg.Argument) {
				i.apps[id] = bundle
			}
			if i.states == nil {
				i.states = map[string]appState{}
			}
			for id, st := range statesIn(msg.Argument) {
				i.states[id] = st
			}
			i.mu.Unlock()
		case "_rpc_applicationSentListing:":
			// An application marked silent that answers after all — too late
			// for the listing that asked, which had stopped waiting for it —
			// is waited for again from the next one. Without this, one that
			// merely answered late stayed silent and its pages were never
			// seen again.
			if id, ok := msg.Argument["WIRApplicationIdentifierKey"].(string); ok {
				i.mu.Lock()
				delete(i.silent, id)
				if i.heard == nil {
					i.heard = map[string]int{}
				}
				i.heard[id]++
				i.mu.Unlock()
			}
		case "_rpc_applicationDisconnected:":
			if id, ok := msg.Argument["WIRApplicationIdentifierKey"].(string); ok {
				i.mu.Lock()
				delete(i.apps, id)
				delete(i.states, id)
				delete(i.silent, id)
				i.mu.Unlock()
			}
		}
	}
}

// knownApps is a copy of the current set, and of which of them are silent,
// so a listing is not raced by an announcement arriving in the middle of it.
func (i *Inspector) knownApps() (map[string]string, map[string]bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	apps := make(map[string]string, len(i.apps))
	for k, v := range i.apps {
		apps[k] = v
	}
	quiet := make(map[string]bool, len(i.silent))
	for k := range i.silent {
		quiet[k] = true
	}
	return apps, quiet
}

// list asks every known application for its pages, then records which of
// them stayed silent: marked when one misses a listing, cleared the moment it
// answers one.
//
// An answer the watch heard after the listing stopped waiting is an answer:
// marking the application silent regardless undid the watch's clearing of it
// whenever the reply landed between the two, and an application that always
// answered just late stayed silent. Found as a test that failed one run in
// five on a busy machine.
func (i *Inspector) list(ctx context.Context) ([]iosPage, error) {
	apps, quiet := i.knownApps()
	i.mu.Lock()
	before := make(map[string]int, len(i.heard))
	for id, n := range i.heard {
		before[id] = n
	}
	i.mu.Unlock()
	pages, unanswered, err := i.in.listPages(ctx, apps, quiet)
	missed := map[string]bool{}
	for _, id := range unanswered {
		missed[id] = true
	}
	i.mu.Lock()
	for id := range apps {
		if missed[id] && i.heard[id] == before[id] {
			i.silent[id] = true
		} else {
			delete(i.silent, id)
		}
	}
	// Read at the end, so a switch heard during the listing counts.
	for n := range pages {
		pages[n].Behind = behind(pages[n].App, i.states)
	}
	i.mu.Unlock()
	return pages, err
}

// OpenInspector connects to a booted simulator's web inspector.
func OpenInspector(ctx context.Context, sim *device.Simctl) (*Inspector, error) {
	socket, err := InspectorSocket(ctx, sim)
	if err != nil {
		return nil, err
	}
	in, err := dialInspector(socket, connectionID())
	if err != nil {
		return nil, err
	}
	return openWith(ctx, in)
}

// OpenPhoneInspector connects to a real iPhone's web inspector, through
// usbmuxd and lockdown (see internal/device/lockdown.go), and from there on
// is exactly the simulator's Inspector.
func OpenPhoneInspector(ctx context.Context, udid string) (*Inspector, error) {
	conn, err := device.WebInspectorConn(ctx, udid)
	if err != nil {
		return nil, fmt.Errorf("could not reach the iPhone's web inspector: %w", err)
	}
	return openWith(ctx, newInspector(conn, connectionID()))
}

// Close drops the connection.
func (i *Inspector) Close() error { return i.in.Close() }

// Contexts lists every inspectable page, in the shape app_contexts uses.
func (i *Inspector) Contexts(ctx context.Context) ([]Context, error) {
	pages, err := i.list(ctx)
	if err != nil {
		return nil, err
	}
	return contextsFromPages(pages), nil
}

// Pages is Contexts without the presentation, for Attach.
func (i *Inspector) Pages(ctx context.Context) ([]iosPage, error) {
	return i.list(ctx)
}

func contextsFromPages(pages []iosPage) []Context {

	var out []Context
	for _, p := range pages {
		bundle := p.Bundle
		if bundle == "" {
			bundle = "webkit"
		}
		out = append(out, Context{
			ID:     fmt.Sprintf("WEBVIEW_%s", bundle),
			Base:   fmt.Sprintf("WEBVIEW_%s", bundle),
			Title:  p.Title,
			URL:    p.URL,
			App:    p.Bundle,
			Behind: p.Behind,
			// Reused to carry the identifiers back to Attach, which is what
			// they are for: they mean nothing outside this connection.
			Socket: fmt.Sprintf("%s/%v", p.App, p.Page),
		})
	}
	// Several pages in one app would otherwise collide on ID; number them
	// only when they actually do, so the common case stays readable.
	byID := map[string]int{}
	for _, c := range out {
		byID[c.ID]++
	}
	nth := map[string]int{}
	for i := range out {
		if byID[out[i].ID] > 1 {
			nth[out[i].ID]++
			out[i].ID = fmt.Sprintf("%s_%d", out[i].ID, nth[out[i].ID])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// connectionID identifies us to webinspectord. It shows up in Safari's develop
// menu, so it says who is driving.
func connectionID() string {
	return fmt.Sprintf("mobium-%d", os.Getpid())
}
