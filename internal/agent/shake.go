package agent

import (
	"context"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// ShakeView is the result of app_shake.
type ShakeView struct {
	Device string `json:"device"`
	// Sent says what the device was sent. Whether an app took it for a
	// shake is the app's own detector's business, and not something read
	// back here.
	Sent string `json:"sent"`
}

func (h *Handlers) shake(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	sh, ok := mobiumdriver.AsShaker(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapShake, "shake the device")
	}
	if err := sh.Shake(ctx); err != nil {
		return nil, err
	}
	// A shake can open an undo prompt or a report screen, so the last map's
	// refs may be gone.
	delete(h.refs, s.dev.Serial)
	sent := "the simulator's shake, as its Device menu sends it"
	if s.backend != BackendWDA {
		sent = "a shake on the emulator's accelerometer, ±20 m/s² side to side for about 1.5s, then back at rest"
	}
	return Result("sent "+sent+"; whether the app reacts is up to its own shake detector",
		ShakeView{Device: s.dev.Serial, Sent: sent}), nil
}
