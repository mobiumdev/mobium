package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// The permission vocabulary, compiled per platform the same way locators are.
//
// One name means the same thing on both platforms wherever both platforms have
// the thing, and the gaps are explicit rather than papered over. `camera` and
// `notifications` have no simulator equivalent at all — `xcrun simctl help
// privacy` lists thirteen services and neither is among them — so asking for
// them on iOS says so instead of silently doing nothing.
//
// Android names map to a *set*, because one user-facing permission is often
// several: "location" is coarse and fine, "contacts" is read and write.
// Granting the set is what a user tapping "Allow" once would produce.
type permissionSpec struct {
	android []string
	ios     string
	// note explains an absence, shown when the platform cannot do it.
	note string
}

var permissionVocabulary = map[string]permissionSpec{
	"camera": {
		android: []string{"android.permission.CAMERA"},
		note:    "the iOS simulator has no camera privacy service",
	},
	"microphone": {
		android: []string{"android.permission.RECORD_AUDIO"},
		ios:     "microphone",
	},
	"location": {
		android: []string{
			"android.permission.ACCESS_FINE_LOCATION",
			"android.permission.ACCESS_COARSE_LOCATION",
		},
		ios: "location",
	},
	"location-always": {
		android: []string{
			"android.permission.ACCESS_BACKGROUND_LOCATION",
			"android.permission.ACCESS_FINE_LOCATION",
			"android.permission.ACCESS_COARSE_LOCATION",
		},
		ios: "location-always",
	},
	"contacts": {
		android: []string{
			"android.permission.READ_CONTACTS",
			"android.permission.WRITE_CONTACTS",
			"android.permission.GET_ACCOUNTS",
		},
		ios: "contacts",
	},
	"calendar": {
		android: []string{
			"android.permission.READ_CALENDAR",
			"android.permission.WRITE_CALENDAR",
		},
		ios: "calendar",
	},
	"photos": {
		android: []string{
			"android.permission.READ_MEDIA_IMAGES",
			"android.permission.READ_MEDIA_VIDEO",
			"android.permission.READ_MEDIA_VISUAL_USER_SELECTED",
			"android.permission.READ_EXTERNAL_STORAGE",
		},
		ios: "photos",
	},
	"photos-add": {
		android: []string{"android.permission.WRITE_EXTERNAL_STORAGE"},
		ios:     "photos-add",
	},
	"media-library": {
		android: []string{"android.permission.READ_MEDIA_AUDIO"},
		ios:     "media-library",
	},
	"motion": {
		android: []string{"android.permission.ACTIVITY_RECOGNITION"},
		ios:     "motion",
	},
	"notifications": {
		android: []string{"android.permission.POST_NOTIFICATIONS"},
		note:    "the iOS simulator has no notifications privacy service",
	},
	"phone": {
		android: []string{
			"android.permission.READ_PHONE_STATE",
			"android.permission.READ_PHONE_NUMBERS",
			"android.permission.CALL_PHONE",
		},
		note: "iOS has no phone permission",
	},
	"sms": {
		android: []string{
			"android.permission.SEND_SMS",
			"android.permission.READ_SMS",
			"android.permission.RECEIVE_SMS",
		},
		note: "iOS has no SMS permission",
	},
	"bluetooth": {
		android: []string{
			"android.permission.BLUETOOTH_CONNECT",
			"android.permission.BLUETOOTH_SCAN",
			"android.permission.BLUETOOTH_ADVERTISE",
		},
		note: "iOS has no bluetooth privacy service in the simulator",
	},
	"reminders": {
		ios:  "reminders",
		note: "Android has no reminders permission",
	},
	"siri": {
		ios:  "siri",
		note: "Android has no Siri",
	},
}

// PermissionNames lists the vocabulary, for help text and schemas.
func PermissionNames() []string {
	names := make([]string, 0, len(permissionVocabulary)+1)
	names = append(names, "all")
	for n := range permissionVocabulary {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// compilePermission turns one vocabulary word into the platform's own names.
//
// A name that is not in the vocabulary is passed through untouched, so an
// Android permission the table does not know — a manufacturer's own, or one
// added in a newer API level — still works. The vocabulary is a convenience,
// not a gate.
func compilePermission(name string, ios bool) ([]string, error) {
	spec, known := permissionVocabulary[name]
	if !known {
		if strings.Contains(name, ".permission.") || !ios {
			return []string{name}, nil
		}
		return []string{name}, nil
	}
	if ios {
		if spec.ios == "" {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "%q cannot be set on iOS: %s", name, spec.note)
		}
		return []string{spec.ios}, nil
	}
	if len(spec.android) == 0 {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "%q cannot be set on Android: %s", name, spec.note)
	}
	return spec.android, nil
}

