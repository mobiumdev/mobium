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
)

// An iPad's home screen, once an app has been used, is read by WebDriverAgent
// as the Dock's recent-apps service, with nothing in it to map; named
// SpringBoard, it reads the home screen. The fake answers as the iPad mini
// did: the Dock's service unless SpringBoard is the app to read. The read
// must come back as the home screen, and leave the choice to WebDriverAgent
// again afterwards. CHALLENGES 224.
func TestAnIPadHomeScreenIsReadAsSpringBoard(t *testing.T) {
	app := func(bundle, child string) string {
		return `<?xml version="1.0" encoding="UTF-8"?><XCUIElementTypeApplication type="XCUIElementTypeApplication" ` +
			`name="x" label="x" enabled="true" visible="true" accessible="false" x="0" y="0" width="744" height="1133" ` +
			`bundleId="` + bundle + `">` + child + `</XCUIElementTypeApplication>`
	}
	dock := app(dockFolderService, "")
	home := app(springboardBundleID, `<XCUIElementTypeIcon type="XCUIElementTypeIcon" name="Settings" label="Settings" `+
		`enabled="true" visible="true" accessible="true" x="300" y="500" width="80" height="80"/>`)

	var mu sync.Mutex
	hint := "auto"
	var settings []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/appium/settings"):
			var body struct {
				Settings map[string]string `json:"settings"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			hint = body.Settings["defaultActiveApplication"]
			settings = append(settings, hint)
			w.Write([]byte(`{"value":null}`))
		case strings.HasSuffix(r.URL.Path, "/source"):
			xml := dock
			if hint == springboardBundleID {
				xml = home
			}
			b, _ := json.Marshal(map[string]string{"value": xml})
			w.Write(b)
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1}

	tree, err := d.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Package(); got != springboardBundleID {
		t.Errorf("the home screen read as %s", got)
	}
	if len(tree.Map()) == 0 {
		t.Error("the home screen mapped nothing")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(settings) != 2 || settings[0] != springboardBundleID || settings[1] != "auto" {
		t.Errorf("the app to read was set %v, want SpringBoard for one read and then auto", settings)
	}
}
