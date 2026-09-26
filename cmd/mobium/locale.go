package main

import "github.com/spf13/cobra"

func newLocaleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "locale <app> [tag]",
		Short: "Read or set the language one app runs in",
		Long: "With no tag, prints the app's language and the device's.\n\n" +
			"Per app rather than device-wide: it is reversible, needs no restart,\n" +
			"and it is the question actually being asked — does this screen work in\n" +
			"Japanese. Pass an empty string to follow the device again.\n\n" +
			"Android 13 and later. What mobium confirms is that the device stored\n" +
			"the tag; whether the app has that translation is not something Android\n" +
			"reports, so look at the screen. Relaunch the app to re-render it.",
		Example: `  mobium locale org.wikipedia          # what language is it in?
  mobium locale org.wikipedia ja-JP
  mobium locale org.wikipedia ""       # back to the device's language`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"app": args[0]}
			if len(args) == 2 {
				call["locale"] = args[1]
			}
			return runTool("app_locale", call)
		},
	}
}
