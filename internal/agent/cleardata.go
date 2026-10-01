package agent

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// ClearDataView is the result of app_clear_data.
type ClearDataView struct {
	App    string `json:"app"`
	Device string `json:"device"`
	// Emptied is what was read back empty, each saying how it was checked.
	Emptied []string `json:"emptied"`
	// Kept is what clearing leaves as it was. Resetting state for a test
	// means knowing this list.
	Kept []string `json:"kept,omitempty"`
	// StillGranted is, on Android, the runtime permissions still granted
	// afterwards, read back. Never omitted: an empty list is "read, and none
	// is", and null is "not read" — omitempty made the two the same.
	StillGranted []string `json:"still_granted"`
	// NotReadBack is what the reset changed that nothing here can read back,
	// with the measurement that says it does: a phone's permissions.
	NotReadBack []string `json:"not_read_back,omitempty"`
}

func (h *Handlers) clearData(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.clearDataOn(ctx, s, args)
}

// clearDataOn is app_clear_data once the device is resolved.
func (h *Handlers) clearDataOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	id, err := appID(args)
	if err != nil {
		return nil, err
	}
	// The app's bundle, where the reset is a reinstall: a real iPhone. As
	// app_install takes it — a path, or the bundle itself from a caller
	// whose disk is not the daemon's.
	bundle := stringArg(args, "path")
	if content := stringArg(args, "content"); content != "" {
		p, cleanup, err := materializeApp(content, stringArg(args, "name"))
		if err != nil {
			return nil, err
		}
		defer cleanup()
		bundle = p
	}

	var res device.ClearedData
	if bundle != "" {
		r, ok := mobiumdriver.AsBundleResetter(s.driver)
		if !ok {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "this device clears an app's data in place, so it "+
				"takes no bundle — leave out path (--bundle on the CLI)")
		}
		if abs, err := filepath.Abs(bundle); err == nil {
			bundle = abs
		}
		s.closeWeb()
		if res, err = r.ResetFromBundle(ctx, id, bundle); err != nil {
			return nil, err
		}
	} else {
		c, ok := mobiumdriver.AsDataClearer(s.driver)
		if !ok {
			return nil, cannot(s, mobiumdriver.CapClearData, "clear an app's data")
		}
		// Clearing stops the app, so whatever was on screen from it is gone.
		s.closeWeb()
		if res, err = c.ClearData(ctx, id); err != nil {
			return nil, err
		}
	}
	delete(h.refs, s.dev.Serial)

	lines := []string{"cleared " + id}
	for _, e := range res.Emptied {
		lines = append(lines, "  emptied: "+e)
	}
	for _, k := range res.Kept {
		lines = append(lines, "  kept:    "+k)
	}
	for _, n := range res.NotReadBack {
		lines = append(lines, "  reset, not read back: "+n)
	}
	if res.StillGranted != nil {
		g := "none"
		if len(res.StillGranted) > 0 {
			g = strings.Join(res.StillGranted, ", ")
		}
		lines = append(lines, "  runtime permissions still granted: "+g)
	}
	return Result(strings.Join(lines, "\n"), ClearDataView{
		App: id, Device: s.dev.Serial,
		Emptied: res.Emptied, Kept: res.Kept, StillGranted: res.StillGranted, NotReadBack: res.NotReadBack,
	}), nil
}
