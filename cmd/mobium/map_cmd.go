package main

import "github.com/spf13/cobra"

func newMapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "map",
		Short: "Map actionable elements on screen with @refs",
		Example: `  mobium map
  # @e1 Search (input)
  # @e2 Sign In (button)
  # Use refs with other commands: mobium tap @e2`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_map", map[string]interface{}{})
		},
	}
}
