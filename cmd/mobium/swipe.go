package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newSwipeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swipe <up|down|left|right | x1 y1 x2 y2>",
		Short: "Swipe the screen",
		Example: `  mobium swipe up                    # scroll down the page
  mobium swipe left                  # next pager screen
  mobium swipe 540 1800 540 600      # exact drag
  mobium swipe up --duration 800ms   # slow drag rather than a fling`,
		Args: cobra.RangeArgs(1, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _ := cmd.Flags().GetDuration("duration")
			toolArgs := map[string]interface{}{"duration_ms": int(d.Milliseconds())}

			switch len(args) {
			case 1:
				toolArgs["direction"] = args[0]
			case 4:
				for i, key := range []string{"x1", "y1", "x2", "y2"} {
					v, err := strconv.Atoi(args[i])
					if err != nil {
						return fmt.Errorf("%q is not a coordinate", args[i])
					}
					toolArgs[key] = v
				}
			default:
				return fmt.Errorf("give a direction, or all four of x1 y1 x2 y2")
			}
			return runTool("app_swipe", toolArgs)
		},
	}
	cmd.Flags().Duration("duration", 300_000_000, "How long the drag takes")
	return cmd
}

func newLongPressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "long-press <@ref | locator | x y>",
		Short: "Press and hold an element or a point",
		Example: `  mobium long-press @e4
  mobium long-press 540 1200
  mobium long-press @e4 --duration 2s`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _ := cmd.Flags().GetDuration("duration")
			toolArgs := map[string]interface{}{"duration_ms": int(d.Milliseconds())}

			if len(args) == 2 {
				x, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("%q is not a coordinate", args[0])
				}
				y, err := strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("%q is not a coordinate", args[1])
				}
				toolArgs["x"], toolArgs["y"] = x, y
			} else {
				toolArgs["target"] = args[0]
			}
			return runTool("app_long_press", toolArgs)
		},
	}
	cmd.Flags().Duration("duration", 800_000_000, "How long to hold")
	return cmd
}
