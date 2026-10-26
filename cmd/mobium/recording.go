package main

import (
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/spf13/cobra"
)

func newRecordingCmd() *cobra.Command {
	var record, output string
	var offset int
	cmd := &cobra.Command{
		Use:   "recording <summary | calls | call ID | screen ID | screenshot ID | search QUERY> -i <recording.zip>",
		Short: "Read a recording the way a verifier does",
		Long: "Reads a trace's .zip through the views `check` gives its model: a summary,\n" +
			"the calls a page at a time, one call in full, the screen after a call as\n" +
			"text, its screenshot, and a search. Read-only. With `verdict` it lets you,\n" +
			"or an agent you run, be the judge of a claim, with no model credential.",
		Example: `  mobium recording summary -i form.zip
  mobium recording calls -i form.zip --offset 25
  mobium recording screen call@3 -i form.zip
  mobium recording screenshot call@3 -i form.zip -o after-3.jpg
  mobium recording search "Accept terms" -i form.zip`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"read": args[0], "record": record}
			switch args[0] {
			case "call", "screen", "screenshot", "search":
				if len(args) != 2 {
					return mobiumerr.New(mobiumerr.InvalidArgument, "recording %s needs a %s", args[0],
						map[bool]string{true: "query", false: "call id, such as call@3"}[args[0] == "search"])
				}
				if args[0] == "search" {
					call["query"] = args[1]
				} else {
					call["id"] = args[1]
				}
			}
			if cmd.Flags().Changed("offset") {
				call["offset"] = offset
			}
			if args[0] == "screenshot" {
				if output == "" {
					output = args[1] + ".jpg"
				}
				call["path"] = output
			}
			return runTool("app_recording", call)
		},
	}
	cmd.Flags().StringVarP(&record, "input", "i", "", "The recording: the .zip that mobium trace stop saved")
	cmd.Flags().IntVar(&offset, "offset", 0, "Where a page of calls or search hits starts")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Where to save a screenshot (default <id>.jpg)")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

func newVerdictCmd() *cobra.Command {
	var record, status, summary string
	var evidence []string
	cmd := &cobra.Command{
		Use:   "verdict <claim> -i <recording.zip> --status passed|failed|inconclusive --summary TEXT --evidence TEXT...",
		Short: "Give a verdict on a claim about a recording, held to the verifier's rules",
		Long: "Checks a verdict the way `check` checks its model's: passed or failed needs\n" +
			"evidence citing at least one call the recording holds, by id (call@3); at\n" +
			"most 12 observations; inconclusive when the recording cannot settle it.\n" +
			"Read the recording with `recording` first.",
		Example: `  mobium verdict "the terms box was ticked" -i form.zip --status passed \
    --summary "Accept terms is checked after the tap" \
    --evidence "call@3: the screen shows Accept terms (checkbox, checked)"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ev := make([]observation, 0, len(evidence))
			for _, e := range evidence {
				ev = append(ev, observation{Type: "observation", Summary: e})
			}
			return runTool("app_verdict", map[string]interface{}{
				"claim": args[0], "record": record, "status": status, "summary": summary, "evidence": ev,
			})
		},
	}
	cmd.Flags().StringVarP(&record, "input", "i", "", "The recording the claim was judged on")
	cmd.Flags().StringVar(&status, "status", "", "passed, failed or inconclusive")
	cmd.Flags().StringVar(&summary, "summary", "", "The verdict in a sentence or two")
	cmd.Flags().StringArrayVar(&evidence, "evidence", nil, "One observation, citing its call (repeat, at most 12)")
	_ = cmd.MarkFlagRequired("input")
	_ = cmd.MarkFlagRequired("status")
	_ = cmd.MarkFlagRequired("summary")
	return cmd
}

// observation is one item of a verdict's evidence, as app_verdict takes it.
type observation struct {
	Type    string `json:"type"`
	Summary string `json:"summary"`
}
