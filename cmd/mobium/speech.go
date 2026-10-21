package main

import (
	"github.com/spf13/cobra"
)

func newSpeechCmd() *cobra.Command {
	var (
		walk   bool
		limit  int
		listen string
	)
	cmd := &cobra.Command{
		Use:   "speech [target]",
		Short: "Say what the screen reader says, about one element or walking the screen",
		Long: "Reports what the screen reader says — VoiceOver on a real iPhone (iOS 27, a\n" +
			"runner built by Xcode 27), TalkBack on Android. With no target, the sentence for\n" +
			"the element its cursor is on; with a target, the cursor is put on it first.\n" +
			"--walk moves forward in the screen reader's own order, a swipe right at a time,\n" +
			"until it goes no further, comes back round, or --limit moves are made. Each\n" +
			"sentence is shown beside the element it was said about, and whether map lists\n" +
			"that element — a heading or a status line is spoken and not listed.\n\n" +
			"The screen reader must be on: mobium accessibility screen_reader on. TalkBack\n" +
			"is read from its log, which holds what it says only with its Developer\n" +
			"settings' Log output level at VERBOSE. A sentence about a password field is\n" +
			"not shown. A simulator's runner cannot reach VoiceOver and is refused.\n\n" +
			"--listen start, then an action, then --listen stop reports everything the screen\n" +
			"reader said in between — on Android each with its cause: the cursor moving, an\n" +
			"announcement by the app, or a change on screen such as a live region's.",
		Example: `  mobium speech
  mobium speech --walk
  mobium speech 'label="Form Demo"' --walk --limit 10
  mobium speech --listen start; mobium tap testid=addLive; mobium speech --listen stop`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["target"] = args[0]
			}
			if walk {
				call["walk"] = true
			}
			if cmd.Flags().Changed("limit") {
				call["limit"] = limit
			}
			if listen != "" {
				call["listen"] = listen
			}
			return runTool("app_speech", call)
		},
	}
	cmd.Flags().BoolVar(&walk, "walk", false, "Move forward through the screen, reporting each sentence")
	cmd.Flags().StringVar(&listen, "listen", "", "start, before an action, or stop, after it, to hear what the screen reader said in between")
	cmd.Flags().IntVar(&limit, "limit", 40, "Most moves a walk makes, or putting the cursor on the target takes on Android")
	return cmd
}
