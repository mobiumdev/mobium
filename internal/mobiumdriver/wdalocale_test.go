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
)

func TestLaunchArgumentsCarryTheLanguagesAndTheFirstAsLocale(t *testing.T) {
	got := strings.Join(launchArguments([]string{"ja-JP", "en"}), " ")
	if got != "-AppleLanguages (ja-JP, en) -AppleLocale ja_JP" {
		t.Errorf("arguments = %q", got)
	}
}

func TestLocaleTags(t *testing.T) {
	for _, ok := range []string{"ja", "ja-JP", "zh-Hant-TW", "es-419", "fil"} {
		if !localeTag.MatchString(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "not a tag", "ja_JP", "-AppleLanguages", "j"} {
		if localeTag.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

// fakeLaunches is WebDriverAgent knowing one app's state, and keeping what it
// was asked to launch and with which arguments.
func fakeLaunches(t *testing.T, state int) (*WDA, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var body struct {
			BundleID  string   `json:"bundleId"`
			Arguments []string `json:"arguments"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/wda/apps/state"):
			fmt.Fprintf(w, `{"value":%d}`, state)
			return
		case strings.HasSuffix(r.URL.Path, "/wda/apps/terminate"):
			calls = append(calls, "terminate")
		case strings.HasSuffix(r.URL.Path, "/wda/apps/launch"):
			calls = append(calls, "launch "+strings.Join(body.Arguments, " "))
		}
		w.Write([]byte(`{"value":null}`))
	}))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	return &WDA{w3c: c, scale: 1, phone: nil, sim: nil}, &calls
}

// A running app is launched again in the language at once; one that is not
// running only keeps it for its next launch, as Android's does.
func TestSetAppLocalesRelaunchesOnlyARunningApp(t *testing.T) {
	ctx := context.Background()
	d, calls := fakeLaunches(t, 4)
	if err := d.SetAppLocales(ctx, "com.example", []string{"ja-JP"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*calls, " | "); got != "terminate | launch -AppleLanguages (ja-JP) -AppleLocale ja_JP" {
		t.Errorf("a running app saw %q", got)
	}
	if tags, _ := d.AppLocales(ctx, "com.example"); len(tags) != 1 || tags[0] != "ja-JP" {
		t.Errorf("read back %v", tags)
	}

	d, calls = fakeLaunches(t, 1)
	if err := d.SetAppLocales(ctx, "com.example", []string{"ja-JP"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Errorf("an app that was not running was launched: %v", *calls)
	}
	if err := d.SetAppLocales(ctx, "com.example", nil); err != nil {
		t.Fatal(err)
	}
	if tags, _ := d.AppLocales(ctx, "com.example"); len(tags) != 0 {
		t.Errorf("cleared, it still reads %v", tags)
	}
	if err := d.SetAppLocales(ctx, "com.example", []string{"not a tag"}); err == nil {
		t.Error("a malformed tag was pinned")
	}
}
