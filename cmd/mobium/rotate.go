package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newRotateCmd() *cobra.Command {
	var target string
	var radius int
	var degrees float64
	cmd := &cobra.Command{
		Use:   "rotate [degrees]",
		Short: "Turn two fingers about an element or the screen",
		Long: "Puts two fingers on the screen and turns them, 90 degrees clockwise\n" +
			"by default. A negative angle turns the other way.\n\n" +
			"It reports that the gesture was delivered and nothing more, and this is\n" +
			"harder to confirm than a zoom: nothing in either accessibility hierarchy\n" +
			"reports a rotation, and there is no WebView property to ask either — a\n" +
			"page has to compute the angle from raw touch events itself.\n\n" +
			"The fingers are stepped around the arc rather than moved straight to the\n" +
			"end. A straight move is a chord, which brings the fingers closer to the\n" +
			"center on the way past, and the platform reads that as a pinch.",
		Example: `  mobium rotate
  mobium rotate --degrees -90        # anticlockwise
  mobium rotate 45 --target @e2 --radius 120`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			switch {
			case cmd.Flags().Changed("degrees") && len(args) == 1:
				return fmt.Errorf("give the angle once, as an argument or --degrees")
			case cmd.Flags().Changed("degrees"):
				call["degrees"] = degrees
			case len(args) == 1:
				d, err := strconv.ParseFloat(args[0], 64)
				if err != nil {
					return fmt.Errorf("%q is not an angle", args[0])
				}
				call["degrees"] = d
			}
			if target != "" {
				call["target"] = target
			}
			if cmd.Flags().Changed("radius") {
				call["radius"] = radius
			}
			return runTool("app_rotate", call)
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "element to turn about (@ref or locator)")
	cmd.Flags().IntVar(&radius, "radius", 0, "how far each finger sits from the center, in device pixels")
	cmd.Flags().Float64Var(&degrees, "degrees", 0, "how far to turn, positive clockwise")
	// A leading minus is a flag on every POSIX command line, so `rotate -90`
	// fails before any of this runs -- which is defect 57, written again in a
	// new command by the person who recorded it. --degrees always works
	// because pflag consumes a flag's value whatever it starts with; the
	// positional form is kept for the common, positive case.
	cmd.Flags().SetInterspersed(false)
	return cmd
}
