package mobiumdriver

import (
	"context"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

type phoneOrEmulator bool

func (e phoneOrEmulator) IsEmulator(context.Context) bool { return bool(e) }

// A phone is refused by name, before anything looks for a control port it
// cannot have; an emulator goes on to the capture.
func TestAndroidAudioRefusesAPhone(t *testing.T) {
	asked := ""
	startEmulatorAudio = func(_ context.Context, serial string) (device.AudioRecording, error) {
		asked = serial
		return nil, nil
	}
	defer func() { startEmulatorAudio = device.StartEmulatorAudio }()
	if _, err := androidAudioOn(context.Background(), phoneOrEmulator(false), "3C191FDJG001QX"); mobiumerr.CodeOf(err) != mobiumerr.Unsupported || asked != "" {
		t.Fatalf("a phone: %v, asked %q", err, asked)
	}
	if _, err := androidAudioOn(context.Background(), phoneOrEmulator(true), "emulator-5554"); err != nil || asked != "emulator-5554" {
		t.Fatalf("an emulator: %v, asked %q", err, asked)
	}
}
