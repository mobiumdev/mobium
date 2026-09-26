package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// inventoryDriver is a device whose installed apps can be listed and removed.
type inventoryDriver struct {
	fakeDriver
	apps []device.InstalledApp
	// undeletable names an app that reports a clean uninstall and stays put,
	// which is what Android does for a system app whose updates it removed.
	undeletable string
	removed     []string
}

func (d *inventoryDriver) ListApps(ctx context.Context, includeSystem bool) ([]device.InstalledApp, error) {
	var out []device.InstalledApp
	for _, a := range d.apps {
		if a.System && !includeSystem {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (d *inventoryDriver) Uninstall(ctx context.Context, appID string) error {
	d.removed = append(d.removed, appID)
	if appID == d.undeletable {
		return nil // reports success, changes nothing
	}
	var kept []device.InstalledApp
	for _, a := range d.apps {
		if a.ID != appID {
			kept = append(kept, a)
		}
	}
	d.apps = kept
	return nil
}

var _ mobiumdriver.AppInventory = (*inventoryDriver)(nil)

func withInventory(t *testing.T, apps ...device.InstalledApp) (*Handlers, *session, *inventoryDriver) {
	t.Helper()
	d := &inventoryDriver{apps: apps}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	h.sessions["fake"] = s
	return h, s, d
}

func listApps(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.listAppsOn(ctx, s, args)
}

func uninstall(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.uninstallAppOn(ctx, s, args)
}

func TestListAppsHidesSystemAppsByDefault(t *testing.T) {
	// A stock Android emulator ships about 240 system packages against two
	// that someone installed. Listing all of them by default would bury the
	// answer.
	h, sess, _ := withInventory(t,
		device.InstalledApp{ID: "com.example.shop", Version: "3"},
		device.InstalledApp{ID: "com.android.settings", System: true},
	)
	res, err := listApps(h, sess, map[string]interface{}{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	view := res.StructuredContent.(AppsView)
	if len(view.Apps) != 1 || view.Apps[0].ID != "com.example.shop" {
		t.Errorf("apps = %+v", view.Apps)
	}
	if view.IncludesSystem {
		t.Error("reported that system apps were included")
	}

	res, err = listApps(h, sess, map[string]interface{}{"system": true})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if got := len(res.StructuredContent.(AppsView).Apps); got != 2 {
		t.Errorf("with system = %d apps, want 2", got)
	}
}

func TestUninstallVerifiesTheAppIsActuallyGone(t *testing.T) {
	// Android reports success for a system app when all it did was remove the
	// updates, leaving the app installed. Verified on the emulator, where
	// uninstalling com.android.settings fails with DELETE_FAILED_INTERNAL_ERROR
	// — but the same command on a system app with updates would have said
	// Success and changed nothing that matters.
	h, sess, d := withInventory(t, device.InstalledApp{ID: "com.android.settings", System: true})
	d.undeletable = "com.android.settings"

	_, err := uninstall(h, sess, map[string]interface{}{"app": "com.android.settings"})
	if err == nil {
		t.Fatal("an uninstall that removed nothing was reported as success")
	}
	if !strings.Contains(err.Error(), "still installed") {
		t.Errorf("error %q does not say what actually happened", err)
	}
}

func TestUninstallRemovesAndInvalidatesRefs(t *testing.T) {
	h, sess, d := withInventory(t, device.InstalledApp{ID: "com.example.shop"})
	h.refs["fake"] = &refTable{entries: map[string]uitree.Locator{
		"@e1": {Kind: uitree.KindText, Value: "Buy"},
	}}
	if _, err := uninstall(h, sess, map[string]interface{}{"app": "com.example.shop"}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(d.apps) != 0 {
		t.Errorf("app still listed: %+v", d.apps)
	}
	if _, err := h.locatorFor("fake", "@e1"); err == nil {
		t.Error("a ref survived the app being removed")
	}
}

func TestUninstallNeedsAnApp(t *testing.T) {
	h, sess, _ := withInventory(t)
	if _, err := uninstall(h, sess, map[string]interface{}{}); err == nil {
		t.Error("accepted an uninstall with no app")
	}
}

func TestInventoryBackendWithoutTheCapabilityRefuses(t *testing.T) {
	h, sess, _ := withFake(t, screen(t, "Hello"))
	if _, err := listApps(h, sess, map[string]interface{}{}); err == nil {
		t.Error("a backend with no inventory support listed anyway")
	}
	if _, err := uninstall(h, sess, map[string]interface{}{"app": "x"}); err == nil {
		t.Error("a backend with no inventory support uninstalled anyway")
	}
}
