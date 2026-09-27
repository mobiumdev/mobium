// Command mobium automates native apps on virtual devices.
//
// Step 1 scope: Android emulators, via adb and uiautomator, with the map/@ref
// model that the rest of the tool will be built on.
package main

import (
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"os"
	"path/filepath"

	"github.com/mobiumdev/mobium/internal/daemon"
	"github.com/spf13/cobra"
)

var version = "dev"

// Global flags, mirroring vibium's shape so the two CLIs feel like siblings.
var (
	deviceSerial string
	backendName  string
	jsonOutput   bool
	verbose      bool
)

func main() {
	progName := filepath.Base(os.Args[0])

	root := &cobra.Command{
		Use:           progName,
		Short:         "Mobile app automation for AI agents and humans",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Runs only once cobra has accepted the command line, so a failure
		// with this still false was the command line's, and exits 2.
		PersistentPreRun: func(*cobra.Command, []string) { commandRan = true },
		Long: "Mobium automates native apps on Android emulators and phones, iOS simulators\n" +
			"and iPhones, using the same map/@ref workflow as vibium:\n\n" +
			"  mobium map && mobium tap @e1 && mobium map",
	}

	root.PersistentFlags().StringVar(&deviceSerial, "device", "",
		"Target device serial (default: the only running device)")
	root.PersistentFlags().StringVar(&backendName, "driver", "",
		"Driver: uiautomator2 (default, Android), uiautomator (Android, installs nothing), "+
			"wda (WebDriverAgent: iOS simulators and iPhones), or the name of a third-party driver "+
			"installed as mobium-driver-<name>")
	root.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Emit JSON instead of text")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Log what mobium is doing to stderr")

	// Slow one-time setup (downloading and installing the UiAutomator2
	// server) is reported to stderr as it happens, so a first run explains
	// itself instead of appearing to hang.
	daemon.OnProgress = func(msg string) {
		fmt.Fprintf(os.Stderr, "%s...\n", msg)
	}

	root.AddCommand(
		newDevicesCmd(),
		newDoctorCmd(),
		newMapCmd(),
		newTapCmd(),
		newDoubleTapCmd(),
		newTextCmd(),
		newDialogsCmd(),
		newSourceCmd(),
		newFindCmd(),
		newWaitCmd(),
		newScrollToCmd(),
		newLaunchCmd(),
		newTerminateCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newClearDataCmd(),
		newAppsCmd(),
		newOpenCmd(),
		newCurrentCmd(),
		newGrantCmd(),
		newRevokeCmd(),
		newResetPermissionsCmd(),
		newAppearanceCmd(),
		newAccessibilityCmd(),
		newOrientationCmd(),
		newScreenCmd(),
		newLocaleCmd(),
		newPressCmd(),
		newLockCmd(),
		newSessionCmd(),
		newCallCmd(),
		newSMSCmd(),
		newTimezoneCmd(),
		newLocationCmd(),
		newClipboardCmd(),
		newAlertCmd(),
		newCheckCmd(),
		newUncheckCmd(),
		newZoomCmd(),
		newRotateCmd(),
		newDragCmd(),
		newPressTapCmd(),
		newPressDragCmd(),
		newNotificationsCmd(),
		newLogsCmd(),
		newCrashesCmd(),
		newKeyboardCmd(),
		newRecordCmd(),
		newEvalCmd(),
		newContextsCmd(),
		newContextCmd(),
		newTypeCmd(),
		newSwipeCmd(),
		newLongPressCmd(),
		newScreenshotCmd(),
		newDaemonCmd(),
		newPipeCmd(),
		newMCPCmd(),
	)

	if err := root.Execute(); err != nil {
		if !commandRan && mobiumerr.CodeOf(err) == mobiumerr.Unclassified {
			err = mobiumerr.Wrap(mobiumerr.InvalidArgument, err, "")
		}
		printError(err)
		os.Exit(exitStatus(err))
	}
}

func logf(format string, args ...interface{}) {
	if verbose {
		fmt.Fprintf(os.Stderr, "[mobium] "+format+"\n", args...)
	}
}
