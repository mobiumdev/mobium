package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// maxBatchSteps bounds one batch. A flow longer than this is a script, and a
// script wants to see each answer before choosing the next call.
const maxBatchSteps = 100

// batchKeepalive is how long a batch runs without saying anything before it
// sends a progress note. The daemon's client gives up on a call that is
// silent for two minutes, which one slow step can nearly reach on its own;
// a short batch stays quiet.
const batchKeepalive = 60 * time.Second

// BatchView is the result of app_batch: every step that ran, in order.
type BatchView struct {
	Steps []BatchStep `json:"steps"`
}

// BatchStep is one step's answer — the same text and data the tool gives
// when called on its own.
type BatchStep struct {
	Name string      `json:"name"`
	Text string      `json:"text"`
	Data interface{} `json:"data,omitempty"`
	// Image says the step answered with an image — a screenshot with no
	// path — which is in the batch's content, after the text, in step order.
	Image bool `json:"image,omitempty"`
}

// batchStep is one step as asked for, checked before anything runs.
type batchStep struct {
	name string
	args map[string]interface{}
}

// batch runs several tools in order, on one device, in one call.
//
// Every step is checked before the first runs — its tool exists, and every
// key it passes is one that tool declares — because a typo found at step four
// would leave the app three steps into a flow with nothing to say where. An
// MCP client checks a call's arguments against its schema; nothing checks the
// arguments nested inside a step, and a handler ignores a key it does not
// read, so this is the only place a misspelled one can be caught.
//
// Each step is dispatched exactly as a call of its own would be: it resolves
// its target against a fresh snapshot, waits and settles as usual, and has
// the whole call timeout to itself. Only the round trips go. It stops at the
// first failure, whose code is the step's own, so a caller's exception says
// what went wrong rather than that a batch did.
func (h *Handlers) batch(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	steps, err := parseBatch(args)
	if err != nil {
		return nil, err
	}

	view := BatchView{Steps: []BatchStep{}}
	var images []Content
	quietSince := time.Now()
	for i, st := range steps {
		if h.progress != nil && time.Since(quietSince) > batchKeepalive {
			h.progress(fmt.Sprintf("batch: step %d of %d, %s", i+1, len(steps), st.name))
			quietSince = time.Now()
		}
		res, err := h.batchStep(ctx, st)
		if err != nil {
			return nil, batchFailed(err, i, steps, view)
		}
		one := BatchStep{Name: st.name, Text: resultText(res), Data: res.StructuredContent}
		for _, c := range res.Content {
			if c.Type == "image" {
				images = append(images, c)
				one.Image = true
			}
		}
		view.Steps = append(view.Steps, one)
	}
	return BatchResult(view, images), nil
}

// BatchResult is a batch's answer: each step's text, numbered, then the
// images steps answered with, in step order. Exported because the caller's
// edge rebuilds it after saving a step's file where the caller asked.
func BatchResult(view BatchView, images []Content) *ToolsCallResult {
	lines := make([]string, 0, len(view.Steps))
	n := 0
	for i, st := range view.Steps {
		text := st.Text
		if st.Image {
			n++
			if text == "" {
				text = fmt.Sprintf("image %d below", n)
			}
		}
		lines = append(lines, fmt.Sprintf("%d. %s: %s", i+1, st.Name, text))
	}
	res := Result(strings.Join(lines, "\n"), view)
	res.Content = append(res.Content, images...)
	return res
}

// batchStep runs one step as its own call would: its own timeout, and its
// own report of any dialog a rule answered on the way.
func (h *Handlers) batchStep(ctx context.Context, st batchStep) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	h.handled = nil
	res, err := h.reportHandled(h.dispatch(ctx, st.name, st.args))
	h.handled = nil
	return res, err
}

