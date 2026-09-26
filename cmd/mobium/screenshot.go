package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newScreenshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Capture the device screen as PNG",
		Example: `  mobium screenshot
  # saves ./screenshot-20260911-231500.png

  mobium screenshot -o login.png`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out, _ := cmd.Flags().GetString("output")
			if out == "" {
				// Same instinct as vibium: a screenshot with no path still
				// lands somewhere obvious rather than erroring.
				out = fmt.Sprintf("screenshot-%s.png", time.Now().Format("20060102-150405"))
			}
			// Always pass a path from the CLI: the tool then writes the file
			// and returns a line of text, instead of base64-ing a megabyte
			// of PNG back through the socket for the terminal to discard.
			return runTool("app_screenshot", map[string]interface{}{"path": out})
		},
	}
	cmd.Flags().StringP("output", "o", "", "Where to write the PNG (default: ./screenshot-<timestamp>.png)")
	return cmd
}
