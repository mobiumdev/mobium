package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// axFake is a device whose accessibility settings are a map, with the raw
// values an undo must restore kept separately, the way a platform keeps a
// setting unset rather than off.
type axFake struct {
	fakeDriver
	values   map[string]string
	undone   []string
	declined bool
}

func (f *axFake) HasCapability(name string) bool { return !f.declined }

func (f *axFake) AccessibilitySetting(ctx context.Context, name string) (string, error) {
	if name == device.AXTextScale {
		return "", mobiumerr.New(mobiumerr.Unsupported, "not on this platform")
	}
	return f.values[name], nil
}

func (f *axFake) SetAccessibilitySetting(ctx context.Context, name, value string) (device.AXUndo, error) {
	was := f.values[name]
	f.values[name] = value
	return func(ctx context.Context) error {
		f.values[name] = was
		f.undone = append(f.undone, name)
		return nil
	}, nil
}

func withAX(t *testing.T) (*Handlers, *session, *axFake) {
	t.Helper()
	h, s, _ := withFake(t, button(t, true))
	f := &axFake{values: map[string]string{device.AXBoldText: device.AXOff, device.AXReduceMotion: device.AXOff}}
	s.driver = f
	return h, s, f
}

// A change is confirmed, reported with what it was, and put back exactly when
// the session ends — to the value found by the first change, not the second.
func TestAnAccessibilityChangeIsPutBackWhenTheSessionEnds(t *testing.T) {
	h, s, f := withAX(t)
	ctx := context.Background()
	h.refs[s.dev.Serial] = &refTable{}

	res, err := h.accessibilityOn(ctx, s, map[string]interface{}{"setting": "bold_text", "value": "on"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content[0].Text, "bold_text on (was off)") || !strings.Contains(res.Content[0].Text, "put back") {
		t.Errorf("result = %q", res.Content[0].Text)
	}
	if _, ok := h.refs[s.dev.Serial]; ok {
		t.Error("refs from the screen before the change were kept")
	}
	// A second change to the same setting keeps the first undo.
	if _, err := h.accessibilityOn(ctx, s, map[string]interface{}{"setting": "bold_text", "value": "off"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.accessibilityOn(ctx, s, map[string]interface{}{"setting": "bold_text", "value": "on"}); err != nil {
		t.Fatal(err)
	}
	s.close()
	if f.values[device.AXBoldText] != device.AXOff {
		t.Errorf("bold_text after the session = %q, want the original off", f.values[device.AXBoldText])
	}
	if len(f.undone) != 1 {
		t.Errorf("undone %v, want bold_text once", f.undone)
	}
}

// A read of everything lists what the device has, and names why it lacks
// the rest.
func TestReadingEveryAccessibilitySetting(t *testing.T) {
	h, s, _ := withAX(t)
	res, err := h.accessibilityOn(context.Background(), s, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(AccessibilityView)
	if v.Settings[device.AXBoldText] != device.AXOff {
		t.Errorf("settings = %v", v.Settings)
	}
	if _, ok := v.Unsupported[device.AXTextScale]; !ok {
		t.Errorf("unsupported = %v, want text_scale with a reason", v.Unsupported)
	}
}

// Nothing unknown reaches a device, and a value needs a setting.
func TestBadAccessibilityArgumentsAreRefused(t *testing.T) {
	h, s, _ := withAX(t)
	for _, args := range []map[string]interface{}{
		{"setting": "bold"},
		{"value": "on"},
	} {
		_, err := h.accessibilityOn(context.Background(), s, args)
		if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
			t.Errorf("%v: code %s, err %v", args, mobiumerr.CodeOf(err), err)
		}
	}
}

// A driver that declines the capability — a real iPhone — is refused before
// anything is read.
func TestAccessibilityOnADriverThatDeclines(t *testing.T) {
	h, s, f := withAX(t)
	f.declined = true
	_, err := h.accessibilityOn(context.Background(), s, map[string]interface{}{"setting": "bold_text", "value": "on"})
	if mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Errorf("code %s: %v", mobiumerr.CodeOf(err), err)
	}
	if f.values[device.AXBoldText] != device.AXOff {
		t.Error("a declining driver was written to")
	}
}
