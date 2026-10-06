package main

import "github.com/spf13/cobra"

func newHookCmd() *cobra.Command {
	var timeoutMs int
	cmd := &cobra.Command{
		Use:   "hook <name> [args...]",
		Short: "Call a hook the app registered with its gray-box library",
		Long: "A hook is a function the app registered by name with Mobium's gray-box\n" +
			"library — sign in, seed data, raise a toast — so a test can set up state\n" +
			"without walking the UI. The app must be launched with `launch --gray-box`.\n" +
			"The call is written into a field the library adds only in a gray-box launch,\n" +
			"and the answer read from the device log; what the hook returns is printed as\n" +
			"JSON. On iOS the call is typed, about 16 ms a character. An unknown name is\n" +
			"refused, naming the hooks the app registered.",
		Example: `  mobium launch --gray-box dev.mobium.mobiumapp
  mobium hook raiseToast "Toast raised by test script"
  mobium hook signIn mobium
  mobium hook screen`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hookArgs := make([]interface{}, 0, len(args)-1)
			for _, a := range args[1:] {
				hookArgs = append(hookArgs, a)
			}
			call := map[string]interface{}{"hook": args[0], "args": hookArgs}
			if timeoutMs > 0 {
				call["timeout_ms"] = timeoutMs
			}
			return runTool("app_hook", call)
		},
	}
	cmd.Flags().IntVar(&timeoutMs, "timeout-ms", 0, "How long to wait for the app's answer (default 15000)")
	return cmd
}
