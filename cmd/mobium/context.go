package main

import "github.com/spf13/cobra"

func newContextsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "contexts",
		Short: "List automatable contexts (native shell and WebViews)",
		Example: `  mobium contexts
  # NATIVE_APP  (current)
  # WEBVIEW_com.example  — Checkout https://shop.example/cart`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_contexts", map[string]interface{}{})
		},
	}
}

func newContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "context [name]",
		Short: "Switch context, or show the current one",
		Long: "While a WebView context is active, `map` and `tap` operate on the page's\n" +
			"elements. Taps are still delivered as real touches on the device.",
		Example: `  mobium context                       # show the current context
  mobium context WEBVIEW_com.example   # drive the web content
  mobium context NATIVE_APP            # back to the native shell`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			toolArgs := map[string]interface{}{}
			if len(args) == 1 {
				toolArgs["context"] = args[0]
			}
			return runTool("app_context", toolArgs)
		},
	}
}
