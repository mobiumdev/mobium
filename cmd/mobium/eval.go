package main

import "github.com/spf13/cobra"

func newEvalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "eval <expression>",
		Short: "Run a JavaScript expression in the current WebView",
		Long: "Needs a WebView context — `mobium context WEBVIEW_...` first.\n\n" +
			"Objects come back as JSON. This is the escape hatch for anything map\n" +
			"and text do not cover, and the way to ask a page a question directly\n" +
			"rather than inferring the answer from what is on screen.",
		Example: `  mobium eval "document.title"
  mobium eval "JSON.stringify([innerWidth, innerHeight])"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_eval", map[string]interface{}{"expression": args[0]})
		},
	}
}
