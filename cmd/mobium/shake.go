package main

import "github.com/spf13/cobra"

func newShakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shake",
		Short: "Shake the device (emulators and simulators)",
		Long: "What shake-to-undo, shake-to-report and debug menus listen for. A simulator\n" +
			"gets the shake its Device menu sends; an emulator's accelerometer is swung\n" +
			"side to side and put back at rest. Whether the app reacts is up to its own\n" +
			"detector, so check the screen after. A real phone refuses.",
		Example: `  mobium shake`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_shake", map[string]interface{}{})
		},
	}
}
