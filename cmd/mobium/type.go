package main

import "github.com/spf13/cobra"

func newTypeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "type <@ref | locator> <text>",
		Short: "Type text into an element, after what it holds",
		Long: "Types into the element you name rather than whatever holds focus, so quotes,\n" +
			"spaces and non-ASCII arrive intact, after what the field already holds.\n" +
			"`mobium fill` replaces it instead. Not on Android's uiautomator dump driver,\n" +
			"which declines text entry rather than enter it wrong.",
		Example: `  mobium type @e3 "hello@example.com"
  mobium type testid=search "O'Brien & Sons"
  mobium type @e3 ""                # clear the field`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_type", map[string]interface{}{
				"target": args[0],
				"text":   args[1],
			})
		},
	}
}

// newFillCmd is Vibium's fill: the field cleared, then typed into. It is its
// own tool, app_fill, rather than a flag on type, because an agent that knows
// Vibium looks for a tool by that name and cannot see a flag.
func newFillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fill <@ref | locator> <text>",
		Short: "Clear an element and type text into it",
		Long: "Replaces what the field holds with the text: cleared, then typed into, located\n" +
			"and checked as `mobium type` is. Not on Android's uiautomator dump driver.\n\n" +
			"On a slider the text is a position, from 0 (the start of its track) to 1 (the\n" +
			"end), and the answer is the value the slider then reports.",
		Example: `  mobium fill @e3 "hello@example.com"
  mobium fill testid=search "new query"
  mobium fill label=Volume 0.3                 # a slider, 30% of the way along`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_fill", map[string]interface{}{
				"target": args[0],
				"text":   args[1],
			})
		},
	}
}
