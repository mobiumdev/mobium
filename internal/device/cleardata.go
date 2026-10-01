package device

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// ClearedData is what clearing an app's data did, in the terms each platform
// can check.
type ClearedData struct {
	// Emptied names the stores that were read back empty afterwards.
	Emptied []string `json:"emptied"`
	// Kept names what clearing leaves as it was, and why. A caller resetting
	// state needs these more than the list of what went.
	Kept []string `json:"kept,omitempty"`
	// StillGranted is Android's runtime permissions still granted afterwards,
	// read back rather than assumed: `pm clear` revokes what the user granted
	// and leaves what the system fixed. Nil where it was not read.
	StillGranted []string `json:"still_granted"`
	// NotReadBack names what the reset changed that nothing here can read
	// back, and the measurement that says it does — a phone's privacy
	// permissions. Said apart from Emptied, which is only what was read.
	NotReadBack []string `json:"not_read_back,omitempty"`
}

// ClearAppData deletes an app's data, as `pm clear` does it: the app is
// stopped, its private storage and its directory on shared storage emptied,
// and its runtime permissions revoked.
//
// `pm clear` answers "Success" or "Failed" — the latter with exit 1, and for
// a package that is not installed as much as for one it would not clear — so
// installation is asked first, which turns the ambiguous failure into the
// right error. Then what can be read without root is read back: the app's
// directory on shared storage, which the shell may list, and its permissions.
// Its private storage cannot be listed from a shell on a phone at all.
func (a *ADB) ClearAppData(ctx context.Context, pkg string) (ClearedData, error) {
	if _, err := a.Shell(ctx, "pm", "path", shellQuote(pkg)); err != nil {
		return ClearedData{}, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this device", pkg)
	}
	out, err := a.Shell(ctx, "pm", "clear", shellQuote(pkg))
	if err != nil {
		return ClearedData{}, mobiumerr.New(mobiumerr.DeviceServer, "pm clear %s: %w", pkg, err)
	}
	if !strings.Contains(string(out), "Success") {
		return ClearedData{}, mobiumerr.New(mobiumerr.DeviceServer, "pm clear %s: %s", pkg,
			strings.TrimSpace(firstLine(string(out))))
	}

	res := ClearedData{
		Emptied: []string{"private storage (pm clear reported Success; not readable without root)"},
		Kept:    []string{"the app itself"},
	}
	ext := "/sdcard/Android/data/" + pkg
	left, diag, err := a.ShellDiagnostics(ctx, "ls", "-A", shellQuote(ext))
	switch {
	case err == nil && strings.TrimSpace(string(left)) != "":
		return res, mobiumerr.New(mobiumerr.NotConfirmed, "pm clear %s reported Success, but %s still holds %s",
			pkg, ext, strings.Join(strings.Fields(string(left)), ", "))
	case err == nil, strings.Contains(string(diag)+errText(err), "No such file"):
		res.Emptied = append(res.Emptied, ext+" (read back empty)")
	}
	// Anything else — a shell that may not list shared storage — is not
	// claimed either way.

	if perms, err := a.RuntimePermissions(ctx, pkg); err == nil {
		res.StillGranted = []string{}
		for p, granted := range perms {
			if granted {
				res.StillGranted = append(res.StillGranted, p)
			}
		}
		sort.Strings(res.StillGranted)
	}
	return res, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// simContainerDirs are the parts of an app's data container that are the
// app's to write. SystemData and the container manager's metadata belong to
// the simulator and are left alone; so are the directories themselves, since
// a fresh install has them and the app expects to find them.
var simContainerDirs = []string{"Documents", "Library", "tmp"}

// ClearAppData deletes an app's data on the simulator. simctl has no command
// for it, so this is what a fresh install would have: the app stopped, its
// preferences deleted, and its data container's Documents, Library and tmp
// emptied.
//
// Preferences go first, and through `defaults`, because deleting the file is
// not enough: cfprefsd holds the domain in memory and went on serving a key
// after its plist was deleted — and restarting the daemon did not clear it
// either. Measured on an iPhone 17 Pro simulator. `defaults delete` goes
// through the daemon, so its cache and the disk agree.
//
// The container's path is asked for each time: reinstalling an app moves it.
func (s *Simctl) ClearAppData(ctx context.Context, bundleID string) (ClearedData, error) {
	if !s.AppInstalled(ctx, bundleID) {
		return ClearedData{}, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this simulator", bundleID)
	}
	if err := s.TerminateApp(ctx, bundleID); err != nil {
		return ClearedData{}, err
	}
	// A terminate is a request. Clearing under a running app would let it
	// write its state straight back.
	if err := s.waitStopped(ctx, bundleID); err != nil {
		return ClearedData{}, err
	}

	out, err := s.Run(ctx, "get_app_container", s.UDID, bundleID, "data")
	if err != nil {
		return ClearedData{}, err
	}
	root := strings.TrimSpace(string(out))
	if root == "" || !filepath.IsAbs(root) {
		return ClearedData{}, mobiumerr.New(mobiumerr.DeviceServer, "simctl gave no data container for %s: %q", bundleID, root)
	}

	if _, err := s.Run(ctx, "spawn", s.UDID, "defaults", "delete", bundleID); err != nil &&
		!strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "does not exist") {
		return ClearedData{}, mobiumerr.New(mobiumerr.DeviceServer, "deleting %s's preferences: %w", bundleID, err)
	}
	for _, d := range simContainerDirs {
		if err := emptyDir(filepath.Join(root, d)); err != nil {
			return ClearedData{}, mobiumerr.New(mobiumerr.DeviceServer, "emptying %s: %w", d, err)
		}
	}

	// Read both back.
	var left []string
	for _, d := range simContainerDirs {
		entries, err := os.ReadDir(filepath.Join(root, d))
		if err != nil && !os.IsNotExist(err) {
			return ClearedData{}, mobiumerr.New(mobiumerr.DeviceServer, "reading %s back: %w", d, err)
		}
		for _, e := range entries {
			left = append(left, d+"/"+e.Name())
		}
	}
	if len(left) > 0 {
		return ClearedData{}, mobiumerr.New(mobiumerr.NotConfirmed, "cleared %s, but its container still holds %s",
			bundleID, strings.Join(left, ", "))
	}
	if _, err := s.Run(ctx, "spawn", s.UDID, "defaults", "read", bundleID); err == nil {
		return ClearedData{}, mobiumerr.New(mobiumerr.NotConfirmed,
			"cleared %s, but its preferences still read back through cfprefsd", bundleID)
	}

	return ClearedData{
		Emptied: []string{
			"Documents, Library and tmp in the data container (read back empty)",
			"preferences (read back through cfprefsd: none)",
		},
		Kept: []string{
			"the app itself",
			"privacy grants, the keychain and app group containers — none is in the app's data container",
		},
	}, nil
}

// waitStopped waits for an app's process to leave launchd's list.
func (s *Simctl) waitStopped(ctx context.Context, bundleID string) error {
	marker := "UIKitApplication:" + bundleID + "["
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := s.Run(ctx, "spawn", s.UDID, "launchctl", "list")
		if err != nil {
			return err
		}
		if !runningIn(string(out), marker) {
			return nil
		}
		if time.Now().After(deadline) {
			return mobiumerr.New(mobiumerr.Timeout, "%s was asked to stop and was still running 5s later", bundleID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// runningIn reports whether `launchctl list` shows a process for the marker.
// A job that is listed with "-" for its pid is known to launchd and not
// running.
func runningIn(list, marker string) bool {
	for _, line := range strings.Split(list, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		f := strings.Fields(line)
		if len(f) > 0 && f[0] != "-" {
			return true
		}
	}
	return false
}

// emptyDir deletes everything inside dir and keeps dir. A directory that is
// not there has nothing in it.
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
