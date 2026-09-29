package agent

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/trace"
)

func traceOf(t *testing.T, st *sessionTrace) string {
	t.Helper()
	raw, err := st.rec.Zip()
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	for _, f := range zr.File {
		if f.Name == "trace.trace" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			return string(b)
		}
	}
	t.Fatal("no trace.trace")
	return ""
}

// A call is traced on the device it names, or the one device with a trace
// running; with two and none named, it is not guessed.
func TestACallIsTracedOnItsDevice(t *testing.T) {
	h := NewHandlers()
	a := &session{dev: &device.Device{Serial: "A"}, trace: &sessionTrace{rec: trace.New(trace.Options{})}}
	b := &session{dev: &device.Device{Serial: "B"}}
	h.sessions["a"], h.sessions["b"] = a, b
	if got := h.tracedSession(map[string]interface{}{}); got != a {
		t.Error("the one traced device was not picked")
	}
	if got := h.tracedSession(map[string]interface{}{"device": "B"}); got != nil {
		t.Error("a call on an untraced device was traced")
	}
	b.trace = &sessionTrace{rec: trace.New(trace.Options{})}
	if got := h.tracedSession(map[string]interface{}{}); got != nil {
		t.Error("two traced devices and none named: guessed one")
	}
	if got := h.tracedSession(map[string]interface{}{"device": "B"}); got != b {
		t.Error("the named device was not picked")
	}
}

// Typed text never reaches a trace, only its length; a tap's point does,
// typing's absent point does not, and a failure is recorded as one.
func TestATraceKeepsNoTypedText(t *testing.T) {
	st := &sessionTrace{rec: trace.New(trace.Options{})}
	s := &session{dev: fakeDevice(), driver: &fakeDriver{}, trace: st}
	h := NewHandlers()

	id := traceBefore(st, "app_fill", map[string]interface{}{"target": "testid=password", "text": "Sup3rSecret!"})
	h.traceAfter(s, st, id, Result("filled", ActionView{Action: "fill", Target: "testid=password"}), nil)
	id = traceBefore(st, "app_tap", map[string]interface{}{"target": "@e3"})
	h.traceAfter(s, st, id, Result("tapped", ActionView{Action: "tap", X: 540, Y: 1200}), nil)
	id = traceBefore(st, "app_tap", map[string]interface{}{"target": "@e9"})
	h.traceAfter(s, st, id, nil, errors.New("no element matches @e9"))

	out := traceOf(t, st)
	if strings.Contains(out, "Sup3rSecret") {
		t.Fatal("the typed text is in the trace")
	}
	for _, want := range []string{`"(12 characters, not recorded)"`, `"title":"fill testid=password"`,
		`"point":{"x":540,"y":1200}`, `"message":"no element matches @e9"`} {
		if !strings.Contains(out, want) {
			t.Errorf("the trace has no %s", want)
		}
	}
	if strings.Count(out, `"type":"input"`) != 1 {
		t.Errorf("want one input event, the tap's:\n%s", out)
	}
}
