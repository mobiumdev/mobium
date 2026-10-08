package mobiumdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

func TestIOSRotationDegreesRoundTrip(t *testing.T) {
	for mode, z := range iosRotationZ {
		got, err := iosOrientationForZ(z)
		if err != nil || got != mode {
			t.Errorf("%d degrees read as %q (%v), want %q", z, got, err, mode)
		}
	}
	// Android's first quarter turn is the device turned counterclockwise,
	// which WebDriverAgent calls landscape left, at 270 degrees.
	if q, _ := OrientationQuarter(OrientationLandscape); q != 1 || iosRotationZ[OrientationLandscape] != 270 {
		t.Error("landscape is not the same turn on both platforms")
	}
	if _, err := iosOrientationForZ(-1); err == nil {
		t.Error("WebDriverAgent's unknown orientation, -1, was read as a turn")
	}
}

// fakeRotation is WebDriverAgent in front of an app that supports the
// rotations in allowed: a rotation it does not support is refused, as
// WebDriverAgent refuses it, and the app stays where it was.
func fakeRotation(t *testing.T, allowed ...int) *WDA {
	t.Helper()
	var mu sync.Mutex
	z := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var body struct{ Z int }
			json.NewDecoder(r.Body).Decode(&body)
			for _, a := range allowed {
				if a == body.Z {
					z = body.Z
					w.Write([]byte(`{"value":null}`))
					return
				}
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"value":{"error":"invalid element state","message":"The current rotation cannot be set"}}`))
			return
		}
		fmt.Fprintf(w, `{"value":{"x":0,"y":0,"z":%d}}`, z)
	}))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	return &WDA{w3c: c, scale: 1}
}

func TestWDASetOrientationTurnsAndReadsBack(t *testing.T) {
	d := fakeRotation(t, 0, 90, 270)
	ctx := context.Background()
	for _, mode := range []string{OrientationLandscape, OrientationLandscapeReverse, OrientationPortrait} {
		if err := d.SetOrientation(ctx, mode); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if got, locked, _ := d.Orientation(ctx); got != mode || locked {
			t.Errorf("after %s read %s, locked %v", mode, got, locked)
		}
	}
}

// MobiumApp supports portrait alone on an iPhone: the turn is refused, the app
// stays upright, and the refusal says where it stayed.
func TestWDASetOrientationRefusesAnAppThatDoesNotTurn(t *testing.T) {
	d := fakeRotation(t, 0)
	err := d.SetOrientation(context.Background(), OrientationLandscape)
	if mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed || !strings.Contains(err.Error(), "still portrait") {
		t.Fatalf("a pinned app was reported as %v", err)
	}
	err = d.SetOrientation(context.Background(), OrientationAuto)
	if mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Errorf("auto, which iOS has no way to do from outside, was %v", err)
	}
}

// A refused turn takes its request back. WebDriverAgent sets the device's
// orientation whether or not the app in front turns, so on the iPhone a
// landscape refused by Settings turned Wikipedia, launched next. The fake
// keeps the app upright and records every rotation asked for: the last one
// must be where the app stayed.
func TestWDARefusedTurnIsTakenBack(t *testing.T) {
	var mu sync.Mutex
	var asked []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var body struct{ Z int }
			json.NewDecoder(r.Body).Decode(&body)
			asked = append(asked, body.Z)
			w.Write([]byte(`{"value":null}`)) // accepted, as on the phone
			return
		}
		w.Write([]byte(`{"value":{"x":0,"y":0,"z":0}}`)) // the app stays upright
	}))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1}

	err := d.SetOrientation(context.Background(), OrientationLandscape)
	if mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed || !strings.Contains(err.Error(), "taken back") {
		t.Fatalf("a turn the app did not take was reported as %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 2 || asked[0] != 270 || asked[1] != 0 {
		t.Errorf("rotations asked for: %v, want landscape and then portrait again", asked)
	}
}

// CHALLENGES 289: Safari brought back from the background took landscape
// and still read portrait at once — three times in three — and was taken
// back; a second later it turned. The turn is read until it shows.
func TestWDATurnIsWaitedFor(t *testing.T) {
	var mu sync.Mutex
	reads, z := 0, 0
	var asked []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var body struct{ Z int }
			json.NewDecoder(r.Body).Decode(&body)
			asked = append(asked, body.Z)
			z = body.Z
			w.Write([]byte(`{"value":null}`))
			return
		}
		reads++
		shown := z
		if reads <= 2 { // still turning
			shown = 0
		}
		fmt.Fprintf(w, `{"value":{"x":0,"y":0,"z":%d}}`, shown)
	}))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1}

	if err := d.SetOrientation(context.Background(), OrientationLandscape); err != nil {
		t.Fatalf("a turn that showed a moment later was refused: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0] != 270 {
		t.Errorf("rotations asked for: %v, want landscape once", asked)
	}
}
