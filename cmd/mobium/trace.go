package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newTraceCmd() *cobra.Command {
	var output, name string
	var noShots, noMaps bool
	cmd := &cobra.Command{
		Use:   "trace [start | stop]",
		Short: "Record a session as a trace, in Vibium's record format",
		Long: "`trace start` begins; every call on the device is then a step, with the\n" +
			"screen after it and the map's elements drawn over it. `trace stop -o t.zip`\n" +
			"saves a zip in Vibium's record format, which player.vibium.dev opens.\n" +
			"`trace` alone says whether one is running.\n\n" +
			"Text typed into a field is not recorded, only its length. On a real phone\n" +
			"the screenshots are its owner's screen: --no-screenshots keeps none.",
		Example: `  mobium trace start --name "sign in"
  mobium tap "label=Login Demo"
  mobium fill testid=username mobium
  mobium trace stop -o sign-in.zip
  # open sign-in.zip at https://player.vibium.dev`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["action"] = args[0]
			}
			if len(args) == 1 && args[0] == "start" {
				if name != "" {
					call["name"] = name
				}
				if noShots {
					call["screenshots"] = false
				}
				if noMaps {
					call["maps"] = false
				}
			}
			if len(args) == 1 && args[0] == "stop" {
				if output == "" {
					output = fmt.Sprintf("trace-%s.zip", time.Now().Format("20060102-150405"))
				}
				call["path"] = output
			}
			return runTool("app_trace", call)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Where to save the trace on stop (default: ./trace-<timestamp>.zip)")
	cmd.Flags().StringVar(&name, "name", "", "The trace's title, on start")
	cmd.Flags().BoolVar(&noShots, "no-screenshots", false, "Keep no screenshot of each step, on start")
	cmd.Flags().BoolVar(&noMaps, "no-maps", false, "Draw no map over each screenshot, on start")
	return cmd
}
