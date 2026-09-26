package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// permDriver is a device whose permission state can be inspected and changed,
// and which records exactly what it was asked to do.
type permDriver struct {
	fakeDriver
	state map[string]bool
	calls []string
	// silentlyIgnores names a permission whose write reports success and
	// changes nothing, which is how `pm grant` behaves for one the app never
	// declared.
	silentlyIgnores string
}

func (d *permDriver) SetPermission(ctx context.Context, appID, perm string, grant bool) error {
	d.calls = append(d.calls, verbOf(grant)+" "+perm)
	if perm == d.silentlyIgnores {
		return nil // reports success, changes nothing
	}
	if _, declared := d.state[perm]; declared {
		d.state[perm] = grant
	}
	return nil
}

func (d *permDriver) ResetPermissions(ctx context.Context, appID string) error {
	d.calls = append(d.calls, "reset "+appID)
	return nil
}

func (d *permDriver) PermissionState(ctx context.Context, appID string) (map[string]bool, error) {
	out := map[string]bool{}
	for k, v := range d.state {
		out[k] = v
	}
	return out, nil
}

func withPerms(t *testing.T, backend Backend, state map[string]bool) (*Handlers, *session, *permDriver) {
	t.Helper()
	d := &permDriver{state: state}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: backend}
	h.sessions["fake"] = s
	return h, s, d
}

func grant(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.setPermissionsOn(ctx, s, args, true)
}

func revoke(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.setPermissionsOn(ctx, s, args, false)
}

var _ mobiumdriver.Permissions = (*permDriver)(nil)
var _ mobiumdriver.PermissionReader = (*permDriver)(nil)

