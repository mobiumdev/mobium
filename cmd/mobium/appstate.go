package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

func newStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "state <app>",
		Short: "Say whether an app is in front, in the background, not running or not installed",
		Long: "For any app, where `mobium current` names only the one in front. An app\n" +
			"under its own permission prompt is still in front, and the answer names\n" +
			"what covers it. On iOS a background app also says whether it is suspended.",
		Example: `  mobium state org.wikipedia`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_state", map[string]interface{}{"app": args[0]})
		},
	}
}

func newBackgroundCmd() *cobra.Command {
	var app string
	cmd := &cobra.Command{
		Use:   "background <seconds>",
		Short: "Send the app in front away for a while, and bring it back",
		Long: "Resumed where it was, not relaunched, and confirmed in front again — how\n" +
			"a resume path is tested. Takes seconds, or a duration such as 5s or 1m;\n" +
			"at most 180 seconds.",
		Example: `  mobium background 5
  mobium background 30s --app org.wikipedia`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			secs, err := parseSeconds(args[0])
			if err != nil {
				return err
			}
			call := map[string]interface{}{"seconds": secs}
			if app != "" {
				call["app"] = app
			}
			return runTool("app_background", call)
		},
	}
	cmd.Flags().StringVar(&app, "app", "", "The app to send away, which must be in front (default: the one in front)")
	return cmd
}

// parseSeconds reads a bare number of seconds or a Go duration.
func parseSeconds(s string) (float64, error) {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}
	if d, err := time.ParseDuration(strings.TrimSpace(s)); err == nil {
		return d.Seconds(), nil
	}
	return 0, mobiumerr.New(mobiumerr.InvalidArgument, "%q is not a number of seconds or a duration such as 5s", s)
}
