package mobiumdriver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// CHALLENGES 288: with a notification banner over MobiumApp, WebDriverAgent
// reads SpringBoard, and the keyboard and focus reads asked it rather than
// the app: "the keyboard is hidden" over a keyboard that was up, so
// `keyboard --hide` answered "already hidden" and the next tap was refused
// as covered. This server answers as the simulator did — SpringBoard with the
// banner until the reads are pointed at the app, then the app with its
// keyboard up and a field focused.
func TestKeyboardIsReadUnderABanner(t *testing.T) {
	banner, err := os.ReadFile("testdata/springboard-banner-ios26.xml")
	if err != nil {
		t.Fatal(err)
	}
	app := `<?xml version="1.0" encoding="UTF-8"?><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="MobiumApp" label="MobiumApp" bundleId="dev.mobium.mobiumapp" x="0" y="0" width="402" height="874" visible="true" enabled="true"><XCUIElementTypeTextField type="XCUIElementTypeTextField" name="otpField" x="20" y="200" width="300" height="44" visible="true" enabled="true"/><XCUIElementTypeKeyboard type="XCUIElementTypeKeyboard" x="0" y="583" width="402" height="291" visible="true" enabled="true"/></XCUIElementTypeApplication>`
	var mu sync.Mutex
	hint := "auto"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		inApp := hint == "dev.mobium.mobiumapp"
		reply := func(v interface{}) { _ = json.NewEncoder(w).Encode(map[string]interface{}{"value": v}) }
		switch {
		case strings.HasSuffix(r.URL.Path, "/appium/settings"):
			var body struct {
				Settings map[string]string `json:"settings"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			hint = body.Settings["defaultActiveApplication"]
			reply(nil)
		case strings.HasSuffix(r.URL.Path, "/source"):
			if inApp {
				reply(app)
			} else {
				reply(string(banner))
			}
		case strings.HasSuffix(r.URL.Path, "/element/active"):
			if !inApp {
				w.WriteHeader(http.StatusNotFound)
				reply(map[string]string{"error": "no such element", "message": "nothing has focus"})
				return
			}
			reply(map[string]string{"ELEMENT": "EL1", "element-6066-11e4-a52e-4f735466cecf": "EL1"})
		case strings.HasSuffix(r.URL.Path, "/attribute/name"):
			reply("otpField")
		case strings.HasSuffix(r.URL.Path, "/attribute/type"):
			reply("XCUIElementTypeTextField")
		case strings.HasSuffix(r.URL.Path, "/attribute/value"):
			reply("111111")
		default:
			reply(nil)
		}
	}))
	defer srv.Close()
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 3}
	ctx := context.Background()

	shown, err := d.KeyboardShown(ctx)
	if err != nil || !shown {
		t.Errorf("under a banner the keyboard read as shown=%v (%v)", shown, err)
	}
	f, err := d.FocusedField(ctx)
	if err != nil || f == nil || f.ID != "otpField" || f.Value != "111111" {
		t.Errorf("under a banner the focused field read as %+v (%v)", f, err)
	}
	if hint != "auto" {
		t.Errorf("the reads left WebDriverAgent pointed at %q", hint)
	}
}