// permissionArgs reads the app and the requested permission names.
func permissionArgs(args map[string]interface{}) (app string, names []string, err error) {
	app = stringArg(args, "app")
	if app == "" {
		return "", nil, mobiumerr.New(mobiumerr.InvalidArgument, "this tool needs an app id — a package name on Android "+
			"(\"com.example.shop\") or a bundle id on iOS (\"com.example.Shop\")")
	}
	switch v := args["permissions"].(type) {
	case string:
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				names = append(names, p)
			}
		}
	case []interface{}:
		for _, item := range v {
			if p, ok := item.(string); ok && strings.TrimSpace(p) != "" {
				names = append(names, strings.TrimSpace(p))
			}
		}
	case []string:
		names = append(names, v...)
	}
	if len(names) == 0 {
		return "", nil, mobiumerr.New(mobiumerr.InvalidArgument, "this tool needs permissions — one or more of %s, "+
			"\"all\", or a platform name like \"android.permission.CAMERA\"",
			strings.Join(PermissionNames(), ", "))
	}
	return app, names, nil
}

func (h *Handlers) grantPermissions(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.setPermissionsOn(ctx, s, args, true)
}

func (h *Handlers) revokePermissions(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.setPermissionsOn(ctx, s, args, false)
}

// setPermissionsOn is app_grant and app_revoke once the device is resolved.
func (h *Handlers) setPermissionsOn(ctx context.Context, s *session, args map[string]interface{}, grant bool) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsPermissions(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapPermissions, "change permissions")
	}
	app, names, err := permissionArgs(args)
	if err != nil {
		return nil, err
	}
	ios := s.backend == BackendWDA

	// Read the state first where the platform can report it, both to expand
	// "all" and to know afterwards whether anything actually happened.
	var before map[string]bool
	reader, canRead := mobiumdriver.AsPermissionReader(s.driver)
	if canRead {
		if before, err = reader.PermissionState(ctx, app); err != nil {
			return nil, err
		}
	}

	targets, err := resolvePermissionTargets(names, ios, before, canRead)
	if err != nil {
		return nil, err
	}

	view := PermissionView{App: app, Device: s.dev.Serial, Granted: grant,
		Permissions: []PermissionEntry{}}
	var failures []string
	for _, perm := range targets {
		entry := PermissionEntry{Name: perm}
		switch {
		case canRead && !declared(before, perm):
			// `pm grant` exits 0 and prints nothing for a permission the app
			// never declared, having done nothing. Saying so is the whole
			// point of reading the state.
			entry.Skipped = "the app does not declare it"
		default:
			if err := ctrl.SetPermission(ctx, app, perm, grant); err != nil {
				entry.Error = err.Error()
				failures = append(failures, perm)
			}
		}
		view.Permissions = append(view.Permissions, entry)
	}

	// Verify by outcome. The command reporting success is not evidence.
	if canRead {
		after, err := reader.PermissionState(ctx, app)
		if err == nil {
			for i, e := range view.Permissions {
				if e.Skipped != "" {
					continue
				}
				got, present := after[e.Name]
				view.Permissions[i].Applied = present && got == grant
				if present && got != grant && e.Error == "" {
					view.Permissions[i].Error = "the command reported success but the state did not change"
					failures = append(failures, e.Name)
				}
			}
		}
	}

	if len(failures) > 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "%s failed for %s on %s:\n%s", verbOf(grant),
			strings.Join(failures, ", "), app, permissionProblems(view))
	}
	return Result(permissionSummary(view, canRead), view), nil
}

func verbOf(grant bool) string {
	if grant {
		return "grant"
	}
	return "revoke"
}

// pastVerbOf exists because "revoke" + "ed" is "revokeed".
func pastVerbOf(grant bool) string {
	if grant {
		return "granted"
	}
	return "revoked"
}

func declared(state map[string]bool, perm string) bool {
	_, ok := state[perm]
	return ok
}

// resolvePermissionTargets expands the requested names into platform names,
// with "all" meaning everything the app declares.
func resolvePermissionTargets(names []string, ios bool, state map[string]bool, canRead bool) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	for _, name := range names {
		if name == "all" {
			if ios {
				add("all") // simctl has its own "all" service
				continue
			}
			if !canRead {
				return nil, mobiumerr.New(mobiumerr.Unsupported, "\"all\" needs to read what the app declares, "+
					"which this backend cannot do — name the permissions instead")
			}
			declared := make([]string, 0, len(state))
			for p := range state {
				declared = append(declared, p)
			}
			sort.Strings(declared)
			for _, p := range declared {
				add(p)
			}
			continue
		}
		compiled, err := compilePermission(name, ios)
		if err != nil {
			return nil, err
		}
		for _, p := range compiled {
			add(p)
		}
	}
	return out, nil
}

