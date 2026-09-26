package main

import "github.com/spf13/cobra"

func newPressCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "press <back | home | recents | volume-up | volume-down>",
		Short: "Press a hardware button",
		Long: "On Android, back is primary navigation — an app that opened a detail\n" +
			"screen expects it, and no amount of tapping substitutes for it.\n\n" +
			"iOS has no back button by design: navigation back is a per-app\n" +
			"affordance there. Mobium refuses rather than sending an edge swipe,\n" +
			"which is a different event and one an app can tell apart.\n\n" +
			"Any press can move the screen, so refs from the last map are discarded.",
		Example: `  mobium press back
  mobium press home
  mobium press recents`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_press", map[string]interface{}{"button": args[0]})
		},
	}
}

func newLockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lock [lock | unlock]",
		Short: "Read whether the screen is locked, or lock and unlock it",
		Long: "With no argument, prints whether the screen is locked.\n\n" +
			"A state rather than a power-button press: power is a toggle, so asking\n" +
			"for it twice leaves the device where it started and you cannot tell\n" +
			"which way it went. Both directions are confirmed against the device.\n\n" +
			"A device with a PIN, pattern or password cannot be unlocked from\n" +
			"outside, and mobium says so rather than reporting success.",
		Example: `  mobium lock            # is it locked?
  mobium lock lock
  mobium lock unlock`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["state"] = args[0]
			}
			return runTool("app_lock", call)
		},
	}
}
