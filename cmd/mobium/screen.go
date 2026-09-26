package main

import "github.com/spf13/cobra"

func newScreenCmd() *cobra.Command {
	var inspect bool
	cmd := &cobra.Command{
		Use:   "screen [profile | reset]",
		Short: "Read the screen, or make the device pretend to be a different one",
		Long: "A flow that works on the screen you happen to have is a flow tested\n" +
			"once. This makes one Android device impersonate several, so the same\n" +
			"flow can meet a small phone, a flagship, a tablet and an accessibility\n" +
			"display size.\n\n" +
			"With no argument it reports the screen, whether an override is in force,\n" +
			"and the profiles this platform knows.\n\n" +
			"An override outlives this command and the session: it is a state left on\n" +
			"the device, so run \"mobium screen reset\" when you are done. Applying a\n" +
			"profile invalidates the refs from the last map, because nothing is where\n" +
			"it was.\n\n" +
			"On iOS the screen is fixed when the simulator is created and nothing can\n" +
			"resize a running one, so this reads only and names the simulator to boot\n" +
			"instead.\n\n" +
			"--inspect also reports what is wrong with the layout at this size:\n" +
			"elements past the edge, touch targets below the platform minimum, text\n" +
			"the platform truncated, and tappable elements with nothing to announce.\n" +
			"Treat a touch-target finding as worth a look rather than as a defect:\n" +
			"Android can enlarge a tap area without changing the element's bounds.",
		Example: `  mobium screen                        # what screen is this, really?
  mobium screen small-phone            # pretend to be a cheap phone
  mobium screen display-size-large     # the accessibility case
  mobium screen --inspect              # what is wrong at this size
  mobium screen reset                  # put the device back`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			if len(args) == 1 {
				call["profile"] = args[0]
			}
			if inspect {
				call["inspect"] = true
			}
			return runTool("app_screen", call)
		},
	}
	cmd.Flags().BoolVar(&inspect, "inspect", false,
		"also report layout findings at this screen")
	return cmd
}
