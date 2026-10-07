package mobiumdriver

import (
	"context"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// AudioRecorder is implemented by backends that can capture what the device
// plays.
type AudioRecorder interface {
	// StartAudio captures for app, or the app in front when it is "".
	StartAudio(ctx context.Context, app string) (device.AudioRecording, error)
}

// StartAudio captures an emulator's audio from its control port.
func (a *Android) StartAudio(ctx context.Context, app string) (device.AudioRecording, error) {
	return androidAudio(ctx, a.adb, app)
}

// StartAudio captures an emulator's audio from its control port.
func (u *UIA2) StartAudio(ctx context.Context, app string) (device.AudioRecording, error) {
	return androidAudio(ctx, u.adb, app)
}

// emulatorAudio is the part of adb a capture needs, so the refusal on a phone
// can be tested without one.
type emulatorAudio interface {
	IsEmulator(context.Context) bool
	StreamVolumes(context.Context) ([]device.StreamVolume, error)
	DeviceClock(context.Context) (time.Time, error)
	PackageUID(context.Context, string) (int, error)
	PackageInFront(context.Context) (string, error)
	AudioInterruptions(ctx context.Context, uid int, start, end time.Time) ([]device.AudioInterruption, error)
}

// startEmulatorAudio is device.StartEmulatorAudio, replaceable in tests.
var startEmulatorAudio = device.StartEmulatorAudio

func androidAudio(ctx context.Context, adb *device.ADB, app string) (device.AudioRecording, error) {
	return androidAudioOn(ctx, adb, adb.Serial, app)
}

// androidAudioOn captures on an emulator, sound and all, and on a phone
// records what interrupted the app — nothing outside a phone hears what it
// plays (docs/ROADMAP.md, "Audio"). Either way the app's interruptions come
// from the audio service's log, read for the app's uid from the device's
// clock at the start.
func androidAudioOn(ctx context.Context, adb emulatorAudio, serial, app string) (device.AudioRecording, error) {
	if app == "" {
		front, err := adb.PackageInFront(ctx)
		if err != nil {
			return nil, err
		}
		app = front
	}
	uid, err := adb.PackageUID(ctx, app)
	if err != nil {
		return nil, err
	}
	began, err := adb.DeviceClock(ctx)
	if err != nil {
		return nil, err
	}
	interruptions := func(ctx context.Context) ([]device.AudioInterruption, error) {
		now, err := adb.DeviceClock(ctx)
		if err != nil {
			return nil, err
		}
		return adb.AudioInterruptions(ctx, uid, began, now)
	}
	if !adb.IsEmulator(ctx) {
		return forApp{device.StartAudioEvents(ctx, adb.StreamVolumes, interruptions), app}, nil
	}
	r, err := startEmulatorAudio(ctx, serial, adb.StreamVolumes, interruptions)
	if err != nil {
		return nil, err
	}
	return forApp{r, app}, nil
}

// forApp says which app a capture's interruptions were read for.
type forApp struct {
	device.AudioRecording
	app string
}

func (f forApp) Stop(ctx context.Context) (device.AudioCapture, error) {
	c, err := f.AudioRecording.Stop(ctx)
	c.App = f.app
	return c, err
}

// StartAudio is refused on iOS: neither a simulator's audio nor a phone's is
// captured yet.
func (w *WDA) StartAudio(ctx context.Context, app string) (device.AudioRecording, error) {
	if w.phone != nil {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "an iPhone's audio is not captured yet — nothing "+
			"outside the phone hears what it plays. Capture on an Android emulator")
	}
	return nil, mobiumerr.New(mobiumerr.Unsupported, "a simulator's audio is not captured yet — it plays "+
		"through the Mac's own audio device for simulators, which Mobium does not read. Capture on an "+
		"Android emulator")
}
