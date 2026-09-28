package device

import (
	"context"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// The emulator's shake: the accelerometer swung sideways between +20 and
// -20 m/s², holding each for shakeHold, shakeSwings times.
//
// Measured on the Pixel 7 AVD against a detector in Chrome listening to
// devicemotion (about 60 readings a second): the console's values arrive, and
// paced like this — two seconds, 24 values — a seismic-style detector (most
// readings over 13 m/s² for at least a quarter second, Square's library's
// rule) fired and the sideways reading reversed 23 times, which a
// direction-counting detector also takes for a shake. Sent without the
// pauses, 24 values took 0.24s, the sensor sampled almost none of them, and
// neither fired. Stillness before and after fired nothing.
const (
	shakeSwings = 12
	shakeHold   = 60 * time.Millisecond
	shakeForce  = "20"
)

// Shake plays a shake on an emulator's accelerometer and puts it back at
// rest, as it was, read back.
func (a *ADB) Shake(ctx context.Context) error {
	rest, err := a.acceleration(ctx)
	if err != nil {
		return err
	}
	// y keeps gravity so the device is still upright while it swings.
	_, y, _ := strings.Cut(rest, ":")
	y, _, _ = strings.Cut(y, ":")
	for i := 0; i < shakeSwings; i++ {
		for _, x := range []string{shakeForce, "-" + shakeForce} {
			if err := a.emuConsole(ctx, "sensor", "set", "acceleration", x+":"+y+":0"); err != nil {
				_ = a.emuConsole(ctx, "sensor", "set", "acceleration", rest)
				return err
			}
			select {
			case <-time.After(shakeHold):
			case <-ctx.Done():
				_ = a.emuConsole(context.Background(), "sensor", "set", "acceleration", rest)
				return ctx.Err()
			}
		}
	}
	if err := a.emuConsole(ctx, "sensor", "set", "acceleration", rest); err != nil {
		return err
	}
	after, err := a.acceleration(ctx)
	if err != nil {
		return err
	}
	if after != rest {
		return mobiumerr.New(mobiumerr.NotConfirmed, "shook the emulator, and its accelerometer reads %s where it rested at %s", after, rest)
	}
	return nil
}

// acceleration reads the emulator's accelerometer as the console gives it:
// "x:y:z".
func (a *ADB) acceleration(ctx context.Context) (string, error) {
	out, diag, err := a.run(ctx, "emu", "sensor", "get", "acceleration")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out)+"\n"+string(diag), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "acceleration = "); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "", mobiumerr.New(mobiumerr.DeviceServer, "the emulator did not say where its accelerometer rests: %s",
		strings.TrimSpace(string(out)))
}

// Shake sends the simulator the shake its Device menu sends: a Darwin
// notification UIKit turns into a motion event. Measured on the iPhone 17 Pro
// simulator: typed into a text field, then this, and iOS raised its own
// "Undo Typing" alert, as a shaken phone does.
func (s *Simctl) Shake(ctx context.Context) error {
	_, err := s.Run(ctx, "notify_post", s.UDID, "com.apple.UIKit.SimulatorShake")
	return err
}
