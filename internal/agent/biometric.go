package agent

import (
	"context"
	"fmt"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// BiometricView is the result of app_biometric.
type BiometricView struct {
	Device string `json:"device"`
	// Kind is "face" or "fingerprint": the one this device can be shown.
	Kind     string `json:"kind"`
	Enrolled bool   `json:"enrolled"`
	// LockedOut is an emulator's sensor refusing every touch for now, after
	// too many that did not match.
	LockedOut bool `json:"locked_out,omitempty"`
	// Outcome is what the device made of a face or finger presented to it —
	// accepted, not recognized, failed (the prompt gave up) or locked out —
	// read back, and empty when nothing on screen could say. Whether the app
	// signed in is the app's to show.
	Outcome string `json:"outcome,omitempty"`
	Note    string `json:"note,omitempty"`
}

func (h *Handlers) biometric(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	action := stringArg(args, "action")
	if action == "" {
		action = mobiumdriver.BioStatus
	}
	switch action {
	case mobiumdriver.BioStatus, mobiumdriver.BioEnroll, mobiumdriver.BioUnenroll,
		mobiumdriver.BioMatch, mobiumdriver.BioNoMatch:
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "action must be status, enroll, unenroll, match "+
			"or nomatch, not %q", action)
	}
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	b, ok := mobiumdriver.AsBiometrics(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapBiometric, "enroll or present a face or finger")
	}
	st, err := b.Biometric(ctx, action)
	if err != nil {
		return nil, err
	}
	if action == mobiumdriver.BioMatch || action == mobiumdriver.BioNoMatch || action == mobiumdriver.BioEnroll {
		// A sign-in that went through, or Settings visited, changes the
		// screen: the last map's refs may be gone.
		delete(h.refs, s.dev.Serial)
	}
	view := BiometricView{Device: s.dev.Serial, Kind: st.Kind, Enrolled: st.Enrolled, LockedOut: st.LockedOut, Outcome: st.Outcome, Note: st.Note}
	enrolled := "no " + st.Kind + " enrolled"
	if st.Enrolled {
		enrolled = "a " + st.Kind + " enrolled"
	}
	msg := enrolled
	if st.LockedOut && action == mobiumdriver.BioStatus {
		msg += "; the sensor is locked out after too many that did not match"
	}
	switch {
	case st.Outcome != "":
		msg = fmt.Sprintf("presented a %s: %s", st.Kind, st.Outcome)
	case action == mobiumdriver.BioMatch || action == mobiumdriver.BioNoMatch:
		msg = "presented a " + st.Kind
	}
	if st.Note != "" {
		msg += "; " + st.Note
	}
	return Result(msg, view), nil
}
