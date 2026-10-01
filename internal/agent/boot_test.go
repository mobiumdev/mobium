package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// What app_boot and app_shutdown refuse before touching anything: no name, a
// name nothing on the machine has, a name that is both an AVD and a
// simulator, and a phone.
func TestBootAndShutdownRefuseWhatTheyCannotDo(t *testing.T) {
	ctx := context.Background()
	defer func(a func(context.Context) ([]string, error), s func(context.Context, string) bool,
		st func(context.Context, string) (string, bool, error), k func(context.Context, string) string) {
		bootAVDs, bootSimulator, shutdownTarget, iosKindOf = a, s, st, k
	}(bootAVDs, bootSimulator, shutdownTarget, iosKindOf)
	bootAVDs = func(context.Context) ([]string, error) { return []string{"mobium-test", "Pixel"}, nil }
	bootSimulator = func(_ context.Context, name string) bool { return name == "Pixel" || name == "iPhone 17 Pro" }
	h := NewHandlers()

	for _, c := range []struct {
		args map[string]interface{}
		code mobiumerr.Code
		says string
	}{
		{map[string]interface{}{}, mobiumerr.InvalidArgument, "needs a name"},
		{map[string]interface{}{"name": "Nexus"}, mobiumerr.NoDevice, "AVDs mobium-test, Pixel"},
		{map[string]interface{}{"name": "Pixel"}, mobiumerr.InvalidArgument, "both an AVD and a simulator"},
	} {
		_, err := h.boot(ctx, c.args)
		if mobiumerr.CodeOf(err) != c.code || !strings.Contains(err.Error(), c.says) {
			t.Errorf("boot %v: %v", c.args, err)
		}
	}

	if _, err := h.shutdown(ctx, map[string]interface{}{}); !strings.Contains(err.Error(), "needs a name") {
		t.Errorf("shutdown with no name: %v", err)
	}
	// Nothing running by that name is said so, before anything is ended.
	shutdownTarget = func(ctx context.Context, name string) (string, bool, error) {
		return "", false, mobiumerr.New(mobiumerr.NoDevice, "nothing running is named %q", name)
	}
	if _, err := h.shutdown(ctx, map[string]interface{}{"name": "ghost"}); mobiumerr.CodeOf(err) != mobiumerr.NoDevice {
		t.Errorf("shutdown of nothing running: %v", err)
	}
}

// When the emulator cannot be found, a name that is not a simulator is
// refused with that cause, not with "this machine has no AVDs": a daemon
// started without ANDROID_HOME said so of a machine with several.
func TestBootSaysWhyItCannotListAVDs(t *testing.T) {
	defer func(a func(context.Context) ([]string, error), s func(context.Context, string) bool) {
		bootAVDs, bootSimulator = a, s
	}(bootAVDs, bootSimulator)
	bootAVDs = func(context.Context) ([]string, error) {
		return nil, mobiumerr.New(mobiumerr.ToolchainMissing, "cannot find the Android emulator")
	}
	bootSimulator = func(context.Context, string) bool { return false }
	_, err := NewHandlers().boot(context.Background(), map[string]interface{}{"name": "mobium-test"})
	if mobiumerr.CodeOf(err) != mobiumerr.ToolchainMissing || strings.Contains(err.Error(), "no AVDs") {
		t.Errorf("got %v", err)
	}
}
