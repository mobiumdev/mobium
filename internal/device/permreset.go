package device

import (
	"context"
	"regexp"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// runtimePermFlagsRe reads one runtime permission line of `dumpsys package`
// with its flags: "android.permission.CAMERA: granted=false, flags=[ USER_SET|... ]".
//
// The flags are read as whatever sits between the brackets. Android prints a
// flag it has no name for as a number — 524288 — and a pattern of capitals
// alone failed to match the whole list, so every flag on that line read as
// absent, the reset skipped clearing them, and the read-back, using the same
// parser, agreed that nothing was left (CHALLENGES 151).
var runtimePermFlagsRe = regexp.MustCompile(
	`^\s+([a-zA-Z0-9._]+\.permission\.[A-Z_0-9]+):\s+granted=(true|false)(?:,\s*flags=\[([^\]]*)\])?`)

// accuracyFlag is how `dumpsys` prints FLAG_PERMISSION_SELECTED_LOCATION_ACCURACY
// (1<<19), which it has no name for: the precise-or-approximate choice a
// person made on the location prompt, set on the permission they chose. The
// prompt preselects it next time. `pm clear-permission-flags` accepts five
// named flags and not this one, and nothing else clears it for one app;
// `pm reset-permissions`, device-wide, does.
//
// Android 17 names it: the Pixel 8 Pro printed SELECTED_LOCATION_ACCURACY
// where the Android 15 AVD printed the number.
const accuracyFlag = "524288"

func (p permissionRecord) accuracyChosen() bool {
	return p.flags[accuracyFlag] || p.flags["SELECTED_LOCATION_ACCURACY"]
}

// platformGranted is a permission Android grants by itself until the app
// asks for it — REVOKE_WHEN_REQUESTED. Android 17 grants
// ACCESS_LOCAL_NETWORK to MobiumApp this way, with no answer recorded, and
// `pm revoke` exits 0 and leaves it granted. A fresh install has it too, so
// a reset leaves it.
func (p permissionRecord) platformGranted() bool {
	return p.granted && p.flags["REVOKE_WHEN_REQUESTED"] && !p.decided()
}

// permissionRecord is one runtime permission as `dumpsys package` has it.
type permissionRecord struct {
	name    string
	granted bool
	flags   map[string]bool
}

// fixed is a permission the system or a device policy decides, which the
// person using the phone cannot change either — so it is not asking, and
// resetting it to asking is not a thing that exists.
func (p permissionRecord) fixed() bool {
	return p.flags["SYSTEM_FIXED"] || p.flags["POLICY_FIXED"]
}

// decided is a permission the person answered: USER_SET once they have,
// USER_FIXED once they have denied it twice and Android stops asking.
func (p permissionRecord) decided() bool {
	return p.flags["USER_SET"] || p.flags["USER_FIXED"]
}

// parseRuntimePermissions reads the first user's runtime permissions block.
func parseRuntimePermissions(dump string) []permissionRecord {
	var out []permissionRecord
	inBlock := false
	for _, line := range strings.Split(dump, "\n") {
		if strings.Contains(line, "runtime permissions:") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		m := runtimePermFlagsRe.FindStringSubmatch(line)
		if m == nil {
			if strings.TrimSpace(line) != "" {
				break
			}
			continue
		}
		p := permissionRecord{name: m[1], granted: m[2] == "true", flags: map[string]bool{}}
		for _, f := range strings.Split(m[3], "|") {
			if f = strings.TrimSpace(f); f != "" {
				p.flags[f] = true
			}
		}
		out = append(out, p)
	}
	return out
}

func (a *ADB) runtimePermissionRecords(ctx context.Context, pkg string) ([]permissionRecord, error) {
	out, err := a.Shell(ctx, "dumpsys", "package", pkg)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(out), "Unable to find package") {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed", pkg)
	}
	return parseRuntimePermissions(string(out)), nil
}

// ResetAppPermissions puts one app's runtime permissions back to asking, as a
// fresh install has them, and leaves every other app's alone.
//
// `pm reset-permissions` has no per-package form, so this is the two things
// it would do, per permission: revoke it, and clear the flags that record a
// person's answer — USER_SET, and USER_FIXED, which is what makes Android stop
// asking after a second denial. Measured on the Pixel 7 AVD and the Pixel 8
// Pro: the prompt came back after an accept and after two denials, the flags
// then matched a fresh install's, and 91 permission lines of every other
// installed app were unchanged. A permission the system or a policy fixed is
// left, since nobody can change it, and so is a choice of approximate
// location, since nothing can clear it for one app; both are returned so the
// caller can say so.
//
// Revoking a granted permission ends the app's process, as it does when a
// person revokes one in Settings.
func (a *ADB) ResetAppPermissions(ctx context.Context, pkg string) (PermissionReset, error) {
	var res PermissionReset
	perms, err := a.runtimePermissionRecords(ctx, pkg)
	if err != nil {
		return res, err
	}
	for _, p := range perms {
		if p.fixed() {
			res.Fixed = append(res.Fixed, p.name)
			continue
		}
		if p.platformGranted() {
			res.PlatformGranted = append(res.PlatformGranted, p.name)
			continue
		}
		if p.granted {
			if err := a.SetPermission(ctx, pkg, p.name, false); err != nil {
				return res, err
			}
		}
		if p.decided() {
			out, diag, err := a.ShellDiagnostics(ctx, "pm", "clear-permission-flags", pkg, p.name, "user-set", "user-fixed")
			if err != nil {
				return res, err
			}
			if s := strings.TrimSpace(string(diag) + string(out)); strings.Contains(s, "Exception") || strings.Contains(s, "Error") {
				return res, mobiumerr.New(mobiumerr.DeviceServer, "pm clear-permission-flags %s %s: %s", pkg, p.name, lastLine(s))
			}
		}
	}

	// Read back rather than believe pm, which reports success for changes it
	// did not make.
	after, err := a.runtimePermissionRecords(ctx, pkg)
	if err != nil {
		return res, err
	}
	for _, p := range after {
		if p.fixed() || p.platformGranted() {
			continue
		}
		if p.granted {
			return res, mobiumerr.New(mobiumerr.NotConfirmed,
				"reset %s's permissions, and %s is still granted", pkg, p.name)
		}
		if p.decided() {
			return res, mobiumerr.New(mobiumerr.NotConfirmed,
				"reset %s's permissions, and %s still has a person's answer recorded", pkg, p.name)
		}
		if p.name == "android.permission.ACCESS_COARSE_LOCATION" && p.accuracyChosen() {
			res.ApproximateKept = true
		}
	}
	return res, nil
}

// PermissionReset is what a reset of one app's permissions left as it was.
type PermissionReset struct {
	// Fixed are permissions the system or a device policy decides.
	Fixed []string
	// PlatformGranted are permissions Android grants until the app asks.
	PlatformGranted []string
	// ApproximateKept is a person's choice of approximate location, which
	// the next location prompt preselects; see accuracyFlag.
	ApproximateKept bool
}
