package main

import "github.com/spf13/cobra"

func newClipboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clipboard [text]",
		Short: "Read the device clipboard, or write it",
		Long: "With no argument, prints what the clipboard holds.\n\n" +
			"The platforms are not symmetric and mobium says so. iOS reads and\n" +
			"writes through simctl, so a write is confirmed by reading it back.\n" +
			"Android can only write: since Android 10 only an app with focus may\n" +
			"read the clipboard, and the UiAutomator2 server has no activity of its\n" +
			"own, so it answers with an empty string however full the clipboard is.\n" +
			"Reporting that as \"empty\" would be a different claim and usually a\n" +
			"false one, so the read refuses there instead.\n\n" +
			"To check an Android write, paste into a field and read the field:\n" +
			"that is how this was verified in the first place.",
		Example: `  mobium clipboard                     # what is on it? (iOS)
  mobium clipboard "hello there"       # put text on it
  mobium clipboard "$(cat note.txt)"   # quotes and newlines survive`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["text"] = args[0]
			}
			return runTool("app_clipboard", call)
		},
	}
}
