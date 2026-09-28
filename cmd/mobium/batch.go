package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

func newBatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "batch <file|->",
		Short: "Run several tools in order, in one call",
		Long: "Reads a list of steps as JSON — from a file, or from stdin with - — and runs\n" +
			"them in order on one device. Each step is a tool and its arguments, exactly as\n" +
			"that tool takes them on its own (docs/API.md), and runs as a call of its own\n" +
			"would: it finds its target on a fresh screen and waits as usual.\n\n" +
			"Every step is checked before the first runs, so a misspelled tool or argument\n" +
			"refuses the batch with nothing done. It stops at the first failure, with that\n" +
			"step's own error and exit status; nothing after it runs.\n\n" +
			"The file is a list of steps, or an object with \"steps\" holding one. Give the\n" +
			"device with --device, not in a step. A screenshot step with no path is saved\n" +
			"as ./screenshot-<timestamp>-<step>.png, as `mobium screenshot` would.",
		Example: `  mobium batch login.json

  echo '[{"name": "app_tap", "arguments": {"target": "text=Sign in"}},
         {"name": "app_fill", "arguments": {"target": "testid=user", "text": "mobium"}},
         {"name": "app_wait_for", "arguments": {"target": "text=Welcome"}}]' | mobium batch -`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var raw []byte
			var err error
			if args[0] == "-" {
				raw, err = io.ReadAll(cmd.InOrStdin())
			} else {
				raw, err = os.ReadFile(args[0])
			}
			if err != nil {
				return mobiumerr.New(mobiumerr.InvalidArgument, "cannot read the steps: %v", err)
			}
			steps, err := batchSteps(raw)
			if err != nil {
				return err
			}
			nameScreenshots(steps, time.Now())
			return runTool("app_batch", map[string]interface{}{"steps": steps})
		},
	}
}

// nameScreenshots gives a screenshot step with no path the one `mobium
// screenshot` would choose, numbered by step: the command line has nowhere to
// show an image. It edits the steps, not the batch's own arguments.
func nameScreenshots(steps []interface{}, now time.Time) {
	stamp := now.Format("20060102-150405")
	for i, item := range steps {
		step, _ := item.(map[string]interface{})
		if step["name"] != "app_screenshot" {
			continue
		}
		a, _ := step["arguments"].(map[string]interface{})
		if a == nil {
			a = map[string]interface{}{}
			step["arguments"] = a
		}
		if p, _ := a["path"].(string); p == "" {
			a["path"] = fmt.Sprintf("screenshot-%s-%d.png", stamp, i+1)
		}
	}
}

// batchSteps reads a batch file: a list of steps, or {"steps": [...]}.
func batchSteps(raw []byte) ([]interface{}, error) {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the steps are not JSON: %v", err)
	}
	if obj, ok := v.(map[string]interface{}); ok {
		for k := range obj {
			if k != "steps" {
				return nil, mobiumerr.New(mobiumerr.InvalidArgument,
					"the batch file has %q; it holds only \"steps\" (give the device with --device)", k)
			}
		}
		v = obj["steps"]
	}
	steps, ok := v.([]interface{})
	if !ok {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"the steps must be a list of {\"name\": tool, \"arguments\": {...}}, or an object with \"steps\"")
	}
	return steps, nil
}
