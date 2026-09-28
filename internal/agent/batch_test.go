package agent

import (
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// withBox is a daemon with one open session, on a screen with one checkbox.
func withBox(t *testing.T) (*Handlers, *boxDriver) {
	t.Helper()
	d := &boxDriver{class: "android.widget.CheckBox"}
	h := NewHandlers()
	h.implicitWait, h.settleWindow = 0, 0
	h.sessions["fake"] = &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	return h, d
}

func step(name string, args map[string]interface{}) map[string]interface{} {
	s := map[string]interface{}{"name": name}
	if args != nil {
		s["arguments"] = args
	}
	return s
}

func batchOf(steps ...map[string]interface{}) map[string]interface{} {
	list := make([]interface{}, len(steps))
	for i, s := range steps {
		list[i] = s
	}
	return map[string]interface{}{"steps": list}
}

// Each step is the call it would be alone, in order, and the answer carries
// each one's text and data. app_check is the witness because its second call
// must see what the first did: asked for the state it is already in, it does
// not tap.
func TestBatchRunsEachStepInOrder(t *testing.T) {
	h, d := withBox(t)
	res, err := h.Call("app_batch", batchOf(
		step("app_check", map[string]interface{}{"target": "testid=box"}),
		step("app_check", map[string]interface{}{"target": "testid=box"}),
		step("app_check", map[string]interface{}{"target": "testid=box", "checked": false}),
	))
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if d.taps != 2 || d.checked {
		t.Fatalf("taps=%d checked=%v, want 2 taps ending unchecked", d.taps, d.checked)
	}
	v, ok := res.StructuredContent.(BatchView)
	if !ok || len(v.Steps) != 3 {
		t.Fatalf("structured = %#v, want three steps", res.StructuredContent)
	}
	for i, s := range v.Steps {
		if s.Name != "app_check" || s.Text == "" || s.Data == nil {
			t.Errorf("step %d = %+v, want its name, text and data", i+1, s)
		}
	}
	text := res.Content[0].Text
	if !strings.HasPrefix(text, "1. app_check: ") || !strings.Contains(text, "\n3. app_check: ") {
		t.Errorf("text = %q, want each step numbered", text)
	}
}

// The first failure ends the batch, with the failing step's own code — the
// exception a client raises must say what went wrong, not that a batch did —
// and nothing after it runs.
func TestBatchStopsAtTheFirstFailure(t *testing.T) {
	h, d := withBox(t)
	_, err := h.Call("app_batch", batchOf(
		step("app_check", map[string]interface{}{"target": "testid=box"}),
		step("app_tap", map[string]interface{}{"target": "text=Not on this screen"}),
		step("app_check", map[string]interface{}{"target": "testid=box", "checked": false}),
	))
	if err == nil {
		t.Fatal("a batch with a failing step succeeded")
	}
	if got := mobiumerr.CodeOf(err); got != mobiumerr.NoSuchElement {
		t.Errorf("code = %s, want the step's own no_such_element", got)
	}
	if !strings.Contains(err.Error(), "step 2 of 3 (app_tap)") {
		t.Errorf("error %q does not say which step", err)
	}
	if d.taps != 1 || !d.checked {
		t.Errorf("taps=%d checked=%v: step 3 ran after step 2 failed", d.taps, d.checked)
	}
	e, _ := mobiumerr.As(err)
	if e.Details["step"] != 2 {
		t.Errorf("details.step = %v, want 2", e.Details["step"])
	}
	if done, _ := e.Details["completed"].([]BatchStep); len(done) != 1 {
		t.Errorf("details.completed = %#v, want the one step that ran", e.Details["completed"])
	}
}

// Everything wrong with a step is found before the first step runs, so a
// mistake in the fourth leaves the app where it was rather than three steps
// into a flow.
func TestBatchRefusesBadStepsBeforeRunningAny(t *testing.T) {
	good := step("app_check", map[string]interface{}{"target": "testid=box"})
	cases := []struct {
		name string
		bad  map[string]interface{}
		want string
	}{
		{"unknown tool", step("app_tapp", nil), `"app_tapp", which is not a tool`},
		{"misspelled argument", step("app_tap", map[string]interface{}{"targt": "@e1"}), `argument "targt"`},
		{"required missing", step("app_check", map[string]interface{}{}), `missing "target"`},
		{"device in a step", step("app_tap", map[string]interface{}{"target": "@e1", "device": "x"}), "give it to the batch"},
		{"nested batch", step("app_batch", nil), "cannot contain another"},
		{"extra key", map[string]interface{}{"name": "app_tap", "args": map[string]interface{}{}}, `has "args"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, d := withBox(t)
			_, err := h.Call("app_batch", batchOf(good, c.bad))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one containing %q", err, c.want)
			}
			if !strings.Contains(err.Error(), "step 2") {
				t.Errorf("err %q does not name step 2", err)
			}
			if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
				t.Errorf("code = %s, want invalid_argument", mobiumerr.CodeOf(err))
			}
			if d.taps != 0 {
				t.Errorf("step 1 ran (%d taps) before step 2 was refused", d.taps)
			}
		})
	}
}

func TestBatchNeedsSteps(t *testing.T) {
	h, _ := withBox(t)
	for _, args := range []map[string]interface{}{
		{},
		{"steps": []interface{}{}},
		{"steps": "app_tap"},
	} {
		if _, err := h.Call("app_batch", args); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
			t.Errorf("args %v: err = %v, want invalid_argument", args, err)
		}
	}
	many := make([]map[string]interface{}, maxBatchSteps+1)
	for i := range many {
		many[i] = step("app_current", nil)
	}
	if _, err := h.Call("app_batch", batchOf(many...)); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("%d steps: err = %v, want the limit", len(many), err)
	}
}

// The batch's device reaches every step whose tool takes one, and only those.
func TestBatchGivesItsDeviceToEachStep(t *testing.T) {
	steps, err := parseBatch(map[string]interface{}{
		"device": "emulator-5556",
		"driver": "uiautomator2",
		"steps": []interface{}{
			step("app_tap", map[string]interface{}{"target": "@e1"}),
			step("app_doctor", nil),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].args["device"] != "emulator-5556" || steps[0].args["driver"] != "uiautomator2" {
		t.Errorf("app_tap args = %v, want the batch's device and driver", steps[0].args)
	}
	if _, ok := steps[1].args["device"]; ok {
		t.Errorf("app_doctor takes no device, and was given one: %v", steps[1].args)
	}
}

// A screenshot with no path answers with an image, and a batch keeps it:
// after the text, in step order, with its step marked.
func TestBatchKeepsAStepsImage(t *testing.T) {
	h, _ := withBox(t)
	res, err := h.Call("app_batch", batchOf(
		step("app_check", map[string]interface{}{"target": "testid=box"}),
		step("app_screenshot", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 2 || res.Content[0].Type != "text" || res.Content[1].Type != "image" {
		t.Fatalf("content = %+v, want the text and then the image", res.Content)
	}
	v := res.StructuredContent.(BatchView)
	if v.Steps[0].Image || !v.Steps[1].Image {
		t.Errorf("steps = %+v, want only the screenshot marked as an image", v.Steps)
	}
	if !strings.Contains(res.Content[0].Text, "2. app_screenshot: image 1 below") {
		t.Errorf("text = %q, want the screenshot step to point at its image", res.Content[0].Text)
	}
}
