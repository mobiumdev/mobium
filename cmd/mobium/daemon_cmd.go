package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mobiumdev/mobium/internal/daemon"
	"github.com/mobiumdev/mobium/internal/paths"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the background daemon",
		Long: "Commands normally start the daemon on demand and it exits when idle.\n" +
			"These subcommands are for inspecting or controlling it directly.",
	}
	cmd.AddCommand(newDaemonStartCmd(), newDaemonStopCmd(), newDaemonStatusCmd())
	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Run the daemon in the foreground",
		Example: `  mobium daemon start
  mobium daemon start --idle-timeout 5m`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			idle, _ := cmd.Flags().GetDuration("idle-timeout")

			// Refuse to start a second daemon on the same socket: the winner
			// of that race is arbitrary, and the loser's failure to bind is
			// far more confusing than this message.
			daemon.CleanStale()
			if pid, err := daemon.ReadPID(); err == nil && pid != 0 && daemon.Running(pid) {
				return fmt.Errorf("a daemon is already running (pid %d) — `mobium daemon stop` to replace it", pid)
			}

			d := daemon.New(daemon.Options{Version: version, IdleTimeout: idle})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sigs := make(chan os.Signal, 2)
			// SIGHUP too: a daemon started in the foreground gets it when
			// its terminal closes, and Go's default for it is to exit on the
			// spot, leaving every device session as it was. The auto-started
			// daemon is in a session of its own and never sees one.
			signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			go func() {
				<-sigs
				cancel()
				// A second signal means "now", the convention every
				// long-running tool follows. The first shutdown can take up
				// to half a minute when a tool call is stuck, and waiting it
				// out should be a choice.
				<-sigs
				logf("second signal — exiting without closing device sessions")
				if err := daemon.RemovePID(); err != nil {
					// The next start's stale check clears it once this
					// process is gone; said so the refusal is not a surprise.
					logf("could not remove the PID file: %v", err)
				}
				os.Exit(1)
			}()

			socket, err := paths.SocketPath()
			if err != nil {
				return err
			}
			logf("daemon listening on %s (idle timeout %s)", socket, idle)
			return d.Run(ctx)
		},
	}
	cmd.Flags().Duration("idle-timeout", 30*time.Minute,
		"Exit after this long with no commands (0 disables)")
	return cmd
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Shutdown(); err != nil {
				if daemon.IsConnectionError(err) {
					daemon.CleanStale()
					if jsonOutput {
						return printJSON(map[string]string{"status": "not running"})
					}
					fmt.Println("No daemon is running")
					return nil
				}
				return err
			}
			if jsonOutput {
				return printJSON(map[string]string{"status": "stopped"})
			}
			fmt.Println("Daemon stopped")
			return nil
		},
	}
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the daemon is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := daemon.Status()
			if err != nil {
				if daemon.IsConnectionError(err) {
					if jsonOutput {
						return printJSON(map[string]interface{}{"running": false})
					}
					fmt.Println("Daemon is not running")
					return nil
				}
				return err
			}
			if jsonOutput {
				return printJSON(status)
			}
			fmt.Printf("Daemon running (pid %d, up %s)\n", status.PID, status.Uptime)
			fmt.Printf("  version %s\n", status.Version)
			fmt.Printf("  socket  %s\n", status.Socket)
			if status.Session != "" {
				fmt.Printf("  session %s\n", status.Session)
			}
			return nil
		},
	}
}
