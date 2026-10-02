package testrun

import (
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// unreached is the error a call gets while a device's link is down: the
// lookup failed, so nothing was sent.
func unreached() error {
	return mobiumerr.New(mobiumerr.DeviceNotReady, "device 10.0.0.77:5555 is offline, not ready").
		WithDetail(device.ReachedKey, false)
}

func stepsRun(t *testing.T, steps []Step, single bool, call Caller) *Failure {
	t.Helper()
	old := linkPause
	linkPause = time.Millisecond
	t.Cleanup(func() { linkPause = old })
	dev := func(a map[string]interface{}) map[string]interface{} { return a }
	fail := func(step int, s *Step, err error) *Failure {
		return &Failure{Step: step, Code: string(mobiumerr.CodeOf(err)), Message: err.Error()}
	}
	var soft []*Failure
	return runSteps(steps, 1, time.Now().Add(time.Minute), call, dev, fail, &soft, &hooks{single: single})
}

func press(b string) Step {
	return Step{Name: "app_press", Arguments: map[string]interface{}{"button": b}}
}

// A step whose call never reached the device is made again once it is back,
// and the test goes on: a Fire TV's link dropped several times a minute.
func TestAStepWaitsOutADroppedLink(t *testing.T) {
	calls := 0
	f := stepsRun(t, []Step{press("dpad-down")}, true, func(tool string, args map[string]interface{}, _ ...map[string]interface{}) (*agent.ToolsCallResult, error) {
		calls++
		if calls <= 2 {
			return nil, unreached()
		}
		return &agent.ToolsCallResult{}, nil
	})
	if f != nil || calls != 3 {
		t.Errorf("failure %+v after %d calls; want the step to pass on its third", f, calls)
	}
}

// A failure that may have reached the device is never made again: a press
// sent before the link went would be pressed twice.
func TestAFailureThatMayHaveReachedTheDeviceIsNotRepeated(t *testing.T) {
	calls := 0
	f := stepsRun(t, []Step{press("select")}, true, func(string, map[string]interface{}, ...map[string]interface{}) (*agent.ToolsCallResult, error) {
		calls++
		return nil, mobiumerr.New(mobiumerr.DeviceNotReady, "10.0.0.77:5555 cannot be reached: device offline")
	})
	if f == nil || calls != 1 {
		t.Errorf("failure %+v after %d calls; want it to fail on the first", f, calls)
	}
}

// A batch that stopped at a step which never reached the device goes on from
// that step: the steps before it ran and are not run again.
func TestABatchResumesFromTheStepTheLinkDroppedAt(t *testing.T) {
	var batches [][]interface{}
	f := stepsRun(t, []Step{press("dpad-up"), press("dpad-down"), press("select")}, false,
		func(tool string, args map[string]interface{}, _ ...map[string]interface{}) (*agent.ToolsCallResult, error) {
			steps := args["steps"].([]interface{})
			batches = append(batches, steps)
			if len(batches) == 1 {
				return nil, unreached().(*mobiumerr.Error).WithDetail("step", 2)
			}
			return &agent.ToolsCallResult{}, nil
		})
	if f != nil || len(batches) != 2 {
		t.Fatalf("failure %+v after %d batches", f, len(batches))
	}
	if len(batches[1]) != 2 || batches[1][0].(map[string]interface{})["arguments"].(map[string]interface{})["button"] != "dpad-down" {
		t.Errorf("the second batch was %v; want it to start at dpad-down", batches[1])
	}
}

// The wait is bounded: a device that does not come back fails the step.
func TestADeviceThatStaysAwayFailsTheStep(t *testing.T) {
	old := linkWait
	linkWait = 20 * time.Millisecond
	t.Cleanup(func() { linkWait = old })
	f := stepsRun(t, []Step{press("dpad-down")}, true, func(string, map[string]interface{}, ...map[string]interface{}) (*agent.ToolsCallResult, error) {
		return nil, unreached()
	})
	if f == nil || f.Code != string(mobiumerr.DeviceNotReady) {
		t.Errorf("got %+v, want the step to fail as device_not_ready", f)
	}
}
