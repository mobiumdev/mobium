package main

import "github.com/spf13/cobra"

func newZoomCmd() *cobra.Command {
	var target string
	var from, to int
	cmd := &cobra.Command{
		Use:   "zoom [in|out]",
		Short: "Pinch to zoom, about an element or the screen",
		Long: "Puts two fingers on the screen and moves them apart, or together\n" +
			"with `out`.\n\n" +
			"It reports that the gesture was delivered and nothing more. Neither\n" +
			"platform exposes a zoom level in the accessibility hierarchy, so there\n" +
			"is nothing to read back the way `check` reads a checkbox — confirming a\n" +
			"zoom means asking whatever was zoomed. A WebView can answer:\n\n" +
			"    mobium context WEBVIEW_...\n" +
			"    mobium eval 'visualViewport.scale'\n\n" +
			"Pinching about the wrong point scales the right amount in the wrong\n" +
			"place, so pass --target when that matters.\n\n" +
			"A direction pinches by a default amount. --from and --to say exactly how\n" +
			"far the fingers travel, for a pinch sized to an element or a deliberately\n" +
			"small one — the same split `swipe` makes between a direction and exact\n" +
			"coordinates.",
		Example: `  mobium zoom
  mobium zoom out
  mobium zoom in --target @e2
  mobium zoom --from 40 --to 90      # a small pinch, in exact pixels`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"in", "out"},
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["direction"] = args[0]
			}
			if target != "" {
				call["target"] = target
			}
			// Pass through exactly what was given, including only one of the
			// pair. The tool layer owns the validation — a CLI that quietly
			// supplied the missing half would turn "you gave half a pinch"
			// into "a distance must be above zero", which is a true sentence
			// about a mistake the caller did not make.
			if cmd.Flags().Changed("from") {
				call["from"] = from
			}
			if cmd.Flags().Changed("to") {
				call["to"] = to
			}
			return runTool("app_zoom", call)
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "element to pinch about (@ref or locator)")
	cmd.Flags().IntVar(&from, "from", 0,
		"half the gap between the fingers at the start, in device pixels")
	cmd.Flags().IntVar(&to, "to", 0,
		"half the gap at the end; larger than --from zooms in")
	return cmd
}
