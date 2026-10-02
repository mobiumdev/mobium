package main

import (
	"path"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/spf13/cobra"
)

func newUploadCmd() *cobra.Command {
	var name, app, devicePath string
	cmd := &cobra.Command{
		Use:   "upload <file>",
		Short: "Put a file where the device keeps downloads, for an app's file picker to find",
		Long: "Uploads a file from this machine to where the device keeps what a person\n" +
			"downloads, which is where an app's file picker looks: Android's shared\n" +
			"Download folder, or on iOS an app's own Documents folder (the app in front,\n" +
			"or --app). On Android the file is indexed by MediaStore, which is what the\n" +
			"picker reads, and read back there. On a real iPhone the upload is read back\n" +
			"from the phone and its bytes compared.\n\n" +
			"--device-path sends a file or a whole folder to a path you name instead: on\n" +
			"Android an absolute path (/sdcard/..., /data/local/tmp/...), or with --app a\n" +
			"path in that app's private data, which Android allows only for a debuggable\n" +
			"build; on iOS a path in the app's data container (Documents/..., Library/...).\n" +
			"Every file's size is read back on the device.",
		Example: `  mobium upload ./fixtures/invoice.pdf
  mobium upload report.csv --name q3.csv
  mobium upload photo.jpg --app com.example.shop    # iOS: that app's Documents
  mobium upload ./seed --device-path /sdcard/seed                      # a folder, Android
  mobium upload prefs.xml --app com.example.shop --device-path shared_prefs/prefs.xml
  mobium upload db.sqlite --app com.example.Shop --device-path Library/db.sqlite   # iOS`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"path": args[0]}
			if name != "" {
				call["name"] = name
			}
			if app != "" {
				call["app"] = app
			}
			if devicePath != "" {
				call["device_path"] = devicePath
			}
			return runTool("app_upload", call)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the name to give it on the device (default: the file's own)")
	cmd.Flags().StringVar(&app, "app", "", "iOS: the app whose Documents, or with --device-path whose container; "+
		"Android with --device-path: the app whose private data")
	cmd.Flags().StringVar(&devicePath, "device-path", "", "send it, or a folder, to this path on the device")
	return cmd
}

func newDownloadCmd() *cobra.Command {
	var output, app, devicePath string
	cmd := &cobra.Command{
		Use:   "download [name]",
		Short: "Bring back a file from where the device keeps downloads, or list them",
		Long: "Downloads a file from where the device keeps what a person downloads —\n" +
			"Android's shared Download folder, or on iOS an app's own Documents — to this\n" +
			"machine, to check what an app saved. With no name, lists the folder. The\n" +
			"copy's size is read back against the device's, a real iPhone's included.\n\n" +
			"--device-path brings back a file or a whole folder from a path you name\n" +
			"instead — the paths upload takes — to -o, or ./<its name>. A folder goes to a\n" +
			"new folder, never into one already there.",
		Example: `  mobium download                              # what is there
  mobium download mobium-report.txt            # saves ./mobium-report.txt
  mobium download invoice.pdf -o out/invoice.pdf
  mobium download --device-path /sdcard/Android/media/com.example.shop -o media
  mobium download --app com.example.shop --device-path databases/shop.db
  mobium download --app com.example.Shop --device-path Library/Caches -o caches   # iOS`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if app != "" {
				call["app"] = app
			}
			if devicePath != "" {
				if len(args) == 1 {
					return mobiumerr.New(mobiumerr.InvalidArgument, "give a name in the Download folder or --device-path, not both")
				}
				call["device_path"] = devicePath
				call["path"] = output
				if output == "" {
					call["path"] = path.Base(strings.TrimSuffix(devicePath, "/"))
				}
				return runTool("app_download", call)
			}
			if len(args) == 1 {
				call["name"] = args[0]
				// Always a path from the CLI, as screenshot does: the file is
				// written, not base64'd back to be discarded.
				call["path"] = output
				if output == "" {
					call["path"] = args[0]
				}
			}
			return runTool("app_download", call)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "where to save it (default: ./<name>)")
	cmd.Flags().StringVar(&app, "app", "", "iOS: the app whose Documents, or with --device-path whose container; "+
		"Android with --device-path: the app whose private data")
	cmd.Flags().StringVar(&devicePath, "device-path", "", "bring back the file or folder at this path on the device")
	return cmd
}
