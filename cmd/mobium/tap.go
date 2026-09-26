package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// Two verbs, one tool — the same shape as `check` and `uncheck`. A double tap
// is app_tap with one argument set, because everything else about it is
// identical: the ref is resolved the same way, against a fresh snapshot, and
// refused the same way when the screen has moved. A second tool would have
// duplicated all of that to change which driver method gets the coordinates.
func newTapCmd() *cobra.Command {
	var fingers int
	cmd := &cobra.Command{
		Use:   "tap <@ref | locator | x y>",
		Short: "Tap an element or a point",
		Example: `  mobium tap @e2                 # tap the ref from the last map
  mobium tap text="Sign In"      # tap by visible text
  mobium tap testid=submit_btn   # tap by resource-id
  mobium tap 540 1200            # tap raw device coordinates
  mobium tap @e2 --fingers 2     # a two-finger tap`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if cmd.Flags().Changed("fingers") {
				call["fingers"] = fingers
			}
			if len(args) == 2 {
				x, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("%q is not a coordinate", args[0])
				}
				y, err := strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("%q is not a coordinate", args[1])
				}
				call["x"], call["y"] = x, y
				return runTool("app_tap", call)
			}
			call["target"] = args[0]
			return runTool("app_tap", call)
		},
	}
	cmd.Flags().IntVar(&fingers, "fingers", 1,
		"how many fingers tap at once, side by side (1 to 5)")
	return cmd
}

func newDoubleTapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "double-tap <@ref | locator | x y>",
		Short: "Tap an element or a point twice, as one gesture",
		Long: "Two taps close enough together that the platform reads them as one\n" +
			"gesture rather than as two taps. The window is narrow — AOSP puts it\n" +
			"between 40 and 300ms — so the whole chain is sent in a single request\n" +
			"and played back device-side, with no round trip inside it.\n\n" +
			"Refused by the uiautomator dump backend, which taps through one adb\n" +
			"call at a time and so cannot promise the interval.",
		Example: `  mobium double-tap @e2
  mobium double-tap text="Photo"
  mobium double-tap 540 1200`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 {
				x, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("%q is not a coordinate", args[0])
				}
				y, err := strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("%q is not a coordinate", args[1])
				}
				return runTool("app_tap", map[string]interface{}{
					"x": x, "y": y, "double": true,
				})
			}
			return runTool("app_tap", map[string]interface{}{
				"target": args[0], "double": true,
			})
		},
	}
}
