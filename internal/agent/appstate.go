package agent

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// maxBackground bounds app_background. The call holds the device for the
// whole absence, and a longer one is Home and a launch later, which leaves
// the caller free in between.
const maxBackground = 180 * time.Second

// backgroundNotice is how long an absence runs before the caller is told
// about it: the daemon's client gives up on a call silent for two minutes.
const backgroundNotice = 60 * time.Second

// AppStateView is the result of app_state.
type AppStateView struct {
	App    string `json:"app"`
	Device string `json:"device"`
	// State is not_installed, not_running, background or foreground.
	State string `json:"state"`
	// Suspended is, for an app in the background on iOS, whether it is
	// suspended. Absent on Android, which has nothing that says.
	Suspended *bool `json:"suspended,omitempty"`
	// CoveredBy is, for an app in front, what app_current reports when that
	// is something else — a permission prompt is another process's window
	// over an app that is still in front.
	CoveredBy string `json:"covered_by,omitempty"`
}

// BackgroundView is the result of app_background.
type BackgroundView struct {
	App     string  `json:"app"`
	Device  string  `json:"device"`
	Seconds float64 `json:"seconds"`
	// AwayMS is how long it took from sending the app away to seeing it in
	// front again, in milliseconds.
	AwayMS int64 `json:"away_ms"`
}

func (h *Handlers) appState(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	a, ok := mobiumdriver.AsAppStates(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapAppState, "read an app's state")
	}
	id, err := appID(args)
	if err != nil {
		return nil, err
	}
	st, err := a.AppState(ctx, id)
	if err != nil {
		return nil, err
	}
	view := AppStateView{App: id, Device: s.dev.Serial, State: st.State, Suspended: st.Suspended}
	var msg string
	switch st.State {
	case device.AppNotInstalled:
		msg = id + " is not installed"
	case device.AppNotRunning:
		msg = id + " is installed and not running"
	case device.AppBackground:
		msg = id + " is running in the background"
		if st.Suspended != nil && *st.Suspended {
			msg += ", suspended"
		}
	case device.AppForeground:
		msg = id + " is in front"
		if front, _ := h.screenNow(ctx, s); front != "" && front != id {
			view.CoveredBy = front
			msg += ", under " + front
		}
	}
	return Result(msg, view), nil
}

func (h *Handlers) background(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	a, ok := mobiumdriver.AsAppStates(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapAppState, "send an app to the background")
	}
	secs, err := floatArg(args, "seconds")
	if err != nil {
		return nil, err
	}
	if secs <= 0 || math.IsNaN(secs) || time.Duration(secs*float64(time.Second)) > maxBackground {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"seconds must be more than 0 and at most %d", int(maxBackground.Seconds())).
			WithRemedy("for a longer absence, app_press home, and app_launch the app when it should come back")
	}
	d := time.Duration(secs * float64(time.Second))

	// The app is named, or it is the one in front. Either way it has to be in
	// front now: backgrounding anything else would send the wrong app away.
	id := stringArg(args, "app")
	if id == "" {
		if id, _ = h.screenNow(ctx, s); id == "" {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "could not tell which app is in front").
				WithRemedy("name the app with app")
		}
	}
	if st, err := a.AppState(ctx, id); err != nil {
		return nil, err
	} else if st.State != device.AppForeground {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not in front (%s), so there is nothing to send away",
			id, st.State).WithRemedy("app_launch it first")
	}

	s.closeWeb()
	if h.progress != nil && d > backgroundNotice {
		h.progress(fmt.Sprintf("sending %s to the background for %s", id, d))
	}
	start := time.Now()
	if err := a.Background(ctx, id, d); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)
	if app := h.awaitForeground(ctx, s, id, "", ""); app != id {
		if err := h.lockedInstead(ctx, s, "brought "+id+" back"); err != nil {
			return nil, err
		}
	}
	st, err := a.AppState(ctx, id)
	if err != nil {
		return nil, err
	}
	if st.State != device.AppForeground {
		return nil, mobiumerr.New(mobiumerr.NotConfirmed,
			"%s was sent to the background and did not come back: it is %s", id, st.State)
	}
	away := time.Since(start)
	return Result(fmt.Sprintf("%s was in the background for %gs and is in front again", id, secs),
		BackgroundView{App: id, Device: s.dev.Serial, Seconds: secs, AwayMS: away.Milliseconds()}), nil
}
