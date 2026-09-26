package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// location is app_location: read or set where the device believes it is.
//
// The two platforms answer different numbers of questions here and the tool
// says which. Android sets through a test provider and reads the position back
// tagged as injected; iOS sets through simctl and cannot be read at all, since
// `simctl location` has set, clear, run and start and no get. That asymmetry
// is stated per platform rather than flattened into one pessimistic sentence
// that would be wrong on Android — the mistake CHALLENGES 50 records.
func (h *Handlers) location(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.locationOn(ctx, s, args)
}

// locationOn is app_location once the device is resolved.
func (h *Handlers) locationOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	reader, canRead := mobiumdriver.AsGeolocationReader(s.driver)

	_, hasLat := args["latitude"]
	_, hasLon := args["longitude"]
	clear := boolArg(args, "clear")
	gpx, waypoints := stringArg(args, "gpx"), args["waypoints"]
	// A route is a write, and has to be recognized before the read branch
	// below — otherwise `location --gpx file` reads as "no arguments given"
	// and answers the question nobody asked.
	route := gpx != "" || waypoints != nil

	// Reading is the whole request when nothing was asked to change.
	if !hasLat && !hasLon && !clear && !route {
		if !canRead {
			return nil, mobiumerr.New(mobiumerr.Unsupported,
				"the %s backend cannot report where the device is — `simctl location` "+
					"has set, clear, run and start and no get, so on iOS a position can be "+
					"set but never read back. Pass latitude and longitude to set one",
				s.backend)
		}
		fix, err := reader.Location(ctx)
		if err != nil {
			return nil, err
		}
		if fix == nil {
			return Result("the device reports no position at all",
				LocationView{Serial: s.dev.Serial}), nil
		}
		return Result(describeFix(fix),
			LocationView{Latitude: fix.Lat, Longitude: fix.Lon, Mock: fix.Mock,
				Mocking: fix.Mocking, Known: true, Serial: s.dev.Serial}), nil
	}

	ctrl, ok := mobiumdriver.AsGeolocation(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapGeolocation, "place the device anywhere")
	}

	if route {
		return h.startRoute(ctx, s, ctrl, gpx, waypoints, args)
	}

	if clear {
		// A running route has to stop first, or it carries on setting
		// positions over the top of the clear.
		s.stopRoute()
		if err := ctrl.ClearLocation(ctx); err != nil {
			return nil, err
		}
		// Say exactly what was cleared, per platform. The mechanisms differ
		// and a message naming the wrong one is worse than a vague one: on
		// Android removing the provider leaves the last-known location behind,
		// and there is no such cache to warn about on iOS.
		if !canRead {
			return Result("the simulated position is cleared", LocationView{Serial: s.dev.Serial}), nil
		}
		return Result("the test provider is removed — the device will report its own "+
			"position the next time something asks for one. Its last known location is "+
			"still the injected one until then; Android keeps that cache and nothing "+
			"here can clear it",
			LocationView{Serial: s.dev.Serial}), nil
	}

	if !hasLat || !hasLon {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_location needs both latitude and longitude, "+
			"e.g. latitude 51.5074, longitude -0.1278")
	}
	lat, err := floatArg(args, "latitude")
	if err != nil {
		return nil, err
	}
	lon, err := floatArg(args, "longitude")
	if err != nil {
		return nil, err
	}
	if lat < -90 || lat > 90 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "latitude %v is outside -90..90", lat)
	}
	if lon < -180 || lon > 180 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "longitude %v is outside -180..180", lon)
	}

	if err := ctrl.SetLocation(ctx, lat, lon); err != nil {
		return nil, err
	}

	view := LocationView{Latitude: lat, Longitude: lon, Serial: s.dev.Serial}
	if !canRead {
		// Say which question was answered. The set is real; the state is not
		// observable, and claiming otherwise would be the shape of defect 27.
		return Result(fmt.Sprintf("the device was sent to %.6f,%.6f — iOS cannot report "+
			"its position back, so this confirms the request was accepted and not that "+
			"an app will read it", lat, lon), view), nil
	}
	view.Mock, view.Mocking, view.Known = true, true, true
	return Result(fmt.Sprintf("the device is at %.6f,%.6f, injected and read back", lat, lon),
		view), nil
}

