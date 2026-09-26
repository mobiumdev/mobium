package main

import (
	"fmt"
	"github.com/mobiumdev/mobium/internal/agent"

	"github.com/spf13/cobra"
)

func newDevicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "devices",
		Short: "List running emulators and devices",
		Example: `  mobium devices
  # emulator-5554  device  (emulator, model: sdk_gphone64_arm64)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_devices", map[string]interface{}{})
		},
	}
}

// runTool calls a tool and prints its result the way the caller asked.
//
// With --json the tool's structured answer is printed, not its prose. Wrapping
// the text as {"result": "..."} — which this used to do — left anything
// machine-readable to a regular expression.
func runTool(tool string, args map[string]interface{}) error {
	return emit(daemonCall(tool, args))
}

func emit(result *agent.ToolsCallResult, err error) error {
	if err != nil {
		return err
	}

	if jsonOutput {
		if result.StructuredContent != nil {
			return printJSON(result.StructuredContent)
		}
		// A tool with nothing structured to say still answers in JSON mode.
		return printJSON(map[string]string{"result": resultText(result)})
	}
	fmt.Println(resultText(result))
	return nil
}
