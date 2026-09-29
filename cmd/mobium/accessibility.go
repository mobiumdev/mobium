package main

import "github.com/spf13/cobra"

func newAccessibilityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accessibility [setting [value]]",
		Short: "Read the device's accessibility settings, or change one for the session",
		Long: "With no argument, prints every accessibility setting the device has. With a\n" +
			"setting, prints that one; with a setting and a value, changes it, confirms it\n" +
			"by reading it back, and puts it back as it was when the session ends.\n\n" +
			"Settings: reduce_motion, bold_text, increase_contrast, reduce_transparency,\n" +
			"button_shapes, differentiate_without_color, invert_colors, grayscale, and text\n" +
			"size — text_size (a category) on iOS, text_scale (a number) on Android.\n" +
			"A switch takes \"on\" or \"off\". A platform that lacks one says so.\n\n" +
			"On a real iPhone, where nothing outside can change them, Mobium goes through\n" +
			"the Settings app for the six switches and comes back to the app in front.",
		Example: `  mobium accessibility                       # everything, as it is now
  mobium accessibility bold_text on
  mobium accessibility text_size accessibility-large   # iOS
  mobium accessibility text_scale 1.3                   # Android`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) >= 1 {
				call["setting"] = args[0]
			}
			if len(args) == 2 {
				call["value"] = args[1]
			}
			return runTool("app_accessibility", call)
		},
	}
}
