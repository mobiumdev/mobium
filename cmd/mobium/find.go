package main

import "github.com/spf13/cobra"

func newFindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "find <locator>",
		Short: "Find elements matching a locator, without tapping",
		Example: `  mobium find text="Sign In"
  mobium find role=button
  mobium find testid=submit`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_find", map[string]interface{}{"locator": args[0]})
		},
	}
}
