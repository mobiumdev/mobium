package main

import (
	"time"

	"github.com/spf13/cobra"
)

func newWaitCmd() *cobra.Command {
	var (
		condition  string
		text       string
		timeout    time.Duration
		not, exact bool
		count      int
	)
	cmd := &cobra.Command{
		Use:   "wait <@ref | locator>",
		Short: "Wait for an element to appear, disappear, show text, or reach a state",
		Long: "Wait until the screen says what you are waiting for, instead of sleeping and\n" +
			"hoping. On success the screen is remapped, so the element already has a ref.",
		Example: `  mobium wait text="Welcome back"          # wait for it to appear
  mobium wait role=progressbar --for hidden # wait for a spinner to go
  mobium wait @e4 --for text --text "Sent"  # wait for its text to change
  mobium wait testid=terms --for checked    # a checkbox, radio or switch
  mobium wait testid=search --for value --text ""  # the field is empty
  mobium wait @e4 --for text --text Sending --not    # until it changes
  mobium wait testid=row --count 3                  # three rows on screen
  mobium wait text=Done --timeout 30s`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"target": args[0]}
			if condition != "" {
				call["condition"] = condition
			}
			if cmd.Flags().Changed("text") {
				call["text"] = text
			}
			if not {
				call["not"] = true
			}
			if exact {
				call["exact"] = true
			}
			if cmd.Flags().Changed("count") {
				call["count"] = count
				if condition == "" {
					call["condition"] = "count"
				}
			}
			if cmd.Flags().Changed("timeout") {
				call["timeout_ms"] = int(timeout / time.Millisecond)
			}
			return runTool("app_wait_for", call)
		},
	}
	cmd.Flags().StringVar(&condition, "for", "", "Condition: visible (default), hidden, text, value, enabled, disabled, checked, unchecked, focused, or count")
	cmd.Flags().StringVar(&text, "text", "", "Text to wait for, with --for text; the whole value, with --for value")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "How long to wait before failing")
	cmd.Flags().BoolVar(&not, "not", false, "Wait for the opposite: --for text --not waits for the text to change")
	cmd.Flags().BoolVar(&exact, "exact", false, "With --for text, match the whole text rather than a part")
	cmd.Flags().IntVar(&count, "count", 0, "Wait until the locator matches this many elements (--for count)")
	return cmd
}
