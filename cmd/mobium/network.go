package main

import (
	"github.com/spf13/cobra"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

func newNetworkCmd() *cobra.Command {
	var (
		offline, online, reset bool
		latency, down, up      int
		proxy                  string
		noProxy                bool
	)
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Read or set network conditions: offline, latency, bandwidth, a proxy (Android)",
		Long: "With no flags, reports the device's network conditions. --offline turns\n" +
			"airplane mode on and waits for the network to go, on an emulator or a real\n" +
			"phone; --online turns it off. --latency, --download and --upload shape the\n" +
			"traffic on an emulator, replacing any shaping set before. --proxy points the\n" +
			"device's HTTP proxy at host:port on any Android device, and a proxy on\n" +
			"127.0.0.1 is this machine's, through adb reverse; --no-proxy takes it off.\n" +
			"Everything is read\n" +
			"back, and the end of the session puts the network back as it was.",
		Example: `  mobium network                           # what is in place
  mobium network --offline                 # airplane mode, and wait to be offline
  mobium network --online
  mobium network --latency 300 --download 1600 --upload 750   # a slow 3G
  mobium network --proxy 127.0.0.1:8888    # a proxy listening on this machine
  mobium network --no-proxy
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
			switch {
			case noProxy && cmd.Flags().Changed("proxy"):
				return mobiumerr.New(mobiumerr.InvalidArgument, "--proxy and --no-proxy contradict each other")
			case noProxy:
				call["proxy"] = ""
			case cmd.Flags().Changed("proxy"):
				if proxy == "" {
					return mobiumerr.New(mobiumerr.InvalidArgument, "--proxy takes host:port; --no-proxy takes the proxy off")
				}
				call["proxy"] = proxy
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
	cmd.Flags().StringVar(&proxy, "proxy", "", "The device's HTTP proxy, host:port; 127.0.0.1 is this machine's")
	cmd.Flags().BoolVar(&noProxy, "no-proxy", false, "Take the device's HTTP proxy off")
	cmd.Flags().BoolVar(&reset, "reset", false, "Remove the shaping and the proxy and turn airplane mode off")
	return cmd
}
