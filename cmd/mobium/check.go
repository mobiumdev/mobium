package main

import "github.com/spf13/cobra"

// Two verbs, one tool. `check` and `uncheck` are what the action is called
// everywhere it exists, and a single `app_check` with a state is the smaller
// surface for an agent — there is no third option to enumerate.
func newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <@ref | locator>",
		Short: "Tick a checkbox or turn a switch on",
		Long: "Puts the control into the checked state, rather than toggling it.\n\n" +
			"Idempotent: if it is already checked this does nothing and says so,\n" +
			"which is what makes it safe to call without reading the state first.\n" +
			"That is the difference from `tap`, which flips whatever is there —\n" +
			"reaching a known state with tap means read, compare, tap, read again.\n\n" +
			"Refuses anything with no checked state rather than tapping it and\n" +
			"calling it checked, and confirms the tap by reading the state back.",
		Example: `  mobium check @e3
  mobium check testid=termsCheck`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_check", map[string]interface{}{
				"target": args[0], "checked": true,
			})
		},
	}
}

func newUncheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uncheck <@ref | locator>",
		Short: "Clear a checkbox or turn a switch off",
		Long: "Puts the control into the unchecked state, rather than toggling it.\n\n" +
			"Idempotent, like `check`. A radio button is refused: a radio is not\n" +
			"unchecked, its group is cleared by choosing a different member.",
		Example: `  mobium uncheck @e3
  mobium uncheck testid=termsCheck`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_check", map[string]interface{}{
				"target": args[0], "checked": false,
			})
		},
	}
}
