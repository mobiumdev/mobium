package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var level, source, app string
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Read the WebView console, or the device's own log",
		Long: "With no --source it follows the context: in a WebView, what the page has\n" +
			"written to its console, including uncaught errors and unhandled promise\n" +
			"rejections; on the native shell, the device log — logcat on Android, the\n" +
			"unified log on an iOS simulator, and on a real iPhone what the session has\n" +
			"captured since it started, since a phone keeps no history.\n\n" +
			"Each read reports what arrived since the last one. That is what makes\n" +
			"\"nothing was logged during this step\" something you can assert. A\n" +
			"console's capture starts when the context is entered; the device log's\n" +
			"first read returns the most recent lines. A read narrowed by --app or\n" +
			"--level leaves the rest unread.\n\n" +
			"For why an app died, `mobium crashes` is the record that survives — the\n" +
			"log is a ring that noise overruns.",
		Example: `  mobium logs
  mobium logs --level error
  mobium logs --source device --app com.example.shop
  mobium logs --source device --lines 20`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if level != "" {
				call["level"] = level
			}
			if source != "" {
				call["source"] = source
			}
			if app != "" {
				call["app"] = app
			}
			if cmd.Flags().Changed("lines") {
				call["lines"] = lines
			}
			return runTool("app_logs", call)
		},
	}
	cmd.Flags().StringVar(&level, "level", "", "Only entries at this level (console: log, info, warn, error, debug; "+
		"device: verbose, debug, info, warn, error, fatal)")
	cmd.Flags().StringVar(&source, "source", "", "Which log: webview or device (default: follow the context)")
	cmd.Flags().StringVar(&app, "app", "", "Device log only: one app's lines, by package or bundle id")
	cmd.Flags().IntVar(&lines, "lines", 100, "Device log only: the most recent lines to return, at most")
	return cmd
}

func newCrashesCmd() *cobra.Command {
	var app string
	var limit int
	cmd := &cobra.Command{
		Use:   "crashes [id]",
		Short: "List the crashes the device recorded, or read one in full",
		Long: "Java exceptions, native signals and apps that stopped responding, newest\n" +
			"first, each with the line worth reading first. Give an id from the list to\n" +
			"read that report in full.\n\n" +
			"Not drained, unlike `logs`: a crash is a record, kept across reboots until\n" +
			"it ages out, so asking twice shows it twice. Android reads dropbox; an iOS\n" +
			"simulator reads the Mac's crash reports for that simulator, and a real\n" +
			"iPhone its own, over USB.",
		Example: `  mobium crashes
  mobium crashes --app com.example.shop
  mobium crashes data_app_crash@1790357773461`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["id"] = args[0]
			}
			if app != "" {
				call["app"] = app
			}
			if cmd.Flags().Changed("limit") {
				call["limit"] = limit
			}
			return runTool("app_crashes", call)
		},
	}
	cmd.Flags().StringVar(&app, "app", "", "Only this app's crashes, by package or bundle id")
	cmd.Flags().IntVar(&limit, "limit", 20, "The newest reports to list, at most")
	return cmd
}

func newKeyboardCmd() *cobra.Command {
	var text, key string
	var hide bool
	cmd := &cobra.Command{
		Use:   "keyboard",
		Short: "Read, type into, press a key on, or hide the soft keyboard",
		Long: "With no flags, says whether the keyboard is up and which field has focus.\n" +
			"--text types at that field's cursor, confirmed by reading the field back —\n" +
			"for a field nothing names, where `type` needs a target. --key presses\n" +
			"enter, delete or space, after any text. --hide hides the keyboard,\n" +
			"confirmed; an iPhone keyboard has no hide key, and when the app offers\n" +
			"no way the refusal names the one that usually works.\n\n" +
			"A password field's value is never printed.",
		Example: `  mobium keyboard
  mobium keyboard --text "hello"
  mobium keyboard --text "coffee" --key enter
  mobium keyboard --hide`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if cmd.Flags().Changed("text") {
				call["text"] = text
			}
			if key != "" {
				call["key"] = key
			}
			if hide {
				call["hide"] = true
			}
			return runTool("app_keyboard", call)
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "Type this at the cursor of the focused field")
	cmd.Flags().StringVar(&key, "key", "", "Press a named key: enter, delete or space")
	cmd.Flags().BoolVar(&hide, "hide", false, "Hide the keyboard")
	return cmd
}

func newRecordCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "record [start | stop]",
		Short: "Record the screen to a video",
		Long: "`record start` begins; `record stop -o session.mp4` finishes and saves it;\n" +
			"`record` alone says whether one is running. The saved file is checked by\n" +
			"its own header — frames and duration — so a recording that could not be\n" +
			"finished is reported rather than saved as if it had been.\n\n" +
			"Android records a frame only when the screen changes: a still screen is\n" +
			"one frame, which is not a failure. A real iPhone records WebDriverAgent's\n" +
			"screen stream: about ten frames a second, at full size.",
		Example: `  mobium record start
  mobium record stop -o login-flow.mp4
  mobium record`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["action"] = args[0]
			}
			if len(args) == 1 && args[0] == "stop" {
				if output == "" {
					output = fmt.Sprintf("recording-%s.mp4", time.Now().Format("20060102-150405"))
				}
				call["path"] = output
			}
			return runTool("app_record", call)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Where to save the video on stop (default: ./recording-<timestamp>.mp4)")
	return cmd
}

func newAudioCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "audio [start | stop]",
		Short: "Capture what the device plays, and say what it held",
		Long: "`audio start` begins; `audio stop -o capture.wav` finishes, saves a WAV and\n" +
			"prints a timeline: when there was sound and when silence, to a tenth of a\n" +
			"second, and each sound's pitch and level. `audio` alone says whether a\n" +
			"capture is running.\n\n" +
			"Assert on sound, silence and pitch, not on level: the level follows the\n" +
			"device's volume. Everything the device played is in it — with touch sounds\n" +
			"on, a tap is a tenth of a second of sound. An Android emulator only.",
		Example: `  mobium audio start
  mobium tap testid=play
  mobium audio stop -o capture.wav`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["action"] = args[0]
			}
			if len(args) == 1 && args[0] == "stop" {
				if output == "" {
					output = fmt.Sprintf("audio-%s.wav", time.Now().Format("20060102-150405"))
				}
				call["path"] = output
			}
			return runTool("app_audio", call)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Where to save the WAV on stop (default: ./audio-<timestamp>.wav)")
	return cmd
}

func newDialogsCmd() *cobra.Command {
	var when, press string
	var clear bool
	cmd := &cobra.Command{
		Use:   "dialogs",
		Short: "Declare how to answer a dialog that gets in an action's way",
		Long: "With --when and --press, adds a rule: when a dialog whose text contains\n" +
			"--when is in the way of an action, press the button captioned --press, confirm\n" +
			"the dialog went, and carry on. The action's result says which dialog was\n" +
			"answered. With no flags, lists the rules; --clear removes them.\n\n" +
			"A rule names a button, not accept or dismiss: which button those press differs\n" +
			"by platform and by dialog. Captions match ignoring case. Rules last for the\n" +
			"daemon, per device.",
		Example: `  mobium dialogs --when "Save Password" --press "Not Now"
  mobium dialogs --when "to use your location" --press "Allow While Using App"
  mobium dialogs
  mobium dialogs --clear`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if when != "" {
				call["when"] = when
			}
			if press != "" {
				call["press"] = press
			}
			if clear {
				call["clear"] = true
			}
			return runTool("app_dialogs", call)
		},
	}
	cmd.Flags().StringVar(&when, "when", "", "Text the dialog contains, ignoring case")
	cmd.Flags().StringVar(&press, "press", "", "Caption of the button to press, ignoring case")
	cmd.Flags().BoolVar(&clear, "clear", false, "Remove every rule for this device")
	return cmd
}
