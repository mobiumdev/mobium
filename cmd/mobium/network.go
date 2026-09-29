package main

import (
	"github.com/spf13/cobra"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

func newNetworkCmd() *cobra.Command {
	var (
		offline, online, reset bool
		latency, down, up      int
	)
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Read or set network conditions: offline, latency, bandwidth (Android)",
		Long: "With no flags, reports the device's network conditions. --offline turns\n" +
			"airplane mode on and waits for the network to go, on an emulator or a real\n" +
			"phone; --online turns it off. --latency, --download and --upload shape the\n" +
			"traffic on an emulator, replacing any shaping set before. Everything is read\n" +
			"back, and the end of the session puts the network back as it was.",
		Example: `  mobium network                           # what is in place
  mobium network --offline                 # airplane mode, and wait to be offline
  mobium network --online
  mobium network --latency 300 --download 1600 --upload 750   # a slow 3G
  mobium network --reset`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			switch {
			case offline && online:
				return mobiumerr.New(mobiumerr.InvalidArgument, "--offline and --online contradict each other")
			case offline:
				call["offline"] = true
			case online:
				call["offline"] = false
			}
			if cmd.Flags().Changed("latency") {
				call["latency_ms"] = latency
			}
			if cmd.Flags().Changed("download") {
				call["download_kbps"] = down
			}
			if cmd.Flags().Changed("upload") {
				call["upload_kbps"] = up
			}
			if reset {
				call["reset"] = true
			}
			return runTool("app_network", call)
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "Turn airplane mode on, and wait for the network to go")
	cmd.Flags().BoolVar(&online, "online", false, "Turn airplane mode off, and wait for the network to return")
	cmd.Flags().IntVar(&latency, "latency", 0, "Milliseconds added to every round trip (emulator)")
	cmd.Flags().IntVar(&down, "download", 0, "Download limit, kbit/s (emulator)")
	cmd.Flags().IntVar(&up, "upload", 0, "Upload limit, kbit/s (emulator)")
	cmd.Flags().BoolVar(&reset, "reset", false, "Remove the shaping and turn airplane mode off")
	return cmd
}
