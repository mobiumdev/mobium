package main

import "github.com/spf13/cobra"

func newScrollToCmd() *cobra.Command {
	var direction string
	cmd := &cobra.Command{
		Use:   "scroll-to <@ref | locator>",
		Short: "Scroll until an element is on screen",
		Long: "Bring something below the fold into view. `mobium map` only sees what is\n" +
			"currently visible, and tap, type and long-press already scroll to a target\n" +
			"that is not on screen — so reach for this to look without acting, or to\n" +
			"scroll back up.",
		Example: `  mobium scroll-to "text=Sign out"
  mobium scroll-to role=switch --direction up
  mobium scroll-to 'label=Card 8' --direction right   # a horizontal pager`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"target": args[0]}
			if direction != "" {
				call["direction"] = direction
			}
			return runTool("app_scroll_to", call)
		},
	}
	cmd.Flags().StringVar(&direction, "direction", "",
		"Which way to look: down (default), up, left or right")
	return cmd
}
