package main

import "github.com/spf13/cobra"

func newAlertCmd() *cobra.Command {
	var text string
	cmd := &cobra.Command{
		Use:   "alert [accept|dismiss]",
		Short: "Read a system dialog, or answer it",
		Long: "With no argument, prints what the dialog says, or that none is up.\n\n" +
			"A permission prompt is not part of the app: it is another process's\n" +
			"window, and `mobium current` reports that process while one is on\n" +
			"screen — com.google.android.permissioncontroller on Android, SpringBoard\n" +
			"on iOS.\n\n" +
			"It is answered through the W3C alert endpoints rather than by tapping a\n" +
			"button, so it works without knowing what the buttons say. A flow that\n" +
			"taps \"While using the app\" works until the device is in Japanese, and\n" +
			"this project pins apps to other languages on purpose.\n\n" +
			"These answer a dialog; they do not choose an outcome. On a permission\n" +
			"prompt they do not mean grant and deny, and on iOS they are the other\n" +
			"way round: measured on iOS 26.5, `accept` left the permission denied and\n" +
			"`dismiss` left it granted, because W3C accept presses the affirmative\n" +
			"button and Apple puts \"Don't Allow\" last. To choose an outcome, tap the\n" +
			"button — `mobium map` returns them like any other element.",
		Example: `  mobium alert              # is anything asking? what does it say?
  mobium alert accept       # grant the permission, or confirm the action
  mobium alert dismiss
  mobium alert accept --text "my draft"   # fill a prompt, then confirm it`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"accept", "dismiss", "read"},
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["action"] = args[0]
			}
			if cmd.Flags().Changed("text") {
				call["text"] = text
			}
			return runTool("app_alert", call)
		},
	}
	cmd.Flags().StringVar(&text, "text", "",
		"type into a prompt's field before answering")
	return cmd
}
