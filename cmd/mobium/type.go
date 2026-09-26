package main

import "github.com/spf13/cobra"

func newTypeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "type <@ref | locator> <text>",
		Short: "Type text into an element",
		Long: "Types into the element you name rather than whatever holds focus, so quotes,\n" +
			"spaces and non-ASCII arrive intact. Needs the uiautomator2 backend.",
		Example: `  mobium type @e3 "hello@example.com"
  mobium type testid=search "O'Brien & Sons"
  mobium type @e3 ""                # clear the field
  mobium type @e3 "new" --clear     # replace what is there`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clear, _ := cmd.Flags().GetBool("clear")
			return runTool("app_type", map[string]interface{}{
				"target": args[0],
				"text":   args[1],
				"clear":  clear,
			})
		},
	}
	cmd.Flags().Bool("clear", false, "Clear the field before typing")
	return cmd
}
