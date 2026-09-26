package main

import "github.com/spf13/cobra"

func newCallCmd() *cobra.Command {
	var number string
	cmd := &cobra.Command{
		Use:   "call [ring | accept | hang]",
		Short: "Simulate an incoming call",
		Long: "Interruption is where mobile apps fail and desktop software does not:\n" +
			"a call mid-form, state lost on resume, a callback that never fires.\n" +
			"None of it can be provoked by tapping.\n\n" +
			"Emulator only. A real phone cannot be made to ring from outside —\n" +
			"that is a property of phones, and mobium says so rather than failing\n" +
			"obscurely. Confirmed against the device's telephony state, not the\n" +
			"emulator console's acknowledgement.",
		Example: `  mobium call                 # ring
  mobium call accept
  mobium call hang
  mobium call ring --number 5559876`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["action"] = args[0]
			}
			if number != "" {
				call["number"] = number
			}
			return runTool("app_call", call)
		},
	}
	cmd.Flags().StringVar(&number, "number", "", "Calling number (default 5551234)")
	return cmd
}

func newSMSCmd() *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "sms <text>",
		Short: "Deliver a simulated text message",
		Long: "To see what an app does when a message arrives mid-flow.\n\n" +
			"Emulator only, for the same reason as `mobium call`.",
		Example: `  mobium sms "your code is 123456"
  mobium sms "hello" --from 5559876`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"text": args[0]}
			if from != "" {
				call["from"] = from
			}
			return runTool("app_sms", call)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "Sender number (default 5551234)")
	return cmd
}

func newTimezoneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "timezone [zone]",
		Short: "Read the device timezone, or change it",
		Long: "With no argument, prints the current zone.\n\n" +
			"Unlike `mobium call` this works on real hardware. A clock that jumps a\n" +
			"zone mid-session breaks scheduling, caching, and anything that stored a\n" +
			"local timestamp.\n\n" +
			"Takes an IANA name. Confirmed by reading it back — an unknown zone is\n" +
			"accepted and ignored, so the check is what makes the answer mean anything.",
		Example: `  mobium timezone
  mobium timezone Asia/Tokyo
  mobium timezone Europe/London`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["timezone"] = args[0]
			}
			return runTool("app_timezone", call)
		},
	}
}
