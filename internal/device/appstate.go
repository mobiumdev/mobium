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

var (
	topResumedRe = regexp.MustCompile(`topResumedActivity=ActivityRecord\{\S+ u\d+ ([^/\s]+)/\S+ t(\d+)`)
	histRe       = regexp.MustCompile(`\* Hist\s+#\d+: ActivityRecord\{\S+ u\d+ ([^/\s]+)/\S+ t(\d+)`)
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
	top, task := m[1], m[2]

	var lastPkg, lastTask string
	sc := bufio.NewScanner(strings.NewReader(dump))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if h := histRe.FindStringSubmatch(line); h != nil {
			lastPkg, lastTask = h[1], h[2]
			continue
		}
		if strings.Contains(line, "rootOfTask=true") && lastTask == task {
			return top, lastPkg
		}
	}
	return top, ""
}