// resultText is a result's prose, as a caller reading it would see it.
func resultText(res *ToolsCallResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, c := range res.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// batchFailed is a step's failure, as the batch's: the step's own code,
// remedy and details, its position in the message, and what ran before it.
func batchFailed(err error, i int, steps []batchStep, done BatchView) error {
	msg := fmt.Sprintf("step %d of %d (%s) failed: %v", i+1, len(steps), steps[i].name, err)
	switch i {
	case 0:
		msg += "; nothing ran after it"
	case 1:
		msg += "; step 1 ran before it, and nothing after"
	default:
		msg += fmt.Sprintf("; steps 1-%d ran before it, and nothing after", i)
	}
	out := &mobiumerr.Error{Code: mobiumerr.Unclassified, Message: msg, Cause: err}
	if e, ok := mobiumerr.As(err); ok {
		out.Code, out.Remedy, out.Retryable = e.Code, e.Remedy, e.Retryable
		for k, v := range e.Details {
			out = out.WithDetail(k, v)
		}
	}
	return out.WithDetail("step", i+1).WithDetail("completed", done.Steps)
}

// parseBatch reads and checks every step before any runs.
func parseBatch(args map[string]interface{}) ([]batchStep, error) {
	raw, ok := args["steps"].([]interface{})
	if !ok || len(raw) == 0 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"steps must be a non-empty list of {\"name\": tool, \"arguments\": {...}}")
	}
	if len(raw) > maxBatchSteps {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"a batch takes at most %d steps, got %d", maxBatchSteps, len(raw))
	}

	schemas := map[string]map[string]interface{}{}
	for _, t := range GetToolSchemas() {
		schemas[t.Name] = t.InputSchema
	}

	steps := make([]batchStep, 0, len(raw))
	for i, item := range raw {
		n := i + 1
		obj, ok := item.(map[string]interface{})
		if !ok {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument,
				"step %d is not an object with \"name\" and \"arguments\"", n)
		}
		for k := range obj {
			if k != "name" && k != "arguments" {
				return nil, mobiumerr.New(mobiumerr.InvalidArgument,
					"step %d has %q; a step has only \"name\" and \"arguments\"", n, k)
			}
		}
		name, _ := obj["name"].(string)
		if name == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "step %d has no tool name", n)
		}
		if name == "app_batch" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument,
				"step %d is app_batch; a batch cannot contain another — list its steps in this one", n)
		}
		schema, ok := schemas[name]
		if !ok {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument,
				"step %d names %q, which is not a tool (have: %s)", n, name, strings.Join(ToolNames(), ", "))
		}

		stepArgs := map[string]interface{}{}
		if a, present := obj["arguments"]; present && a != nil {
			m, ok := a.(map[string]interface{})
			if !ok {
				return nil, mobiumerr.New(mobiumerr.InvalidArgument, "step %d (%s): arguments must be an object", n, name)
			}
			for k, v := range m {
				stepArgs[k] = v
			}
		}
		if err := checkStepArgs(n, name, schema, stepArgs); err != nil {
			return nil, err
		}

		// The device and driver are the batch's, and reach every step whose
		// tool takes them.
		props, _ := schema["properties"].(map[string]interface{})
		for _, k := range []string{"device", "driver"} {
			if v, ok := args[k]; ok {
				if _, takes := props[k]; takes {
					stepArgs[k] = v
				}
			}
		}
		steps = append(steps, batchStep{name: name, args: stepArgs})
	}
	return steps, nil
}

// checkStepArgs refuses a step whose arguments its tool would not accept as
// a call of its own: a key it does not declare, or one it requires missing.
func checkStepArgs(n int, name string, schema map[string]interface{}, args map[string]interface{}) error {
	props, _ := schema["properties"].(map[string]interface{})
	for k := range args {
		if k == "device" || k == "driver" {
			return mobiumerr.New(mobiumerr.InvalidArgument,
				"step %d (%s) names a %s; a batch runs on one device, so give it to the batch", n, name, k)
		}
		if _, ok := props[k]; !ok {
			known := make([]string, 0, len(props))
			for p := range props {
				if p != "device" && p != "driver" {
					known = append(known, p)
				}
			}
			sort.Strings(known)
			takes := "nothing"
			if len(known) > 0 {
				takes = strings.Join(known, ", ")
			}
			return mobiumerr.New(mobiumerr.InvalidArgument,
				"step %d (%s) has an argument %q that %s does not take (it takes: %s)",
				n, name, k, name, takes)
		}
	}
	required, _ := schema["required"].([]string)
	for _, k := range required {
		if _, ok := args[k]; !ok {
			return mobiumerr.New(mobiumerr.InvalidArgument, "step %d (%s) is missing %q", n, name, k)
		}
	}
	return nil
}

// CheckStep checks one step as app_batch would before running anything: the
// tool exists, is not a batch, and takes every argument given and every one
// it requires. For the test runner, which checks every file before the first
// test runs, as a batch checks every step.
func CheckStep(n int, name string, args map[string]interface{}) error {
	if name == "app_batch" {
		return mobiumerr.New(mobiumerr.InvalidArgument, "step %d is app_batch; list its steps instead", n)
	}
	for _, t := range GetToolSchemas() {
		if t.Name == name {
			return checkStepArgs(n, name, t.InputSchema, args)
		}
	}
	return mobiumerr.New(mobiumerr.InvalidArgument, "step %d names %q, which is not a tool (have: %s)",
		n, name, strings.Join(ToolNames(), ", "))
}