func TestGrantCompilesOneNameToSeveralAndroidPermissions(t *testing.T) {
	// "location" is one thing to a user and two permissions to Android.
	// Granting one and not the other is the kind of half-state that makes an
	// app behave differently from how a user tapping Allow would leave it.
	h, sess, d := withPerms(t, BackendUIA2, map[string]bool{
		"android.permission.ACCESS_FINE_LOCATION":   false,
		"android.permission.ACCESS_COARSE_LOCATION": false,
	})
	if _, err := grant(h, sess, map[string]interface{}{
		"app": "com.example", "permissions": []interface{}{"location"},
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if len(d.calls) != 2 {
		t.Fatalf("calls = %v, want both location permissions", d.calls)
	}
	for _, p := range []string{"android.permission.ACCESS_FINE_LOCATION",
		"android.permission.ACCESS_COARSE_LOCATION"} {
		if !d.state[p] {
			t.Errorf("%s was not granted", p)
		}
	}
}

func TestGrantReportsPermissionsTheAppDoesNotDeclare(t *testing.T) {
	// Verified on the emulator: `pm grant` for a permission the app never
	// declared exits 0, prints nothing, and changes nothing. Reporting that
	// as success is the failure mode worth preventing.
	h, sess, d := withPerms(t, BackendUIA2, map[string]bool{
		"android.permission.CAMERA": false,
	})
	res, err := grant(h, sess, map[string]interface{}{
		"app": "com.example", "permissions": []interface{}{"camera", "calendar"},
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	for _, call := range d.calls {
		if strings.Contains(call, "CALENDAR") {
			t.Errorf("asked the device for an undeclared permission: %v", d.calls)
		}
	}
	view := res.StructuredContent.(PermissionView)
	var skipped int
	for _, e := range view.Permissions {
		if e.Skipped != "" {
			skipped++
		}
	}
	if skipped != 2 {
		t.Errorf("skipped %d of the calendar pair, want 2: %+v", skipped, view.Permissions)
	}
	if !strings.Contains(textOf(res), "does not declare") {
		t.Errorf("summary %q does not say what was skipped", textOf(res))
	}
}

func TestGrantFailsWhenTheStateDoesNotChange(t *testing.T) {
	// The verification step: a write that reports success but leaves the
	// state alone must be an error, not a quiet lie.
	h, sess, d := withPerms(t, BackendUIA2, map[string]bool{
		"android.permission.CAMERA": false,
	})
	d.silentlyIgnores = "android.permission.CAMERA"

	_, err := grant(h, sess, map[string]interface{}{
		"app": "com.example", "permissions": []interface{}{"camera"},
	})
	if err == nil {
		t.Fatal("a grant that did nothing was reported as success")
	}
	if !strings.Contains(err.Error(), "did not change") {
		t.Errorf("error %q does not explain what happened", err)
	}
}

func TestGrantAllUsesWhatTheAppDeclares(t *testing.T) {
	h, sess, d := withPerms(t, BackendUIA2, map[string]bool{
		"android.permission.CAMERA":       false,
		"android.permission.RECORD_AUDIO": false,
		"android.permission.NFC":          false,
	})
	if _, err := grant(h, sess, map[string]interface{}{
		"app": "com.example", "permissions": []interface{}{"all"},
	}); err != nil {
		t.Fatalf("grant all: %v", err)
	}
	if len(d.calls) != 3 {
		t.Errorf("calls = %v, want all three declared permissions", d.calls)
	}
	for p, granted := range d.state {
		if !granted {
			t.Errorf("%s left ungranted by \"all\"", p)
		}
	}
}

func TestUnknownNamesPassThroughUntouched(t *testing.T) {
	// The vocabulary is a convenience, not a gate: a permission it has never
	// heard of — a manufacturer's own, or one from a newer API level — must
	// still reach the device.
	h, sess, d := withPerms(t, BackendUIA2, map[string]bool{
		"com.samsung.android.permission.SOMETHING": false,
	})
	if _, err := grant(h, sess, map[string]interface{}{
		"app":         "com.example",
		"permissions": []interface{}{"com.samsung.android.permission.SOMETHING"},
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if len(d.calls) != 1 || !strings.Contains(d.calls[0], "samsung") {
		t.Errorf("calls = %v", d.calls)
	}
}

func TestPlatformGapsAreRefusedNotIgnored(t *testing.T) {
	// `xcrun simctl help privacy` lists thirteen services and neither camera
	// nor notifications is among them. Quietly doing nothing would be the
	// worst answer; the error names the reason.
	h, sess, _ := withPerms(t, BackendWDA, map[string]bool{})
	for _, name := range []string{"camera", "notifications"} {
		_, err := grant(h, sess, map[string]interface{}{
			"app": "com.example.Shop", "permissions": []interface{}{name},
		})
		if err == nil {
			t.Fatalf("%q was accepted on iOS", name)
		}
		if !strings.Contains(err.Error(), "simulator") {
			t.Errorf("error for %q does not explain the gap: %v", name, err)
		}
	}
	// And the reverse: iOS-only services are refused on Android.
	h, sess, _ = withPerms(t, BackendUIA2, map[string]bool{})
	if _, err := grant(h, sess, map[string]interface{}{
		"app": "com.example", "permissions": []interface{}{"siri"},
	}); err == nil {
		t.Error("siri was accepted on Android")
	}
}

func TestRevokeReadsAsRevoked(t *testing.T) {
	// "revoke" + "ed" is "revokeed"; the summary said so on a real run.
	h, sess, _ := withPerms(t, BackendUIA2, map[string]bool{
		"android.permission.CAMERA": true,
	})
	res, err := revoke(h, sess, map[string]interface{}{
		"app": "com.example", "permissions": []interface{}{"camera"},
	})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got := textOf(res); !strings.Contains(got, "revoked") || strings.Contains(got, "revokeed") {
		t.Errorf("summary reads %q", got)
	}
}

func TestPermissionsNeedAnAppAndSomethingToSet(t *testing.T) {
	h, sess, _ := withPerms(t, BackendUIA2, map[string]bool{})
	if _, err := grant(h, sess, map[string]interface{}{
		"permissions": []interface{}{"camera"}}); err == nil {
		t.Error("accepted a grant with no app")
	}
	if _, err := grant(h, sess, map[string]interface{}{"app": "com.example"}); err == nil {
		t.Error("accepted a grant with no permissions")
	}
}

func TestCommaSeparatedPermissionsAreAccepted(t *testing.T) {
	// The CLI passes a list, but a JSON caller may well send a string.
	h, sess, d := withPerms(t, BackendUIA2, map[string]bool{
		"android.permission.CAMERA":       false,
		"android.permission.RECORD_AUDIO": false,
	})
	if _, err := grant(h, sess, map[string]interface{}{
		"app": "com.example", "permissions": "camera, microphone",
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if len(d.calls) != 2 {
		t.Errorf("calls = %v", d.calls)
	}
}
