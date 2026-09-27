// Mobium quick start: start a session, drive Settings, quit.
//
//	MOBIUM_PLATFORM=android go run .    # or ios
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	mobium "github.com/mobiumdev/mobium/clients/go"
)

// Settings is on every emulator, simulator and phone, with nothing to install.
var platforms = map[string]struct{ app, row, next string }{
	"android": {"com.android.settings", "Network & internet", "text=Internet"},
	"ios":     {"com.apple.Preferences", "General", "label=About,role=button"},
}

func main() {
	platform := os.Getenv("MOBIUM_PLATFORM")
	if platform == "" {
		platform = "android"
	}
	p := platforms[platform]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Start the session: the driver is started on the device and Settings
	//    is launched.
	opts := []mobium.Option{mobium.WithPlatform(platform), mobium.WithApp(p.app)}
	if d := os.Getenv("MOBIUM_DEVICE"); d != "" {
		opts = append(opts, mobium.WithDevice(d))
	}
	device, err := mobium.Start(ctx, opts...)
	if err != nil {
		log.Fatal(err)
	}
	// 5. Quit when main returns: the device's session is closed.
	defer func() {
		if err := device.Quit(ctx); err != nil {
			log.Fatal(err)
		}
		fmt.Println("session ended")
	}()
	s := device.Session()
	fmt.Printf("session on %s (%s, %s)\n", s.Device, s.Platform, s.Driver)

	// 2. Map the screen: every element you can act on, each with a @ref.
	elements, err := device.Map(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range elements[:min(5, len(elements))] {
		fmt.Println(" ", e.Ref, e.Label, "("+e.Role+")")
	}

	// 3. Tap a row by its ref, then wait for the screen it opens. A row's
	//    label can carry its summary too ("Network & internet Mobile, Wi-Fi,
	//    ..."), so match its start.
	for _, e := range elements {
		if strings.HasPrefix(e.Label, p.row) {
			if err := device.Tap(ctx, e.Ref); err != nil {
				log.Fatal(err)
			}
			break
		}
	}
	if _, err := device.WaitFor(ctx, p.next, nil); err != nil {
		log.Fatal(err)
	}
	fmt.Println("opened", p.row)

	// 4. Take a screenshot.
	name := "quickstart-" + platform + ".png"
	if _, err := device.Screenshot(ctx, name); err != nil {
		log.Fatal(err)
	}
	fmt.Println("saved", name)
}
