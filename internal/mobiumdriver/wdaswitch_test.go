package mobiumdriver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
)

// fakeSwitch is WebDriverAgent mid-switch on a phone: the app being left
// reads as in front (4) for the first `overlap` state queries, then as in the
// background (3) — or for ever, with overlap -1.
func fakeSwitch(t *testing.T, overlap int) (*WDA, *int) {
	t.Helper()
	var mu sync.Mutex
	queries := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/wda/apps/state") {
			queries++
			if overlap < 0 || queries <= overlap {
				w.Write([]byte(`{"value":4}`))
			} else {
				w.Write([]byte(`{"value":3}`))
			}
			return
		}
		w.Write([]byte(`{"value":null}`))
	}))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1, phone: &device.Devicectl{}}
	d.expecting, d.leaving = "com.example.new", "com.example.old"
	return d, &queries
}

// The hint comes off only once the app being left has left the front: a
// light read can see the new app while iOS still reports both in front, and
// the hangs measured were launches confirmed then.
func TestTheHintWaitsForTheAppBeingLeft(t *testing.T) {
	ctx := context.Background()
	d, queries := fakeSwitch(t, 3)
	if !d.switchSettled(ctx, "com.example.new") {
		t.Fatal("the switch never settled though the old app left the front")
	}
	if *queries != 4 {
		t.Errorf("asked %d times, want until the old app left: 4", *queries)
	}

	d, _ = fakeSwitch(t, -1)
	start := time.Now()
	if d.switchSettled(ctx, "com.example.new") {
		t.Error("settled while the old app was still in front")
	}
	if took := time.Since(start); took < switchSettleWait {
		t.Errorf("gave up after %s, before switchSettleWait", took)
	}

	// SpringBoard always reads as in front, and leaving it is not waited for.
	d, queries = fakeSwitch(t, -1)
	d.leaving = springboardBundleID
	if !d.switchSettled(ctx, "com.example.new") || *queries != 0 {
		t.Errorf("leaving the home screen waited: %d queries", *queries)
	}
}
