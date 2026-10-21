package main

import (
	"strings"

	"github.com/spf13/cobra"
)

func newAssistantCmd() *cobra.Command {
	var (
		app     string
		timeout float64
	)
	cmd := &cobra.Command{
		Use:   "assistant <request>",
		Short: "Ask the platform's assistant, in typed text, and report what came back",
		Long: "Puts a request to the device's assistant as typed text — Gemini or Google\n" +
			"Assistant on Android, opened with the assist key, and Siri on a real iPhone —\n" +
			"then waits for an app to come forward or the answer to settle, and closes the\n" +
			"assistant's panel if it is still up. It reports the text the assistant showed\n" +
			"and the app in front. The request is typed, never spoken, and the answer says so.\n\n" +
			"It is for testing an app through the assistant: whether the app did the task\n" +
			"is for the app to show, never for the assistant to say. --app names the app\n" +
			"expected to come forward and reports whether it did; a request that acts for\n" +
			"the device's owner — a call, a message, a purchase — is refused without it.\n\n" +
			"A simulator's Siri answers nothing, and a TV's assistant listens only through\n" +
			"its remote: both are refused.",
		Example: `  mobium assistant "Open Clock"
  mobium assistant "Open Clock" --app com.google.android.deskclock
  mobium assistant "What is 15 times 3" --timeout 20`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"request": strings.Join(args, " ")}
			if app != "" {
				call["app"] = app
			}
			if cmd.Flags().Changed("timeout") {
				call["timeout"] = timeout
			}
			return runTool("app_assistant", call)
		},
	}
	cmd.Flags().StringVar(&app, "app", "", "The app expected to come forward; required for a request that acts for the owner")
	cmd.Flags().Float64Var(&timeout, "timeout", 15, "Seconds to wait for an app or an answer")
	return cmd
}
