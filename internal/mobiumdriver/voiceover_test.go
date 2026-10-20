package mobiumdriver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// fakeVoiceOver is a WebDriverAgent whose VoiceOver is on, off, or beyond a
// runner built before Xcode 27, recording every request it was sent.
type fakeVoiceOver struct {
	mu      sync.Mutex
	on      bool
	old     bool // answers as a runner built before Xcode 27 does
	paths   []string
	srv     *httptest.Server
	wda     *WDA
	speaker string
}

func newFakeVoiceOver(t *testing.T, on, old bool) *fakeVoiceOver {
	t.Helper()
	f := &fakeVoiceOver{on: on, old: old, speaker: "Pro radio button, unchecked"}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		on, old := f.on, f.old
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/wda/voiceOver/") && old:
			w.Write([]byte(`{"value":{"error":"unknown error","message":"Error Domain=com.facebook.WebDriverAgent ` +
				`Code=1 \"The current OS runtime does not support VoiceOver control. This API requires an iOS 27+ ` +
				`runtime\""}}`))
		case r.URL.Path == "/wda/voiceOver/enabled":
			if on {
				w.Write([]byte(`{"value":{"enabled":true}}`))
			} else {
				w.Write([]byte(`{"value":{"enabled":false}}`))
			}
		case r.URL.Path == "/wda/voiceOver/currentSpeech":
			w.Write([]byte(`{"value":{"utterance":"` + f.speaker + `"}}`))
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	t.Cleanup(f.srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(f.srv.URL)
	c.sessionID = "S1"
	f.wda = &WDA{w3c: c, scale: 3}
	c.touchGuard = f.wda.voiceOverGuard
	return f
}

func (f *fakeVoiceOver) sent(suffix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.paths {
		if strings.HasSuffix(p, suffix) {
			n++
		}
	}
	return n
}

// With VoiceOver on, every touch that did not ask first is refused, saying
// why — a tap reported done, and a swipe blamed on the end of a list, were
// the failures — and nothing reaches the device.
func TestEveryTouchIsRefusedWhileVoiceOverIsOn(t *testing.T) {
	f := newFakeVoiceOver(t, true, false)
	ctx := context.Background()
	for name, touch := range map[string]func() error{
		"tap":        func() error { return f.wda.Tap(ctx, 300, 600) },
		"swipe":      func() error { return f.wda.Swipe(ctx, 300, 1800, 300, 600, 300*time.Millisecond) },
		"long press": func() error { return f.wda.LongPress(ctx, 300, 600, time.Second) },
		"double tap": func() error { return f.wda.w3c.wdaDoubleTap(ctx, 100, 200) },
	} {
		err := touch()
		if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "VoiceOver is on") {
			t.Errorf("%s with VoiceOver on answered %v, want device_not_ready saying VoiceOver is on", name, err)
		}
	}
	if n := f.sent("/actions") + f.sent("/wda/doubleTap"); n != 0 {
		t.Errorf("%d touches reached the device", n)
	}
}

// A touch made as VoiceOver requires moves the cursor, reads it back, and
// activates it with WebDriverAgent's double tap, in points.
func TestATouchForVoiceOverMovesTheCursorAndReadsItBack(t *testing.T) {
	f := newFakeVoiceOver(t, true, false)
	ctx := context.Background()
	spoken, err := f.wda.FocusAt(ctx, 300, 600)
	if err != nil || spoken != "Pro radio button, unchecked" {
		t.Fatalf("FocusAt = %q, %v", spoken, err)
	}
	if err := f.wda.ActivateFocused(ctx, 300, 600); err != nil {
		t.Fatalf("ActivateFocused: %v", err)
	}
	if f.sent("/actions") != 1 || f.sent("/wda/doubleTap") != 1 {
		t.Errorf("sent %v, want one touch and one double tap", f.paths)
	}
}

// The negative control: with VoiceOver off a tap is sent as before.
func TestATapIsSentWithVoiceOverOff(t *testing.T) {
	f := newFakeVoiceOver(t, false, false)
	if err := f.wda.Tap(context.Background(), 300, 600); err != nil {
		t.Fatalf("tap with VoiceOver off: %v", err)
	}
	if f.sent("/actions") != 1 {
		t.Errorf("sent %v, want one touch", f.paths)
	}
}

// A runner built before Xcode 27 cannot say, and the touch goes ahead as it
// always did — asked once, not before every touch.
func TestARunnerThatCannotTellIsAskedOnce(t *testing.T) {
	f := newFakeVoiceOver(t, false, true)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := f.wda.Tap(ctx, 300, 600); err != nil {
			t.Fatalf("tap %d on an older runner: %v", i, err)
		}
	}
	if on, known := f.wda.ScreenReaderOn(ctx); on || known {
		t.Errorf("ScreenReaderOn = %v, %v on a runner that cannot tell, want false, false", on, known)
	}
	if n := f.sent("/wda/voiceOver/enabled"); n != 1 {
		t.Errorf("asked %d times, want once", n)
	}
}
