package mobiumdriver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// fakeZoneWDA is WebDriverAgent on a device in Los Angeles with com.example
// in front, keeping the environment of every launch.
func fakeZoneWDA(t *testing.T) (*WDA, *[]map[string]string) {
	t.Helper()
	var mu sync.Mutex
	var envs []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/wda/device/info"):
			w.Write([]byte(`{"value":{"timeZone":"America/Los_Angeles"}}`))
		case strings.HasSuffix(r.URL.Path, "/source"):
			out, _ := json.Marshal(map[string]string{"value": `<XCUIElementTypeApplication type="XCUIElementTypeApplication" ` +
				`name="Example" bundleId="com.example" x="0" y="0" width="402" height="874" visible="true" enabled="true"/>`})
			w.Write(out)
		case strings.HasSuffix(r.URL.Path, "/wda/apps/launch"):
			var body struct {
				Environment map[string]string `json:"environment"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			envs = append(envs, body.Environment)
			w.Write([]byte(`{"value":null}`))
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	return &WDA{w3c: c, scale: 1}, &envs
}

func TestTheTimezoneIsALaunchEnvironmentForTheSession(t *testing.T) {
	ctx := context.Background()
	d, envs := fakeZoneWDA(t)
	if z, _ := d.Timezone(ctx); z != "America/Los_Angeles" {
		t.Fatalf("before: %s, want the device's", z)
	}
	if err := d.SetTimezone(ctx, "Mars/Olympus"); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("an unknown zone was %v", err)
	}
	if err := d.SetTimezone(ctx, "Asia/Tokyo"); err != nil {
		t.Fatal(err)
	}
	if len(*envs) != 1 || (*envs)[0]["TZ"] != "Asia/Tokyo" {
		t.Fatalf("the app in front was launched with %v", *envs)
	}
	if z, _ := d.Timezone(ctx); z != "Asia/Tokyo" {
		t.Errorf("read back %s", z)
	}
	// The device's own zone ends it: the read is the device's again, and the
	// app in front is launched without TZ.
	if err := d.SetTimezone(ctx, "America/Los_Angeles"); err != nil {
		t.Fatal(err)
	}
	if z := d.sessionZone(); z != "" {
		t.Errorf("the session still holds %s", z)
	}
	if len(*envs) != 2 || (*envs)[1]["TZ"] != "" {
		t.Errorf("after the reset the app in front was launched with %v", *envs)
	}
}
