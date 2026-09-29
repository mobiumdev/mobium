package main

import "github.com/spf13/cobra"

func newMapCmd() *cobra.Command {
	var diff bool
	cmd := &cobra.Command{
		Use:   "map",
		Short: "Map actionable elements on screen with @refs",
		Long: "Maps the actionable elements on screen, each with a ref, a label and a role.\n" +
			"With --diff, answers only what changed since the last map of this device —\n" +
			"+ appeared, - went away, ~ changed label, checked state or place — which is\n" +
			"what an action just did. The refs are the new map's either way.",
		Example: `  mobium map
  # @e1 Search (input)
  # @e2 Sign In (button)
  # Use refs with other commands: mobium tap @e2

  mobium tap @e2 && mobium map --diff
  # + @e9 Welcome, mobium (text)
  # - Sign In (button)
  # ~ @e3 Accept terms (checkbox, checked) — was unchecked`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if diff {
				call["diff"] = true
			}
			return runTool("app_map", call)
		},
	}
	cmd.Flags().BoolVar(&diff, "diff", false, "Answer only what changed since the last map of this device")
	return cmd
}
