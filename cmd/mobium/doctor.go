package main

import (
	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that the environment mobium needs is present",
		Long: "Run this first when something fails for a reason that does not make sense.\n\n" +
			"Several of the toolchain's own errors name the wrong cause — the emulator\n" +
			"dies with \"Broken AVD system path\" when the real problem is a missing\n" +
			"platform-tools directory, while adb sits on PATH working perfectly.\n\n" +
			"Needs no device: its whole job is to be runnable when nothing works yet.",
		Args: cobra.NoArgs,
		// SilenceUsage on a failing doctor: the problem is the machine, not
		// how the command was typed, and printing usage buries the report.
		RunE: func(cmd *cobra.Command, args []string) error {
			h := agent.NewHandlers()
			defer h.Close()
			result, err := h.Call("app_doctor", nil)
			if err != nil {
				return err
			}
			if err := emit(result, nil); err != nil {
				return err
			}
			if view, ok := result.StructuredContent.(agent.DoctorView); ok && view.Problems > 0 {
				// Exit non-zero so a script or a setup step can act on it.
				// The report has already been printed, so say nothing more.
				return errSilent
			}
			return nil
		},
	}
}

// errSilent exits non-zero without printing anything further: doctor has
// already said everything useful, and an error line after the report would
// only repeat it.
var errSilent = silentError{}

type silentError struct{}

func (silentError) Error() string { return "" }
