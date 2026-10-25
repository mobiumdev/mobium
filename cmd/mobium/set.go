package main

import "github.com/spf13/cobra"

// Two verbs, one tool. `set` and `unset` name a state, not a click — the
// names Vibium gave them when it freed `check` for checking a claim — and a
// single `app_set` with a value is the smaller surface for an agent: there
// is no third option to enumerate.
func newSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <@ref | locator>",
		Short: "Tick a checkbox or turn a switch on",
		Long: "Puts the control into the checked state, rather than toggling it.\n\n" +
			"Idempotent: if it is already checked this does nothing and says so,\n" +
			"which is what makes it safe to call without reading the state first.\n" +
			"That is the difference from `tap`, which flips whatever is there —\n" +
			"reaching a known state with tap means read, compare, tap, read again.\n\n" +
			"Refuses anything with no checked state rather than tapping it and\n" +
			"calling it checked, and confirms the tap by reading the state back.",
		Example: `  mobium set @e3
  mobium set testid=termsCheck`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_set", map[string]interface{}{
				"target": args[0], "value": true,
			})
		},
	}
}

func newUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <@ref | locator>",
		Short: "Clear a checkbox or turn a switch off",
		Long: "Puts the control into the unchecked state, rather than toggling it.\n\n" +
			"Idempotent, like `set`. A radio button is refused: a radio is not\n" +
			"unset, its group is cleared by choosing a different member.",
		Example: `  mobium unset @e3
  mobium unset testid=termsCheck`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_set", map[string]interface{}{
				"target": args[0], "value": false,
			})
		},
	}
}
