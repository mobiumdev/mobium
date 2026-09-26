package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// writeOnlyClipboard is Android: it accepts a write and cannot read.
type writeOnlyClipboard struct {
	fakeDriver
	wrote []string
}

func (d *writeOnlyClipboard) SetClipboard(ctx context.Context, text string) error {
	d.wrote = append(d.wrote, text)
	return nil
}

var _ mobiumdriver.Clipboard = (*writeOnlyClipboard)(nil)

// readWriteClipboard is iOS: simctl does both.
type readWriteClipboard struct {
	fakeDriver
	text string
	// stuck reports success and changes nothing, the failure mode this
	// project checks for everywhere else.
	stuck bool
}

func (d *readWriteClipboard) SetClipboard(ctx context.Context, text string) error {
	if !d.stuck {
		d.text = text
	}
	return nil
}

func (d *readWriteClipboard) ClipboardText(ctx context.Context) (string, error) {
	return d.text, nil
}

var (
	_ mobiumdriver.Clipboard       = (*readWriteClipboard)(nil)
	_ mobiumdriver.ClipboardReader = (*readWriteClipboard)(nil)
)

func clipboardOn(t *testing.T, d mobiumdriver.Driver, args map[string]interface{}) (*ToolsCallResult, error) {
	t.Helper()
	h := NewHandlers()
	h.implicitWait, h.settleWindow = 0, 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.clipboardOn(ctx, s, args)
}

// The load-bearing one. A backend that cannot read must say so rather than
// answer with the empty string the device would have given it — "cannot read"
// and "is empty" are different claims and the second is usually false.
func TestClipboardRefusesToReadWhenItCannot(t *testing.T) {
	_, err := clipboardOn(t, &writeOnlyClipboard{}, map[string]interface{}{})
	if err == nil {
		t.Fatal("a backend that cannot read the clipboard reported one anyway")
	}
	for _, want := range []string{"cannot read", "focus", "paste"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

func TestClipboardWriteIsReportedAsSentWhenUnreadable(t *testing.T) {
	d := &writeOnlyClipboard{}
	res, err := clipboardOn(t, d, map[string]interface{}{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.wrote) != 1 || d.wrote[0] != "hello" {
		t.Errorf("wrote %v", d.wrote)
	}
	if v := res.StructuredContent.(ClipboardView); v.Known {
		t.Error("Known is true on a platform that cannot read the clipboard back")
	}
	if !strings.Contains(textOf(res), "cannot read it back") {
		t.Errorf("the answer claims more than it checked: %q", textOf(res))
	}
}

func TestClipboardWriteIsConfirmedWhenItCanBe(t *testing.T) {
	res, err := clipboardOn(t, &readWriteClipboard{}, map[string]interface{}{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(ClipboardView)
	if !v.Known || v.Text != "hello" {
		t.Errorf("view = %+v", v)
	}
	if !strings.Contains(textOf(res), "read back") {
		t.Errorf("a confirmed write does not say so: %q", textOf(res))
	}
}

// A write that succeeds while doing nothing is the shape defect 26 is about,
// and reading back is the only thing that catches it.
func TestClipboardCatchesAWriteThatDidNothing(t *testing.T) {
	_, err := clipboardOn(t, &readWriteClipboard{text: "old", stuck: true},
		map[string]interface{}{"text": "new"})
	if err == nil {
		t.Fatal("a clipboard write that changed nothing was reported as success")
	}
	if !strings.Contains(err.Error(), "reads back as") {
		t.Errorf("the error does not say what it found: %v", err)
	}
}

func TestClipboardReadsEmptyWithoutClaimingItCannot(t *testing.T) {
	res, err := clipboardOn(t, &readWriteClipboard{}, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if v := res.StructuredContent.(ClipboardView); !v.Known || v.Text != "" {
		t.Errorf("an empty clipboard on a readable platform came back as %+v", v)
	}
}
