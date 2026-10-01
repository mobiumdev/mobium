package main

import "github.com/spf13/cobra"

func newAuditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit",
		Short: "Run the platform's own accessibility audit on the screen in front (iOS)",
		Long: "On iOS this is Apple's audit, XCUITest's performAccessibilityAudit through\n" +
			"WebDriverAgent, on a simulator or a real iPhone (iOS 17 and later): hit regions\n" +
			"too small to touch, contrast, labels that repeat their traits, Dynamic Type,\n" +
			"clipped text. Each finding names the element and gives a locator for it.\n" +
			"It audits only what is on screen. Its hit-region rule is Apple's and far below\n" +
			"the 44pt guideline, so `mobium screen --inspect` can name a small target this\n" +
			"passes. Android refuses: its audits run inside the app.",
		Example: "  mobium audit\n  mobium audit --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_audit", map[string]interface{}{})
		},
	}
}
