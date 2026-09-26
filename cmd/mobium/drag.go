package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newDragCmd() *cobra.Command {
	var holdMS, durationMS int

	cmd := &cobra.Command{
		Use:   "drag <from> <to> | drag <x1> <y1> <x2> <y2>",
		Short: "Drag something onto something else",
		Long: "Presses, holds until the thing is picked up, carries it, holds again\n" +
			"and releases.\n\n" +
			"This is not `swipe` with different arguments. A swipe has no hold at\n" +
			"either end, which is what makes it a swipe — point it at a row in a\n" +
			"reorderable list and it scrolls the list rather than moving the row.\n" +
			"The default hold is 700ms, above Android's 500ms long-press timeout,\n" +
			"because that timeout is what such a list arms on. When a drag picks\n" +
			"nothing up, --hold-ms is the first thing to raise.\n\n" +
			"Both ends are resolved from one snapshot before anything is touched:\n" +
			"resolving the destination afterwards would read a screen the drag is\n" +
			"already moving.\n\n" +
			"What comes back is that the gesture was delivered and where it went.\n" +
			"Whether the drop was accepted is the app's own state — run `map`\n" +
			"again to see it.",
		Example: `  mobium drag @e3 @e7
  mobium drag text="Notes" testid=folder_work
  mobium drag 540 1200 540 400
  mobium drag @e3 @e7 --hold-ms 1200      # a list that arms slowly`,
		Args: cobra.RangeArgs(2, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if cmd.Flags().Changed("hold-ms") {
				call["hold_ms"] = holdMS
			}
			if cmd.Flags().Changed("duration-ms") {
				call["duration_ms"] = durationMS
			}

			switch len(args) {
			case 2:
				call["from"], call["to"] = args[0], args[1]
			case 4:
				for i, key := range []string{"x1", "y1", "x2", "y2"} {
					n, err := strconv.Atoi(args[i])
					if err != nil {
						return fmt.Errorf("%q is not a coordinate", args[i])
					}
					call[key] = n
				}
			default:
				return fmt.Errorf("drag takes two targets or four coordinates, got %d "+
					"arguments", len(args))
			}
			return runTool("app_drag", call)
		},
	}

	cmd.Flags().IntVar(&holdMS, "hold-ms", 700,
		"how long to hold at each end, in milliseconds")
	cmd.Flags().IntVar(&durationMS, "duration-ms", 800,
		"how long the travel takes, in milliseconds")
	return cmd
}
