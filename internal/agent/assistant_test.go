package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// overlayScreen is what UiAutomator2 reads while an assistant's overlay is
// up: its window alone, cut down to the root measured — Gemini's on the
// Pixel 8 Pro, Google Assistant's on the Android 15 emulator.
func overlayScreen(t *testing.T, rootID, label string) *uitree.Tree {
	t.Helper()
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" package="com.google.android.googlequicksearchbox" ` +
		`bounds="[0,0][1080,2400]">` +
		`<node index="0" text="" resource-id="` + rootID + `" class="android.widget.FrameLayout" ` +
		`package="com.google.android.googlequicksearchbox" content-desc="" clickable="true" enabled="true" ` +
		`bounds="[0,1500][1080,2300]">` +
		`<node index="0" text="` + label + `" resource-id="" class="android.widget.EditText" ` +
		`package="com.google.android.googlequicksearchbox" content-desc="" clickable="true" enabled="true" ` +
		`bounds="[60,2000][900,2140]" /></node></node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

const (
	geminiRoot    = "com.google.android.googlequicksearchbox:id/assistant_robin_floaty_root_container"
	assistantRoot = "com.google.android.googlequicksearchbox:id/assistant_fragment_host_container"
)

func TestAssistantOverlaysAreNamed(t *testing.T) {
	if got := uitree.AssistantOverlay(overlayScreen(t, geminiRoot, "Ask Gemini")); got != "Gemini" {
		t.Errorf("Gemini's overlay was named %q", got)
	}
	if got := uitree.AssistantOverlay(overlayScreen(t, assistantRoot, "Try typing")); got != "Google Assistant" {
		t.Errorf("Google Assistant's panel was named %q", got)
	}
	// The negative control: an app's own screen.
	if got := uitree.AssistantOverlay(twoThings(t)); got != "" {
		t.Errorf("an app's own screen was taken for %q", got)
	}
}

// A tap behind Gemini's overlay is refused at once, naming it and the back
// that closes it, where it took five and a half seconds to say only that
// nothing matched and to map again. Nothing is tapped.
func TestATapBehindGeminiIsRefusedNamingItAndBack(t *testing.T) {
	h, s, d := withFake(t, overlayScreen(t, geminiRoot, "Ask Gemini"))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	start := time.Now()
	_, err := h.tapOn(ctx, s, map[string]interface{}{"target": "text=Notes"})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "Gemini is open over the app") {
		t.Fatalf("tap behind Gemini answered %v, want device_not_ready naming Gemini", err)
	}
	if !strings.Contains(mobiumerr.Text(err), "app_press back") {
		t.Errorf("the refusal's text does not name back: %s", mobiumerr.Text(err))
	}
	if len(d.tapped) != 0 {
		t.Errorf("sent %d taps with Gemini's overlay in front", len(d.tapped))
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("took %s: an overlay that does not go by itself is not waited for", took)
	}
}
