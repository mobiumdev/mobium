package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// press-tap and press-drag are the two gestures of the touch-gesture charts
// in which fingers do different things: one holds while another acts. Their
// arguments follow drag's — refs or locators, or the same points as numbers,
// with the keys spelled out inline as drag spells them, so apisurface can see
// which arguments the CLI sets.

func newPressTapCmd() *cobra.Command {
	var leadMS int
	cmd := &cobra.Command{
		Use:   "press-tap <hold> <tap> | press-tap <x1> <y1> <x2> <y2>",
		Short: "Hold one thing with a finger and tap another with a second",
		Long: "One finger presses and stays down; a second finger taps; then the\n" +
			"first lifts. Both targets are resolved before anything is touched.\n\n" +
			"The second finger lands after --lead-ms, 300 by default: under\n" +
			"Android's 500ms long-press timeout, so the held element does not\n" +
			"open its own menu first.\n\n" +
			"What comes back is that the gesture was delivered. What it meant is\n" +
			"the app's own state — run `map` again to see it.\n\n" +
			"Android 15 and earlier only. On iOS, XCTest adds a zero-length touch\n" +
			"at the second finger's target when the gesture starts, so\n" +
			"WebDriverAgent refuses rather than touch the target twice. On Android\n" +
			"16 and later, UiAutomator2 gives the second finger its own down time,\n" +
			"which Android rejects along with every injected touch after it.",
		Example: `  mobium press-tap @e3 @e7
  mobium press-tap 300 1200 700 1200
  mobium press-tap @e3 @e7 --lead-ms 800     # let the hold register first`,
		Args: cobra.RangeArgs(2, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if cmd.Flags().Changed("lead-ms") {
				call["lead_ms"] = leadMS
			}
			switch len(args) {
			case 2:
				call["hold"], call["tap"] = args[0], args[1]
			case 4:
				for i, key := range []string{"x1", "y1", "x2", "y2"} {
					n, err := strconv.Atoi(args[i])
					if err != nil {
						return fmt.Errorf("%q is not a coordinate", args[i])
					}
					call[key] = n
				}
			default:
				return fmt.Errorf("press-tap takes two targets or four coordinates, got %d arguments", len(args))
			}
			return runTool("app_press_tap", call)
		},
	}
	cmd.Flags().IntVar(&leadMS, "lead-ms", 300,
		"how long the first finger rests before the second taps, in milliseconds")
	return cmd
}

func newPressDragCmd() *cobra.Command {
	var leadMS, durationMS int
	cmd := &cobra.Command{
		Use:   "press-drag <hold> <from> <to> | press-drag <x1> <y1> <x2> <y2> <x3> <y3>",
		Short: "Hold one thing with a finger and drag a second finger elsewhere",
		Long: "One finger presses and stays down; a second lands on <from>, moves\n" +
			"to <to> and lifts; then the first lifts. Not `drag`, which is one\n" +
			"finger carrying something: here one finger anchors and the other\n" +
			"moves. All three targets are resolved before anything is touched.\n\n" +
			"What comes back is that the gesture was delivered — run `map` again\n" +
			"to see what it did. Android 15 and earlier only, for press-tap's reasons.",
		Example: `  mobium press-drag @e3 @e5 @e9
  mobium press-drag 300 1200 700 1000 700 400`,
		Args: cobra.RangeArgs(3, 6),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if cmd.Flags().Changed("lead-ms") {
				call["lead_ms"] = leadMS
			}
			if cmd.Flags().Changed("duration-ms") {
				call["duration_ms"] = durationMS
			}
			switch len(args) {
			case 3:
				call["hold"], call["from"], call["to"] = args[0], args[1], args[2]
			case 6:
				for i, key := range []string{"x1", "y1", "x2", "y2", "x3", "y3"} {
					n, err := strconv.Atoi(args[i])
					if err != nil {
						return fmt.Errorf("%q is not a coordinate", args[i])
					}
					call[key] = n
				}
			default:
				return fmt.Errorf("press-drag takes three targets or six coordinates, got %d arguments", len(args))
			}
			return runTool("app_press_drag", call)
		},
	}
	cmd.Flags().IntVar(&leadMS, "lead-ms", 300,
		"how long the first finger rests before the second lands, in milliseconds")
	cmd.Flags().IntVar(&durationMS, "duration-ms", 600,
		"how long the second finger's travel takes, in milliseconds")
	return cmd
}