func (h *Handlers) resetPermissions(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	ctrl, ok := mobiumdriver.AsPermissions(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapPermissions, "change permissions")
	}
	app := stringArg(args, "app")
	var left device.PermissionReset
	if r, ok := mobiumdriver.AsAppPermissionResetter(s.driver); ok && app != "" {
		if left, err = r.ResetAppPermissions(ctx, app); err != nil {
			return nil, err
		}
	} else if err := ctrl.ResetPermissions(ctx, app); err != nil {
		return nil, err
	}
	view := PermissionView{App: app, Device: s.dev.Serial, Reset: true}
	if app == "" {
		return Result("reset permissions for every app on the device", view), nil
	}

	// Where the platform can say, say what each permission is now. One still
	// granted after a reset is one nobody can change: the system or a device
	// policy fixed it.
	reader, ok := mobiumdriver.AsPermissionReader(s.driver)
	if !ok {
		return Result("reset permissions for "+app, view), nil
	}
	state, err := reader.PermissionState(ctx, app)
	if errors.Is(err, device.ErrNoRuntimePermissions) {
		return Result(app+" declares no runtime permissions, so it had none to reset", view), nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(state))
	for n := range state {
		names = append(names, n)
	}
	sort.Strings(names)
	why := map[string]string{}
	for _, n := range left.PlatformGranted {
		why[n] = keptByPlatform
	}
	var reset []string
	kept := map[string][]string{}
	for _, n := range names {
		e := PermissionEntry{Name: n, Applied: !state[n]}
		if state[n] {
			e.Skipped = keptFixed
			if w, ok := why[n]; ok {
				e.Skipped = w
			}
			kept[e.Skipped] = append(kept[e.Skipped], shortPermission(n))
		} else {
			reset = append(reset, shortPermission(n))
		}
		view.Permissions = append(view.Permissions, e)
	}
	msg := fmt.Sprintf("reset %d permission%s for %s to asking", len(reset), plural(len(reset)), app)
	if len(reset) > 0 {
		msg += ": " + strings.Join(reset, ", ")
	}
	for _, reason := range []string{keptFixed, keptByPlatform} {
		if len(kept[reason]) > 0 {
			msg += "\nkept " + strings.Join(kept[reason], ", ") + ": " + reason
		}
	}
	if left.ApproximateKept {
		view.ApproximateKept = true
		msg += "\nkept the choice of approximate location, which the location prompt will preselect: " +
			"Android has no command that clears it for one app, and reset-permissions with no app does"
	}
	return Result(msg, view), nil
}

// Why a permission is still granted after one app's reset.
const (
	keptFixed      = "fixed by the system or a device policy"
	keptByPlatform = "granted by Android itself until the app asks for it, as on a fresh install"
)

// permissionSummary is the prose a CLI user reads.
func permissionSummary(v PermissionView, verified bool) string {
	var applied, skipped []string
	for _, e := range v.Permissions {
		switch {
		case e.Skipped != "":
			skipped = append(skipped, shortPermission(e.Name))
		default:
			applied = append(applied, shortPermission(e.Name))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d permission%s for %s", pastVerbOf(v.Granted),
		len(applied), plural(len(applied)), v.App)
	if len(applied) > 0 {
		fmt.Fprintf(&b, ": %s", strings.Join(applied, ", "))
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "\nskipped %d the app does not declare: %s",
			len(skipped), strings.Join(skipped, ", "))
	}
	if !verified {
		b.WriteString("\n(not verified — this platform cannot report permission state)")
	}
	return b.String()
}

func permissionProblems(v PermissionView) string {
	var lines []string
	for _, e := range v.Permissions {
		if e.Error != "" {
			lines = append(lines, "  "+shortPermission(e.Name)+": "+e.Error)
		}
	}
	return strings.Join(lines, "\n")
}

// shortPermission drops the android.permission. prefix, which is the same on
// every line and makes a list of eight unreadable.
func shortPermission(p string) string {
	return strings.TrimPrefix(p, "android.permission.")
}

// PermissionView is the result of app_grant, app_revoke and
// app_reset_permissions.
type PermissionView struct {
	App         string            `json:"app,omitempty"`
	Device      string            `json:"device"`
	Granted     bool              `json:"granted,omitempty"`
	Reset       bool              `json:"reset,omitempty"`
	Permissions []PermissionEntry `json:"permissions,omitempty"`
	// ApproximateKept is, after resetting one Android app, a person's choice
	// of approximate location that the reset could not clear.
	ApproximateKept bool `json:"approximate_kept,omitempty"`
}

// PermissionEntry is what happened to one permission.
type PermissionEntry struct {
	Name string `json:"name"`
	// Applied is true only when the state was read back and agrees. It stays
	// false on a platform that cannot report state, where Skipped and Error
	// are the only signals available.
	Applied bool   `json:"applied"`
	Skipped string `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}
