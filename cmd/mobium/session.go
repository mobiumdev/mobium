package main

import "github.com/spf13/cobra"

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session [start | end | status]",
		Short: "Start or end the session on a device",
		// `vibium stop` ends a browser session; `mobium stop` is not a
		// command, since it could mean one device's session, every device's,
		// or one app. The three are suggested instead of one being guessed.
		SuggestFor: []string{"stop"},
		Long: "The explicit start and end of a device session. Never required: every other command opens a session on\n" +
			"first use.\n\n" +
			"start opens it now, so the slow first start — installing UiAutomator2,\n" +
			"building WebDriverAgent on an iPhone — happens here rather than inside\n" +
			"your first tap. --platform ios picks wda; --app launches an\n" +
			"app fresh once the session is up: stopped first if it was running,\n" +
			"so the session begins at its first screen. Its data\n" +
			"is kept.\n\n" +
			"end closes one device's session with the daemon's own teardown:\n" +
			"accessibility settings put back, a recording or route stopped, WebViews\n" +
			"detached, the device-side server stopped, and the app --app launched\n" +
			"stopped too — apps it did not launch are left alone. Ending a session\n" +
			"that is not open succeeds and says so. `mobium daemon stop` ends every\n" +
			"device's.\n\n" +
			"With no argument, or status, lists the sessions open.",
		Example: `  mobium session start --device emulator-5554 --app com.android.settings
  mobium session start --platform ios --app com.apple.Preferences
  mobium session status
  mobium session end --device emulator-5554`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"start", "end", "status"},
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["action"] = args[0]
			}
			if p, _ := cmd.Flags().GetString("platform"); p != "" {
				call["platform"] = p
			}
			if a, _ := cmd.Flags().GetString("app"); a != "" {
				call["app"] = a
			}
			return runTool("app_session", call)
		},
	}
	cmd.Flags().String("platform", "", `"android" or "ios"; with start, "ios" picks wda`)
	cmd.Flags().String("app", "", "with start: a package name or bundle id to launch once the session is up")
	return cmd
}
