package mobiumdriver

import (
	"context"
	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"testing"
)

type fakeKeys struct {
	sent []string
	err  error
}

func (f *fakeKeys) PressKey(_ context.Context, code string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, code)
	return nil
}

func TestAndroidButtonsMapToKeycodes(t *testing.T) {
	f := &fakeKeys{}
	for _, b := range androidButtons() {
		if err := pressAndroid(context.Background(), f, b); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	want := []string{"KEYCODE_BACK", "KEYCODE_HOME", "KEYCODE_APP_SWITCH",
		"KEYCODE_VOLUME_UP", "KEYCODE_VOLUME_DOWN",
		"KEYCODE_DPAD_UP", "KEYCODE_DPAD_DOWN", "KEYCODE_DPAD_LEFT", "KEYCODE_DPAD_RIGHT",
		"KEYCODE_DPAD_CENTER", "KEYCODE_MEDIA_PLAY_PAUSE", "KEYCODE_MEDIA_STOP",
		"KEYCODE_MEDIA_NEXT", "KEYCODE_MEDIA_PREVIOUS", "KEYCODE_MEDIA_REWIND",
		"KEYCODE_MEDIA_FAST_FORWARD"}
	if strings.Join(f.sent, ",") != strings.Join(want, ",") {
		t.Errorf("sent %v, want %v", f.sent, want)
	}
}

// TestIOSRefusesBackWithTheReason is the one that matters. Mapping back to an
// edge swipe on iOS would be inventing an event the platform never sends, and
// an app can tell the difference — so the refusal has to explain what to do
// instead, not merely list what iOS does have.
func TestIOSRefusesBackWithTheReason(t *testing.T) {
	w := &WDA{}
	err := w.Press(context.Background(), ButtonBack)
	if err == nil {
		t.Fatal("iOS accepted a back press")
	}
	for _, want := range []string{"no back button", "chevron", "different events"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
	// The remedy asks for the swipe by name, where a row cannot take it:
	// the swipe measured on the iPhone did nothing when it started on a
	// row with swipe actions (docs/BACK.md).
	e, ok := mobiumerr.As(err)
	if !ok || !strings.Contains(e.Remedy, "app_press back with gesture") || !strings.Contains(e.Remedy, "navigation bar") {
		t.Errorf("the remedy does not name the gesture and where it starts: %+v", e)
	}

	// A button iOS simply lacks gets the shorter treatment: there is nothing
	// useful to say beyond what it does have.
	err = w.Press(context.Background(), ButtonRecents)
	if err == nil || !strings.Contains(err.Error(), `"home"`) {
		t.Errorf("recents refusal = %v", err)
	}

	// A D-pad is the Apple TV's, and the refusal says what to do on a
	// phone instead.
	for _, b := range []string{ButtonDpadDown, ButtonSelect} {
		err = w.Press(context.Background(), b)
		if mobiumerr.CodeOf(err) != mobiumerr.Unsupported || !strings.Contains(err.Error(), "no D-pad") ||
			!strings.Contains(err.Error(), "Tap the element") {
			t.Errorf("%s refusal = %v", b, err)
		}
	}
}

// TestPlatformsDisagreeAboutButtons pins the asymmetry rather than letting it
// drift: every platform list must be a subset of the vocabulary, and the two
// must genuinely differ or the split is pointless.
func TestPlatformsDisagreeAboutButtons(t *testing.T) {
	all := map[string]bool{}
	for _, b := range AllButtons() {
		all[b] = true
	}
	for _, list := range [][]string{androidButtons(), iosButtons(), tvButtons()} {
		for _, b := range list {
			if !all[b] {
				t.Errorf("%q is not in the vocabulary", b)
			}
		}
	}
	if len(androidButtons()) == len(iosButtons()) {
		t.Error("the two platforms have the same buttons; one of the lists is wrong")
	}
	for _, b := range iosButtons() {
		if b == ButtonBack {
			t.Error("iOS is listed as having a back button")
		}
	}
}

// TestAppleTVHasTheRemotesButtons: on an Apple TV simulator back is the
// remote's Menu button, the D-pad and select are the remote's, and a media
// key the Siri Remote lacks is refused by naming what it has.
func TestAppleTVHasTheRemotesButtons(t *testing.T) {
	for _, b := range tvButtons() {
		if tvButtonNames[b] == "" {
			t.Errorf("%s has no WebDriverAgent name", b)
		}
	}
	if tvButtonNames[ButtonBack] != "menu" || tvButtonNames[ButtonSelect] != "select" ||
		tvButtonNames[ButtonPlayPause] != "playpause" {
		t.Errorf("tvOS names = %v", tvButtonNames)
	}
	w := NewWDA(&device.Simctl{TV: true})
	if got := strings.Join(w.SupportedButtons(), ","); got != strings.Join(tvButtons(), ",") {
		t.Errorf("an Apple TV simulator supports %s", got)
	}
	for _, b := range []string{ButtonFastForward, ButtonVolumeUp, ButtonRecents} {
		err := w.Press(context.Background(), b)
		if mobiumerr.CodeOf(err) != mobiumerr.Unsupported || !strings.Contains(err.Error(), "Apple TV remote") ||
			!strings.Contains(err.Error(), `"play-pause"`) {
			t.Errorf("%s refusal = %v", b, err)
		}
	}
}

// TestAppleTVRefusesTouch: tvOS has no touch screen, so every gesture is
// refused before anything reaches WebDriverAgent, naming the remote.
func TestAppleTVRefusesTouch(t *testing.T) {
	w := NewWDA(&device.Simctl{TV: true})
	ctx := context.Background()
	for name, err := range map[string]error{
		"tap":        w.Tap(ctx, 10, 10),
		"long press": w.LongPress(ctx, 10, 10, 0),
		"swipe":      w.Swipe(ctx, 10, 10, 20, 20, 0),
		"multi tap":  w.MultiTap(ctx, []Point{{X: 1, Y: 1}, {X: 2, Y: 2}}),
	} {
		e, ok := mobiumerr.As(err)
		if !ok || e.Code != mobiumerr.Unsupported || !strings.Contains(err.Error(), "no touch screen") ||
			!strings.Contains(e.Remedy, "press select") {
			t.Errorf("%s on an Apple TV = %v", name, err)
		}
	}
	if NewWDA(&device.Simctl{}).w3c.noTouch != nil {
		t.Error("an iOS simulator refuses touch")
	}
}
