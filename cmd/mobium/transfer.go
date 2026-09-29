package main

import (
	"github.com/spf13/cobra"
)

func newUploadCmd() *cobra.Command {
	var name, app string
	cmd := &cobra.Command{
		Use:   "upload <file>",
		Short: "Put a file where the device keeps downloads, for an app's file picker to find",
		Long: "Uploads a file from this machine to where the device keeps what a person\n" +
			"downloads, which is where an app's file picker looks: Android's shared\n" +
			"Download folder, or on iOS an app's own Documents folder (the app in front,\n" +
			"or --app). On Android the file is indexed by MediaStore, which is what the\n" +
			"picker reads, and read back there. On a real iPhone the upload is read back\n" +
			"from the phone and its bytes compared.",
		Example: `  mobium upload ./fixtures/invoice.pdf
  mobium upload report.csv --name q3.csv
  mobium upload photo.jpg --app com.example.shop    # iOS: that app's Documents`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"path": args[0]}
			if name != "" {
				call["name"] = name
			}
			if app != "" {
				call["app"] = app
			}
			return runTool("app_upload", call)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the name to give it on the device (default: the file's own)")
	cmd.Flags().StringVar(&app, "app", "", "iOS: the bundle id whose Documents it goes to (default: the app in front)")
	return cmd
}

func newDownloadCmd() *cobra.Command {
	var output, app string
	cmd := &cobra.Command{
		Use:   "download [name]",
		Short: "Bring back a file from where the device keeps downloads, or list them",
		Long: "Downloads a file from where the device keeps what a person downloads —\n" +
			"Android's shared Download folder, or on iOS an app's own Documents — to this\n" +
			"machine, to check what an app saved. With no name, lists the folder. The\n" +
			"copy's size is read back against the device's, a real iPhone's included.",
		Example: `  mobium download                              # what is there
  mobium download mobium-report.txt            # saves ./mobium-report.txt
  mobium download invoice.pdf -o out/invoice.pdf`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if app != "" {
				call["app"] = app
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
	cmd.Flags().StringVar(&app, "app", "", "iOS: the bundle id whose Documents to read (default: the app in front)")
	return cmd
}
