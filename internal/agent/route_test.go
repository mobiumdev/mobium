package agent

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
)

// Shorthands so the arithmetic below reads as arithmetic.
type devicePoint = device.Point

func point(lat, lon float64) device.Point { return device.Point{Lat: lat, Lon: lon} }

// A file with all three shapes at once, in the order the parser must prefer
// them: a recorded track is the common case and should win over a loose
// waypoint that happens to be in the same file.
const gpxSample = `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="test" xmlns="http://www.topografix.com/GPX/1/1">
  <wpt lat="1.5" lon="1.6"><name>loose</name></wpt>
  <trk><name>t</name><trkseg>
    <trkpt lat="51.5074" lon="-0.1278"><ele>10</ele><time>2026-09-17T10:00:00Z</time></trkpt>
    <trkpt lat="51.5081" lon="-0.1300"></trkpt>
    <trkpt lat="51.5090" lon="-0.1325"/>
  </trkseg></trk>
  <rte><rtept lat="2.5" lon="2.6"/></rte>
</gpx>`

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "r.gpx")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseGPXPrefersTrackPoints(t *testing.T) {
	pts, err := parseGPX(writeTemp(t, gpxSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 5 {
		t.Fatalf("parsed %d points, want 5: %+v", len(pts), pts)
	}
	// Track points first, then route points, then loose waypoints.
	if pts[0].Lat != 51.5074 || pts[0].Lon != -0.1278 {
		t.Errorf("first point = %+v, want the first trkpt", pts[0])
	}
	if pts[2].Lat != 51.5090 {
		t.Errorf("third point = %+v, want the self-closing trkpt", pts[2])
	}
	if pts[3].Lat != 2.5 {
		t.Errorf("fourth point = %+v, want the rtept", pts[3])
	}
	if pts[4].Lat != 1.5 {
		t.Errorf("fifth point = %+v, want the loose wpt", pts[4])
	}
}

func TestParseGPXRefusesWhatItCannotUse(t *testing.T) {
	if _, err := parseGPX(writeTemp(t, `<gpx version="1.1"></gpx>`)); err == nil {
		t.Error("a GPX file with no points should be refused, not silently empty")
	}
	if _, err := parseGPX(writeTemp(t, "not xml at all")); err == nil {
		t.Error("a file that is not GPX should be refused")
	}
	if _, err := parseGPX(filepath.Join(t.TempDir(), "missing.gpx")); err == nil {
		t.Error("a missing file should be refused")
	}
}

func TestParseWaypointsAcceptsBothShapes(t *testing.T) {
	pairs, err := parseWaypoints([]interface{}{
		[]interface{}{51.5074, -0.1278},
		[]interface{}{48.8566, 2.3522},
	})
	if err != nil {
		t.Fatal(err)
	}
	strs, err := parseWaypoints([]interface{}{"51.5074,-0.1278", "48.8566,2.3522"})
	if err != nil {
		t.Fatal(err)
	}
	if pairs[0] != strs[0] || pairs[1] != strs[1] {
		t.Errorf("the two spellings disagree: %+v vs %+v", pairs, strs)
	}
	// A negative longitude has to survive both, since that is half the planet
	// and the CLI had a separate defect about exactly this.
	if strs[0].Lon != -0.1278 {
		t.Errorf("longitude = %v, want -0.1278", strs[0].Lon)
	}
}

func TestParseWaypointsRefusesTheAmbiguous(t *testing.T) {
	for _, bad := range []interface{}{
		"not a list",
		[]interface{}{[]interface{}{1.0}},
		[]interface{}{[]interface{}{1.0, 2.0, 3.0}},
		[]interface{}{"51.5"},
		[]interface{}{42.0},
	} {
		if _, err := parseWaypoints(bad); err == nil {
			t.Errorf("%v was accepted and should not be", bad)
		}
	}
}

func TestMetersAgainstAKnownDistance(t *testing.T) {
	// London to Paris, about 343.5 km great-circle.
	d := meters(point(51.5074, -0.1278), point(48.8566, 2.3522))
	if math.Abs(d-343500) > 3000 {
		t.Errorf("London to Paris = %.0f m, want about 343500", d)
	}
	// A degree of latitude is about 111 km anywhere.
	if d := meters(point(0, 0), point(1, 0)); math.Abs(d-111195) > 500 {
		t.Errorf("one degree of latitude = %.0f m, want about 111195", d)
	}
	if d := meters(point(10, 10), point(10, 10)); d != 0 {
		t.Errorf("a point is %.6f m from itself", d)
	}
}

func TestRouteDurationIsDistanceOverSpeed(t *testing.T) {
	pts := []devicePoint{point(0, 0), point(1, 0)}
	got := routeDuration(pts, 111.195).Seconds()
	if math.Abs(got-1000) > 10 {
		t.Errorf("111km at 111.195 m/s took %.0fs, want about 1000", got)
	}
	if d := routeDuration(pts, 0); d != 0 {
		t.Errorf("a zero speed gave a duration of %s rather than refusing to guess", d)
	}
}
