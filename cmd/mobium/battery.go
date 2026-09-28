package main

import "github.com/spf13/cobra"

func newBatteryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "battery",
		Short: "Read the battery: level, charging state, what it is plugged into",
		Long: "The level in percent and whether it is charging, discharging, not charging\n" +
			"or full; on Android also what powers it. An iOS simulator has no battery\n" +
			"and says so.",
		Example: `  mobium battery`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_battery", map[string]interface{}{})
		},
	}
}

func newTimeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "time",
		Short: "Read the device's clock, in its own zone",
		Long: "What a clock-dependent screen shows. A phone and an emulator read their own\n" +
			"clock; an iOS simulator has none and reads the Mac's, and says so. To change\n" +
			"the zone, `mobium timezone`.",
		Example: `  mobium time`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTool("app_time", map[string]interface{}{})
		},
	}
}
