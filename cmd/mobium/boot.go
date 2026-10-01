package main

import "github.com/spf13/cobra"

func newBootCmd() *cobra.Command {
	var window bool
	cmd := &cobra.Command{
		Use:   "boot <avd | simulator>",
		Short: "Start an emulator or a simulator, and wait until it has booted",
		Long: "Starts an Android emulator by its AVD's name, or an iOS simulator by its name or\n" +
			"UDID, and answers with its serial or UDID once it has booted. One already running\n" +
			"is said to be. An emulator cold-boots, headless unless --window.",
		Example: `  mobium boot mobium-test                 # an AVD
  mobium boot "iPhone 17 Pro"             # a simulator, by name
  mobium boot mobium-test --window`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"name": args[0]}
			if window {
				call["window"] = true
			}
			return runTool("app_boot", call)
		},
	}
	cmd.Flags().BoolVar(&window, "window", false, "Show the emulator's window (default: headless)")
	return cmd
}

func newShutdownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shutdown <serial | avd | udid | simulator>",
		Short: "Shut down an emulator or a simulator, its session ended first",
		Long: "Ends this daemon's session on the device, then shuts it down and waits until it\n" +
			"is gone. A real phone is refused.",
		Example: `  mobium shutdown emulator-5554
  mobium shutdown "iPhone 17 Pro"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_shutdown", map[string]interface{}{"name": args[0]})
		},
	}
}
