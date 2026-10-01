package device

import (
	"bufio"
	"context"
	"regexp"
	"strings"
)

// An app's state, in the words app_state reports. The same four on both
// platforms; iOS also says whether a background app is suspended.
const (
	AppNotInstalled = "not_installed"
	AppNotRunning   = "not_running"
	AppBackground   = "background"
	AppForeground   = "foreground"
)

// AppState is one app's state as the device reports it.
type AppState struct {
	State string
	// Suspended is, for an app in the background, whether it is suspended —
	// iOS says; Android has nothing that does, and leaves it nil rather than
	// guessing.
	Suspended *bool
}

// AppState reads one app's state from the package manager, the process
// table and the activity manager's tasks.
//
// In front means the app owns the task in front, not that its window has
// focus: a permission prompt is the permission controller's activity, run in
// the asking app's task, and the app is still what the person is using —
// measured with MobiumApp's camera prompt, where the resumed activity was
// the controller's and the task was MobiumApp's. iOS agrees: an app under
// its own permission alert reads as running in the foreground.
func (a *ADB) AppState(ctx context.Context, pkg string) (AppState, error) {
	// Both exit 1 when there is nothing to report, which is an answer, not
	// a failure.
	path, err := a.Shell(ctx, "pm path "+shellQuote(pkg)+" || true")
	if err != nil {
		return AppState{}, err
	}
	if !strings.Contains(string(path), "package:") {
		return AppState{State: AppNotInstalled}, nil
	}
	pid, err := a.Shell(ctx, "pidof "+shellQuote(pkg)+" || true")
	if err != nil {
		return AppState{}, err
	}
	if strings.TrimSpace(string(pid)) == "" {
		return AppState{State: AppNotRunning}, nil
	}
	acts, err := a.Shell(ctx, "dumpsys", "activity", "activities")
	if err != nil {
		return AppState{}, err
	}
	if top, root := taskInFront(string(acts)); pkg == top || pkg == root {
		return AppState{State: AppForeground}, nil
	}
	return AppState{State: AppBackground}, nil
}

// Hosted is an app whose screen a browser draws, in the app's own task: an
// installed web app (a WebAPK) or a Trusted Web Activity, and any app that
// opened a page in a browser's custom tab. The browser owns the windows, so
// the hierarchy and every foreground read name it, never the app — a launch
// of either was reported as the browser in front, and a back out of one as
// leaving the browser (CHALLENGES 202, 205).
type Hosted struct {
	// Owner is the app the task belongs to; Host is the browser drawing it.
	Owner, Host string
	// Kind is "installed web app", "Trusted Web Activity" or "app in a
	// custom tab", the last an ordinary app that opened a page in one.
	Kind string
}

// HostedInFront reads whether the task in front is an app a browser draws.
func (a *ADB) HostedInFront(ctx context.Context) (Hosted, bool) {
	acts, err := a.Shell(ctx, "dumpsys", "activity", "activities")
	if err != nil {
		return Hosted{}, false
	}
	return hostedInFront(string(acts))
}

// hostedInFront is the reading. Only a browser's own activities count — a
// WebApkActivity or a CustomTabActivity on top — because a permission prompt
// is also another package over an app's task, and that is something else in
// front. A Trusted Web Activity is a custom tab whose task is rooted in the
// TWA library's launcher: Bubblewrap's, androidbrowserhelper.
func hostedInFront(dump string) (Hosted, bool) {
	m := topActivityRe.FindStringSubmatch(dump)
	if m == nil {
		return Hosted{}, false
	}
	top, class := m[1], m[2]
	kind := ""
	switch {
	case strings.HasSuffix(class, "WebApkActivity"):
		kind = "installed web app"
	case strings.HasSuffix(class, "CustomTabActivity"):
		kind = "app in a custom tab"
	default:
		return Hosted{}, false
	}
	root, rootClass := taskRoot(dump)
	if root == "" || root == top {
		return Hosted{}, false
	}
	if kind == "app in a custom tab" && strings.Contains(rootClass, "androidbrowserhelper.trusted") {
		kind = "Trusted Web Activity"
	}
	return Hosted{Owner: root, Host: top, Kind: kind}, true
}

var (
	topActivityRe = regexp.MustCompile(`topResumedActivity=ActivityRecord\{\S+ u\d+ ([^/\s]+)/(\S+) t\d+`)
	topResumedRe  = regexp.MustCompile(`topResumedActivity=ActivityRecord\{\S+ u\d+ ([^/\s]+)/\S+ t(\d+)`)
	histRe        = regexp.MustCompile(`\* Hist\s+#\d+: ActivityRecord\{\S+ u\d+ ([^/\s]+)/(\S+) t(\d+)`)
)

// taskInFront reads `dumpsys activity activities`: the package of the
// activity in front, and of the activity at the root of its task — the app
// the task belongs to. A task's affinity is not a package (Settings' is
// com.android.settings.root), so the root activity is what names it.
func taskInFront(dump string) (top, root string) {
	m := topResumedRe.FindStringSubmatch(dump)
	if m == nil {
		return "", ""
	}
	root, _ = taskRoot(dump)
	return m[1], root
}

// taskRoot is the package and class of the activity at the root of the task
// in front.
func taskRoot(dump string) (pkg, class string) {
	m := topResumedRe.FindStringSubmatch(dump)
	if m == nil {
		return "", ""
	}
	task := m[2]

	var lastPkg, lastClass, lastTask string
	sc := bufio.NewScanner(strings.NewReader(dump))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if h := histRe.FindStringSubmatch(line); h != nil {
			lastPkg, lastClass, lastTask = h[1], h[2], h[3]
			continue
		}
		if strings.Contains(line, "rootOfTask=true") && lastTask == task {
			return lastPkg, lastClass
		}
	}
	return "", ""
}
