package main

import "github.com/spf13/cobra"

func newNotificationsCmd() *cobra.Command {
	var post, title, tag, shade string
	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "Read the notification shade, post to it, or open it",
		Long: "With no flags, lists what is currently in the shade — which is how you\n" +
			"assert an app posted what it should.\n\n" +
			"--post is an interruption arriving from somewhere else, confirmed by\n" +
			"reading the shade back: `cmd notification post` prints what it thinks it\n" +
			"built and says nothing about whether the system accepted it.\n\n" +
			"--shade open pulls the panel down, which is what makes a notification\n" +
			"tappable: until then it is not on screen and `mobium map` cannot see it.",
		Example: `  mobium notifications
  mobium notifications --post "your code is 123456"
  mobium notifications --shade open && mobium map
  mobium notifications --shade close`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if post != "" {
				call["text"] = post
			}
			if title != "" {
				call["title"] = title
			}
			if tag != "" {
				call["tag"] = tag
			}
			if shade != "" {
				call["shade"] = shade
			}
			return runTool("app_notifications", call)
		},
	}
	cmd.Flags().StringVar(&post, "post", "", "Post a notification with this body")
	cmd.Flags().StringVar(&title, "title", "", "Title for --post (default \"Mobium\")")
	cmd.Flags().StringVar(&tag, "tag", "", "Tag for --post (default \"mobium\")")
	cmd.Flags().StringVar(&shade, "shade", "", "\"open\" or \"close\" the notification panel")
	return cmd
}
