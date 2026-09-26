package agent

import (
	"context"
	"encoding/xml"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"math"
	"os"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
)

// routeTick is how often a stepped route moves. One second is what a real GPS
// delivers, and matching it keeps an app's own filtering honest — a position
// arriving twenty times a second would exercise a code path no phone produces.
const routeTick = time.Second

// defaultSpeed is meters per second, roughly a brisk drive. Chosen because a
// route with no speed still has to take some definite time.
const defaultSpeed = 15.0

// gpxFile is the little of GPX that a position needs. A track point and a
// waypoint carry the same two attributes; everything else in the format —
// elevation, time, extensions, the schema itself — is someone else's problem.
type gpxFile struct {
	Waypoints []gpxPoint `xml:"wpt"`
	Tracks    []struct {
		Segments []struct {
			Points []gpxPoint `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
	Routes []struct {
		Points []gpxPoint `xml:"rtept"`
	} `xml:"rte"`
}

type gpxPoint struct {
	Lat float64 `xml:"lat,attr"`
	Lon float64 `xml:"lon,attr"`
}

// parseGPX reads waypoints from a GPX file, in document order.
//
// Track points first, then route points, then loose waypoints — which is the
// order a file that has more than one kind almost always means. A recorded
// track is the common case and the one that should win.
func parseGPX(path string) ([]device.Point, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var f gpxFile
	if err := xml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s is not readable as GPX: %w", path, err)
	}
	var out []device.Point
	for _, t := range f.Tracks {
		for _, seg := range t.Segments {
			for _, p := range seg.Points {
				out = append(out, device.Point{Lat: p.Lat, Lon: p.Lon})
			}
		}
	}
	for _, r := range f.Routes {
		for _, p := range r.Points {
			out = append(out, device.Point{Lat: p.Lat, Lon: p.Lon})
		}
	}
	for _, p := range f.Waypoints {
		out = append(out, device.Point{Lat: p.Lat, Lon: p.Lon})
	}
	if len(out) == 0 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s has no track points, route points or waypoints", path)
	}
	return out, nil
}

// parseWaypoints accepts [[lat,lon], ...] or ["lat,lon", ...], since a client
// hand-writing JSON finds the second easier and both are unambiguous.
func parseWaypoints(raw interface{}) ([]device.Point, error) {
	list, ok := raw.([]interface{})
	if !ok {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "waypoints must be a list of [latitude, longitude] pairs")
	}
	var out []device.Point
	for i, item := range list {
		switch v := item.(type) {
		case []interface{}:
			if len(v) != 2 {
				return nil, mobiumerr.New(mobiumerr.InvalidArgument, "waypoint %d has %d values, want latitude and longitude",
					i+1, len(v))
			}
			lat, err := floatArg(map[string]interface{}{"latitude": v[0]}, "latitude")
			if err != nil {
				return nil, fmt.Errorf("waypoint %d: %w", i+1, err)
			}
			lon, err := floatArg(map[string]interface{}{"longitude": v[1]}, "longitude")
			if err != nil {
				return nil, fmt.Errorf("waypoint %d: %w", i+1, err)
			}
			out = append(out, device.Point{Lat: lat, Lon: lon})
		case string:
			var p device.Point
			if _, err := fmt.Sscanf(v, "%f,%f", &p.Lat, &p.Lon); err != nil {
				return nil, mobiumerr.New(mobiumerr.InvalidArgument, "waypoint %d (%q) is not \"lat,lon\"", i+1, v)
			}
			out = append(out, p)
		default:
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "waypoint %d is neither a pair nor \"lat,lon\"", i+1)
		}
	}
	return out, nil
}

// meters between two points, by the haversine formula. Good to a fraction of
// a percent at these distances, which is far better than the positions being
// interpolated between deserve.
func meters(a, b device.Point) float64 {
	const r = 6371000.0
	rad := math.Pi / 180
	dLat := (b.Lat - a.Lat) * rad
	dLon := (b.Lon - a.Lon) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(h)))
}

// routeDuration is how long a route takes at a speed, for telling the caller
// before it starts rather than after it fails to end.
func routeDuration(pts []device.Point, speedMPS float64) time.Duration {
	var d float64
	for i := 0; i+1 < len(pts); i++ {
		d += meters(pts[i], pts[i+1])
	}
	if speedMPS <= 0 {
		return 0
	}
	return time.Duration(d / speedMPS * float64(time.Second))
}

// stepRoute walks a route by setting a position once per tick.
//
// This is what a backend without RouteRunner gets. It runs in the daemon for
// the life of the route, so it takes a context that outlives the request that
// started it and stops on cancel — which is what `clear`, a replacement route
// and closing the session all do.
func stepRoute(ctx context.Context, set func(context.Context, float64, float64) error,
	pts []device.Point, speedMPS float64) {
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		steps := int(math.Ceil(meters(a, b) / speedMPS / routeTick.Seconds()))
		if steps < 1 {
			steps = 1
		}
		for k := 1; k <= steps; k++ {
			f := float64(k) / float64(steps)
			lat := a.Lat + (b.Lat-a.Lat)*f
			lon := a.Lon + (b.Lon-a.Lon)*f
			if err := set(ctx, lat, lon); err != nil {
				// Nothing to report this to: the caller left when the route
				// started. Stopping is the honest response — carrying on
				// would leave the device somewhere nobody asked for.
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(routeTick):
			}
		}
	}
}