// startRoute moves the device along a series of waypoints.
//
// Who owns the timer differs by platform and the answer is visible in the
// reply, because it changes what the caller can conclude. simctl interpolates
// natively and returns immediately; Android has no such command, so the daemon
// steps a test provider until the route ends or something cancels it.
func (h *Handlers) startRoute(ctx context.Context, s *session, ctrl mobiumdriver.Geolocation,
	gpx string, wp interface{}, args map[string]interface{}) (*ToolsCallResult, error) {

	var pts []device.Point
	var err error
	switch {
	case gpx != "" && wp != nil:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "give either gpx or waypoints, not both")
	case gpx != "":
		pts, err = parseGPX(gpx)
	default:
		pts, err = parseWaypoints(wp)
	}
	if err != nil {
		return nil, err
	}
	if len(pts) < 2 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "a route needs at least two waypoints, got %d — "+
			"use latitude and longitude for a single position", len(pts))
	}

	speed := defaultSpeed
	if _, given := args["speed"]; given {
		if speed, err = floatArg(args, "speed"); err != nil {
			return nil, err
		}
		if speed <= 0 {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "speed must be above zero meters per second, got %v", speed)
		}
	}

	took := routeDuration(pts, speed)
	view := LocationView{Serial: s.dev.Serial, Waypoints: len(pts),
		Speed: speed, Seconds: took.Seconds(), Routing: true}

	// One route per device. Two things driving one position is not a state
	// anybody can reason about, so a new route replaces the old.
	s.stopRoute()

	if runner, ok := mobiumdriver.AsRouteRunner(s.driver); ok {
		if err := runner.StartRoute(ctx, pts, speed); err != nil {
			return nil, err
		}
		return Result(fmt.Sprintf("following %d waypoints at %.0f m/s, about %s — "+
			"the simulator interpolates this itself. It reports that the waypoints "+
			"parsed, not that an app sees the device move; read that from inside an app",
			len(pts), speed, took.Round(time.Second)), view), nil
	}

	// Background, so the call returns rather than blocking for the length of
	// the route. context.Background() on purpose: the request's context ends
	// when this function does, and the route has to outlive it.
	rctx, cancel := context.WithCancel(context.Background())
	s.route = cancel
	go stepRoute(rctx, ctrl.SetLocation, pts, speed)

	return Result(fmt.Sprintf("following %d waypoints at %.0f m/s, about %s — "+
		"stepped once a second by the daemon, since Android has no route command. "+
		"`location --clear` stops it, and so does starting another",
		len(pts), speed, took.Round(time.Second)), view), nil
}

func describeFix(f *device.Fix) string {
	switch {
	case f.Mocking:
		return fmt.Sprintf("the device is at %.6f,%.6f, injected and still being injected",
			f.Lat, f.Lon)
	case f.Mock:
		// The two disagree after a clear, and the difference is the whole
		// reason both are read.
		return fmt.Sprintf("the device's last known position is %.6f,%.6f, which was "+
			"injected — but no test provider is installed now, so it will report its "+
			"own position the next time something asks", f.Lat, f.Lon)
	default:
		return fmt.Sprintf("the device is at %.6f,%.6f, which it reports on its own",
			f.Lat, f.Lon)
	}
}

// LocationView is the result of app_location.
type LocationView struct {
	Latitude  float64 `json:"latitude,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`
	// Mock is true when the position came from a test provider. Without it a
	// read is not evidence a set took, since a device holds its last position.
	Mock bool `json:"mock"`
	// Mocking is whether a test provider is installed now, which is a
	// different question from whether this fix came from one.
	Mocking bool `json:"mocking"`
	// Known is false when the platform cannot report a position at all, which
	// is different from reporting that it has none.
	Known bool `json:"known"`
	// Routing and the three fields below are set only when a route started.
	Routing   bool    `json:"routing,omitempty"`
	Waypoints int     `json:"waypoints,omitempty"`
	Speed     float64 `json:"speed,omitempty"`
	Seconds   float64 `json:"seconds,omitempty"`
	Serial    string  `json:"device"`
}
