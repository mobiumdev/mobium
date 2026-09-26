package main

import (
	"fmt"
	"os"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run as an MCP server on stdio",
		Long: "Serves the same tools the CLI uses, over the Model Context Protocol.\n\n" +
			"Register with an agent, for example:\n" +
			"  claude mcp add mobium -- mobium mcp",
		Example: `  mobium mcp
  mobium mcp --device emulator-5554`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The MCP server runs in this process rather than proxying to the
			// daemon: an editor's client owns its own long-lived session, and
			// stdio is already the connection that keeps it alive.
			srv := agent.NewServer(version)
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
}
