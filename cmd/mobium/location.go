package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newLocationCmd() *cobra.Command {
	var (
		clear    bool
		gpx      string
		speed    float64
		latFlag  float64
		lonFlag  float64
		haveFlag = func(cmd *cobra.Command) bool {
			return cmd.Flags().Changed("lat") || cmd.Flags().Changed("lon")
		}
	)
	cmd := &cobra.Command{
		Use:   "location [lat lon]",
		Short: "Read where the device thinks it is, or move it",
		Long: "With no arguments, prints the device's position and whether that fix\n" +
			"was injected.\n\n" +
			"A negative coordinate looks like a flag to any POSIX command line, so\n" +
			"--lat and --lon are the forms that always work; the positional pair is a\n" +
			"convenience that handles a negative longitude but not a negative\n" +
			"latitude. This is why the examples below use the flags.\n\n" +
			"The platforms differ and mobium says how. Android sets through a test\n" +
			"provider, reads the position back, and labels it mock — so a set is\n" +
			"confirmed, and it works on a real phone as well as an emulator. iOS sets\n" +
			"through simctl and cannot be read at all: `simctl location` has set,\n" +
			"clear, run and start, and no get. There the answer says the request was\n" +
			"accepted and nothing stronger.\n\n" +
			"Clearing removes the test provider. It does not clear the device's last\n" +
			"known position, which Android caches — so a read straight afterwards\n" +
			"still returns the injected fix, and says so.",
		Example: `  mobium location                            # where does it think it is?
  mobium location --lat 51.5074 --lon -0.1278    # London
  mobium location --lat -33.8688 --lon 151.2093  # Sydney
  mobium location 51.5074 -0.1278                # the same, positionally
  mobium location --gpx walk.gpx --speed 5       # follow a recorded track
  mobium location --clear                        # stop pretending, and stop a route`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{}
			switch {
			case clear:
				call["clear"] = true
			case gpx != "":
				call["gpx"] = gpx
			case len(args) > 2:
				// Positional waypoints: three or more coordinates read as a
				// route rather than a point.
				var wp []interface{}
				for i := 0; i+1 < len(args); i += 2 {
					wp = append(wp, args[i]+","+args[i+1])
				}
				call["waypoints"] = wp
			case haveFlag(cmd):
				if !cmd.Flags().Changed("lat") || !cmd.Flags().Changed("lon") {
					return fmt.Errorf("give both --lat and --lon")
				}
				call["latitude"], call["longitude"] = latFlag, lonFlag
			case len(args) == 2:
				lat, err := strconv.ParseFloat(args[0], 64)
				if err != nil {
					return fmt.Errorf("latitude %q is not a number", args[0])
				}
				lon, err := strconv.ParseFloat(args[1], 64)
				if err != nil {
					return fmt.Errorf("longitude %q is not a number", args[1])
				}
				call["latitude"], call["longitude"] = lat, lon
			case len(args) == 1:
				return fmt.Errorf("give both a latitude and a longitude, " +
					"e.g. `mobium location --lat 51.5074 --lon -0.1278`")
			}
			if speed > 0 {
				call["speed"] = speed
			}
			return runTool("app_location", call)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false,
		"remove the injected position so the device reports its own")
	cmd.Flags().Float64Var(&latFlag, "lat", 0, "latitude in degrees, -90 to 90")
	cmd.Flags().Float64Var(&lonFlag, "lon", 0, "longitude in degrees, -180 to 180")
	cmd.Flags().StringVar(&gpx, "gpx", "", "a GPX file to follow")
	cmd.Flags().Float64Var(&speed, "speed", 0, "meters per second along a route (default 15)")
	// Stop parsing flags at the first positional argument, so a negative
	// longitude in `location 51.5074 -0.1278` is a coordinate rather than an
	// unknown shorthand flag. A negative *latitude* is first and so still
	// looks like a flag, which is what --lat and --lon are for.
	cmd.Flags().SetInterspersed(false)
	return cmd
}
