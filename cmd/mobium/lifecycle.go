package main

import "github.com/spf13/cobra"

func newLaunchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "launch <package | bundle-id>",
		Short: "Bring an app to the foreground",
		Example: `  mobium launch com.google.android.dialer
  mobium launch com.apple.Preferences`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_launch", map[string]interface{}{"app": args[0]})
		},
	}
}

func newTerminateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "terminate <package | bundle-id>",
		Aliases: []string{"stop"},
		Short:   "Stop a running app",
		Example: `  mobium terminate com.google.android.dialer`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_terminate", map[string]interface{}{"app": args[0]})
		},
	}
}

func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <path>",
		Short: "Install an app from a local .apk or .app",
		Example: `  mobium install ./build/app-debug.apk
  mobium install ./build/Shop.app`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_install", map[string]interface{}{"path": args[0]})
		},
	}
}

func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <url>",
		Short: "Open a URL or deep link",
		Long: "The quickest way to reach a specific screen without tapping through\n" +
			"to it, when the app exposes a deep link for it.",
		Example: `  mobium open https://example.com
  mobium open myapp://cart`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_open_url", map[string]interface{}{"url": args[0]})
		},
	}
}

func newCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the app in the foreground",
		Example: `  mobium current
  # com.google.android.dialer`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_current", map[string]interface{}{})
		},
	}
}
