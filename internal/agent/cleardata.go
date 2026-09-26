package agent

import (
	"context"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
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
	c, ok := mobiumdriver.AsDataClearer(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapClearData, "clear an app's data")
	}
	id, err := appID(args)
	if err != nil {
		return nil, err
	}

	// Clearing stops the app, so whatever was on screen from it is gone.
	s.closeWeb()
	res, err := c.ClearData(ctx, id)
	if err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)

	lines := []string{"cleared " + id}
	for _, e := range res.Emptied {
		lines = append(lines, "  emptied: "+e)
	}
	for _, k := range res.Kept {
		lines = append(lines, "  kept:    "+k)
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
		Emptied: res.Emptied, Kept: res.Kept, StillGranted: res.StillGranted,
	}), nil
}
