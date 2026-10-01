package main

import "github.com/spf13/cobra"

func newAppsCmd() *cobra.Command {
	var system bool
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "List the apps installed on the device",
		Long: "By default only apps someone installed, which is nearly always the\n" +
			"question — a stock Android emulator ships about 240 system packages.",
		Example: `  mobium apps
  mobium apps --system   # include the platform's own`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_list_apps", map[string]interface{}{"system": system})
		},
	}
	cmd.Flags().BoolVar(&system, "system", false, "Include the platform's own apps")
	return cmd
}

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <app>",
		Short: "Remove an app from the device",
		Long: "Verified by listing afterwards, because `adb uninstall` reports success\n" +
			"when it has only removed the updates to a system app.",
		Example: `  mobium uninstall com.example.shop`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_uninstall", map[string]interface{}{"app": args[0]})
		},
	}
}

func newClearDataCmd() *cobra.Command {
	var bundle string
	cmd := &cobra.Command{
		Use:   "clear-data <app>",
		Short: "Delete an app's data and leave it installed",
		Long: "The state of a fresh install, without reinstalling. The app is stopped first.\n\n" +
			"Android runs `pm clear`, which also revokes the runtime permissions the user\n" +
			"granted. An iOS simulator has its preferences deleted and its data container\n" +
			"emptied; privacy grants and the keychain are not in the container and are kept.\n" +
			"The answer says what was read back empty and what was kept.\n\n" +
			"A real iPhone cannot clear in place. Given the app's own bundle with --bundle,\n" +
			"it uninstalls the app and installs it again — the reset a phone has — which\n" +
			"empties its data container, read back, and resets its privacy permissions,\n" +
			"which nothing outside the app can read. Installing over the app without\n" +
			"uninstalling would keep both.",
		Example: "  mobium clear-data com.example.shop\n" +
			"  mobium clear-data dev.mobium.mobiumapp --bundle build/MobiumApp.app   # a real iPhone",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"app": args[0]}
			if bundle != "" {
				call["path"] = bundle
			}
			return runTool("app_clear_data", call)
		},
	}
	cmd.Flags().StringVar(&bundle, "bundle", "", "A real iPhone: the app's .app or .ipa, reinstalled as the reset")
	return cmd
}
