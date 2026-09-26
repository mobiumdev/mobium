package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// The appearance vocabulary, mirroring mobiumdriver's so the tool layer can check
// a value before any backend is asked to act on it.
const (
	appearanceLight = "light"
	appearanceDark  = "dark"
	appearanceAuto  = "auto"
)

// appearance is app_appearance: read the light/dark setting, or change it.
//
// Dark mode is one of the few device-state settings worth having early. It is
// a genuinely different rendering of every screen, it is where contrast and
// hard-coded colors break, and testing it by hand means digging through
// Settings on two platforms that put it in different places.
func (h *Handlers) appearance(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.appearanceOn(ctx, s, args)
}

// appearanceOn is app_appearance once the device is resolved.
func (h *Handlers) appearanceOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsAppearance(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapAppearance, "read or change the appearance")
	}

	want := strings.ToLower(stringArg(args, "appearance"))
	// Checked here rather than only in the driver, so the message is the same
	// whichever backend is in use and a typo never reaches the device. The
	// drivers keep their own checks: "auto" is a real mode on Android and a
	// platform fact that iOS lacks, which only they can speak to.
	switch want {
	case "", appearanceLight, appearanceDark, appearanceAuto:
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown appearance %q (want %q, %q, or %q on Android)",
			want, appearanceLight, appearanceDark, appearanceAuto)
	}

	if want == "" {
		mode, err := ctrl.Appearance(ctx)
		if err != nil {
			return nil, err
		}
		return Result(mode, AppearanceView{Appearance: mode, Device: s.dev.Serial}), nil
	}

	before, err := ctrl.Appearance(ctx)
	if err != nil {
		return nil, err
	}
	if before == want {
		return Result(fmt.Sprintf("already %s", want),
			AppearanceView{Appearance: want, Device: s.dev.Serial}), nil
	}
	// The driver confirms the change by reading it back, so reaching here
	// means the setting really moved rather than the command having said so.
	if err := ctrl.SetAppearance(ctx, want); err != nil {
		return nil, err
	}
	// Every ref from the previous screen belongs to a differently rendered
	// version of it. Bounds usually survive a theme change and sometimes do
	// not, and a stale ref is worse than no ref.
	delete(h.refs, s.dev.Serial)

	return Result(fmt.Sprintf("%s (was %s)", want, before),
		AppearanceView{Appearance: want, Previous: before, Device: s.dev.Serial}), nil
}

// AppearanceView is the result of app_appearance.
type AppearanceView struct {
	Appearance string `json:"appearance"`
	// Previous is set only when the call changed something.
	Previous string `json:"previous,omitempty"`
	Device   string `json:"device"`
}
