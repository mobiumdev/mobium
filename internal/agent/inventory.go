package agent

import (
	"context"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

func (h *Handlers) listApps(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.listAppsOn(ctx, s, args)
}

// listAppsOn is app_list_apps once the device is resolved.
func (h *Handlers) listAppsOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	inv, ok := mobiumdriver.AsAppInventory(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapInventory, "list installed apps")
	}
	includeSystem := boolArg(args, "system")
	apps, err := inv.ListApps(ctx, includeSystem)
	if err != nil {
		return nil, err
	}

	view := AppsView{Apps: []AppEntry{}, Device: s.dev.Serial, IncludesSystem: includeSystem}
	var lines []string
	for _, a := range apps {
		view.Apps = append(view.Apps, AppEntry{
			ID: a.ID, Name: a.Name, Version: a.Version, System: a.System,
		})
		lines = append(lines, appLine(a))
	}
	if len(lines) == 0 {
		msg := "No apps installed by anyone"
		if includeSystem {
			msg = "No apps found at all, which should not happen"
		}
		return Result(msg, view), nil
	}
	return Result(strings.Join(lines, "\n"), view), nil
}

func appLine(a device.InstalledApp) string {
	line := a.ID
	if a.Name != "" {
		line += "  " + a.Name
	}
	if a.Version != "" {
		line += "  (" + a.Version + ")"
	}
	if a.System {
		line += "  [system]"
	}
	return line
}

func (h *Handlers) uninstallApp(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.uninstallAppOn(ctx, s, args)
}

// uninstallAppOn is app_uninstall once the device is resolved.
func (h *Handlers) uninstallAppOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	inv, ok := mobiumdriver.AsAppInventory(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapInventory, "uninstall apps")
	}
	id, err := appID(args)
	if err != nil {
		return nil, err
	}

	// An app being removed cannot be the app on screen for much longer.
	s.closeWeb()
	if err := inv.Uninstall(ctx, id); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)

	// Verify by outcome. `adb uninstall` says "Success" and `simctl uninstall`
	// says nothing at all, and neither is evidence the app is gone.
	if apps, err := inv.ListApps(ctx, true); err == nil {
		for _, a := range apps {
			if a.ID == id {
				return nil, mobiumerr.New(mobiumerr.NotConfirmed, "uninstall %s reported success but it is still installed "+
					"— on Android a system app can only have its updates removed", id)
			}
		}
	}
	return Result("uninstalled "+id, AppView{App: id, Device: s.dev.Serial}), nil
}

// AppsView is the result of app_list_apps.
type AppsView struct {
	Apps   []AppEntry `json:"apps"`
	Device string     `json:"device"`
	// IncludesSystem says which question was asked, since an empty list means
	// different things for each.
	IncludesSystem bool `json:"includes_system"`
}

// AppEntry is one installed app. Name is empty on Android, where reading a
// package's label costs a dumpsys per app.
type AppEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	System  bool   `json:"system"`
}
