package main

import "github.com/spf13/cobra"

func newHitTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hit-test <target>",
		Short: "Ask UIKit whether a tap on the target would reach it (iOS)",
		Long: "The accessibility tree leaves out a view hidden from accessibility, so a tap\n" +
			"under such an overlay lands on it while every check passes. This asks UIKit's\n" +
			"own hit test, inside the app, which view a touch at the point `tap` would use\n" +
			"goes to, and fails when it is not the target, naming what it is. Opt-in: it\n" +
			"attaches lldb to the app, about two seconds on a simulator and nine on an\n" +
			"iPhone, where the app must be built for development (get-task-allow), as one\n" +
			"installed from Xcode is. iOS only.",
		Example: `  mobium hit-test testid=submit
  mobium hit-test @e5`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_hit_test", map[string]interface{}{"target": args[0]})
		},
	}
}
