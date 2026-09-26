package main

import "github.com/spf13/cobra"

func newTextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "text [target]",
		Short: "Read the text on screen, or of one element",
		Example: `  mobium text                  # everything readable on screen
  mobium text @e2              # the text of one mapped element
  mobium text testid=title     # the text of a matched element`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			toolArgs := map[string]interface{}{}
			if len(args) == 1 {
				toolArgs["target"] = args[0]
			}
			return runTool("app_text", toolArgs)
		},
	}
}

func newSourceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "source",
		Short: "Print the raw hierarchy, as the device-side server sent it",
		Long: "What Appium calls the page source: the platform's own XML on a native screen,\n" +
			"the page's current markup in a WebView context. `map` is what to act on; this is\n" +
			"for when map leaves out the thing you need to see.\n\n" +
			"Password fields have their contents hidden, keeping the length. Geometry is in\n" +
			"the platform's units — pixels on Android, points on iOS, where map, taps and\n" +
			"screenshots are in pixels. --json says which, and the scale between them.",
		Example: `  mobium source > screen.xml
  mobium source --json | jq -r .units`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_source", map[string]interface{}{})
		},
	}
}
