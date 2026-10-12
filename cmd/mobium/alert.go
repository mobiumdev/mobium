package main

import "github.com/spf13/cobra"

func newAlertCmd() *cobra.Command {
	var text, button string
	cmd := &cobra.Command{
		Use:   "alert [accept|dismiss]",
		Short: "Read a system dialog, or answer it",
		Long: "With no argument, prints what the dialog says and its buttons, or that\n" +
			"none is up.\n\n" +
			"A permission prompt is not part of the app: it is another process's\n" +
			"window, and `mobium current` reports that process while one is on\n" +
			"screen — com.google.android.permissioncontroller on Android, SpringBoard\n" +
			"on iOS.\n\n" +
			"accept and dismiss mean yes and no, so they answer a dialog of one or\n" +
			"two buttons: a two-button permission prompt is granted by accept and\n" +
			"denied by dismiss. They need no knowledge of what the buttons say, so\n" +
			"they work in any language. A dialog of three or more is refused, naming\n" +
			"its buttons — iOS's location prompt has Allow Once and Allow While Using\n" +
			"App, and Android's camera and location prompts While using the app and\n" +
			"Only this time, so more than one means yes.\n\n" +
			"--button presses one by its caption or its position from 1, on any\n" +
			"dialog. Every answer says which button it pressed.",
		Example: `  mobium alert                                  # is anything asking? what are its buttons?
  mobium alert accept                           # yes, on a dialog of one or two buttons
  mobium alert dismiss                          # no
  mobium alert --button "Allow While Using App" # any dialog, by caption
  mobium alert --button 2                       # or by position, in any language
  mobium alert accept --text "my draft"         # fill a prompt, then confirm it`,
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
			if cmd.Flags().Changed("button") {
				call["button"] = button
			}
			return runTool("app_alert", call)
		},
	}
	cmd.Flags().StringVar(&text, "text", "",
		"type into a prompt's field before answering")
	cmd.Flags().StringVar(&button, "button", "",
		"press this button: its caption, or its position from 1")
	return cmd
}
