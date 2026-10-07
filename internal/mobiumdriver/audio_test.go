package mobiumdriver

import (
	"context"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

type phoneOrEmulator bool

func (e phoneOrEmulator) IsEmulator(context.Context) bool { return bool(e) }
func (e phoneOrEmulator) StreamVolumes(context.Context) ([]device.StreamVolume, error) {
	return nil, nil
}
func (e phoneOrEmulator) DeviceClock(context.Context) (time.Time, error) { return time.Unix(0, 0), nil }
func (e phoneOrEmulator) PackageUID(_ context.Context, pkg string) (int, error) {
	if pkg != "dev.mobium.mobiumapp" {
		return 0, mobiumerr.New(mobiumerr.NoSuchElement, "%s is not installed", pkg)
	}
	return 10368, nil
}
func (e phoneOrEmulator) PackageInFront(context.Context) (string, error) {
	return "dev.mobium.mobiumapp", nil
}
func (e phoneOrEmulator) AudioInterruptions(context.Context, int, time.Time, time.Time) ([]device.AudioInterruption, error) {
	return nil, nil
}

// A phone records what interrupted the app, with no sound; an emulator goes
// on to the capture; and either way the app is the one in front unless named.
func TestAndroidAudioOnAPhoneAndAnEmulator(t *testing.T) {
	asked := ""
	startEmulatorAudio = func(_ context.Context, serial string, _ device.VolumeReader, _ device.InterruptionReader) (device.AudioRecording, error) {
		asked = serial
		return device.StartAudioEvents(context.Background(), nil, nil), nil
	}
	defer func() { startEmulatorAudio = device.StartEmulatorAudio }()
	r, err := androidAudioOn(context.Background(), phoneOrEmulator(false), "3C191FDJG001QX", "")
	if err != nil || r.Captures() || asked != "" {
		t.Fatalf("a phone: %v, captures %v, asked %q", err, r != nil && r.Captures(), asked)
	}
	if c, _ := r.Stop(context.Background()); c.App != "dev.mobium.mobiumapp" {
		t.Errorf("the app in front was not used: %+v", c)
	}
	if _, err := androidAudioOn(context.Background(), phoneOrEmulator(true), "emulator-5554", ""); err != nil || asked != "emulator-5554" {
		t.Fatalf("an emulator: %v, asked %q", err, asked)
	}
	if _, err := androidAudioOn(context.Background(), phoneOrEmulator(true), "emulator-5554", "no.such.app"); mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement {
		t.Fatalf("an app not installed: %v", err)
	}
}
