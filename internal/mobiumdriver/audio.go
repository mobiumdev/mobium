package mobiumdriver

import (
	"context"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// AudioRecorder is implemented by backends that can capture what the device
// plays.
type AudioRecorder interface {
	StartAudio(ctx context.Context) (device.AudioRecording, error)
}

// StartAudio captures an emulator's audio from its control port.
func (a *Android) StartAudio(ctx context.Context) (device.AudioRecording, error) {
	return androidAudio(ctx, a.adb)
}

// StartAudio captures an emulator's audio from its control port.
func (u *UIA2) StartAudio(ctx context.Context) (device.AudioRecording, error) {
	return androidAudio(ctx, u.adb)
}

// emulatorAudio is the part of adb a capture needs, so the refusal on a phone
// can be tested without one.
type emulatorAudio interface {
	IsEmulator(context.Context) bool
	StreamVolumes(context.Context) ([]device.StreamVolume, error)
}

// startEmulatorAudio is device.StartEmulatorAudio, replaceable in tests.
var startEmulatorAudio = device.StartEmulatorAudio

func androidAudio(ctx context.Context, adb *device.ADB) (device.AudioRecording, error) {
	return androidAudioOn(ctx, adb, adb.Serial)
}

func androidAudioOn(ctx context.Context, adb emulatorAudio, serial string) (device.AudioRecording, error) {
	if !adb.IsEmulator(ctx) {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "a real phone's audio is not captured yet — nothing "+
			"outside the phone hears what it plays, and a capture there needs a helper on the phone that "+
			"has not been built. Capture on an emulator, which hands its audio out")
	}
	return startEmulatorAudio(ctx, serial, adb.StreamVolumes)
}

// StartAudio is refused on iOS: neither a simulator's audio nor a phone's is
// captured yet.
func (w *WDA) StartAudio(ctx context.Context) (device.AudioRecording, error) {
	if w.phone != nil {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "an iPhone's audio is not captured yet — nothing "+
			"outside the phone hears what it plays. Capture on an Android emulator")
	}
	return nil, mobiumerr.New(mobiumerr.Unsupported, "a simulator's audio is not captured yet — it plays "+
		"through the Mac's own audio device for simulators, which Mobium does not read. Capture on an "+
		"Android emulator")
}
