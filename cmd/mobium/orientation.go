package main

import "github.com/spf13/cobra"

func newOrientationCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "orientation [portrait | landscape | portrait-reverse | landscape-reverse | auto]",
		Short: "Read which way the screen is turned, or turn it",
		Long: "With no argument, prints the current orientation and whether it is\n" +
			"pinned — a screen that happens to be portrait can rotate under you, so\n" +
			"the two are different facts.\n\n" +
			"A rotation re-lays out every screen, so a layout that exists only in\n" +
			"landscape is one nothing has tested. Setting an orientation pins it;\n" +
			"\"auto\" hands it back to the sensor.\n\n" +
			"Not \"left\" and \"right\": which landscape is which depends on whether\n" +
			"you mean the device or the image, and the platforms disagree.\n" +
			"An activity that locks its own orientation cannot be turned from\n" +
			"outside, and mobium says so rather than reporting a success.",
		Example: `  mobium orientation              # which way is it, and is it pinned?
  mobium orientation landscape
  mobium orientation portrait
  mobium orientation auto         # follow the sensor again`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["orientation"] = args[0]
			}
			return runTool("app_orientation", call)
		},
	}
}
