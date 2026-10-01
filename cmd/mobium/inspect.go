package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/mobiumdev/mobium/internal/inspect"
)

func newInspectCmd() *cobra.Command {
	var port int
	var open bool
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "See the screen, the locator for anything on it, and record what you do as a test",
		Long: "Serves a page on this machine showing the device's screen with every element\n" +
			"map finds outlined. Click one for its locator — the one a test should name it\n" +
			"by — and act on it: tap, type, check, wait for it. Type a locator to see what\n" +
			"it matches. What you do is kept as steps, and downloads as a *.test.json that\n" +
			"mobium test runs. It calls the same tools as every command, through the same\n" +
			"daemon, so a terminal and the page drive one session.\n\n" +
			"It listens on 127.0.0.1 only, and answers only requests carrying the token in\n" +
			"the URL it prints — any other page in the browser can reach 127.0.0.1. See\n" +
			"docs/guides/inspector.md.",
		Example: `  mobium inspect                       # the only device running
  mobium inspect --device emulator-5554 --open
  mobium inspect --driver wda --device <simulator-udid>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := inspect.New(daemonCall)
			if err != nil {
				return err
			}
			ln, err := srv.Listen(strconv.Itoa(port))
			if err != nil {
				return err
			}
			url := srv.URL()
			fmt.Printf("mobium inspect on %s — Ctrl-C to stop\n", url)
			if open {
				opener := map[string][]string{"darwin": {"open"}, "windows": {"cmd", "/c", "start", ""}}[runtime.GOOS]
				if opener == nil {
					opener = []string{"xdg-open"}
				}
				_ = exec.Command(opener[0], append(opener[1:], url)...).Start()
			}
			hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, os.Interrupt)
			go func() { <-stop; _ = hs.Close() }()
			if err := hs.Serve(ln); err != http.ErrServerClosed {
				return err
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "port on 127.0.0.1 (default: any free one)")
	cmd.Flags().BoolVar(&open, "open", false, "open the page in the browser")
	return cmd
}
