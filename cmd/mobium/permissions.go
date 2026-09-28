package main

import (
	"strings"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/spf13/cobra"
)

func newGrantCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grant <app> <permission...>",
		Short: "Grant permissions so no dialog blocks the flow",
		Long: "Grant up front and the first-launch permission dialog never appears.\n\n" +
			"Names are cross-platform: " + strings.Join(agent.PermissionNames(), ", ") + ".\n" +
			"A platform name such as android.permission.CAMERA also works.\n\n" +
			"On Android the result is verified by reading the state back, because\n" +
			"`pm grant` reports success for permissions the app never declared.",
		Example: `  mobium grant com.example.shop all
  mobium grant com.example.shop camera microphone
  mobium grant com.example.shop android.permission.NFC`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_grant", permissionArgs(args))
		},
	}
}

func newRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "revoke <app> <permission...>",
		Short:   "Deny permissions, to test the app without them",
		Example: `  mobium revoke com.example.shop location`,
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_revoke", permissionArgs(args))
		},
	}
}

func newResetPermissionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset-permissions [app]",
		Short: "Put permissions back to their defaults",
		Long: "The app prompts again on next use.\n\n" +
			"Name an app to reset only that app's. On Android each runtime permission\n" +
			"it declares is revoked and the answers recorded against it cleared —\n" +
			"including the \"don't ask again\" two denials leave — and read back; one\n" +
			"the system or a device policy fixed is kept and named. Revoking a granted\n" +
			"permission stops the app, as it does from Settings. With no app, every\n" +
			"app on the device is reset.",
		Example: `  mobium reset-permissions com.example.shop  # one app
  mobium reset-permissions                   # every app on the device`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["app"] = args[0]
			}
			return runTool("app_reset_permissions", call)
		},
	}
}

func permissionArgs(args []string) map[string]interface{} {
	perms := make([]interface{}, 0, len(args)-1)
	for _, p := range args[1:] {
		perms = append(perms, p)
	}
	return map[string]interface{}{"app": args[0], "permissions": perms}
}
