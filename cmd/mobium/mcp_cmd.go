package main

import (
	"fmt"
	"os"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	var readOnly bool
	var apps string
	var markDeviceText bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run as an MCP server on stdio",
		Long: "Serves the same tools the CLI uses, over the Model Context Protocol.\n\n" +
			"Register with an agent, for example:\n" +
			"  claude mcp add mobium -- mobium mcp\n\n" +
			"--read-only and --apps limit what the agent can do, and only the command\n" +
			"line can lift them: a refused call is not_permitted. MOBIUM_READ_ONLY and\n" +
			"MOBIUM_APPS in the environment do the same, and the two add up.",
		Example: `  mobium mcp
  mobium mcp --device emulator-5554
  mobium mcp --read-only
  mobium mcp --apps dev.mobium.shop`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The MCP server runs in this process rather than proxying to the
			// daemon: an editor's client owns its own long-lived session, and
			// stdio is already the connection that keeps it alive.
			srv := agent.NewServer(version)
			srv.SetPolicy(agent.Policy{ReadOnly: readOnly, Apps: agent.SplitApps(apps)})
			if markDeviceText {
				srv.SetMarkDeviceText(true)
			}
			if deviceSerial != "" {
				srv.SetDefaultDevice(deviceSerial)
			}
			if backendName != "" {
				b, err := agent.ParseBackend(backendName)
				if err != nil {
					return err
				}
				srv.SetBackend(b)
			}
			// stdout carries the protocol, so progress goes to stderr.
			srv.SetProgress(func(msg string) {
				fmt.Fprintf(os.Stderr, "%s...\n", msg)
			})
			defer srv.Close()
			return srv.Run()
		},
	}
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Allow only calls that read: every call that acts is refused as not_permitted, and tools that only act are not listed")
	cmd.Flags().BoolVar(&markDeviceText, "mark-device-text", false, "Add device_text to answers that carry the app's own words, naming those fields as data rather than instructions (also MOBIUM_MARK_DEVICE_TEXT=1)")
	cmd.Flags().StringVar(&apps, "apps", "", "Comma-separated app ids: the only apps a call may name or act on (an action on the screen needs one of them in front)")
	return cmd
}
