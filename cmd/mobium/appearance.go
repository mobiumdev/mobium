package main

import "github.com/spf13/cobra"

func newAppearanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "appearance [light | dark | auto]",
		Short: "Read or change the device's light/dark setting",
		Long: "With no argument, prints the current setting.\n\n" +
			"Dark mode is a different rendering of every screen and is where contrast\n" +
			"and hard-coded colors break, so a flow is worth running in both.\n" +
			"Android also accepts \"auto\"; iOS has no such thing and says so.",
		Example: `  mobium appearance          # what is it now?
  mobium appearance dark
  mobium appearance light`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["appearance"] = args[0]
			}
			return runTool("app_appearance", call)
		},
	}
}
