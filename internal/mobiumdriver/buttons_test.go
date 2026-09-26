package mobiumdriver

import (
	"context"
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
		"KEYCODE_VOLUME_UP", "KEYCODE_VOLUME_DOWN"}
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
	for _, want := range []string{"no back button", "chevron", "app_swipe", "different events"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}

	// A button iOS simply lacks gets the shorter treatment: there is nothing
	// useful to say beyond what it does have.
	err = w.Press(context.Background(), ButtonRecents)
	if err == nil || !strings.Contains(err.Error(), `"home"`) {
		t.Errorf("recents refusal = %v", err)
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
	for _, list := range [][]string{androidButtons(), iosButtons()} {
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
