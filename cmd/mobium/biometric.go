package main

import "github.com/spf13/cobra"

func newBiometricCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "biometric [status|enroll|unenroll|match|nomatch]",
		Short: "Enroll and present a face or finger (emulators and simulators)",
		Long: "With no argument, says whether a face or finger is enrolled. enroll and unenroll\n" +
			"set it: a simulator as its Features menu does; an emulator through Settings,\n" +
			"setting PIN 1111 first when it has no screen lock — unenroll removes both.\n" +
			"match and nomatch present a matching or a stranger's face or finger to the\n" +
			"prompt that is up, and say what the device made of it: accepted, not\n" +
			"recognized, or locked out. With no prompt up, nothing is sent. Whether the\n" +
			"app signed in is the app's to show. A real phone refuses.",
		Example: `  mobium biometric
  mobium biometric enroll
  mobium biometric match
  mobium biometric nomatch`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"status", "enroll", "unenroll", "match", "nomatch"},
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["action"] = args[0]
			}
			return runTool("app_biometric", call)
		},
	}
}
