package main

import "github.com/spf13/cobra"

func newAuditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit",
		Short: "Run the platform's own accessibility audit on the screen in front",
		Long: "On iOS this is Apple's audit, XCUITest's performAccessibilityAudit through\n" +
			"WebDriverAgent, on a simulator or a real iPhone (iOS 17 and later): hit regions\n" +
			"too small to touch, contrast, labels that repeat their traits, Dynamic Type,\n" +
			"clipped text. Each finding names the element and gives a locator for it.\n" +
			"It audits only what is on screen. Its hit-region rule is Apple's and far below\n" +
			"the 44pt guideline, so `mobium screen --inspect` can name a small target this\n" +
			"passes.\n\n" +
			"On Android, which has no audit a tool outside the app can run, it is Mobium's\n" +
			"checks on the hierarchy, named as Google's Accessibility Test Framework names\n" +
			"them: touch targets under 48dp, controls with nothing to say, controls that say\n" +
			"the same thing, two controls on one rectangle, a description that names its own\n" +
			"kind, a text field with a description. Contrast is not checked.",
		Example: "  mobium audit\n  mobium audit --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_audit", map[string]interface{}{})
		},
	}
}
